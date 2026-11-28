package identity

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
)

const (
	operatorAccountsMaxPageSize        = 50
	operatorAccountsMaxSearchRunes     = 100
	operatorAccountsEnrichConcurrency  = 4
	operatorAccountsEnrichReadTimeout  = 5 * time.Second
)

// ListOperatorAccounts reads all OPL Gateway users directly from the Sub2API
// administrative directory and overlays authoritative CloudIdentity policies
// (bootstrap/delegated admin roles and workspace purchase eligibility).
func (s *Service) ListOperatorAccounts(ctx context.Context, r *api.ListOperatorAccountsRpcRequest) (*api.OperatorGatewayAccountPage, error) {
	if err := s.requirePlatformAdminCall(
		ctx,
		r.GetContext(),
		api.OwnerEnum_OWNER_ENUM_TENANT,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTOPERATORACCOUNTS,
		api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PLATFORM,
		"",
	); err != nil {
		return nil, err
	}
	page := int(r.GetPage())
	pageSize := int(r.GetPageSize())
	search := strings.TrimSpace(r.GetSearch())
	if page < 1 || pageSize < 1 || pageSize > operatorAccountsMaxPageSize || utf8.RuneCountInString(search) > operatorAccountsMaxSearchRunes {
		return nil, status.Error(codes.InvalidArgument, "valid pagination and search bounds are required")
	}
	usersPage, err := s.Gateway.DirectoryUsers(ctx, page, pageSize, search)
	if err != nil {
		return nil, err
	}
	if len(usersPage.Items) == 0 {
		return &api.OperatorGatewayAccountPage{
			Total:    usersPage.Total,
			Page:     int32(page),
			PageSize: int32(pageSize),
		}, nil
	}

	userIDs := make([]int64, 0, len(usersPage.Items))
	actorIDs := make([]string, 0, len(usersPage.Items))
	for _, user := range usersPage.Items {
		userIDs = append(userIDs, user.ID)
		actorIDs = append(actorIDs, strconv.FormatInt(user.ID, 10))
	}

	purchaseMap, err := s.queryPurchaseEligibilityMap(ctx, actorIDs)
	if err != nil {
		return nil, err
	}
	adminMap, err := s.platformAdminStates(ctx, actorIDs)
	if err != nil {
		return nil, err
	}
	relayMap, err := s.linkRelayEntitlementStates(ctx, actorIDs)
	if err != nil {
		return nil, err
	}

	usageMap, keyCounts := s.enrichGatewayDirectoryUsers(ctx, userIDs)

	items := make([]*api.OperatorGatewayAccount, 0, len(usersPage.Items))
	for _, user := range usersPage.Items {
		actorID := strconv.FormatInt(user.ID, 10)
		isBootstrap := s.isBootstrapAdmin(actorID)
		isAdmin := isBootstrap || adminMap[actorID]
		purchaseEnabled, hasPurchaseRow := purchaseMap[actorID]
		if !hasPurchaseRow {
			purchaseEnabled = isAdmin
		}
		relayEnabled, hasRelayRow := relayMap[actorID]
		if !hasRelayRow {
			relayEnabled = isAdmin
		}
		account := &api.OperatorGatewayAccount{
			ActorId:                  actorID,
			Email:                    user.Email,
			Status:                   user.Status,
			WorkspacePurchaseEnabled: purchaseEnabled,
			PlatformAdminEnabled:     isAdmin,
			IsBootstrapAdmin:         isBootstrap,
			LinkRelayEnabled:         relayEnabled,
			WalletAvailable:          !user.BalanceUnavailable,
			WalletUsdMicros:          user.BalanceUSDMicros,
		}
		if !user.UpdatedAt.IsZero() {
			account.GatewayUpdatedAt = timestamppb.New(user.UpdatedAt)
		}
		if usage, ok := usageMap[user.ID]; ok {
			account.UsageAvailable = true
			account.TodayActualCostUsdMicros = usage.TodayActualCostUSDMicros
			account.TotalActualCostUsdMicros = usage.TotalActualCostUSDMicros
			for _, platform := range usage.ByPlatform {
				account.UsageByPlatform = append(account.UsageByPlatform, &api.OperatorGatewayPlatformUsage{
					Platform:                 platform.Platform,
					TodayActualCostUsdMicros: platform.TodayActualCostUSDMicros,
					TotalActualCostUsdMicros: platform.TotalActualCostUSDMicros,
				})
			}
		}
		if keyCount, ok := keyCounts[user.ID]; ok {
			account.KeyCountAvailable = true
			account.KeyCount = keyCount
		}
		items = append(items, account)
	}

	return &api.OperatorGatewayAccountPage{
		Items:    items,
		Total:    usersPage.Total,
		Page:     int32(page),
		PageSize: int32(pageSize),
	}, nil
}

func (s *Service) queryPurchaseEligibilityMap(ctx context.Context, actorIDs []string) (map[string]bool, error) {
	if len(actorIDs) == 0 {
		return map[string]bool{}, nil
	}
	args := make([]any, 0, len(actorIDs))
	placeholders := make([]string, 0, len(actorIDs))
	for _, id := range actorIDs {
		args = append(args, id)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT actor_id,workspace_purchase_enabled FROM tenant.workspace_purchase_eligibility WHERE actor_id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return nil, persistence(err)
	}
	defer rows.Close()
	out := make(map[string]bool, len(actorIDs))
	for rows.Next() {
		var actorID string
		var enabled bool
		if scanErr := rows.Scan(&actorID, &enabled); scanErr != nil {
			return nil, persistence(scanErr)
		}
		out[actorID] = enabled
	}
	if err := rows.Err(); err != nil {
		return nil, persistence(err)
	}
	return out, nil
}

func (s *Service) enrichGatewayDirectoryUsers(ctx context.Context, userIDs []int64) (map[int64]GatewayBatchUserUsage, map[int64]int32) {
	enrichCtx, cancel := context.WithTimeout(ctx, operatorAccountsEnrichReadTimeout)
	defer cancel()

	var usageMap map[int64]GatewayBatchUserUsage
	keyCounts := make(map[int64]int32, len(userIDs))
	var keyMu sync.Mutex
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		if stats, err := s.Gateway.DirectoryBatchUsersUsage(enrichCtx, userIDs); err == nil {
			usageMap = stats
		}
	}()

	gate := make(chan struct{}, operatorAccountsEnrichConcurrency)
	for _, userID := range userIDs {
		wg.Add(1)
		go func(uid int64) {
			defer wg.Done()
			select {
			case gate <- struct{}{}:
			case <-enrichCtx.Done():
				return
			}
			defer func() { <-gate }()
			if count, err := s.Gateway.DirectoryUserKeyCount(enrichCtx, uid); err == nil {
				keyMu.Lock()
				keyCounts[uid] = count
				keyMu.Unlock()
			}
		}(userID)
	}
	wg.Wait()
	return usageMap, keyCounts
}
