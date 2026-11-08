package httpapi

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"opl-cloud/apps/console-bff/internal/clients"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

type operatorIdentity struct {
	denied       bool
	deniedAction api.AuthorizationActionEnum
	calls        []*api.AuthorizationRequest
}

func (i *operatorIdentity) Session(context.Context, string) (*api.Session, error) {
	return &api.Session{ActorId: "operator-1"}, nil
}
func (i *operatorIdentity) Authorize(_ context.Context, r *api.AuthorizationRequest) (*api.AuthorizationDecision, error) {
	i.calls = append(i.calls, proto.Clone(r).(*api.AuthorizationRequest))
	d := allowOperatorDecision(r.ActorId)
	d.Action = r.Action
	d.AudienceOwner = r.AudienceOwner
	d.Resource = r.Resource
	d.Scope = r.Scope
	d.SessionId = r.SessionId
	d.AuthorizationContextId = proto.String(r.Action.String())
	if i.denied || r.GetAction() == i.deniedAction {
		d.Result = api.AuthorizationResult_AUTHORIZATION_RESULT_DENIED
	}
	return d, nil
}
func operatorNativeFixture() *api.OperatorWorkspace {
	now := timestamppb.New(time.Date(2026, 11, 7, 1, 0, 0, 0, time.UTC))
	return &api.OperatorWorkspace{Workspace: &api.Workspace{Id: "native-workspace", Name: "native", Status: api.WorkspaceStatusEnum_WORKSPACE_STATUS_ENUM_ACTIVE, ResourceReadiness: api.WorkspaceResourceReadinessEnum_WORKSPACE_RESOURCE_READINESS_ENUM_READY, ApplicationAvailability: api.WorkspaceApplicationAvailabilityEnum_WORKSPACE_APPLICATION_AVAILABILITY_ENUM_UNKNOWN, DeliveryModel: api.WorkspaceDeliveryModelEnum_WORKSPACE_DELIVERY_MODEL_ENUM_AGENT_SAAS, ComputePlanId: "compute", StoragePlanId: "storage", CreatedAt: now, UpdatedAt: now}, TenantId: "native-tenant", CreatedBy: "native-creator", OperationId: "original-create", ResourceSetId: "native-set"}
}
func TestOperatorWorkspacesComposeNativeOwnersOverAuthenticatedWire(t *testing.T) {
	for _, mode := range []string{"available", "multiple-workspaces", "unknown", "unavailable", "mismatched-workspace", "mismatched-set", "denied", "serve-denied", "serve-access-denied", "serve-owner-denied", "serve-access-owner-denied", "workspace-unavailable", "not-assigned", "deletion-pending", "deletion-unknown", "duplicate-fact", "invalid-empty-id", "application-unknown", "application-no-deployment", "application-access-unavailable", "application-access-mismatched"} {
		t.Run(mode, func(t *testing.T) {
			identity := &operatorIdentity{denied: mode == "denied"}
			if mode == "serve-denied" {
				identity.deniedAction = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTOPERATORDEPLOYMENTS
			}
			if mode == "serve-access-denied" {
				identity.deniedAction = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETOPERATORWORKSPACEACCESS
			}
			item := operatorNativeFixture()
			if mode == "not-assigned" {
				item.ResourceSetId = ""
			}
			retainedCalls, workspaceCalls, fabricCalls, deploymentCalls, accessCalls := 0, 0, 0, 0, 0
			retained := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				retainedCalls++
				http.Error(w, "retained Workspace must not be read", 500)
			}))
			defer retained.Close()
			server := grpc.NewServer(grpc.UnaryInterceptor(func(ctx context.Context, in any, info *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
				md, _ := metadata.FromIncomingContext(ctx)
				if got := md.Get(owneridentity.PeerHeader); len(got) != 1 || got[0] != "console_bff" {
					t.Errorf("missing BFF identity: %v", got)
				}
				call := in.(interface{ GetContext() *api.CallContext }).GetContext()
				if call.GetActorId() != "operator-1" || call.GetScope().GetPlatform() == nil || call.GetSessionId() != owneridentity.SessionReference("session-1") {
					t.Errorf("lost platform caller: %v", call)
				}
				switch info.FullMethod {
				case api.WorkspaceProductService_ListOperatorWorkspaces_FullMethodName:
					workspaceCalls++
					q := in.(*api.ListOperatorWorkspacesRpcRequest)
					if q.Page != 1 || q.PageSize != 20 {
						t.Errorf("page %v", q)
					}
					if call.AuthorizationContextId != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTOPERATORWORKSPACES.String() {
						t.Errorf("wrong list authorization: %v", call)
					}
					if mode == "workspace-unavailable" {
						return nil, status.Error(codes.Unavailable, "Workspace failed")
					}
					items := []*api.OperatorWorkspace{proto.Clone(item).(*api.OperatorWorkspace)}
					if mode == "multiple-workspaces" {
						second := proto.Clone(item).(*api.OperatorWorkspace)
						second.Workspace.Id, second.ResourceSetId = "second-workspace", "second-set"
						items = append(items, second)
					}
					return &api.OperatorWorkspacePage{Items: items, Total: int64(len(items)), Page: 1, PageSize: 20}, nil
				case api.WorkspaceProductService_GetOperatorWorkspace_FullMethodName:
					workspaceCalls++
					if in.(*api.GetOperatorWorkspaceRpcRequest).WorkspaceId != item.Workspace.Id {
						t.Errorf("wrong id")
					}
					if mode == "workspace-unavailable" {
						return nil, status.Error(codes.Unavailable, "Workspace failed")
					}
					return proto.Clone(item), nil
				case api.FabricCoordination_ReadResources_FullMethodName:
					fabricCalls++
					q := in.(*api.ResourceReadbackRequest)
					expectedID, expectedSet := item.Workspace.Id, item.ResourceSetId
					if mode == "multiple-workspaces" && q.WorkspaceId == "second-workspace" {
						expectedID, expectedSet = "second-workspace", "second-set"
					}
					if q.WorkspaceId != expectedID || q.ResourceSetId != expectedSet || call.AuthorizationContextId != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_READOPERATORRESOURCES.String() {
						t.Errorf("wrong Fabric selector/grant: %v", q)
					}
					if mode == "unavailable" {
						return nil, status.Error(codes.Unavailable, "Fabric failed")
					}
					read := &api.ResourceReadback{WorkspaceId: expectedID, ResourceSetId: expectedSet, ResourceVersion: "fabric-v1", Outcome: api.Observation_OBSERVATION_CONFIRMED, ObservedAt: item.Workspace.UpdatedAt, Resources: []*api.ResourceFact{{Id: "compute-1", Kind: "compute", State: "confirmed", OpaqueProviderReference: "provider-1"}}}
					if mode == "deletion-pending" || mode == "deletion-unknown" {
						state := "not_dispatched"
						if mode == "deletion-unknown" {
							state = "unknown"
						}
						read.Resources = append(read.Resources,
							&api.ResourceFact{Kind: "gateway_secret_binding", State: state},
							&api.ResourceFact{Kind: "storage_attachment", State: "not_dispatched"})
					}
					if mode == "duplicate-fact" {
						read.Resources = append(read.Resources, proto.Clone(read.Resources[0]).(*api.ResourceFact))
					}
					if mode == "invalid-empty-id" {
						read.Resources[0].Id = ""
					}
					if mode == "unknown" {
						read.Outcome = api.Observation_OBSERVATION_UNKNOWN
						read.ObservedAt = nil
						read.Resources[0].State = "unknown"
					}
					if mode == "mismatched-workspace" {
						read.WorkspaceId = "other"
					}
					if mode == "mismatched-set" {
						read.ResourceSetId = "other"
					}
					return read, nil
				case api.ServeProductService_ListOperatorDeployments_FullMethodName:
					deploymentCalls++
					q := in.(*api.ListDeploymentsRpcRequest)
					if (q.WorkspaceId != item.Workspace.Id && !(mode == "multiple-workspaces" && q.WorkspaceId == "second-workspace")) || q.GetQueryLimit() != 1 || call.AuthorizationContextId != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTOPERATORDEPLOYMENTS.String() {
						t.Errorf("wrong Serve deployment selector/grant: %v", q)
					}
					if mode == "serve-owner-denied" {
						return nil, status.Error(codes.PermissionDenied, "Serve denied deployment read")
					}
					if mode == "application-unknown" {
						return nil, status.Error(codes.Unavailable, "Serve failed")
					}
					if mode == "application-no-deployment" {
						return &api.DeploymentPage{}, nil
					}
					deployment := &api.Deployment{Id: "native-deployment", WorkspaceId: q.WorkspaceId, Status: api.DeploymentStatusEnum_DEPLOYMENT_STATUS_ENUM_ACTIVE}
					return &api.DeploymentPage{Items: []*api.Deployment{deployment}, CurrentDeployment: deployment, LatestAttempt: deployment}, nil
				case api.ServeProductService_GetOperatorWorkspaceAccess_FullMethodName:
					accessCalls++
					q := in.(*api.GetWorkspaceAccessRpcRequest)
					if (q.WorkspaceId != item.Workspace.Id && !(mode == "multiple-workspaces" && q.WorkspaceId == "second-workspace")) || call.AuthorizationContextId != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETOPERATORWORKSPACEACCESS.String() {
						t.Errorf("wrong Serve access selector/grant: %v", q)
					}
					if mode == "serve-access-owner-denied" {
						return nil, status.Error(codes.PermissionDenied, "Serve denied access read")
					}
					if mode == "application-access-unavailable" {
						return nil, status.Error(codes.Unavailable, "Serve access failed")
					}
					deploymentID := "native-deployment"
					if mode == "application-access-mismatched" {
						deploymentID = "another-deployment"
					}
					return &api.WorkspaceAccess{WorkspaceId: q.WorkspaceId, DeploymentId: deploymentID, Url: "https://native.example.test"}, nil
				default:
					return next(ctx, in)
				}
			}))
			api.RegisterWorkspaceProductServiceServer(server, &api.UnimplementedWorkspaceProductServiceServer{})
			api.RegisterFabricCoordinationServer(server, &api.UnimplementedFabricCoordinationServer{})
			api.RegisterServeProductServiceServer(server, &api.UnimplementedServeProductServiceServer{})
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			go server.Serve(listener)
			defer server.Stop()
			cfg := clients.Config{ControlPlaneURL: retained.URL, ControlPlaneToken: "0123456789abcdef0123456789abcdef", Addresses: map[owneridentity.Owner]string{owneridentity.Workspace: listener.Addr().String(), owneridentity.Fabric: listener.Addr().String(), owneridentity.Serve: listener.Addr().String()}, Tokens: map[owneridentity.Owner]string{owneridentity.Workspace: "0123456789abcdef0123456789abcdef", owneridentity.Fabric: "0123456789abcdef0123456789abcdef", owneridentity.Serve: "0123456789abcdef0123456789abcdef"}, TLS: owneridentity.TLSConfig{AllowInsecureLocal: true}}
			reader, err := clients.Dial(cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			handler := NewServer(reader, identity).Handler()
			for _, path := range []string{"/api/v2/operator/workspaces", "/api/v2/operator/workspaces/native-workspace"} {
				deploymentsBefore, accessBefore := deploymentCalls, accessCalls
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, sessionRequest("GET", path))
				if mode == "denied" || mode == "serve-denied" || mode == "serve-access-denied" || mode == "serve-owner-denied" || mode == "serve-access-owner-denied" {
					if response.Code != 403 {
						t.Fatalf("denial=%d %s", response.Code, response.Body.String())
					}
					continue
				}
				if mode == "workspace-unavailable" || mode == "mismatched-workspace" || mode == "mismatched-set" || mode == "duplicate-fact" || mode == "invalid-empty-id" {
					if response.Code < 400 {
						t.Fatalf("invalid owner succeeded: %s", response.Body.String())
					}
					continue
				}
				if response.Code != 200 {
					t.Fatalf("%s status=%d %s", path, response.Code, response.Body.String())
				}
				var envelope map[string]any
				if json.Unmarshal(response.Body.Bytes(), &envelope) != nil || envelope["source"] != "workspace+fabric+serve" {
					t.Fatalf("wrong source %s", response.Body.String())
				}
				data := envelope["data"].(map[string]any)
				if rows, ok := data["items"]; ok {
					if mode == "multiple-workspaces" {
						items := rows.([]any)
						second := items[1].(map[string]any)["workspace"].(map[string]any)["data"].(map[string]any)
						if len(items) != 2 || second["id"] != "second-workspace" || second["applicationAvailability"] != "available" {
							t.Fatalf("second Workspace composition lost identity: %v", items)
						}
					}
					data = rows.([]any)[0].(map[string]any)
				}
				workspace := data["workspace"].(map[string]any)
				business := workspace["data"].(map[string]any)
				if workspace["source"] != "workspace" || business["id"] != "native-workspace" || business["state"] != "active" {
					t.Fatalf("wrong Workspace truth %v", workspace)
				}
				wantAvailability := "available"
				switch mode {
				case "application-unknown", "application-access-unavailable", "application-access-mismatched":
					wantAvailability = "unknown"
				case "application-no-deployment":
					wantAvailability = "unavailable"
				}
				if business["applicationAvailability"] != wantAvailability {
					t.Fatalf("application availability = %v, want %s", business["applicationAvailability"], wantAvailability)
				}
				if _, hasURL := business["url"]; hasURL != (wantAvailability == "available") {
					t.Fatalf("accessUrl presence disagrees with availability: %v", business)
				}
				wantAccessCalls := 1
				if mode == "application-unknown" || mode == "application-no-deployment" {
					wantAccessCalls = 0
				}
				wantDeploymentCalls := 1
				if mode == "multiple-workspaces" && path == "/api/v2/operator/workspaces" {
					wantDeploymentCalls, wantAccessCalls = 2, 2
				}
				if deploymentCalls-deploymentsBefore != wantDeploymentCalls || accessCalls-accessBefore != wantAccessCalls {
					t.Fatalf("Serve calls deployments=%d access=%d for %s", deploymentCalls-deploymentsBefore, accessCalls-accessBefore, path)
				}
				read := data["resourcesReadback"].(map[string]any)
				if mode == "unavailable" || mode == "not-assigned" {
					if read["available"] != false || read["data"] != nil || len(data["resources"].([]any)) != 0 {
						t.Fatalf("fabricated resource facts: %v", data)
					}
				} else {
					if read["source"] != "fabric" {
						t.Fatalf("wrong resource owner %v", read)
					}
					if mode == "unknown" && read["data"].(map[string]any)["outcome"] != "unknown" {
						t.Fatalf("unknown was hidden %v", read)
					}
				}
				if mode == "deletion-pending" || mode == "deletion-unknown" {
					facts := data["resources"].([]any)
					if len(facts) != 3 {
						t.Fatalf("deletion facts were dropped: %v", facts)
					}
					secret := facts[1].(map[string]any)
					want := "not_dispatched"
					if mode == "deletion-unknown" {
						want = "unknown"
					}
					if secret["status"].(map[string]any)["data"] != want || secret["providerId"].(map[string]any)["available"] != false {
						t.Fatalf("deletion fact was invented or rewritten: %v", secret)
					}
				}
				if data["tenant"].(map[string]any)["data"].(map[string]any)["id"] != "native-tenant" || data["ownerAccount"].(map[string]any)["available"] != false {
					t.Fatal("Tenant was confused with retained Account")
				}
			}
			if retainedCalls != 0 {
				t.Fatalf("retained owner called %d times", retainedCalls)
			}
			if mode == "denied" && (workspaceCalls != 0 || fabricCalls != 0) {
				t.Fatalf("denied read reached owners")
			}
			if mode == "serve-denied" && (workspaceCalls != 2 || fabricCalls != 0) {
				t.Fatalf("Serve denial did not stop composition after Workspace: workspace=%d fabric=%d", workspaceCalls, fabricCalls)
			}
			if (mode == "denied" || mode == "serve-denied") && (deploymentCalls != 0 || accessCalls != 0) {
				t.Fatalf("denied Workspace read reached Serve")
			}
		})
	}
}
