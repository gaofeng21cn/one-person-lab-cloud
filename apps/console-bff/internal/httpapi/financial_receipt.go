package httpapi

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"net/http"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

func (s *Server) registerFinancialReceiptRoutes(mux *http.ServeMux) {
	s.publisherRoute(mux, "GET /api/v2/workspaces/{workspaceId}/financial-receipts/{receiptId}", owneridentity.Ledger,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACEFINANCIALRECEIPT,
		api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, "workspaceId", nil,
		func(r *http.Request, c *api.CallContext, _ proto.Message) (proto.Message, error) {
			if s.ledger == nil {
				return nil, status.Error(codes.Unavailable, "Ledger owner is not configured")
			}
			if r.URL.RawQuery != "" {
				return nil, status.Error(codes.InvalidArgument, "receipt read accepts no query")
			}
			out, err := s.ledger.GetWorkspaceFinancialReceipt(r.Context(), &api.GetWorkspaceFinancialReceiptRpcRequest{Context: c, WorkspaceId: r.PathValue("workspaceId"), ReceiptId: r.PathValue("receiptId")})
			if err != nil {
				return nil, err
			}
			if out.GetWorkspaceId() != r.PathValue("workspaceId") || out.GetReceipt().GetId() != r.PathValue("receiptId") || out.GetWalletOperation().GetReceiptId() != r.PathValue("receiptId") || out.GetWalletOperation().GetWorkspaceId() != out.GetWorkspaceId() || out.GetQuote() == nil {
				return nil, status.Error(codes.Unavailable, "invalid Ledger financial receipt")
			}
			return out, nil
		})
}
