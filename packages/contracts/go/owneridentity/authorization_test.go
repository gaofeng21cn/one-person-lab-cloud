package owneridentity

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"testing"
	"time"
)

func TestDecisionMustMatchExactRequestAndLifetime(t *testing.T) {
	now := time.Now()
	session, id := "session-a", "resource-a"
	request := &api.AuthorizationRequest{ActorId: "actor-a", SessionId: &session, Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "tenant-a"}}}, AudienceOwner: api.OwnerEnum_OWNER_ENUM_BUILD, Action: api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETOPERATION, Resource: &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_OPERATION, Id: &id}}
	allowed := &api.AuthorizationDecision{Issuer: api.AuthorizationIssuer_AUTHORIZATION_ISSUER_CLOUD_IDENTITY, Result: api.AuthorizationResult_AUTHORIZATION_RESULT_ALLOWED, ActorId: request.ActorId, SessionId: request.SessionId, Scope: request.Scope, AudienceOwner: request.AudienceOwner, Action: request.Action, Resource: request.Resource, PermissionVersion: 1, IssuedAt: timestamppb.New(now.Add(-time.Minute)), ExpiresAt: timestamppb.New(now.Add(time.Minute))}
	if err := ValidateDecision(request, allowed, now); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*api.AuthorizationDecision){
		"actor":    func(d *api.AuthorizationDecision) { d.ActorId = "other" },
		"scope":    func(d *api.AuthorizationDecision) { d.Scope.GetTenant().TenantId = "other" },
		"audience": func(d *api.AuthorizationDecision) { d.AudienceOwner = api.OwnerEnum_OWNER_ENUM_CAPABILITY },
		"action": func(d *api.AuthorizationDecision) {
			d.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RECONCILEOPERATION
		},
		"resource kind": func(d *api.AuthorizationDecision) {
			d.Resource.Kind = api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_BUILD
		},
		"resource id":    func(d *api.AuthorizationDecision) { id := "other"; d.Resource.Id = &id },
		"session":        func(d *api.AuthorizationDecision) { d.SessionId = nil },
		"permission":     func(d *api.AuthorizationDecision) { d.PermissionVersion = 0 },
		"expired":        func(d *api.AuthorizationDecision) { d.ExpiresAt = timestamppb.New(now.Add(-time.Second)) },
		"future":         func(d *api.AuthorizationDecision) { d.IssuedAt = timestamppb.New(now.Add(time.Second)) },
		"missing expiry": func(d *api.AuthorizationDecision) { d.ExpiresAt = nil },
	} {
		t.Run(name, func(t *testing.T) {
			d := proto.Clone(allowed).(*api.AuthorizationDecision)
			mutate(d)
			if err := ValidateDecision(request, d, now); err == nil {
				t.Fatal("mismatched decision accepted")
			}
		})
	}
}
func TestProductionCannotSelectPlaintext(t *testing.T) {
	config := TLSFromEnv(func(key string) string {
		if key == "NODE_ENV" {
			return "production"
		}
		if key == "OPL_GRPC_INSECURE_LOCAL" {
			return "1"
		}
		return ""
	})
	if config.AllowInsecureLocal {
		t.Fatal("production selected plaintext")
	}
	if _, err := config.DialOptions(ConsoleBFF, Service(Build), "0123456789abcdef0123456789abcdef"); err == nil {
		t.Fatal("missing production certificates accepted")
	}
	if Owner(ConsoleBFF).Valid() {
		t.Fatal("BFF became a data owner")
	}
}
