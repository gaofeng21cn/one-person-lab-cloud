package server

import (
	"context"
	"math"
	"sort"
	"strconv"
	"sync"

	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

const operatorMaxSafeInteger int64 = 1<<53 - 1

// operatorGatewaySummary is a live read projection of the complete OPL mapping
// set. It neither owns a wallet nor accepts a partial sum as a complete total.
func (app *controlPlaneServer) operatorGatewaySummary(ctx context.Context, service *controlplane.Service) map[string]any {
	result := map[string]any{}
	unavailable := func(reason string) map[string]any {
		for _, field := range []string{"wallet", "keys", "usage"} {
			result[field] = operatorUnavailableSource("sub2api", reason)
		}
		return result
	}
	unavailable("sub2api_unavailable")
	ctx, cancel := context.WithTimeout(ctx, operatorPageReadTimeout)
	defer cancel()
	accounts, err := app.tables.ListAccounts(ctx, "")
	if err != nil {
		return unavailable("operator_account_collection_unavailable")
	}
	sort.Slice(accounts, func(i, j int) bool { return stringValue(accounts[i]["id"]) < stringValue(accounts[j]["id"]) })
	local := make([]map[string]any, 0, len(accounts))
	items := make([]any, 0, len(accounts))
	ids := make([]int64, 0, len(accounts))
	seenAccounts, seenOwners, seenRemote := map[string]bool{}, map[string]bool{}, map[int64]bool{}
	for _, account := range accounts {
		accountID, ownerID := stringValue(account["id"]), stringValue(account["ownerUserId"])
		remoteID, valid := positiveIntegerField(account, "sub2apiUserId")
		owner, found, err := app.tables.GetUser(ctx, ownerID)
		if err != nil || !found || !valid || !ownsAccount(account, owner) || seenAccounts[accountID] || seenOwners[ownerID] || seenRemote[remoteID] {
			return unavailable("operator_account_identity_conflict")
		}
		seenAccounts[accountID], seenOwners[ownerID], seenRemote[remoteID] = true, true, true
		local = append(local, map[string]any{"owner": owner, "remoteId": remoteID})
		ids = append(ids, remoteID)
		items = append(items, map[string]any{"sub2apiUserId": strconv.FormatInt(remoteID, 10), "keyCount": sourceEnvelope("sub2api", "unavailable", nil, "")})
	}
	gate := make(chan struct{}, operatorPageTotalConcurrency)
	var users map[int64]clients.Sub2APIUser
	usage := make(map[int64]clients.Sub2APIBatchUserUsage, len(ids))
	var usageErr error
	var reads sync.WaitGroup
	reads.Add(3)
	go func() { defer reads.Done(); users = app.operatorCurrentPageUsers(ctx, service, local, gate) }()
	go func() { defer reads.Done(); app.populateOperatorKeyCounts(ctx, service, items, gate) }()
	go func() {
		defer reads.Done()
		for start := 0; start < len(ids); start += clients.MaxSub2APIBatchIDs {
			end := min(start+clients.MaxSub2APIBatchIDs, len(ids))
			release, ok := operatorPageReadPermit(ctx, gate)
			if !ok {
				usageErr = ctx.Err()
				return
			}
			batch, err := service.Sub2APIBatchUsersUsage(ctx, ids[start:end])
			release()
			if err != nil {
				usageErr = err
				return
			}
			for _, id := range ids[start:end] {
				if value, ok := batch[id]; ok && value.UserID == id {
					usage[id] = value
				}
			}
		}
	}()
	reads.Wait()
	walletOK, keysOK, usageOK := true, true, usageErr == nil
	var balance, keys, today, total int64
	for index, joined := range local {
		id := ids[index]
		user, found := users[id]
		owner := joined["owner"].(map[string]any)
		if !found || user.ID != id || user.Email != normalizeEmail(stringValue(owner["email"])) || (user.Status != "active" && user.Status != "disabled") {
			return unavailable("operator_gateway_identity_unavailable")
		}
		if user.BalanceUnavailable || !operatorAddInt64(&balance, user.BalanceUSDMicros) {
			walletOK = false
		}
		keyEnvelope := items[index].(map[string]any)["keyCount"].(map[string]any)
		count, countOK := keyEnvelope["data"].(int)
		if keyEnvelope["available"] != true || !countOK || count < 0 || !operatorAddInt64(&keys, int64(count)) || keys > operatorMaxSafeInteger {
			keysOK = false
		}
		value, usageFound := usage[id]
		if !usageFound || value.UserID != id || value.TodayActualCostUSDMicros < 0 || value.TotalActualCostUSDMicros < 0 ||
			!operatorAddInt64(&today, value.TodayActualCostUSDMicros) || !operatorAddInt64(&total, value.TotalActualCostUSDMicros) || today > operatorMaxSafeInteger || total > operatorMaxSafeInteger {
			usageOK = false
		}
	}
	status := "available"
	if len(accounts) == 0 {
		status = "empty"
	}
	if walletOK {
		result["wallet"] = sourceEnvelope("sub2api", status, map[string]any{"currency": "USD", "usdMicros": strconv.FormatInt(balance, 10)}, "")
	}
	if keysOK {
		result["keys"] = sourceEnvelope("sub2api", status, map[string]any{"total": keys}, "")
	}
	if usageOK {
		result["usage"] = sourceEnvelope("sub2api", status, map[string]any{"todayActualCostUsdMicros": today, "totalActualCostUsdMicros": total}, "")
	}
	return result
}

func operatorAddInt64(total *int64, value int64) bool {
	if value > 0 && *total > math.MaxInt64-value || value < 0 && *total < math.MinInt64-value {
		return false
	}
	*total += value
	return true
}

func operatorUnavailableSource(source, reason string) map[string]any {
	result := sourceEnvelope(source, "unavailable", nil, "")
	result["reasonCode"] = reason
	return result
}
