package launch

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
)

// PurchaseEligibilityReader reads the current Account fact from its sole owner.
// Accepted-order replay and recovery do not call this new-purchase boundary.
type PurchaseEligibilityReader interface {
	Read(context.Context, *api.CallContext) (*contracts.WorkspacePurchaseEligibility, error)
}

type cloudIdentityPurchaseEligibilityReader struct {
	client api.CloudIdentityAuthorizationClient
}

// NewCloudIdentityPurchaseEligibilityReader reads Workspace purchase eligibility
// from the canonical CloudIdentity owner over the existing mTLS gRPC channel.
func NewCloudIdentityPurchaseEligibilityReader(client api.CloudIdentityAuthorizationClient) (PurchaseEligibilityReader, error) {
	if client == nil {
		return nil, fmt.Errorf("CloudIdentity authorization client is required for Workspace purchase eligibility")
	}
	return &cloudIdentityPurchaseEligibilityReader{client: client}, nil
}

func (c *cloudIdentityPurchaseEligibilityReader) Read(ctx context.Context, call *api.CallContext) (*contracts.WorkspacePurchaseEligibility, error) {
	if call.GetActorId() == "" || call.GetSessionId() == "" {
		return nil, status.Error(codes.Unauthenticated, "a live purchase session is required")
	}
	resp, err := c.client.ReadWorkspacePurchaseEligibility(ctx, &api.ReadWorkspacePurchaseEligibilityRequest{Context: call})
	if err != nil {
		switch status.Code(err) {
		case codes.Unauthenticated:
			return nil, status.Error(codes.Unauthenticated, "purchase session or owner identity rejected")
		case codes.PermissionDenied:
			return nil, status.Error(codes.PermissionDenied, "account is not eligible for a new Workspace purchase")
		case codes.FailedPrecondition:
			return nil, status.Error(codes.FailedPrecondition, "purchase account identity is unresolved")
		default:
			return nil, status.Error(codes.Unavailable, "purchase eligibility owner is unavailable")
		}
	}
	if resp == nil || resp.GetActorId() != call.GetActorId() || strings.TrimSpace(resp.GetAccountId()) == "" || resp.GetAccountId() != strings.TrimSpace(resp.GetAccountId()) {
		return nil, status.Error(codes.DataLoss, "invalid purchase eligibility readback")
	}
	enabled := resp.GetWorkspacePurchaseEnabled()
	return &contracts.WorkspacePurchaseEligibility{
		ActorID:                  resp.GetActorId(),
		AccountID:                resp.GetAccountId(),
		WorkspacePurchaseEnabled: &enabled,
	}, nil
}

func (s *Service) requirePurchaseEligibility(ctx context.Context, call *api.CallContext) error {
	if s.PurchaseEligibility == nil {
		return status.Error(codes.Unavailable, "purchase eligibility owner is not configured")
	}
	fact, err := s.PurchaseEligibility.Read(ctx, call)
	if err != nil {
		return err
	}
	if fact == nil || fact.ActorID != call.GetActorId() || strings.TrimSpace(fact.AccountID) == "" || fact.WorkspacePurchaseEnabled == nil {
		return status.Error(codes.DataLoss, "invalid purchase eligibility readback")
	}
	if !*fact.WorkspacePurchaseEnabled {
		return status.Error(codes.PermissionDenied, "account is not eligible for a new Workspace purchase")
	}
	return nil
}
