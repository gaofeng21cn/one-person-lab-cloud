package identity

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

type delegatedCredentialKey struct{ session, subject string }
type delegatedCredential struct {
	key     delegatedCredentialKey
	bearer  string
	expires time.Time
	timer   *time.Timer
}

func reauthenticationRequired() error {
	return owneridentity.WithErrorCode(status.Error(codes.Unauthenticated, "Gateway personal reads require a fresh login"), api.ErrorCodeEnum_ERROR_CODE_ENUM_REAUTHENTICATION_REQUIRED)
}
func (s *Service) rememberDelegated(ref, subject, bearer string, until, sessionUntil time.Time) {
	if sessionUntil.Before(until) {
		until = sessionUntil
	}
	if bearer == "" || !until.After(time.Now()) {
		return
	}
	key := delegatedCredentialKey{ref, subject}
	credential := &delegatedCredential{key: key, bearer: bearer, expires: until}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.delegated == nil {
		s.delegated = make(map[delegatedCredentialKey]*delegatedCredential)
	}
	if previous := s.delegated[key]; previous != nil {
		previous.timer.Stop()
	}
	s.delegated[key] = credential
	credential.timer = time.AfterFunc(time.Until(until), func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.delegated[key] == credential {
			delete(s.delegated, key)
		}
	})
}
func (s *Service) forgetDelegatedSession(ref string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, credential := range s.delegated {
		if key.session == ref {
			credential.timer.Stop()
			delete(s.delegated, key)
		}
	}
}
func (s *Service) personalCredential(ctx context.Context, call *api.CallContext) (*delegatedCredential, error) {
	if err := peer(ctx, owneridentity.ConsoleBFF); err != nil {
		return nil, err
	}
	if call.GetSessionId() == "" {
		return nil, status.Error(codes.Unauthenticated, "active Cloud session required")
	}
	session, _, _, err := s.session(ctx, call.GetSessionId())
	if err != nil {
		return nil, err
	}
	binding, err := s.userBinding(ctx, session.GetActorId())
	if err != nil {
		return nil, err
	}
	key := delegatedCredentialKey{call.GetSessionId(), binding.Sub2APIUserID}
	s.mu.Lock()
	credential := s.delegated[key]
	s.mu.Unlock()
	if credential == nil || !credential.expires.After(time.Now()) {
		return nil, reauthenticationRequired()
	}
	bounded, cancel := context.WithDeadline(ctx, credential.expires)
	defer cancel()
	_, err = s.Gateway.Read(bounded, credential.bearer, key.subject)
	if !credential.expires.After(time.Now()) || status.Code(err) == codes.Unauthenticated {
		s.forgetDelegatedSession(key.session)
		return nil, reauthenticationRequired()
	}
	if err != nil {
		return nil, err
	}
	return credential, nil
}
func (s *Service) personalRequest(ctx context.Context, credential *delegatedCredential, path string, out any) error {
	s.mu.Lock()
	current := s.delegated[credential.key] == credential
	s.mu.Unlock()
	if !current || !credential.expires.After(time.Now()) {
		return reauthenticationRequired()
	}
	bounded, cancel := context.WithDeadline(ctx, credential.expires)
	defer cancel()
	// Reuse the owner's bounded authenticated HTTP transport with the user's
	// bearer. This transport never selects or refreshes a directory credential.
	data, err := s.Gateway.adminRequest(bounded, path, nil, credential.bearer)
	if !credential.expires.After(time.Now()) {
		s.forgetDelegatedSession(credential.key.session)
		return reauthenticationRequired()
	}
	if err != nil {
		var upstream *gatewayHTTPError
		if errors.As(err, &upstream) {
			switch upstream.StatusCode {
			case http.StatusUnauthorized, http.StatusForbidden:
				s.forgetDelegatedSession(credential.key.session)
				return reauthenticationRequired()
			case http.StatusNotFound:
				return status.Error(codes.NotFound, "Gateway personal resource not found")
			case http.StatusTooManyRequests:
				return status.Error(codes.ResourceExhausted, "Gateway personal reads rate limited")
			}
		}
		return status.Error(codes.Unavailable, "Gateway personal read unavailable")
	}
	// These user reads require an explicit success code and an actual data
	// member. Explicit null remains decodable for the native optional rates map;
	// each authoritative collection below must still be a JSON array.
	var envelope struct {
		Code *int            `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(data, &envelope) != nil || envelope.Code == nil || *envelope.Code != 0 || len(envelope.Data) == 0 || json.Unmarshal(envelope.Data, out) != nil {
		return invalidPersonalResponse()
	}
	return nil
}

// personalKeyPayload decodes the native user DTO. It deliberately has no raw key
// field: the ordinary read projection cannot carry that secret to a caller.
type personalKeyPayload struct {
	ID                 int64        `json:"id"`
	UserID             int64        `json:"user_id"`
	Name               string       `json:"name"`
	GroupID            *int64       `json:"group_id"`
	Status             string       `json:"status"`
	IPWhitelist        []string     `json:"ip_whitelist"`
	IPBlacklist        []string     `json:"ip_blacklist"`
	Quota              *json.Number `json:"quota"`
	QuotaUsed          *json.Number `json:"quota_used"`
	RateLimit5h        *json.Number `json:"rate_limit_5h"`
	RateLimit1d        *json.Number `json:"rate_limit_1d"`
	RateLimit7d        *json.Number `json:"rate_limit_7d"`
	Usage5h            *json.Number `json:"usage_5h"`
	Usage1d            *json.Number `json:"usage_1d"`
	Usage7d            *json.Number `json:"usage_7d"`
	CurrentConcurrency int32        `json:"current_concurrency"`
	LastUsedAt         *time.Time   `json:"last_used_at"`
	LastUsedIP         *string      `json:"last_used_ip"`
	ExpiresAt          *time.Time   `json:"expires_at"`
	CreatedAt          *time.Time   `json:"created_at"`
	UpdatedAt          *time.Time   `json:"updated_at"`
}

func invalidPersonalResponse() error {
	return status.Error(codes.Unavailable, "invalid Gateway personal read response")
}
func personalTimestamp(v *time.Time) (*timestamppb.Timestamp, error) {
	if v == nil {
		return nil, nil
	}
	stamp := timestamppb.New(*v)
	if v.IsZero() || stamp.CheckValid() != nil {
		return nil, invalidPersonalResponse()
	}
	return stamp, nil
}
func (s *Service) personalKey(ctx context.Context, item personalKeyPayload, subject string) (*api.PersonalGatewayKey, error) {
	if item.ID <= 0 || strconv.FormatInt(item.UserID, 10) != subject || strings.TrimSpace(item.Name) == "" || item.GroupID != nil && *item.GroupID <= 0 || item.CurrentConcurrency < 0 || item.CreatedAt == nil || item.UpdatedAt == nil {
		return nil, invalidPersonalResponse()
	}
	state := map[string]api.PersonalGatewayKeyStatusEnum{"active": api.PersonalGatewayKeyStatusEnum_PERSONAL_GATEWAY_KEY_STATUS_ENUM_ACTIVE, "inactive": api.PersonalGatewayKeyStatusEnum_PERSONAL_GATEWAY_KEY_STATUS_ENUM_DISABLED, "quota_exhausted": api.PersonalGatewayKeyStatusEnum_PERSONAL_GATEWAY_KEY_STATUS_ENUM_QUOTA_EXHAUSTED, "expired": api.PersonalGatewayKeyStatusEnum_PERSONAL_GATEWAY_KEY_STATUS_ENUM_EXPIRED}[item.Status]
	if state == 0 {
		return nil, invalidPersonalResponse()
	}
	out := &api.PersonalGatewayKey{Id: strconv.FormatInt(item.ID, 10), Name: item.Name, Status: state, Kind: api.PersonalGatewayKeyKindEnum_PERSONAL_GATEWAY_KEY_KIND_ENUM_GENERAL, IpWhitelist: item.IPWhitelist, IpBlacklist: item.IPBlacklist, CurrentConcurrency: item.CurrentConcurrency, LastUsedIp: item.LastUsedIP}
	if item.GroupID != nil {
		id := strconv.FormatInt(*item.GroupID, 10)
		out.GroupId = &id
	}
	for _, pair := range []struct {
		value  *json.Number
		target *int64
	}{{item.Quota, &out.QuotaUsdMicros}, {item.QuotaUsed, &out.QuotaUsedUsdMicros}, {item.RateLimit5h, &out.RateLimit5HUsdMicros}, {item.RateLimit1d, &out.RateLimit1DUsdMicros}, {item.RateLimit7d, &out.RateLimit7DUsdMicros}, {item.Usage5h, &out.Usage5HUsdMicros}, {item.Usage1d, &out.Usage1DUsdMicros}, {item.Usage7d, &out.Usage7DUsdMicros}} {
		if pair.value == nil {
			return nil, invalidPersonalResponse()
		}
		value, err := floorUSDDecimalToMicros(*pair.value)
		if err != nil {
			return nil, invalidPersonalResponse()
		}
		*pair.target = value
	}
	for _, pair := range []struct {
		value  *time.Time
		target **timestamppb.Timestamp
	}{{item.LastUsedAt, &out.LastUsedAt}, {item.ExpiresAt, &out.ExpiresAt}, {item.CreatedAt, &out.CreatedAt}, {item.UpdatedAt, &out.UpdatedAt}} {
		var err error
		*pair.target, err = personalTimestamp(pair.value)
		if err != nil {
			return nil, err
		}
	}
	var managed bool
	err := s.GatewayStore.DB().QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM gateway.key_bindings WHERE external_key_id=$1 AND purpose='workspace_managed')`, out.Id).Scan(&managed)
	if err != nil {
		return nil, persistence(err)
	}
	if managed {
		out.Kind = api.PersonalGatewayKeyKindEnum_PERSONAL_GATEWAY_KEY_KIND_ENUM_WORKSPACE
	}
	return out, nil
}
func personalPagination(page, size int32) error {
	if page < 1 || page > 1_000_000 || size < 1 || size > 100 {
		return status.Error(codes.InvalidArgument, "valid page and page size required")
	}
	return nil
}
func personalPageValid(total int64, page, size, pages int32, count int) bool {
	if total < 0 {
		return false
	}
	expectedPages := int64(1)
	if total > 0 {
		expectedPages = (total-1)/int64(size) + 1
	}
	if int64(pages) != expectedPages {
		return false
	}
	remaining := total - int64(page-1)*int64(size)
	if remaining < 0 {
		remaining = 0
	}
	if remaining > int64(size) {
		remaining = int64(size)
	}
	return remaining == int64(count)
}
func (s *Service) ListPersonalGatewayKeys(ctx context.Context, r *api.ListPersonalGatewayKeysRpcRequest) (*api.PersonalGatewayKeyPage, error) {
	if err := personalPagination(r.GetPage(), r.GetPageSize()); err != nil {
		return nil, err
	}
	sorts := map[string]string{"": "created_at", "name": "name", "id": "id", "currentConcurrency": "current_concurrency", "expiresAt": "expires_at", "status": "status", "lastUsedAt": "last_used_at", "createdAt": "created_at"}
	sortBy, ok := sorts[r.GetSortBy()]
	if !ok {
		return nil, status.Error(codes.InvalidArgument, "invalid key sort")
	}
	order := r.GetSortOrder()
	if order == "" {
		order = "desc"
	}
	states := map[string]string{"": "", "active": "active", "disabled": "inactive", "quota_exhausted": "quota_exhausted", "expired": "expired"}
	state, ok := states[r.GetStatus()]
	search := strings.TrimSpace(r.GetSearch())
	if !ok || order != "asc" && order != "desc" || len([]rune(search)) > 100 {
		return nil, status.Error(codes.InvalidArgument, "invalid key filters")
	}
	values := url.Values{"page": {strconv.Itoa(int(r.Page))}, "page_size": {strconv.Itoa(int(r.PageSize))}, "sort_by": {sortBy}, "sort_order": {order}}
	if search != "" {
		values.Set("search", search)
	}
	if state != "" {
		values.Set("status", state)
	}
	if r.GroupId != nil {
		id, err := strconv.ParseInt(r.GetGroupId(), 10, 64)
		if err != nil || id < 0 {
			return nil, status.Error(codes.InvalidArgument, "invalid key group")
		}
		values.Set("group_id", strconv.FormatInt(id, 10))
	}
	credential, err := s.personalCredential(ctx, r.GetContext())
	if err != nil {
		return nil, err
	}
	var data struct {
		Items    []personalKeyPayload `json:"items"`
		Total    int64                `json:"total"`
		Page     int32                `json:"page"`
		PageSize int32                `json:"page_size"`
		Pages    int32                `json:"pages"`
	}
	if err = s.personalRequest(ctx, credential, "/api/v1/keys?"+values.Encode(), &data); err != nil {
		return nil, err
	}
	if data.Items == nil || data.Page != r.Page || data.PageSize != r.PageSize || !personalPageValid(data.Total, data.Page, data.PageSize, data.Pages, len(data.Items)) {
		return nil, invalidPersonalResponse()
	}
	page := &api.PersonalGatewayKeyPage{Items: make([]*api.PersonalGatewayKey, 0, len(data.Items)), Total: data.Total, Page: data.Page, PageSize: data.PageSize, Pages: data.Pages}
	seen := make(map[int64]bool, len(data.Items))
	for _, item := range data.Items {
		if seen[item.ID] {
			return nil, invalidPersonalResponse()
		}
		seen[item.ID] = true
		key, err := s.personalKey(ctx, item, credential.key.subject)
		if err != nil {
			return nil, err
		}
		page.Items = append(page.Items, key)
	}
	return page, nil
}

func personalKeyID(raw string) (string, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return "", status.Error(codes.InvalidArgument, "a positive Gateway key id is required")
	}
	return strconv.FormatInt(id, 10), nil
}
func (s *Service) readPersonalKey(ctx context.Context, credential *delegatedCredential, id string) (*api.PersonalGatewayKey, error) {
	var item personalKeyPayload
	if err := s.personalRequest(ctx, credential, "/api/v1/keys/"+id, &item); err != nil {
		return nil, err
	}
	if strconv.FormatInt(item.ID, 10) != id {
		return nil, invalidPersonalResponse()
	}
	return s.personalKey(ctx, item, credential.key.subject)
}
func (s *Service) GetPersonalGatewayKey(ctx context.Context, r *api.GetPersonalGatewayKeyRpcRequest) (*api.PersonalGatewayKey, error) {
	id, err := personalKeyID(r.GetKeyId())
	if err != nil {
		return nil, err
	}
	credential, err := s.personalCredential(ctx, r.GetContext())
	if err != nil {
		return nil, err
	}
	return s.readPersonalKey(ctx, credential, id)
}

func (s *Service) ListPersonalGatewayGroups(ctx context.Context, r *api.ListPersonalGatewayGroupsRpcRequest) (*api.PersonalGatewayGroupPage, error) {
	credential, err := s.personalCredential(ctx, r.GetContext())
	if err != nil {
		return nil, err
	}
	var groups []struct {
		ID               int64    `json:"id"`
		Name             string   `json:"name"`
		Description      string   `json:"description"`
		Platform         string   `json:"platform"`
		SubscriptionType string   `json:"subscription_type"`
		Status           string   `json:"status"`
		RateMultiplier   *float64 `json:"rate_multiplier"`
	}
	if err = s.personalRequest(ctx, credential, "/api/v1/groups/available", &groups); err != nil {
		return nil, err
	}
	if groups == nil {
		return nil, invalidPersonalResponse()
	}
	var rates map[string]float64
	if err = s.personalRequest(ctx, credential, "/api/v1/groups/rates", &rates); err != nil {
		return nil, err
	}
	for id, rate := range rates {
		n, err := strconv.ParseInt(id, 10, 64)
		if err != nil || n <= 0 || rate < 0 || math.IsInf(rate, 0) || math.IsNaN(rate) {
			return nil, invalidPersonalResponse()
		}
	}
	if len(groups) > math.MaxInt32 {
		return nil, invalidPersonalResponse()
	}
	out := &api.PersonalGatewayGroupPage{Items: make([]*api.PersonalGatewayGroup, 0, len(groups)), Total: int32(len(groups))}
	seen := make(map[int64]bool, len(groups))
	for _, group := range groups {
		if group.ID <= 0 || seen[group.ID] || strings.TrimSpace(group.Name) == "" || group.RateMultiplier == nil || *group.RateMultiplier < 0 || math.IsInf(*group.RateMultiplier, 0) || math.IsNaN(*group.RateMultiplier) {
			return nil, invalidPersonalResponse()
		}
		seen[group.ID] = true
		id := strconv.FormatInt(group.ID, 10)
		rate := *group.RateMultiplier
		if override, ok := rates[id]; ok {
			rate = override
		}
		out.Items = append(out.Items, &api.PersonalGatewayGroup{Id: id, Name: group.Name, Description: group.Description, Platform: group.Platform, SubscriptionType: group.SubscriptionType, Status: group.Status, RateMultiplier: rate})
	}
	return out, nil
}

func personalUsageQuery(key, period string) (url.Values, error) {
	start, end, ok := gatewayUsageDateRange(period, time.Now())
	if !ok {
		return nil, status.Error(codes.InvalidArgument, "a valid usage period is required")
	}
	return url.Values{"api_key_id": {key}, "start_date": {start}, "end_date": {end}, "timezone": {"Asia/Shanghai"}}, nil
}
func (s *Service) ListPersonalGatewayKeyUsage(ctx context.Context, r *api.ListPersonalGatewayKeyUsageRpcRequest) (*api.PersonalGatewayKeyUsagePage, error) {
	if err := personalPagination(r.GetPage(), r.GetPageSize()); err != nil {
		return nil, err
	}
	id, err := personalKeyID(r.GetKeyId())
	if err != nil {
		return nil, err
	}
	values, err := personalUsageQuery(id, r.GetPeriod())
	if err != nil {
		return nil, err
	}
	values.Set("page", strconv.Itoa(int(r.Page)))
	values.Set("page_size", strconv.Itoa(int(r.PageSize)))
	values.Set("sort_by", "created_at")
	values.Set("sort_order", "desc")
	credential, err := s.personalCredential(ctx, r.GetContext())
	if err != nil {
		return nil, err
	}
	if _, err = s.readPersonalKey(ctx, credential, id); err != nil {
		return nil, err
	}
	var data struct {
		Items []struct {
			UserID              int64        `json:"user_id"`
			APIKeyID            int64        `json:"api_key_id"`
			RequestID           string       `json:"request_id"`
			CreatedAt           *time.Time   `json:"created_at"`
			Model               string       `json:"model"`
			InboundEndpoint     string       `json:"inbound_endpoint"`
			RequestType         string       `json:"request_type"`
			InputTokens         *int64       `json:"input_tokens"`
			OutputTokens        *int64       `json:"output_tokens"`
			CacheCreationTokens *int64       `json:"cache_creation_tokens"`
			CacheReadTokens     *int64       `json:"cache_read_tokens"`
			ActualCost          *json.Number `json:"actual_cost"`
			DurationMS          *int64       `json:"duration_ms"`
			FirstTokenMS        *int64       `json:"first_token_ms"`
		} `json:"items"`
		Total    int64 `json:"total"`
		Page     int32 `json:"page"`
		PageSize int32 `json:"page_size"`
		Pages    int32 `json:"pages"`
	}
	if err = s.personalRequest(ctx, credential, "/api/v1/usage?"+values.Encode(), &data); err != nil {
		return nil, err
	}
	if data.Items == nil || data.Page != r.Page || data.PageSize != r.PageSize || !personalPageValid(data.Total, data.Page, data.PageSize, data.Pages, len(data.Items)) {
		return nil, invalidPersonalResponse()
	}
	out := &api.PersonalGatewayKeyUsagePage{Items: make([]*api.PersonalGatewayKeyUsage, 0, len(data.Items)), Total: data.Total, Page: data.Page, PageSize: data.PageSize, Pages: data.Pages}
	for _, item := range data.Items {
		if strconv.FormatInt(item.UserID, 10) != credential.key.subject || strconv.FormatInt(item.APIKeyID, 10) != id || item.RequestID == "" || item.CreatedAt == nil || item.Model == "" || item.RequestType == "" || item.ActualCost == nil || item.DurationMS != nil && *item.DurationMS < 0 || item.FirstTokenMS != nil && *item.FirstTokenMS < 0 {
			return nil, invalidPersonalResponse()
		}
		for _, count := range []*int64{item.InputTokens, item.OutputTokens, item.CacheCreationTokens, item.CacheReadTokens} {
			if count == nil || *count < 0 {
				return nil, invalidPersonalResponse()
			}
		}
		cost, err := floorUSDDecimalToMicros(*item.ActualCost)
		if err != nil {
			return nil, invalidPersonalResponse()
		}
		created, err := personalTimestamp(item.CreatedAt)
		if err != nil {
			return nil, err
		}
		out.Items = append(out.Items, &api.PersonalGatewayKeyUsage{ApiKeyId: id, RequestId: item.RequestID, CreatedAt: created, Model: item.Model, InboundEndpoint: item.InboundEndpoint, RequestType: item.RequestType, InputTokens: *item.InputTokens, OutputTokens: *item.OutputTokens, CacheCreationTokens: *item.CacheCreationTokens, CacheReadTokens: *item.CacheReadTokens, ActualCostUsdMicros: cost, DurationMs: item.DurationMS, FirstTokenMs: item.FirstTokenMS})
	}
	return out, nil
}

func (s *Service) GetPersonalGatewayKeyUsageSummary(ctx context.Context, r *api.GetPersonalGatewayKeyUsageSummaryRpcRequest) (*api.UsageSummary, error) {
	id, err := personalKeyID(r.GetKeyId())
	if err != nil {
		return nil, err
	}
	values, err := personalUsageQuery(id, r.GetPeriod())
	if err != nil {
		return nil, err
	}
	credential, err := s.personalCredential(ctx, r.GetContext())
	if err != nil {
		return nil, err
	}
	if _, err = s.readPersonalKey(ctx, credential, id); err != nil {
		return nil, err
	}
	var data gatewayUsageStatsPayload
	if err = s.personalRequest(ctx, credential, "/api/v1/usage/stats?"+values.Encode(), &data); err != nil {
		return nil, err
	}
	stats, err := data.stats()
	if err != nil {
		return nil, err
	}
	return &api.UsageSummary{TotalRequests: stats.TotalRequests, TotalInputTokens: stats.TotalInputTokens, TotalOutputTokens: stats.TotalOutputTokens, TotalTokens: stats.TotalTokens, TotalActualCostUsdMicros: stats.TotalActualCostUSDMicros}, nil
}
