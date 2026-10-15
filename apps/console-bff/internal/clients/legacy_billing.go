package clients

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	contracts "opl-cloud/packages/contracts/go"
)

func (c *Clients) ListLegacyBillingReceipts(ctx context.Context, session, cursor string, limit int) (*contracts.LegacyBillingSource[contracts.LegacyBillingPage], error) {
	out := &contracts.LegacyBillingSource[contracts.LegacyBillingPage]{}
	path := "/internal/cloud/legacy-billing/receipts?" + url.Values{"cursor": {cursor}, "limit": {strconv.Itoa(limit)}}.Encode()
	if err := c.controlPlane.request(ctx, session, http.MethodGet, path, "", out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Clients) GetLegacyBillingReceipt(ctx context.Context, session, id string) (*contracts.LegacyBillingSource[contracts.LegacyBillingReceipt], error) {
	out := &contracts.LegacyBillingSource[contracts.LegacyBillingReceipt]{}
	if err := c.controlPlane.request(ctx, session, http.MethodGet, "/internal/cloud/legacy-billing/receipts/"+url.PathEscape(id), "", out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Clients) GetLegacyWorkspaceSettlements(ctx context.Context, session string) (*contracts.LegacyBillingSource[contracts.LegacySettlementTrend], error) {
	out := &contracts.LegacyBillingSource[contracts.LegacySettlementTrend]{}
	if err := c.controlPlane.request(ctx, session, http.MethodGet, "/internal/cloud/legacy-billing/workspace-settlements", "", out); err != nil {
		return nil, err
	}
	return out, nil
}
