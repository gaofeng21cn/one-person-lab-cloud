package catalog

import (
	"fmt"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
)

// TestCreatePublisherNamespaceEnforcesExactRepositoryPrefix pins the approved
// contract's repositoryPrefix (03_api_contract_complete.yaml PublisherNamespace):
// a full registry/repository path with no scheme, tag or digest, maxLength 512.
// The owner must reject every prefix outside that language instead of accepting
// a fuzzy prefix; a rejected prefix must not persist a namespace row.
func TestCreatePublisherNamespaceEnforcesExactRepositoryPrefix(t *testing.T) {
	o := gapNewOwner(t)
	valid := []string{"registry.example/official", "registry.example:5000/team/agent", "registry.example/a_b.c-d"}
	for i, prefix := range valid {
		key := fmt.Sprintf("valid-%d", i)
		admitted, err := o.service.CreatePublisherNamespace(o.ctx, &api.CreatePublisherNamespaceRpcRequest{Context: gapPlatformCall(key), Body: &api.CreatePublisherNamespaceRequest{Name: key, Kind: api.CreatePublisherNamespaceRequestKindEnum_CREATE_PUBLISHER_NAMESPACE_REQUEST_KIND_ENUM_OFFICIAL, RegistryId: key, RepositoryPrefix: prefix, AdmissionReceiptId: "receipt"}})
		if err != nil {
			t.Fatalf("prefix %q rejected: %v", prefix, err)
		}
		if admitted.RepositoryPrefix != prefix || admitted.Status != api.PublisherNamespaceStatusEnum_PUBLISHER_NAMESPACE_STATUS_ENUM_APPROVED {
			t.Fatalf("prefix %q readback prefix=%q status=%v", prefix, admitted.RepositoryPrefix, admitted.Status)
		}
	}
	invalid := []string{
		"", "a", "/leading", "a//b", "a/-b", "a/b.", "a/.", "a/b__c", "a/b..c", "a/b/", "A/b", "a b/c", "a:http/b",
		"a/" + strings.Repeat("b", 511),
	}
	for i, prefix := range invalid {
		key := fmt.Sprintf("invalid-%d", i)
		_, err := o.service.CreatePublisherNamespace(o.ctx, &api.CreatePublisherNamespaceRpcRequest{Context: gapPlatformCall(key), Body: &api.CreatePublisherNamespaceRequest{Name: key, Kind: api.CreatePublisherNamespaceRequestKindEnum_CREATE_PUBLISHER_NAMESPACE_REQUEST_KIND_ENUM_OFFICIAL, RegistryId: key, RepositoryPrefix: prefix, AdmissionReceiptId: "receipt"}})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("prefix %q not refused as InvalidArgument: %v", prefix, err)
		}
		var rows int
		if err := o.db.QueryRowContext(o.ctx, `SELECT count(*) FROM capability.publisher_namespaces WHERE name=$1`, key).Scan(&rows); err != nil {
			t.Fatal(err)
		}
		if rows != 0 {
			t.Fatalf("prefix %q persisted %d namespace rows despite refusal", prefix, rows)
		}
	}
}

// TestCreatePublisherNamespaceAuthorityAndPrefixOverlap proves namespace admission
// stays a platform action and that admitted repository prefixes cannot overlap:
// exact, parent and child prefixes are refused while a disjoint prefix is admitted
// and revocable by the owner's own revoke operation.
func TestCreatePublisherNamespaceAuthorityAndPrefixOverlap(t *testing.T) {
	o := gapNewOwner(t)
	tenant := &api.CallContext{ActorId: "actor", RequestId: "request-tenant", SessionId: proto.String("session"), IdempotencyKey: "tenant-admission", Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "tenant-denied"}}}}
	_, err := o.service.CreatePublisherNamespace(o.ctx, &api.CreatePublisherNamespaceRpcRequest{Context: tenant, Body: &api.CreatePublisherNamespaceRequest{Name: "tenant-publisher", Kind: api.CreatePublisherNamespaceRequestKindEnum_CREATE_PUBLISHER_NAMESPACE_REQUEST_KIND_ENUM_THIRD_PARTY, RegistryId: "registry", RepositoryPrefix: "registry.example/team/agent", AdmissionReceiptId: "receipt"}})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("tenant admission refused with %v", err)
	}
	create := func(key, prefix string) (*api.PublisherNamespace, error) {
		return o.service.CreatePublisherNamespace(o.ctx, &api.CreatePublisherNamespaceRpcRequest{Context: gapPlatformCall(key), Body: &api.CreatePublisherNamespaceRequest{Name: key, Kind: api.CreatePublisherNamespaceRequestKindEnum_CREATE_PUBLISHER_NAMESPACE_REQUEST_KIND_ENUM_OFFICIAL, RegistryId: "registry", RepositoryPrefix: prefix, AdmissionReceiptId: "receipt"}})
	}
	if _, err = create("team-agent", "registry.example/team/agent"); err != nil {
		t.Fatal(err)
	}
	for i, overlap := range []string{"registry.example/team/agent", "registry.example/team", "registry.example/team/agent/inner"} {
		if _, err = create(fmt.Sprintf("overlap-%d", i), overlap); status.Code(err) != codes.AlreadyExists {
			t.Fatalf("overlap prefix %q refused with %v", overlap, err)
		}
	}
	other, err := create("other", "registry.example/other")
	if err != nil {
		t.Fatal(err)
	}
	revoked, err := o.service.RevokePublisherNamespace(o.ctx, &api.RevokePublisherNamespaceRpcRequest{Context: gapPlatformCall("revoke"), PublisherNamespaceId: other.Id, Body: &api.RevokePublisherNamespaceRequest{Reason: "withdrawn publisher"}})
	if err != nil || revoked.GetStatus() != api.PublisherNamespaceStatusEnum_PUBLISHER_NAMESPACE_STATUS_ENUM_REVOKED {
		t.Fatalf("revoke=%v status=%v", err, revoked.GetStatus())
	}
}
