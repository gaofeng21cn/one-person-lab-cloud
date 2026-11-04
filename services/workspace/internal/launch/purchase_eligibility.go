package launch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// PurchaseEligibilityReader reads the current Account fact from its sole owner.
// Accepted-order replay and recovery do not call this new-purchase boundary.
type PurchaseEligibilityReader interface {
	Read(context.Context, *api.CallContext) (*contracts.WorkspacePurchaseEligibility, error)
}

type purchaseEligibilityReader struct {
	origin string
	token  string
	http   *http.Client
}

// NewPurchaseEligibilityReader leaves historical recovery available when the
// retained owner is unconfigured; CreateWorkspace refuses new orders in that case.
func NewPurchaseEligibilityReader(origin, token string) (PurchaseEligibilityReader, error) {
	origin, token = strings.TrimSpace(origin), strings.TrimSpace(token)
	if origin == "" && token == "" {
		return nil, nil
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || len(token) < owneridentity.MinimumTokenLength {
		return nil, fmt.Errorf("Workspace purchase eligibility requires a Control Plane HTTP(S) origin and an independent token of at least %d characters", owneridentity.MinimumTokenLength)
	}
	return &purchaseEligibilityReader{origin: strings.TrimRight(origin, "/"), token: token, http: &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (c *purchaseEligibilityReader) Read(ctx context.Context, call *api.CallContext) (*contracts.WorkspacePurchaseEligibility, error) {
	if call.GetActorId() == "" || call.GetSessionId() == "" {
		return nil, status.Error(codes.Unauthenticated, "a live purchase session is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.origin+"/internal/cloud/workspace-purchase-eligibility", nil)
	if err != nil {
		return nil, status.Error(codes.Internal, "invalid purchase eligibility request")
	}
	req.Header.Set(owneridentity.PeerHeader, owneridentity.Workspace.String())
	req.Header.Set(owneridentity.TokenHeader, c.token)
	req.Header.Set(contracts.CloudSessionHeader, call.GetSessionId())
	req.Header.Set("Accept", "application/json")
	response, err := c.http.Do(req)
	if err != nil {
		return nil, status.Error(codes.Unavailable, "purchase eligibility owner is unavailable")
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return nil, status.Error(codes.Unauthenticated, "purchase session or owner identity rejected")
	case http.StatusForbidden:
		return nil, status.Error(codes.PermissionDenied, "account is not eligible for a new Workspace purchase")
	case http.StatusConflict:
		return nil, status.Error(codes.FailedPrecondition, "purchase account identity is unresolved")
	default:
		return nil, status.Error(codes.Unavailable, "purchase eligibility owner is unavailable")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	if err != nil || len(body) > 4096 {
		return nil, status.Error(codes.DataLoss, "invalid purchase eligibility readback")
	}
	var result contracts.WorkspacePurchaseEligibility
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil || decoder.Decode(new(any)) != io.EOF || result.ActorID != call.GetActorId() || strings.TrimSpace(result.AccountID) == "" || result.AccountID != strings.TrimSpace(result.AccountID) || result.WorkspacePurchaseEnabled == nil {
		return nil, status.Error(codes.DataLoss, "invalid purchase eligibility readback")
	}
	return &result, nil
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
