package clients

import (
	"context"
	"net/http"
	"net/url"
	contracts "opl-cloud/packages/contracts/go"
	"strconv"
)

func (c *Clients) GetPersonalGatewayHistory(ctx context.Context, session string, page, size int) (*contracts.LegacyBillingSource[contracts.PersonalGatewayHistoryPage], error) {
	out := &contracts.LegacyBillingSource[contracts.PersonalGatewayHistoryPage]{}
	path := "/internal/cloud/gateway/balance-history?" + url.Values{"page": {strconv.Itoa(page)}, "pageSize": {strconv.Itoa(size)}}.Encode()
	if err := c.controlPlane.request(ctx, session, http.MethodGet, path, "", out); err != nil {
		return nil, err
	}
	return out, nil
}
func (c *Clients) GetPersonalGatewayEndpoint(ctx context.Context, session string) (*contracts.LegacyBillingSource[contracts.PersonalGatewayEndpoint], error) {
	out := &contracts.LegacyBillingSource[contracts.PersonalGatewayEndpoint]{}
	if err := c.controlPlane.request(ctx, session, http.MethodGet, "/internal/cloud/gateway/endpoint", "", out); err != nil {
		return nil, err
	}
	return out, nil
}
