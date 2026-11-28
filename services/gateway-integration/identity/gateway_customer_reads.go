package identity

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// The Gateway owner serves the customer's own Gateway reads: the balance
// history of the account's spendable balance, and the public endpoint the
// customer's client calls. Both facts live in the Gateway itself, so this owner
// reads them through the identity binding it already owns instead of another
// service proxying them.

// gatewayBalanceDisplayEntry is one customer-visible balance record.
type gatewayBalanceDisplayEntry struct {
	Code           string
	Type           string
	ValueUSDMicros int64
	Status         string
	UsedAt         *time.Time
	CreatedAt      time.Time
}

// gatewayBalanceDisplayPage is one page of the customer's balance history.
type gatewayBalanceDisplayPage struct {
	Items    []gatewayBalanceDisplayEntry
	Total    int64
	Page     int
	PageSize int
	Pages    int
}

// PublicEndpoint is the public Gateway origin a customer's client calls. It is
// a deployment fact of this owner's own Gateway configuration.
func (g *Gateway) PublicEndpoint() string {
	if g == nil || strings.TrimSpace(g.base) == "" {
		return ""
	}
	return strings.TrimRight(g.base, "/") + "/v1"
}

// gatewayBalanceDisplayEntryOf projects one upstream record into the
// customer-visible shape. A record that does not carry its own identity, its
// amount or its creation time is refused rather than displayed as an empty row.
func gatewayBalanceDisplayEntryOf(item gatewayBalanceRecord, recordType string, userID int64) (gatewayBalanceDisplayEntry, error) {
	invalid := status.Error(codes.Unavailable, "invalid Gateway balance history entry")
	if item.Code == "" || item.Type != recordType || item.Status == "" || item.Value == nil || item.CreatedAt == nil || strings.TrimSpace(*item.CreatedAt) == "" {
		return gatewayBalanceDisplayEntry{}, invalid
	}
	createdAt, err := time.Parse(time.RFC3339Nano, *item.CreatedAt)
	if err != nil || createdAt.IsZero() {
		return gatewayBalanceDisplayEntry{}, invalid
	}
	var usedAt *time.Time
	if item.UsedAt != nil && strings.TrimSpace(*item.UsedAt) != "" {
		parsed, err := time.Parse(time.RFC3339Nano, *item.UsedAt)
		if err != nil {
			return gatewayBalanceDisplayEntry{}, invalid
		}
		usedAt = &parsed
	}
	if item.Status == "used" && (item.UsedBy == nil || *item.UsedBy != userID || usedAt == nil) {
		return gatewayBalanceDisplayEntry{}, invalid
	}
	value, err := gatewayDecimalUSDMicros(*item.Value)
	if err != nil {
		return gatewayBalanceDisplayEntry{}, invalid
	}
	return gatewayBalanceDisplayEntry{Code: item.Code, Type: "balance", ValueUSDMicros: value, Status: item.Status, UsedAt: usedAt, CreatedAt: createdAt}, nil
}

// customerBalanceHistoryPage reads one display page of a subject's balance
// history from the Gateway itself. The two monetary record streams are read
// separately and merged here, because an unfiltered upstream read would mix
// subscriptions and concurrency into the customer's own money.
func (g *Gateway) customerBalanceHistoryPage(ctx context.Context, userID int64, page, pageSize int) (gatewayBalanceDisplayPage, error) {
	if userID <= 0 || page < 1 || pageSize < 1 || pageSize > 100 {
		return gatewayBalanceDisplayPage{}, status.Error(codes.InvalidArgument, "a positive wallet identity and a valid page are required")
	}
	if !g.DirectoryConfigured() {
		return gatewayBalanceDisplayPage{}, status.Error(codes.FailedPrecondition, "the Gateway directory identity is not configured")
	}
	token, err := g.directoryToken(ctx)
	if err != nil {
		return gatewayBalanceDisplayPage{}, err
	}
	needed := page * pageSize
	upstreamPageSize := needed
	if upstreamPageSize > gatewayAdjustmentLookups {
		upstreamPageSize = gatewayAdjustmentLookups
	}
	entries := make([]gatewayBalanceDisplayEntry, 0)
	var total int64
	for _, recordType := range gatewayBalanceRecordTypes {
		read := 0
		for upstreamPage := 1; ; upstreamPage++ {
			data, err := g.balanceHistoryPage(ctx, token, userID, upstreamPage, upstreamPageSize, recordType)
			if err != nil {
				return gatewayBalanceDisplayPage{}, err
			}
			if upstreamPage == 1 {
				total += data.Total
			}
			for _, item := range data.Items {
				entry, err := gatewayBalanceDisplayEntryOf(item, recordType, userID)
				if err != nil {
					return gatewayBalanceDisplayPage{}, err
				}
				entries = append(entries, entry)
			}
			read += len(data.Items)
			if read >= needed || upstreamPage >= data.Pages {
				break
			}
		}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		left, right := entries[i].CreatedAt, entries[j].CreatedAt
		if entries[i].UsedAt != nil {
			left = *entries[i].UsedAt
		}
		if entries[j].UsedAt != nil {
			right = *entries[j].UsedAt
		}
		if left.Equal(right) {
			return entries[i].Code < entries[j].Code
		}
		return left.After(right)
	})
	pages := 1
	if total > 0 {
		pages = int((total + int64(pageSize) - 1) / int64(pageSize))
	}
	if page > pages {
		return gatewayBalanceDisplayPage{}, status.Error(codes.InvalidArgument, "invalid Gateway balance history pagination")
	}
	start := min((page-1)*pageSize, len(entries))
	end := min(start+pageSize, len(entries))
	return gatewayBalanceDisplayPage{Items: entries[start:end], Total: total, Page: page, PageSize: pageSize, Pages: pages}, nil
}

// gatewaySessionActor resolves the caller's own verified identity from the
// session it presents. The session must be live — the owner's own fence refuses
// a revoked, expired or suspended-Tenant session — and the actor the request
// names must be the actor that session belongs to, so a caller cannot borrow
// another identity's session.
func (s *Service) gatewaySessionActor(ctx context.Context, call *api.CallContext) (*api.Session, error) {
	if call.GetSessionId() == "" {
		return nil, status.Error(codes.Unauthenticated, "active Cloud session required")
	}
	session, _, _, err := s.session(ctx, call.GetSessionId())
	if err != nil {
		return nil, err
	}
	if call.GetActorId() != "" && call.GetActorId() != session.GetActorId() {
		return nil, denied()
	}
	return session, nil
}

// gatewaySubjectForSession resolves the Gateway subject of the caller's own
// verified identity. The subject is never named by the request: a caller reads
// the balance of the identity it owns.
func (s *Service) gatewaySubjectForSession(ctx context.Context, call *api.CallContext) (int64, error) {
	session, err := s.gatewaySessionActor(ctx, call)
	if err != nil {
		return 0, err
	}
	binding, err := s.userBinding(ctx, session.GetActorId())
	if err != nil {
		return 0, err
	}
	if err := binding.usableIdentity(); err != nil {
		return 0, err
	}
	subject, err := strconv.ParseInt(binding.Sub2APIUserID, 10, 64)
	if err != nil || subject <= 0 {
		return 0, status.Error(codes.FailedPrecondition, "Gateway identity is not bound to a subject")
	}
	return subject, nil
}

// ListPersonalGatewayBalanceHistory publishes one page of the caller's own
// balance history, read from the Gateway that keeps the spendable balance.
func (s *Service) ListPersonalGatewayBalanceHistory(ctx context.Context, r *api.ListPersonalGatewayBalanceHistoryRpcRequest) (*api.PersonalGatewayBalanceHistoryPage, error) {
	if err := peer(ctx, owneridentity.ConsoleBFF); err != nil {
		return nil, err
	}
	page, size := r.GetPage(), r.GetPageSize()
	if err := personalPagination(page, size); err != nil {
		return nil, err
	}
	if s.Gateway == nil {
		return nil, status.Error(codes.Unavailable, "Gateway owner is not configured")
	}
	subject, err := s.gatewaySubjectForSession(ctx, r.GetContext())
	if err != nil {
		return nil, err
	}
	history, err := s.Gateway.customerBalanceHistoryPage(ctx, subject, int(page), int(size))
	if err != nil {
		return nil, err
	}
	out := &api.PersonalGatewayBalanceHistoryPage{Total: history.Total, Page: int32(history.Page), PageSize: int32(history.PageSize), Pages: int32(history.Pages)}
	for _, entry := range history.Items {
		item := &api.PersonalGatewayBalanceHistoryEntry{
			Type:           entry.Type,
			ValueUsdMicros: strconv.FormatInt(entry.ValueUSDMicros, 10),
			Status:         entry.Status,
			CreatedAt:      entry.CreatedAt.UTC().Format(time.RFC3339Nano),
		}
		if entry.UsedAt != nil {
			item.UsedAt = proto.String(entry.UsedAt.UTC().Format(time.RFC3339Nano))
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}

// GetPersonalGatewayEndpoint publishes the public Gateway origin this owner's
// own configuration points at.
//
// The endpoint is a customer-facing fact, so the caller proves it owns the
// identity it reads it for: its session must be live and the actor it names
// must be that session's own actor. A revoked session or a borrowed session
// reads nothing.
func (s *Service) GetPersonalGatewayEndpoint(ctx context.Context, r *api.GetPersonalGatewayEndpointRpcRequest) (*api.PersonalGatewayEndpoint, error) {
	if err := peer(ctx, owneridentity.ConsoleBFF); err != nil {
		return nil, err
	}
	if _, err := s.gatewaySessionActor(ctx, r.GetContext()); err != nil {
		return nil, err
	}
	if s.Gateway == nil {
		return nil, status.Error(codes.Unavailable, "Gateway owner is not configured")
	}
	endpoint := s.Gateway.PublicEndpoint()
	if strings.TrimSpace(endpoint) == "" {
		return nil, status.Error(codes.Unavailable, "Gateway public endpoint is unavailable")
	}
	return &api.PersonalGatewayEndpoint{BaseUrl: endpoint}, nil
}
