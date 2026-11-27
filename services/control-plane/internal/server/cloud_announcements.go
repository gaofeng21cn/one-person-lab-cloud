package server

import (
	"context"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// This is the shared CloudIdentity bridge proof for the retained Control Plane
// owners: the caller credential, the live session reference and the original
// owner's own binding proof. The announcement bridge that used it is retired;
// the announcements and their read receipts are owned by CloudIdentity now.
type cloudAnnouncementSource struct {
	token   string
	gateway api.GatewayCoordinationClient
	conn    *grpc.ClientConn
}

func cloudAnnouncementSourceFromEnv(getenv func(string) string) (*cloudAnnouncementSource, error) {
	token := strings.TrimSpace(getenv("OPL_CONTROL_PLANE_BFF_TOKEN"))
	address := strings.TrimSpace(getenv("OPL_CLOUD_IDENTITY_URL"))
	upstreamToken := strings.TrimSpace(getenv("OPL_CLOUD_IDENTITY_TOKEN"))
	if token == "" && address == "" && upstreamToken == "" {
		return nil, nil
	}
	if len(token) < owneridentity.MinimumTokenLength || address == "" || len(upstreamToken) < owneridentity.MinimumTokenLength {
		return nil, fmt.Errorf("Cloud identity reads require OPL_CONTROL_PLANE_BFF_TOKEN, OPL_CLOUD_IDENTITY_URL and OPL_CLOUD_IDENTITY_TOKEN (tokens at least %d characters)", owneridentity.MinimumTokenLength)
	}
	for _, key := range []string{"OPL_CLOUD_IDENTITY_TOKEN", "OPL_FABRIC_SERVICE_TOKEN", "OPL_LEDGER_SERVICE_TOKEN", "OPL_FABRIC_CAPABILITY_KEY", "OPL_LEDGER_CAPABILITY_KEY"} {
		if token == strings.TrimSpace(getenv(key)) {
			return nil, fmt.Errorf("OPL_CONTROL_PLANE_BFF_TOKEN must be independent of outbound service credentials")
		}
	}
	options, err := owneridentity.TLSFromEnv(getenv).DialOptions(owneridentity.ControlPlane, owneridentity.Tenant.Service(), upstreamToken)
	if err != nil {
		return nil, err
	}
	conn, err := grpc.NewClient(address, options...)
	if err != nil {
		return nil, err
	}
	return &cloudAnnouncementSource{token: token, gateway: api.NewGatewayCoordinationClient(conn), conn: conn}, nil
}

func (source *cloudAnnouncementSource) Close() error {
	if source == nil || source.conn == nil {
		return nil
	}
	return source.conn.Close()
}

func singleCloudHeader(r *http.Request, name string) (string, bool) {
	var values []string
	for key, entries := range r.Header {
		if strings.EqualFold(key, name) {
			values = append(values, entries...)
		}
	}
	if len(values) != 1 || values[0] == "" || values[0] != strings.TrimSpace(values[0]) || strings.Contains(values[0], ",") {
		return "", false
	}
	return values[0], true
}

func validCloudSessionReference(ref string) bool {
	if len(ref) != len("session_")+64 || !strings.HasPrefix(ref, "session_") {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(ref, "session_"))
	return err == nil
}

var errCloudUserLineage = errors.New("announcement_identity_lineage_invalid")
var errCloudUserInactive = fmt.Errorf("%w: inactive original owner", errCloudUserLineage)

type cloudUserIdentity struct {
	cloudUserID, legacyUserID, legacyAccountID string
	legacyAccount                              map[string]any
}

func (app *controlPlaneServer) cloudUserLineage(ctx context.Context, proof *api.UserIdentityReadback) (cloudUserIdentity, error) {
	if proof == nil || proof.GetActorId() == "" || proof.GetActorId() != strings.TrimSpace(proof.GetActorId()) ||
		proof.GetStatus() != api.UserExternalBindingReadbackStatusEnum_USER_EXTERNAL_BINDING_READBACK_STATUS_ENUM_ACTIVE ||
		proof.GetVerification() != api.UserExternalBindingReadbackVerificationEnum_USER_EXTERNAL_BINDING_READBACK_VERIFICATION_ENUM_CONFIRMED {
		return cloudUserIdentity{}, errCloudUserLineage
	}
	subject, err := strconv.ParseInt(proof.GetSub2ApiUserId(), 10, 64)
	if err != nil || subject <= 0 || strconv.FormatInt(subject, 10) != proof.GetSub2ApiUserId() {
		return cloudUserIdentity{}, errCloudUserLineage
	}
	identity := cloudUserIdentity{cloudUserID: "cloud-user:" + proof.GetActorId()}
	account, found, err := app.tables.GetAccountBySub2APIUserID(ctx, subject)
	if errors.Is(err, errSub2APIAccountMappingConflict) {
		return cloudUserIdentity{}, errCloudUserLineage
	}
	if err != nil {
		return cloudUserIdentity{}, err
	}
	if !found {
		return identity, nil
	}
	// The subject belongs to this persisted Account; only its reciprocal owner
	// User link proves historical ACK ownership. Email and actor spelling do not.
	mappedSubject, valid := positiveIntegerField(account, "sub2apiUserId")
	ownerID := stringValue(account["ownerUserId"])
	if !valid || mappedSubject != subject || ownerID == "" || strings.HasPrefix(ownerID, "cloud-user:") || stringValue(account["id"]) == "" {
		return cloudUserIdentity{}, errCloudUserLineage
	}
	if stringValue(account["status"]) != "active" {
		return cloudUserIdentity{}, errCloudUserInactive
	}
	user, userFound, err := app.tables.GetUser(ctx, ownerID)
	if err != nil {
		return cloudUserIdentity{}, err
	}
	if !userFound || stringValue(user["id"]) != ownerID || stringValue(user["accountId"]) != stringValue(account["id"]) {
		return cloudUserIdentity{}, errCloudUserLineage
	}
	if stringValue(user["status"]) != "active" || stringValue(user["disabledAt"]) != "" || stringValue(user["deletedAt"]) != "" {
		return cloudUserIdentity{}, errCloudUserInactive
	}
	identity.legacyAccount = account
	identity.legacyUserID = ownerID
	identity.legacyAccountID = stringValue(account["id"])
	return identity, nil
}

func writeCloudAnnouncementProofError(w http.ResponseWriter, err error) {
	switch status.Code(err) {
	case codes.Unauthenticated:
		writeError(w, http.StatusUnauthorized, "reauthentication_required")
	case codes.PermissionDenied, codes.FailedPrecondition, codes.NotFound:
		writeError(w, http.StatusForbidden, "announcement_identity_lineage_invalid")
	default:
		writeSourceEnvelope(w, http.StatusServiceUnavailable, "control-plane", "unavailable", nil)
	}
}

func (app *controlPlaneServer) cloudSessionProof(w http.ResponseWriter, r *http.Request) (*api.UserIdentityReadback, bool) {
	return app.cloudSessionProofForCaller(w, r, owneridentity.ConsoleBFF)
}

func (app *controlPlaneServer) cloudSessionProofForCaller(w http.ResponseWriter, r *http.Request, caller owneridentity.Service) (*api.UserIdentityReadback, bool) {
	source := app.cloudAnnouncements
	var expectedToken string
	if source != nil && caller == owneridentity.ConsoleBFF {
		expectedToken = source.token
	}
	if source == nil || source.gateway == nil || len(expectedToken) < owneridentity.MinimumTokenLength {
		writeSourceEnvelope(w, http.StatusServiceUnavailable, "control-plane", "unavailable", nil)
		return nil, false
	}
	owner, ownerOK := singleCloudHeader(r, owneridentity.PeerHeader)
	token, tokenOK := singleCloudHeader(r, owneridentity.TokenHeader)
	if !ownerOK || owner != caller.String() || !tokenOK || subtle.ConstantTimeCompare([]byte(token), []byte(expectedToken)) != 1 {
		writeError(w, http.StatusUnauthorized, "invalid_owner_identity")
		return nil, false
	}
	ref, refOK := singleCloudHeader(r, contracts.CloudSessionHeader)
	if !refOK || !validCloudSessionReference(ref) {
		writeError(w, http.StatusUnauthorized, "reauthentication_required")
		return nil, false
	}
	// Only the original session selects a User. Browser identity headers are never accepted.
	for key := range r.Header {
		lower := strings.ToLower(key)
		if strings.HasPrefix(lower, "x-opl-") && lower != owneridentity.PeerHeader && lower != owneridentity.TokenHeader && lower != contracts.CloudSessionHeader {
			writeError(w, http.StatusBadRequest, "invalid_cloud_identity_selector")
			return nil, false
		}
	}
	proof, err := source.gateway.GetSelfUserIdentity(r.Context(), &api.GetSelfUserIdentityRequest{Context: &api.CallContext{SessionId: proto.String(ref)}})
	if err != nil {
		writeCloudAnnouncementProofError(w, err)
		return nil, false
	}
	return proof, true
}
