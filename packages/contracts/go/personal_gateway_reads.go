package contracts

type PersonalGatewayHistoryEntry struct {
	Type           string  `json:"type"`
	ValueUSDMicros string  `json:"valueUsdMicros"`
	Status         string  `json:"status"`
	UsedAt         *string `json:"usedAt"`
	CreatedAt      string  `json:"createdAt"`
}
type PersonalGatewayHistoryPage struct {
	Items    []PersonalGatewayHistoryEntry `json:"items"`
	Total    int64                         `json:"total"`
	Page     int                           `json:"page"`
	PageSize int                           `json:"pageSize"`
	Pages    int64                         `json:"pages"`
}
type PersonalGatewayEndpoint struct {
	BaseURL string `json:"baseUrl"`
}
