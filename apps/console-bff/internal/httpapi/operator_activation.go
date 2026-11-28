package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"google.golang.org/protobuf/proto"

	"opl-cloud/apps/console-bff/internal/clients"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/packages/contracts/go/publicjson"
)

// OperatorProvisioner names the retired retained-owner activation writer. The
// account-opening command is the owner's own now, so no reader implements this
// seam any more; the declaration stays so the server's optional wiring keeps
// compiling and stays inert.
type OperatorProvisioner interface {
	OperatorProvisionAccount(context.Context, string, string, []byte) ([]byte, error)
}

// activationInput is the browser's account-opening command. The BFF validates
// it before forwarding, and re-encodes a canonical body rather than forwarding
// browser bytes.
type activationInput struct {
	Email     *string `json:"email"`
	Password  *string `json:"password"`
	Name      *string `json:"name,omitempty"`
	Admission *string `json:"admission,omitempty"`
}

func (s *Server) registerOperatorActivationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v2/operator/accounts", s.operatorActivationHandler)
}

// operatorActivationHandler publishes the canonical account-opening command
// for platform operators.
//
// The BFF holds no activation state: it checks the live session, the CSRF,
// same-origin and idempotency guards, authorizes the action at platform scope
// and forwards the validated command to the owner that writes the account, its
// Tenant and its wallet binding. The owner's own operation record carries the
// durable result, so a retry of the same command converges there.
func (s *Server) operatorActivationHandler(w http.ResponseWriter, r *http.Request) {
	publisherRequestID(r)
	w.Header().Set("Cache-Control", "private, no-store")
	caller, err := RequireSession(r.Context(), s.identity, r)
	if err != nil {
		writePublisherIdentityError(w, r, err)
		return
	}
	// Activation is a platform operator command. The tenant is removed from the
	// session exactly as the admin publisher routes do, so a tenant administrator
	// cannot inherit platform scope.
	caller.Session = proto.Clone(caller.Session).(*api.Session)
	caller.Session.TenantId = nil
	ctx := WithCaller(r.Context(), caller, r.Header.Get(requestIDHeader))
	if !requirePublisherWriteGuards(w, r, caller, clients.CallContext(ctx)) {
		return
	}
	input, ok := decodeActivationInput(w, r)
	if !ok {
		return
	}
	if s.tenant == nil {
		writePublisherError(w, r, 503, "owner_unconfigured", "tenant owner is not configured")
		return
	}
	requestID := r.Header.Get(requestIDHeader)
	if err := RequireAuthorizedAction(ctx, s.identity, caller, owneridentity.Tenant,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_PROVISIONOPERATORACCOUNT,
		&api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PLATFORM},
		requestID); err != nil {
		writePublisherIdentityError(w, r, err)
		return
	}
	command, err := s.tenant.ProvisionOperatorAccount(ctx, &api.ProvisionOperatorAccountRpcRequest{
		Context: clients.CallContext(ctx),
		Body:    input.body(),
	})
	if err != nil {
		writeOwnerCallFailure(w, r, err)
		return
	}
	raw, err := publicjson.Marshal(command)
	if err != nil {
		writePublisherError(w, r, 502, "invalid_owner_response", "publisher owner returned an invalid response")
		return
	}
	code, declared := successStatus[api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_PROVISIONOPERATORACCOUNT]
	if !declared {
		writePublisherError(w, r, 500, "undeclared_success_status", "this route has no declared success status")
		return
	}
	if command.GetPollAfterSeconds() > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(command.GetPollAfterSeconds())))
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(raw)
}

// body is the canonical owner command the browser input maps to.
func (i activationInput) body() *api.ProvisionAccountRequest {
	body := &api.ProvisionAccountRequest{Email: stringValueOf(i.Email), Password: stringValueOf(i.Password)}
	if i.Name != nil {
		body.Name = proto.String(*i.Name)
	}
	switch stringValueOf(i.Admission) {
	case "":
	case "full_cloud_customer":
		body.Admission = api.ProvisionAccountRequestAdmissionEnum_PROVISION_ACCOUNT_REQUEST_ADMISSION_ENUM_FULL_CLOUD_CUSTOMER.Enum()
	case "gateway_only":
		body.Admission = api.ProvisionAccountRequestAdmissionEnum_PROVISION_ACCOUNT_REQUEST_ADMISSION_ENUM_GATEWAY_ONLY.Enum()
	}
	return body
}

func decodeActivationInput(w http.ResponseWriter, r *http.Request) (activationInput, bool) {
	media := strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0])
	if media != "application/json" {
		writePublisherError(w, r, 415, "json_required", "application/json required")
		return activationInput{}, false
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2<<20))
	if err != nil {
		writePublisherError(w, r, 400, "invalid_request", "invalid or oversized request body")
		return activationInput{}, false
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var input activationInput
	if err := decoder.Decode(&input); err != nil {
		writePublisherError(w, r, 400, "invalid_request", "invalid or oversized request body")
		return activationInput{}, false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writePublisherError(w, r, 400, "invalid_request", "invalid or oversized request body")
		return activationInput{}, false
	}
	if input.Email == nil || input.Password == nil || *input.Password == "" || strings.TrimSpace(*input.Email) == "" {
		writePublisherError(w, r, 400, "invalid_request", "email and password are required")
		return activationInput{}, false
	}
	if input.Name != nil && (*input.Name == "" || *input.Name != strings.TrimSpace(*input.Name) || len([]rune(*input.Name)) > 100) {
		writePublisherError(w, r, 400, "invalid_request", "name must be a trimmed non-empty value of at most 100 characters")
		return activationInput{}, false
	}
	if input.Admission != nil && *input.Admission != "full_cloud_customer" && *input.Admission != "gateway_only" {
		writePublisherError(w, r, 400, "invalid_request", "invalid admission")
		return activationInput{}, false
	}
	return input, true
}

func stringValueOf(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
