package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
)

type publisherProbe struct {
	api.CapabilityProductServiceClient
	received *api.CreatePackageRpcRequest
}

func (p *publisherProbe) CreatePackage(_ context.Context, r *api.CreatePackageRpcRequest, _ ...grpc.CallOption) (*api.Package, error) {
	p.received = r
	return &api.Package{Id: "pkg-1", NamespaceId: r.Body.NamespaceId, Name: r.Body.Name, Visibility: api.PackageVisibilityEnum_PACKAGE_VISIBILITY_ENUM_PRIVATE, Status: api.PackageStatusEnum_PACKAGE_STATUS_ENUM_ACTIVE}, nil
}

func TestPublisherCommandsPreserveOnlyAuthenticatedContext(t *testing.T) {
	identity := allowedIdentity()
	identity.decision.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEPACKAGE
	identity.decision.AudienceOwner = api.OwnerEnum_OWNER_ENUM_CAPABILITY
	identity.decision.Resource = &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT}
	identity.decision.AuthorizationContextId = proto.String("fresh-authority-context")
	probe := &publisherProbe{}
	handler := NewPublisherHandler(probe, nil, identity)
	request := sessionRequest("POST", "/api/v2/packages")
	request.Body = http.NoBody
	prepare := func(body string) {
		request.Body = io.NopCloser(strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-CSRF-Token", "csrf-1")
		request.Header.Set("Idempotency-Key", "stable-command")
		request.Header.Set("x-opl-actor", "forged-actor")
		request.Header.Set("x-opl-tenant", "forged-tenant")
	}
	prepare(`{"namespaceId":"ns-1","name":"agent","description":""}`)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 201 {
		t.Fatalf("write failed: %d %s", response.Code, response.Body.String())
	}
	call := probe.received.Context
	if call.ActorId != "actor-1" || call.GetScope().GetTenant().TenantId != "tenant-1" || call.GetSessionId() != "session-1" || call.IdempotencyKey != "stable-command" || call.AuthorizationContextId != "fresh-authority-context" {
		t.Fatalf("incorrect propagated caller: %v", call)
	}
	probe.received = nil
	prepare(`{"namespaceId":"ns-1","name":"agent","actorId":"forged"}`)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 400 || probe.received != nil {
		t.Fatal("browser-supplied identity reached owner")
	}
	prepare(`{"namespaceId":"ns-1","name":"agent"}`)
	request.Header.Set("Origin", "https://cross-site.test")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 403 || probe.received != nil || !strings.Contains(response.Body.String(), "ORIGIN_REJECTED") {
		t.Fatal("cross-origin write reached owner")
	}
}
