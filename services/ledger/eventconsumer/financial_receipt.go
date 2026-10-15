package eventconsumer

import (
	"context"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
)

// GetWorkspaceFinancialReceipt is a customer financial projection under the
// current team's authorization. The immutable coordination receipt reader keeps
// its original-actor fence; this path never returns that private evidence blob.
func (s *Server) GetWorkspaceFinancialReceipt(ctx context.Context, r *api.GetWorkspaceFinancialReceiptRpcRequest) (*api.WorkspaceFinancialReceipt, error) {
	peer, ok := ownerservice.PeerOwner(ctx)
	if !ok || peer != owneridentity.ConsoleBFF {
		return nil, status.Error(codes.Unauthenticated, "verified Console BFF peer required")
	}
	call := r.GetContext()
	if call.GetSessionId() == "" || call.GetAcceptedOperationGrantId() != "" {
		return nil, status.Error(codes.Unauthenticated, "live customer session required")
	}
	tenantID, workspaceID, receiptID := call.GetScope().GetTenant().GetTenantId(), r.GetWorkspaceId(), r.GetReceiptId()
	if tenantID == "" || strings.TrimSpace(workspaceID) == "" || workspaceID != strings.TrimSpace(workspaceID) || strings.TrimSpace(receiptID) == "" || receiptID != strings.TrimSpace(receiptID) {
		return nil, status.Error(codes.InvalidArgument, "Tenant, Workspace and receipt identities required")
	}
	if err := s.Authorizer.Authorize(ctx, call, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACEFINANCIALRECEIPT,
		&api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, Id: proto.String(workspaceID)},
		ownerservice.ResourceScope{TenantID: tenantID}); err != nil {
		return nil, err
	}
	out, err := s.store.WorkspaceFinancialReceipt(ctx, tenantID, workspaceID, receiptID)
	if err != nil {
		return nil, receiptError(err)
	}
	return out, nil
}
