package server

import (
	"context"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// This is a client of Gateway's persisted binding proof, not another identity
// writer. Every request revalidates the reference through the original owner.
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
		return nil, fmt.Errorf("Cloud announcements require OPL_CONTROL_PLANE_BFF_TOKEN, OPL_CLOUD_IDENTITY_URL and OPL_CLOUD_IDENTITY_TOKEN (tokens at least %d characters)", owneridentity.MinimumTokenLength)
	}
	for _, key := range []string{"OPL_CLOUD_IDENTITY_TOKEN", "OPL_FABRIC_SERVICE_TOKEN", "OPL_LEDGER_SERVICE_TOKEN", "OPL_FABRIC_CAPABILITY_KEY", "OPL_LEDGER_CAPABILITY_KEY"} {
		if token == strings.TrimSpace(getenv(key)) {
			return nil, errors.New("OPL_CONTROL_PLANE_BFF_TOKEN must be independent of outbound service credentials")
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

func (app *controlPlaneServer) cloudAnnouncementHandler(markRead bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		source := app.cloudAnnouncements
		if source == nil {
			writeSourceEnvelope(w, http.StatusServiceUnavailable, "control-plane", "unavailable", nil)
			return
		}
		owner, ownerOK := singleCloudHeader(r, owneridentity.PeerHeader)
		token, tokenOK := singleCloudHeader(r, owneridentity.TokenHeader)
		if !ownerOK || owner != owneridentity.ConsoleBFF.String() || !tokenOK || subtle.ConstantTimeCompare([]byte(token), []byte(source.token)) != 1 {
			writeError(w, http.StatusUnauthorized, "invalid_owner_identity")
			return
		}
		ref, refOK := singleCloudHeader(r, contracts.CloudSessionHeader)
		if !refOK || !validCloudSessionReference(ref) {
			writeError(w, http.StatusUnauthorized, "reauthentication_required")
			return
		}
		// No identity selector is part of this boundary. The session proof alone
		// selects its User; query/body cannot redirect the original-owner lookup.
		for key := range r.Header {
			lower := strings.ToLower(key)
			if strings.HasPrefix(lower, "x-opl-") && lower != owneridentity.PeerHeader && lower != owneridentity.TokenHeader && lower != contracts.CloudSessionHeader {
				writeError(w, http.StatusBadRequest, "invalid_announcement_identity_selector")
				return
			}
		}
		query, queryErr := url.ParseQuery(r.URL.RawQuery)
		if queryErr != nil {
			writeError(w, http.StatusBadRequest, "invalid_pagination")
			return
		}
		for key, values := range query {
			if markRead || (key != "page" && key != "pageSize") || len(values) != 1 {
				writeError(w, http.StatusBadRequest, "invalid_announcement_query")
				return
			}
		}
		if markRead && r.ContentLength != 0 {
			var input struct{}
			if !decodeAnnouncementBody(w, r, &input) {
				writeError(w, http.StatusBadRequest, "invalid_announcement_read")
				return
			}
		} else if !markRead && r.Body != nil && r.ContentLength != 0 {
			writeError(w, http.StatusBadRequest, "invalid_announcement_query")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		proof, err := source.gateway.GetSelfUserIdentity(ctx, &api.GetSelfUserIdentityRequest{Context: &api.CallContext{SessionId: proto.String(ref)}})
		if err != nil {
			writeCloudAnnouncementProofError(w, err)
			return
		}
		identity, err := app.cloudAnnouncementIdentity(ctx, proof)
		if err != nil {
			if errors.Is(err, errAnnouncementLineage) {
				writeError(w, http.StatusConflict, "announcement_identity_lineage_invalid")
			} else {
				writeSourceEnvelope(w, http.StatusServiceUnavailable, "control-plane", "unavailable", nil)
			}
			return
		}
		if markRead {
			app.markCloudAnnouncementRead(w, r.WithContext(ctx), identity)
			return
		}
		app.listCloudAnnouncements(w, r.WithContext(ctx), identity)
	}
}

func validCloudSessionReference(ref string) bool {
	if len(ref) != len("session_")+64 || !strings.HasPrefix(ref, "session_") {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(ref, "session_"))
	return err == nil
}

var errAnnouncementLineage = errors.New("announcement_identity_lineage_invalid")

type announcementIdentity struct{ cloudUserID, legacyUserID string }

func (app *controlPlaneServer) cloudAnnouncementIdentity(ctx context.Context, proof *api.UserIdentityReadback) (announcementIdentity, error) {
	if proof == nil || proof.GetActorId() == "" || proof.GetActorId() != strings.TrimSpace(proof.GetActorId()) ||
		proof.GetStatus() != api.UserExternalBindingReadbackStatusEnum_USER_EXTERNAL_BINDING_READBACK_STATUS_ENUM_ACTIVE ||
		proof.GetVerification() != api.UserExternalBindingReadbackVerificationEnum_USER_EXTERNAL_BINDING_READBACK_VERIFICATION_ENUM_CONFIRMED {
		return announcementIdentity{}, errAnnouncementLineage
	}
	subject, err := strconv.ParseInt(proof.GetSub2ApiUserId(), 10, 64)
	if err != nil || subject <= 0 || strconv.FormatInt(subject, 10) != proof.GetSub2ApiUserId() {
		return announcementIdentity{}, errAnnouncementLineage
	}
	identity := announcementIdentity{cloudUserID: "cloud-user:" + proof.GetActorId()}
	account, found, err := app.tables.GetAccountBySub2APIUserID(ctx, subject)
	if errors.Is(err, errSub2APIAccountMappingConflict) {
		return announcementIdentity{}, errAnnouncementLineage
	}
	if err != nil {
		return announcementIdentity{}, err
	}
	if !found {
		return identity, nil
	}
	// The subject belongs to this persisted Account; only its reciprocal owner
	// User link proves historical ACK ownership. Email and actor spelling do not.
	mappedSubject, valid := positiveIntegerField(account, "sub2apiUserId")
	ownerID := stringValue(account["ownerUserId"])
	if !valid || mappedSubject != subject || ownerID == "" || strings.HasPrefix(ownerID, "cloud-user:") || stringValue(account["id"]) == "" || stringValue(account["status"]) != "active" {
		return announcementIdentity{}, errAnnouncementLineage
	}
	user, userFound, err := app.tables.GetUser(ctx, ownerID)
	if err != nil {
		return announcementIdentity{}, err
	}
	if !userFound || stringValue(user["id"]) != ownerID || stringValue(user["accountId"]) != stringValue(account["id"]) ||
		stringValue(user["status"]) != "active" || stringValue(user["disabledAt"]) != "" || stringValue(user["deletedAt"]) != "" {
		return announcementIdentity{}, errAnnouncementLineage
	}
	identity.legacyUserID = ownerID
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

func (app *controlPlaneServer) listCloudAnnouncements(w http.ResponseWriter, r *http.Request, identity announcementIdentity) {
	page, pageSize, ok := operatorPagination(w, r)
	if !ok {
		return
	}
	rows, err := app.tables.ListAnnouncements(r.Context())
	if err != nil {
		writeSourceEnvelope(w, http.StatusServiceUnavailable, "control-plane", "unavailable", nil)
		return
	}
	readIDs := map[string]bool{}
	// This is an exact two-reference alias read, not a join on names, Tenant,
	// Account or payer. ACK below folds those same references under the row lock.
	for _, userID := range []string{identity.cloudUserID, identity.legacyUserID} {
		if userID == "" {
			continue
		}
		reads, err := app.tables.ListAnnouncementReads(r.Context(), userID)
		if err != nil {
			writeSourceEnvelope(w, http.StatusServiceUnavailable, "control-plane", "unavailable", nil)
			return
		}
		for _, read := range reads {
			readIDs[stringValue(read["announcementId"])] = true
		}
	}
	now := time.Now().UTC()
	items := make([]AnnouncementDTO, 0, len(rows))
	for _, row := range rows {
		if announcementIsActive(row, now) {
			items = append(items, announcementDTO(row, readIDs[stringValue(row["id"])]))
		}
	}
	writeAnnouncementPage(w, page, pageSize, items, latestAnnouncementUpdatedAt(rows))
}

func (app *controlPlaneServer) markCloudAnnouncementRead(w http.ResponseWriter, r *http.Request, identity announcementIdentity) {
	row, err := app.tables.MarkCloudAnnouncementRead(r.Context(), strings.TrimSpace(r.PathValue("announcementId")), identity.cloudUserID, identity.legacyUserID, time.Now().UTC().Format(time.RFC3339Nano))
	if errors.Is(err, errAnnouncementNotActive) {
		writeError(w, http.StatusNotFound, "announcement_not_active")
		return
	}
	if err != nil {
		writeSourceEnvelope(w, http.StatusServiceUnavailable, "control-plane", "unavailable", nil)
		return
	}
	writeJSON(w, http.StatusOK, AnnouncementReadDTO{AnnouncementID: stringValue(row["announcementId"]), ReadAt: stringValue(row["readAt"])})
}
