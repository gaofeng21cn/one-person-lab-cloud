package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

var (
	errWalletAdjustmentAccount    = errors.New("wallet_adjustment_account_invalid")
	errWalletAdjustmentConflict   = errors.New("wallet_adjustment_conflict")
	errWalletAdjustmentState      = errors.New("wallet_adjustment_state_invalid")
	errWalletAdjustmentUpstream   = errors.New("wallet_adjustment_upstream_unavailable")
	walletAmountPattern           = regexp.MustCompile(`^(0|[1-9][0-9]{0,12})(\.[0-9]{1,6})?$`)
	walletRelatedOperationPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,199}$`)
)

type walletAdjustmentRequest struct {
	Kind                  string `json:"kind"`
	AmountUSD             string `json:"amountUsd"`
	Reason                string `json:"reason"`
	RelatedOperationID    string `json:"relatedOperationId,omitempty"`
	ConfirmationAccountID string `json:"confirmationAccountId"`
}

type walletAdjustmentRecoveryRequest struct {
	AccountID   string `json:"accountId"`
	EvidenceRef string `json:"evidenceRef"`
}

type walletAdjustmentUpstreamFailure struct {
	Phase      string `json:"phase"`
	HTTPStatus int    `json:"httpStatus,omitempty"`
	ErrorCode  string `json:"errorCode"`
	RequestID  string `json:"requestId,omitempty"`
}

type walletAdjustmentAuditIdentity struct {
	Actor     auditActor
	IPAddress string
	UserAgent string
}

func (app *controlPlaneServer) walletAdjustmentAuditIdentity(r *http.Request) walletAdjustmentAuditIdentity {
	user, _ := app.sessionUserContext(r)
	actor := auditActor{UserID: stringValue(user["id"]), Role: stringValue(user["role"]), AccountID: stringValue(user["accountId"])}
	if systemActor, ok := r.Context().Value(auditActorContextKey{}).(auditActor); ok {
		actor = systemActor
	}
	return walletAdjustmentAuditIdentity{Actor: actor, IPAddress: boundedAuditText(requestIP(r), maxAuditIPAddressBytes), UserAgent: boundedAuditText(r.UserAgent(), maxAuditUserAgentBytes)}
}

type walletAdjustmentOperation struct {
	PersistedResult      string                           `json:"-"`
	PersistedStatus      string                           `json:"-"`
	RequestHash          string                           `json:"requestHash"`
	Phase                string                           `json:"phase"`
	AccountID            string                           `json:"accountId"`
	Sub2APIUserID        int64                            `json:"sub2apiUserId"`
	Kind                 string                           `json:"kind"`
	AmountUSDMicros      int64                            `json:"amountUsdMicros"`
	AmountUSD            string                           `json:"amountUsd"`
	Reason               string                           `json:"reason"`
	RelatedOperationID   string                           `json:"relatedOperationId,omitempty"`
	ActorUserID          string                           `json:"actorUserId"`
	CanonicalRedeemCode  string                           `json:"canonicalRedeemCode,omitempty"`
	RedeemCodeVersion    string                           `json:"redeemCodeVersion,omitempty"`
	LegacySupersession   string                           `json:"legacySupersessionStatus,omitempty"`
	AdjustmentAttempted  bool                             `json:"adjustmentAttempted,omitempty"`
	BeforeBalanceKnown   bool                             `json:"beforeBalanceKnown,omitempty"`
	BeforeBalanceMicros  int64                            `json:"beforeBalanceUsdMicros,omitempty"`
	BeforeBalanceReadAt  string                           `json:"beforeBalanceReadAt,omitempty"`
	AfterBalanceKnown    bool                             `json:"afterBalanceKnown,omitempty"`
	AfterBalanceMicros   int64                            `json:"afterBalanceUsdMicros,omitempty"`
	AfterBalanceReadAt   string                           `json:"afterBalanceReadAt,omitempty"`
	BalanceHistoryRef    string                           `json:"balanceHistoryRef,omitempty"`
	BalanceHistoryUsedAt string                           `json:"balanceHistoryUsedAt,omitempty"`
	ReceiptID            string                           `json:"receiptId,omitempty"`
	ErrorCode            string                           `json:"errorCode,omitempty"`
	RecoveryRequestHash  string                           `json:"recoveryRequestHash,omitempty"`
	RecoveryAttempted    bool                             `json:"recoveryAttempted,omitempty"`
	RecoveryEvidenceRef  string                           `json:"recoveryEvidenceRef,omitempty"`
	RecoveryActorUserID  string                           `json:"recoveryActorUserId,omitempty"`
	RecoveryAuthorizedAt string                           `json:"recoveryAuthorizedAt,omitempty"`
	UpstreamFailure      *walletAdjustmentUpstreamFailure `json:"upstreamFailure,omitempty"`
	CreatedAt            string                           `json:"createdAt"`
	UpdatedAt            string                           `json:"updatedAt"`
	Status               string                           `json:"-"`
}

func (app *controlPlaneServer) createWalletAdjustment(w http.ResponseWriter, r *http.Request, service *controlplane.Service) {
	key, ok := requiredMutationKey(w, r)
	if !ok {
		return
	}
	accountID := strings.TrimSpace(r.PathValue("accountId"))
	var input walletAdjustmentRequest
	if decodeStrictGatewayRequest(r, &input) != nil {
		writeError(w, http.StatusBadRequest, "invalid_wallet_adjustment")
		return
	}
	amountMicros, ok := validWalletAdjustmentRequest(input, accountID, key)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_wallet_adjustment")
		return
	}
	actor, _ := app.sessionUserContext(r)
	actorID := stringValue(actor["id"])
	operationID := "wallet-adjustment-" + stableID(accountID, key)[:18]
	requestHash := stableID("wallet-adjustment-v1", accountID, actorID, input.Kind, strconv.FormatInt(amountMicros, 10), strings.TrimSpace(input.Reason), strings.TrimSpace(input.RelatedOperationID))

	// Avoid redundant work within this process; the store owns cross-process admission and CAS.
	unlock := app.lockResource("sub2api-wallet", accountID)
	defer unlock()

	operation, found, err := app.walletAdjustment(r.Context(), operationID, requestHash)
	if err != nil {
		writeWalletAdjustmentError(w, err)
		return
	}
	created := !found
	if !found {
		account, remoteUserID, accountErr := app.walletAdjustmentAccount(r.Context(), service, accountID)
		if accountErr != nil {
			writeWalletAdjustmentError(w, accountErr)
			return
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		operation = walletAdjustmentOperation{
			RequestHash: requestHash, Phase: "before_balance", AccountID: accountID, Sub2APIUserID: remoteUserID,
			Kind: input.Kind, AmountUSDMicros: amountMicros, AmountUSD: formatWalletUSD(amountMicros), Reason: strings.TrimSpace(input.Reason),
			RelatedOperationID: strings.TrimSpace(input.RelatedOperationID), ActorUserID: actorID,
			CanonicalRedeemCode: walletAdjustmentRedeemCode(operationID), RedeemCodeVersion: "v2", CreatedAt: now, UpdatedAt: now, Status: "pending",
		}
		if stringValue(account["ownerUserId"]) == "" {
			writeWalletAdjustmentError(w, errWalletAdjustmentAccount)
			return
		}
		if err := app.persistWalletAdjustment(r.Context(), operationID, &operation); err != nil {
			writeWalletAdjustmentError(w, err)
			return
		}
	}
	if operation.Status == "failed" {
		writeWalletAdjustmentError(w, errWalletAdjustmentConflict)
		return
	}

	if operation.Status != "succeeded" && operation.Status != "manual_review" {
		operation, err = app.runWalletAdjustment(r.Context(), service, operationID, operation, app.walletAdjustmentAuditIdentity(r))
		if err != nil {
			writeWalletAdjustmentError(w, err)
			return
		}
	}
	status := http.StatusOK
	if operation.Status == "manual_review" {
		status = http.StatusAccepted
	} else if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, walletAdjustmentDTO(operationID, operation))
}

func (app *controlPlaneServer) getWalletAdjustment(w http.ResponseWriter, r *http.Request) {
	operationID := strings.TrimSpace(r.PathValue("operationId"))
	operation, found, err := app.walletAdjustment(r.Context(), operationID, "")
	if err != nil {
		writeWalletAdjustmentError(w, err)
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "wallet_adjustment_not_found")
		return
	}
	writeJSON(w, http.StatusOK, walletAdjustmentDTO(operationID, operation))
}

func (app *controlPlaneServer) recoverWalletAdjustment(w http.ResponseWriter, r *http.Request, service *controlplane.Service) {
	key, ok := requiredMutationKey(w, r)
	if !ok {
		return
	}
	operationID := strings.TrimSpace(r.PathValue("operationId"))
	var input walletAdjustmentRecoveryRequest
	if decodeStrictGatewayRequest(r, &input) != nil || !validAccountID(input.AccountID) || input.AccountID != strings.TrimSpace(input.AccountID) ||
		!validBillingReviewEvidenceRef(input.EvidenceRef) || operationID == "" || !validBillingReviewOpaqueID(key) {
		writeError(w, http.StatusBadRequest, "invalid_wallet_adjustment_recovery")
		return
	}
	recoveryHash := stableID("wallet-adjustment-recovery-v1", operationID, input.AccountID, input.EvidenceRef, key)
	actorID := app.sessionUserID(r)

	unlock := app.lockResource("sub2api-wallet", input.AccountID)
	defer unlock()
	operation, found, err := app.walletAdjustment(r.Context(), operationID, "")
	if err != nil {
		writeWalletAdjustmentError(w, err)
		return
	}
	if !found || operation.AccountID != input.AccountID {
		writeError(w, http.StatusNotFound, "wallet_adjustment_not_found")
		return
	}
	if operation.Status == "failed" {
		writeWalletAdjustmentError(w, errWalletAdjustmentConflict)
		return
	}
	if operation.Status == "succeeded" {
		if operation.RecoveryRequestHash != recoveryHash {
			writeWalletAdjustmentError(w, errWalletAdjustmentConflict)
			return
		}
	}
	if operation.Status != "succeeded" && operation.Status != "manual_review" && (operation.Status != "pending" || operation.RecoveryRequestHash != recoveryHash) {
		writeWalletAdjustmentError(w, errWalletAdjustmentConflict)
		return
	}

	if operation.Status == "manual_review" {
		operation, err = app.prepareWalletAdjustmentRecovery(r.Context(), service, operationID, operation, recoveryHash, input.EvidenceRef, actorID)
		if err != nil {
			writeWalletAdjustmentError(w, err)
			return
		}
	}
	if operation.Status == "pending" {
		operation, err = app.runWalletAdjustment(r.Context(), service, operationID, operation, app.walletAdjustmentAuditIdentity(r))
		if err != nil {
			writeWalletAdjustmentError(w, err)
			return
		}
	}
	audit := app.auditEvent(r, "gateway.wallet_adjustment.recover", "gateway_wallet", operation.AccountID, operation.AccountID, nil,
		map[string]any{"operationId": operationID, "evidenceRef": input.EvidenceRef, "status": operation.Status}, operation.Status)
	audit["id"] = "audit-" + stableID("gateway.wallet_adjustment.recover", operationID, key)[:12]
	if err := app.tables.SaveAuditEvent(r.Context(), audit); err != nil {
		writeError(w, http.StatusInternalServerError, "state_persist_failed")
		return
	}
	writeJSON(w, http.StatusOK, walletAdjustmentDTO(operationID, operation))
}

func (app *controlPlaneServer) prepareWalletAdjustmentRecovery(ctx context.Context, service *controlplane.Service, operationID string, operation walletAdjustmentOperation, requestHash, evidenceRef, actorID string) (walletAdjustmentOperation, error) {
	if operation.RecoveryRequestHash != "" && operation.RecoveryRequestHash != requestHash {
		return operation, errWalletAdjustmentConflict
	}
	if operation.RecoveryRequestHash == "" {
		operation.RecoveryRequestHash = requestHash
		operation.RecoveryEvidenceRef = evidenceRef
		operation.RecoveryActorUserID = actorID
		operation.RecoveryAuthorizedAt = time.Now().UTC().Format(time.RFC3339Nano)
		if err := app.persistWalletAdjustment(ctx, operationID, &operation); err != nil {
			return operation, errWalletAdjustmentState
		}
	}
	if operation.ErrorCode == "wallet_adjustment_recovery_history_conflict" {
		return operation, errWalletAdjustmentConflict
	}
	legacyCode, v2Code := legacyWalletAdjustmentRedeemCode(operationID), walletAdjustmentRedeemCode(operationID)
	history, err := service.FinancialBalanceHistoryByCodes(ctx, operation.Sub2APIUserID, []string{legacyCode, v2Code})
	if err != nil {
		recordWalletAdjustmentUpstreamFailure(&operation, "recovery_readback", err, "balance_history_unavailable")
		operation.ErrorCode = "wallet_adjustment_recovery_readback_unavailable"
		if persistErr := app.persistWalletAdjustment(ctx, operationID, &operation); persistErr != nil {
			return operation, errWalletAdjustmentState
		}
		return operation, nil
	}
	legacyEntry, legacyState, legacyErr := inspectWalletAdjustmentHistory(history, legacyCode, operation)
	v2Entry, v2State, v2Err := inspectWalletAdjustmentHistory(history, v2Code, operation)
	confirmedLegacy, confirmedV2 := legacyState == "confirmed", v2State == "confirmed"
	if legacyErr != nil || v2Err != nil || confirmedLegacy && confirmedV2 || confirmedLegacy && operation.CanonicalRedeemCode != "" ||
		(confirmedLegacy || confirmedV2) && !operation.BeforeBalanceKnown {
		operation.ErrorCode = "wallet_adjustment_recovery_history_conflict"
		if persistErr := app.persistWalletAdjustment(ctx, operationID, &operation); persistErr != nil {
			return operation, errWalletAdjustmentState
		}
		return operation, errWalletAdjustmentConflict
	}
	operation.ErrorCode = ""
	if confirmedLegacy {
		operation.LegacySupersession = "legacy_history_confirmed"
		operation.BalanceHistoryUsedAt = legacyEntry.UsedAt.UTC().Format(time.RFC3339Nano)
		operation.BalanceHistoryRef = walletAdjustmentBalanceHistoryRef(operation.Sub2APIUserID, legacyEntry)
		operation.Status, operation.Phase = "pending", "after_balance"
		if err := app.persistWalletAdjustment(ctx, operationID, &operation); err != nil {
			return operation, errWalletAdjustmentState
		}
		return operation, nil
	}
	if confirmedV2 {
		wasLegacy := operation.CanonicalRedeemCode == "" && operation.RedeemCodeVersion == ""
		operation.CanonicalRedeemCode, operation.RedeemCodeVersion = v2Code, "v2"
		if wasLegacy {
			operation.LegacySupersession = "v2_history_confirmed"
		}
		operation.BalanceHistoryUsedAt = v2Entry.UsedAt.UTC().Format(time.RFC3339Nano)
		operation.BalanceHistoryRef = walletAdjustmentBalanceHistoryRef(operation.Sub2APIUserID, v2Entry)
		operation.Status, operation.Phase = "pending", "after_balance"
		if err := app.persistWalletAdjustment(ctx, operationID, &operation); err != nil {
			return operation, errWalletAdjustmentState
		}
		return operation, nil
	}
	// An absent audit row cannot prove an earlier wallet write did not happen.
	// Recovery is read-only even with fresh operator authorization.
	operation.ErrorCode = "wallet_adjustment_recovery_readback_unavailable"
	if err := app.persistWalletAdjustment(ctx, operationID, &operation); err != nil {
		return operation, errWalletAdjustmentState
	}
	return operation, nil
}

func validWalletAdjustmentRequest(input walletAdjustmentRequest, accountID, key string) (int64, bool) {
	reason, related := strings.TrimSpace(input.Reason), strings.TrimSpace(input.RelatedOperationID)
	if !validAccountID(accountID) || input.ConfirmationAccountID != accountID || strings.TrimSpace(key) != key || len(key) > 200 ||
		(input.Kind != "recharge" && input.Kind != "debit" && input.Kind != "business_refund") || input.AmountUSD != strings.TrimSpace(input.AmountUSD) ||
		!walletAmountPattern.MatchString(input.AmountUSD) || reason == "" || reason != input.Reason || len([]rune(reason)) > 200 || strings.IndexFunc(reason, unicode.IsControl) >= 0 {
		return 0, false
	}
	if input.Kind == "business_refund" {
		if related == "" || related != input.RelatedOperationID || !walletRelatedOperationPattern.MatchString(related) {
			return 0, false
		}
	} else if related != "" {
		return 0, false
	}
	amount, err := clients.ParseUSDDecimalMicros(input.AmountUSD)
	return amount, err == nil && amount > 0
}

func (app *controlPlaneServer) walletAdjustmentAccount(ctx context.Context, service *controlplane.Service, accountID string) (map[string]any, int64, error) {
	account, found, err := app.tables.GetAccount(ctx, accountID)
	if err != nil {
		return nil, 0, errWalletAdjustmentState
	}
	remoteUserID, remoteOK := positiveIntegerField(account, "sub2apiUserId")
	if !found || stringValue(account["status"]) != "active" || !remoteOK {
		return nil, 0, errWalletAdjustmentAccount
	}
	owner, found, err := app.tables.GetUser(ctx, stringValue(account["ownerUserId"]))
	if err != nil {
		return nil, 0, errWalletAdjustmentState
	}
	if !found || !ownsActiveAccount(account, owner) {
		return nil, 0, errWalletAdjustmentAccount
	}
	remote, err := service.Sub2APIUser(ctx, remoteUserID)
	if err != nil {
		return nil, 0, errWalletAdjustmentUpstream
	}
	if remote.Status != "active" || normalizeEmail(remote.Email) != normalizeEmail(stringValue(owner["email"])) {
		return nil, 0, errWalletAdjustmentAccount
	}
	return account, remoteUserID, nil
}

func (app *controlPlaneServer) walletAdjustment(ctx context.Context, operationID, requestHash string) (walletAdjustmentOperation, bool, error) {
	if operationID == "" {
		return walletAdjustmentOperation{}, false, nil
	}
	row, found, err := app.tables.GetRuntimeOperation(ctx, operationID)
	if err != nil {
		return walletAdjustmentOperation{}, false, errWalletAdjustmentState
	}
	if !found {
		return walletAdjustmentOperation{}, false, nil
	}
	operation, err := decodeWalletAdjustment(row)
	if err != nil {
		return walletAdjustmentOperation{}, false, errWalletAdjustmentState
	}
	if requestHash != "" && operation.RequestHash != requestHash {
		return walletAdjustmentOperation{}, false, errIdempotencyConflict
	}
	return operation, true, nil
}

func decodeWalletAdjustment(row map[string]any) (walletAdjustmentOperation, error) {
	var operation walletAdjustmentOperation
	if stringValue(row["action"]) != "gateway.wallet_adjustment.v1" || json.Unmarshal([]byte(stringValue(row["result"])), &operation) != nil {
		return walletAdjustmentOperation{}, errWalletAdjustmentState
	}
	operation.Status = stringValue(row["status"])
	operation.PersistedResult, operation.PersistedStatus = stringValue(row["result"]), operation.Status
	operationID := stringValue(row["id"])
	v2Identity := operation.RedeemCodeVersion == "v2" && operation.CanonicalRedeemCode == walletAdjustmentRedeemCode(operationID)
	legacyReadOnly := operation.RedeemCodeVersion == "" && operation.CanonicalRedeemCode == "" &&
		(operation.Status == "manual_review" || operation.LegacySupersession == "legacy_history_confirmed")
	validSupersession := operation.LegacySupersession == "" || operation.LegacySupersession == "legacy_history_confirmed" ||
		operation.LegacySupersession == "v2_history_confirmed" || operation.LegacySupersession == "v2_adopted"
	if operation.RequestHash == "" || operation.AccountID == "" || operation.Sub2APIUserID <= 0 || operation.ActorUserID == "" || operation.CreatedAt == "" || operation.UpdatedAt == "" ||
		operation.AmountUSDMicros <= 0 || stringValue(row["accountId"]) != operation.AccountID ||
		stringValue(row["resourceId"]) != operation.AccountID || stringValue(row["resourceKind"]) != "gateway_wallet" || operation.Status == "" ||
		(!v2Identity && !legacyReadOnly) || !validSupersession ||
		(operation.LegacySupersession == "legacy_history_confirmed" && !legacyReadOnly) ||
		((operation.LegacySupersession == "v2_history_confirmed" || operation.LegacySupersession == "v2_adopted") && !v2Identity) ||
		(operation.LegacySupersession == "v2_adopted" && !operation.RecoveryAttempted) ||
		(operation.RecoveryRequestHash != "" && (operation.RecoveryEvidenceRef == "" || operation.RecoveryActorUserID == "" || operation.RecoveryAuthorizedAt == "")) {
		return walletAdjustmentOperation{}, errWalletAdjustmentState
	}
	return operation, nil
}

func (app *controlPlaneServer) persistWalletAdjustment(ctx context.Context, operationID string, operation *walletAdjustmentOperation) error {
	if operation.Status == "pending" && operation.CanonicalRedeemCode == "" && operation.RedeemCodeVersion == "" && operation.LegacySupersession == "" && operation.RecoveryRequestHash == "" {
		operation.CanonicalRedeemCode, operation.RedeemCodeVersion = walletAdjustmentRedeemCode(operationID), "v2"
	}
	operation.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	saved, err := app.tables.SaveWalletAdjustment(ctx, operationID, *operation)
	if err == nil {
		*operation = saved
	}
	return err
}

func walletAdjustmentRow(operationID string, operation walletAdjustmentOperation) map[string]any {
	return map[string]any{
		"id": operationID, "operationId": operationID, "accountId": operation.AccountID, "resourceId": operation.AccountID,
		"resourceKind": "gateway_wallet", "action": "gateway.wallet_adjustment.v1", "status": operation.Status,
		"result": string(mustJSON(operation)), "createdAt": operation.CreatedAt,
	}
}

func (app *controlPlaneServer) runWalletAdjustment(ctx context.Context, service *controlplane.Service, operationID string, operation walletAdjustmentOperation, audit walletAdjustmentAuditIdentity) (walletAdjustmentOperation, error) {
	// Older recovery rows could reset AdjustmentAttempted before replaying a
	// write. Their retained recovery attempt is also an uncertain dispatch;
	// upgrading must not turn that row into a new native wallet adjustment.
	if operation.RecoveryAttempted && !operation.AdjustmentAttempted {
		operation.AdjustmentAttempted = true
		if operation.Phase == "adjustment" {
			operation.Phase = "authoritative_readback"
		}
		if err := app.persistWalletAdjustment(ctx, operationID, &operation); err != nil {
			return operation, errWalletAdjustmentState
		}
	}
	if operation.Kind == "business_refund" && !operation.AdjustmentAttempted {
		if err := app.confirmWalletRefundSource(ctx, service, operation); err != nil {
			return operation, err
		}
	}
	for range 8 {
		switch operation.Phase {
		case "before_balance":
			balance, err := service.Sub2APIBalance(ctx, operation.Sub2APIUserID)
			if err != nil || balance.UserID != operation.Sub2APIUserID || balance.Status != "active" || balance.USDMicros < 0 {
				recordWalletAdjustmentUpstreamFailure(&operation, "before_balance", err, "balance_readback_unavailable")
				if persistErr := app.persistWalletAdjustment(ctx, operationID, &operation); persistErr != nil {
					return operation, errWalletAdjustmentState
				}
				return operation, errWalletAdjustmentUpstream
			}
			if operation.Kind == "debit" && balance.USDMicros < operation.AmountUSDMicros || operation.Kind != "debit" && balance.USDMicros > math.MaxInt64-operation.AmountUSDMicros {
				operation.BeforeBalanceKnown, operation.BeforeBalanceMicros = true, balance.USDMicros
				operation.BeforeBalanceReadAt = time.Now().UTC().Format(time.RFC3339Nano)
				operation.Status, operation.Phase, operation.ErrorCode = "failed", "complete", errWalletAdjustmentConflict.Error()
				if err := app.persistWalletAdjustment(ctx, operationID, &operation); err != nil {
					return operation, errWalletAdjustmentState
				}
				return operation, errWalletAdjustmentConflict
			}
			operation.BeforeBalanceKnown, operation.BeforeBalanceMicros = true, balance.USDMicros
			operation.BeforeBalanceReadAt, operation.Phase = time.Now().UTC().Format(time.RFC3339Nano), "adjustment"
			if err := app.persistWalletAdjustment(ctx, operationID, &operation); err != nil {
				return operation, errWalletAdjustmentState
			}
		case "adjustment":
			if operation.RedeemCodeVersion != "v2" || operation.CanonicalRedeemCode != walletAdjustmentRedeemCode(operationID) {
				return operation, errWalletAdjustmentState
			}
			if !operation.AdjustmentAttempted {
				operation.AdjustmentAttempted, operation.Phase = true, "authoritative_readback"
				if err := app.persistWalletAdjustment(ctx, operationID, &operation); err != nil {
					return operation, errWalletAdjustmentState
				}
				var err error
				if operation.Kind == "debit" {
					_, err = service.ChargeSub2API(ctx, clients.Sub2APIChargeInput{UserID: operation.Sub2APIUserID, Code: operation.CanonicalRedeemCode, ChargeUSDMicros: operation.AmountUSDMicros, Notes: operation.Reason})
				} else {
					_, err = service.RefundSub2API(ctx, clients.Sub2APIRefundInput{UserID: operation.Sub2APIUserID, Code: operation.CanonicalRedeemCode, RefundUSDMicros: operation.AmountUSDMicros, Notes: operation.Reason})
				}
				if err != nil {
					recordWalletAdjustmentUpstreamFailure(&operation, "adjustment", err, "adjustment_unconfirmed")
				}
				if err != nil && !errors.Is(err, clients.ErrSub2APIChargeUnknown) && !errors.Is(err, clients.ErrSub2APIChargeConflict) {
					return app.manualReviewWalletAdjustment(ctx, operationID, operation, "adjustment_unconfirmed", audit)
				}
			} else {
				operation.Phase = "authoritative_readback"
			}
		case "authoritative_readback":
			if operation.RedeemCodeVersion != "v2" || operation.CanonicalRedeemCode != walletAdjustmentRedeemCode(operationID) {
				return operation, errWalletAdjustmentState
			}
			history, err := service.FinancialBalanceHistoryByCodes(ctx, operation.Sub2APIUserID, []string{operation.CanonicalRedeemCode})
			entry, confirmErr := confirmWalletAdjustmentHistory(history, operation.CanonicalRedeemCode, operation)
			if err != nil || confirmErr != nil {
				recordWalletAdjustmentUpstreamFailure(&operation, "authoritative_readback", err, "balance_history_unavailable")
				return app.manualReviewWalletAdjustment(ctx, operationID, operation, "authoritative_readback_unavailable", audit)
			}
			operation.BalanceHistoryUsedAt = entry.UsedAt.UTC().Format(time.RFC3339Nano)
			operation.BalanceHistoryRef = walletAdjustmentBalanceHistoryRef(operation.Sub2APIUserID, entry)
			operation.Phase = "after_balance"
			if err := app.persistWalletAdjustment(ctx, operationID, &operation); err != nil {
				return operation, errWalletAdjustmentState
			}
		case "after_balance":
			balance, err := service.Sub2APIBalance(ctx, operation.Sub2APIUserID)
			// This is an optional current-wallet display, not proof of the confirmed transaction.
			if err == nil && balance.UserID == operation.Sub2APIUserID && balance.Status == "active" && balance.USDMicros >= 0 {
				operation.AfterBalanceKnown, operation.AfterBalanceMicros = true, balance.USDMicros
				operation.AfterBalanceReadAt = time.Now().UTC().Format(time.RFC3339Nano)
			}
			operation.Phase = "ledger"
			if err := app.persistWalletAdjustment(ctx, operationID, &operation); err != nil {
				return operation, errWalletAdjustmentState
			}
		case "ledger":
			receipt, err := service.RecordMonthlyReceipt(ctx, walletAdjustmentReceipt(operationID, operation), operationID+":ledger")
			if err != nil || receipt.ReceiptID == "" {
				return operation, errWalletAdjustmentUpstream
			}
			operation.ReceiptID, operation.Phase = receipt.ReceiptID, "audit"
			if err := app.persistWalletAdjustment(ctx, operationID, &operation); err != nil {
				return operation, errWalletAdjustmentState
			}
		case "audit":
			if err := app.saveWalletAdjustmentAudit(ctx, operationID, operation, "succeeded", audit); err != nil {
				return operation, errWalletAdjustmentState
			}
			operation.Status, operation.Phase, operation.ErrorCode = "succeeded", "complete", ""
			if err := app.persistWalletAdjustment(ctx, operationID, &operation); err != nil {
				return operation, errWalletAdjustmentState
			}
		case "manual_review_audit":
			return app.manualReviewWalletAdjustment(ctx, operationID, operation, operation.ErrorCode, audit)
		case "complete":
			return operation, nil
		default:
			return operation, errWalletAdjustmentState
		}
	}
	return operation, errWalletAdjustmentState
}

func confirmWalletAdjustmentHistory(history map[string]clients.Sub2APIBalanceHistoryEntry, code string, operation walletAdjustmentOperation) (clients.Sub2APIBalanceHistoryEntry, error) {
	entry, state, err := inspectWalletAdjustmentHistory(history, code, operation)
	if err != nil || state != "confirmed" {
		return clients.Sub2APIBalanceHistoryEntry{}, errWalletAdjustmentConflict
	}
	return entry, nil
}

func inspectWalletAdjustmentHistory(history map[string]clients.Sub2APIBalanceHistoryEntry, code string, operation walletAdjustmentOperation) (clients.Sub2APIBalanceHistoryEntry, string, error) {
	signed := operation.AmountUSDMicros
	if operation.Kind == "debit" {
		signed = -signed
	}
	match, found := history[code]
	if !found {
		return clients.Sub2APIBalanceHistoryEntry{}, "absent", nil
	}
	if match.Code != code || match.Type != "balance" || match.Status != "used" || match.UsedBy == nil || *match.UsedBy != operation.Sub2APIUserID || match.UsedAt == nil || match.UsedAt.IsZero() || match.ValueUSDMicros != signed {
		return clients.Sub2APIBalanceHistoryEntry{}, "conflict", errWalletAdjustmentConflict
	}
	return match, "confirmed", nil
}

func walletAdjustmentBalanceHistoryRef(userID int64, entry clients.Sub2APIBalanceHistoryEntry) string {
	return "sub2api:balance-history:" + strconv.FormatInt(userID, 10) + ":" + stableID(entry.Code, entry.CreatedAt.Format(time.RFC3339Nano))[:18]
}

func recordWalletAdjustmentUpstreamFailure(operation *walletAdjustmentOperation, phase string, err error, fallbackCode string) {
	details, ok := clients.Sub2APIFailure(err)
	if !ok && operation.UpstreamFailure != nil {
		return
	}
	if !ok {
		details.ErrorCode = fallbackCode
	}
	if details.ErrorCode == "" {
		details.ErrorCode = fallbackCode
	}
	operation.UpstreamFailure = &walletAdjustmentUpstreamFailure{
		Phase: phase, HTTPStatus: details.HTTPStatus, ErrorCode: details.ErrorCode, RequestID: details.RequestID,
	}
}

func (app *controlPlaneServer) manualReviewWalletAdjustment(ctx context.Context, operationID string, operation walletAdjustmentOperation, errorCode string, audit walletAdjustmentAuditIdentity) (walletAdjustmentOperation, error) {
	if operation.Phase != "manual_review_audit" {
		operation.Status, operation.Phase, operation.ErrorCode = "pending", "manual_review_audit", errorCode
		if err := app.persistWalletAdjustment(ctx, operationID, &operation); err != nil {
			return operation, errWalletAdjustmentState
		}
	}
	if err := app.saveWalletAdjustmentAudit(ctx, operationID, operation, "manual_review", audit); err != nil {
		return operation, errWalletAdjustmentState
	}
	operation.Status, operation.Phase = "manual_review", "authoritative_readback"
	if err := app.persistWalletAdjustment(ctx, operationID, &operation); err != nil {
		return operation, errWalletAdjustmentState
	}
	return operation, nil
}

func walletAdjustmentReceipt(operationID string, operation walletAdjustmentOperation) clients.ReceiptInput {
	inputRefs := map[string]any{"balanceHistoryRef": operation.BalanceHistoryRef}
	if operation.RelatedOperationID != "" {
		inputRefs["relatedOperationId"] = operation.RelatedOperationID
	}
	return clients.ReceiptInput{
		Type: "gateway.wallet_adjustment.v1", Status: "completed", Surface: "control_plane", AccountID: operation.AccountID,
		RequestID: operationID, Actor: map[string]any{"userId": operation.ActorUserID},
		Execution: map[string]any{"operationId": operationID, "kind": operation.Kind, "amountUsdMicros": operation.AmountUSDMicros},
		InputRefs: inputRefs, Owner: map[string]any{"accountId": operation.AccountID},
	}
}

func (app *controlPlaneServer) saveWalletAdjustmentAudit(ctx context.Context, operationID string, operation walletAdjustmentOperation, result string, identity walletAdjustmentAuditIdentity) error {
	before := map[string]any{"balance": walletBalanceEnvelope(operation.BeforeBalanceKnown, operation.BeforeBalanceMicros, operation.BeforeBalanceReadAt)}
	after := map[string]any{
		"kind": operation.Kind, "amountUsd": operation.AmountUSD, "reason": operation.Reason, "status": result,
		"balance": walletBalanceEnvelope(operation.AfterBalanceKnown, operation.AfterBalanceMicros, operation.AfterBalanceReadAt),
	}
	if operation.RelatedOperationID != "" {
		after["relatedOperationId"] = operation.RelatedOperationID
	}
	if operation.BalanceHistoryRef != "" {
		after["balanceHistoryRef"] = operation.BalanceHistoryRef
	}
	return app.tables.SaveAuditEvent(ctx, map[string]any{
		"id": "audit-" + stableID("gateway.wallet_adjustment", operationID)[:12], "createdAt": operation.UpdatedAt,
		"actorUserId": identity.Actor.UserID, "actorRole": identity.Actor.Role, "actorAccountId": identity.Actor.AccountID,
		"targetAccountId": operation.AccountID, "action": "gateway.wallet_adjustment", "resourceKind": "gateway_wallet", "resourceId": operation.AccountID,
		"ipAddress": identity.IPAddress, "userAgent": identity.UserAgent, "before": before, "after": after, "result": result,
	})
}

// walletRefundChargeFacts reads the financial facts retained by the purchase owner.
// It intentionally does not infer a charge from the current Workspace or wallet.
type walletRefundChargeFacts struct {
	AccountID            string              `json:"accountId"`
	Sub2APIUserID        int64               `json:"sub2apiUserId"`
	RedeemCode           string              `json:"sub2apiRedeemCode"`
	TotalChargeUSDMicros int64               `json:"totalChargeUsdMicros"`
	TotalUSDMicros       int64               `json:"totalUsdMicros"`
	Phase                string              `json:"phase"`
	ChargeConfirmation   *walletRefundCharge `json:"chargeConfirmation"`
}

type walletRefundCharge struct {
	Code            string `json:"code"`
	UserID          int64  `json:"userId"`
	AmountUSDMicros int64  `json:"chargeUsdMicros"`
	Status          string `json:"status"`
}

func refundableWalletOperationCharge(row map[string]any) (walletRefundCharge, error) {
	var facts walletRefundChargeFacts
	if json.Unmarshal([]byte(stringValue(row["result"])), &facts) != nil || facts.ChargeConfirmation == nil {
		return walletRefundCharge{}, errWalletAdjustmentConflict
	}
	var amount int64
	switch stringValue(row["action"]) {
	case "workspace.launch", "workspace.launch.v2":
		if stringValue(row["status"]) != "succeeded" {
			operation, err := decodeWorkspaceLaunchReconcileOperation(row)
			if err != nil || !workspaceLaunchRefundAuthorized(operation) || !validSettlementPeriod(operation.stringFact("periodStart"), operation.stringFact("paidThrough")) {
				return walletRefundCharge{}, errWalletAdjustmentConflict
			}
		}
		amount = facts.TotalChargeUSDMicros
	case "workspace.renewal":
		if stringValue(row["status"]) != "active" || facts.Phase != "complete" {
			return walletRefundCharge{}, errWalletAdjustmentConflict
		}
		amount = facts.TotalUSDMicros
	default:
		return walletRefundCharge{}, errWalletAdjustmentConflict
	}
	charge := *facts.ChargeConfirmation
	if facts.AccountID == "" || facts.AccountID != stringValue(row["accountId"]) || amount <= 0 ||
		charge.AmountUSDMicros != amount || charge.Code == "" || charge.Code != facts.RedeemCode || charge.UserID <= 0 || charge.Status != "used" ||
		(isWorkspaceLaunchAction(stringValue(row["action"])) && charge.UserID != facts.Sub2APIUserID) {
		return walletRefundCharge{}, errWalletAdjustmentConflict
	}
	return charge, nil
}

func validateWalletRefundReservation(original map[string]any, rows []map[string]any, operation walletAdjustmentOperation) error {
	charge, err := refundableWalletOperationCharge(original)
	if err != nil || stringValue(original["id"]) != operation.RelatedOperationID ||
		stringValue(original["accountId"]) != operation.AccountID || charge.UserID != operation.Sub2APIUserID {
		return errWalletAdjustmentConflict
	}
	remaining, err := walletRefundRemaining(charge.AmountUSDMicros, operation.RelatedOperationID, rows)
	if err != nil {
		return err
	}
	if operation.AmountUSDMicros <= 0 || operation.AmountUSDMicros > remaining {
		return errWalletAdjustmentConflict
	}
	return nil
}

func walletRefundRemaining(charged int64, originalID string, rows []map[string]any) (int64, error) {
	remaining := charged
	for _, row := range rows {
		existing, decodeErr := decodeWalletAdjustment(row)
		if decodeErr != nil {
			return 0, errWalletAdjustmentState
		}
		if existing.Kind != "business_refund" || existing.RelatedOperationID != originalID {
			continue
		}
		// Only a proven rejection before any dispatch releases its reservation.
		if existing.Status == "failed" && !existing.AdjustmentAttempted {
			continue
		}
		if existing.AmountUSDMicros > remaining {
			return 0, errWalletAdjustmentConflict
		}
		remaining -= existing.AmountUSDMicros
	}
	return remaining, nil
}

type walletRefundOperation struct {
	ID        string
	Operation walletAdjustmentOperation
}

func workspaceLaunchRefundOperationID(operationID string) string {
	return "wallet-adjustment-closeout-" + stableID(operationID)[:24]
}

func workspaceLaunchRefundAuthorized(operation workspaceLaunchReconcileOperation) bool {
	closeout := operation.Closeout
	return closeout != nil && closeout.AuthorizationID != "" && closeout.AuthorizedBy != "" &&
		(closeout.Phase == "refund" || closeout.Phase == "receipt" || closeout.Phase == "complete") &&
		closeout.FrozenAt != "" && closeout.ResourcesAbsentAt != "" && closeout.DebitState == "confirmed"
}

// Called while the original Launch is locked. Unresolved manual refunds retain
// their reservation and must settle before the closing remainder is fixed.
func prepareWorkspaceLaunchCloseoutRefund(original map[string]any, rows []map[string]any, requested workspaceLaunchReconcileOperation) ([]walletRefundOperation, *walletRefundOperation, error) {
	operation, err := decodeWorkspaceLaunchReconcileOperation(original)
	if err != nil || operation.ID != requested.ID || operation.PersistedResult != requested.PersistedResult || !workspaceLaunchRefundAuthorized(operation) {
		return nil, nil, errWalletAdjustmentConflict
	}
	charge, err := refundableWalletOperationCharge(original)
	if err != nil {
		return nil, nil, err
	}
	remaining, err := walletRefundRemaining(charge.AmountUSDMicros, operation.ID, rows)
	if err != nil {
		return nil, nil, err
	}
	refunds := make([]walletRefundOperation, 0, len(rows)+1)
	refundID := workspaceLaunchRefundOperationID(operation.ID)
	if operation.Closeout.RefundOperationID != "" && operation.Closeout.RefundOperationID != refundID {
		return nil, nil, errWalletAdjustmentConflict
	}
	requestHash := stableID("workspace-launch-closeout-refund-v1", operation.ID, operation.Closeout.AuthorizationID, operation.Closeout.AuthorizedBy)
	existing, unresolved := false, false
	for _, row := range rows {
		refund, decodeErr := decodeWalletAdjustment(row)
		if decodeErr != nil {
			return nil, nil, decodeErr
		}
		if refund.Kind != "business_refund" || refund.RelatedOperationID != operation.ID {
			continue
		}
		id := stringValue(row["id"])
		if id == refundID {
			if refund.RequestHash != requestHash {
				return nil, nil, errWalletAdjustmentConflict
			}
			existing = true
		}
		refunds = append(refunds, walletRefundOperation{ID: id, Operation: refund})
		if refund.Status == "failed" && !refund.AdjustmentAttempted {
			continue
		}
		if refund.AccountID != operation.stringFact("accountId") || refund.Sub2APIUserID != charge.UserID {
			return nil, nil, errWalletAdjustmentConflict
		}
		if refund.Status != "succeeded" || refund.ReceiptID == "" || refund.BalanceHistoryRef == "" {
			unresolved = true
		}
	}
	if existing || unresolved || remaining == 0 {
		return refunds, nil, nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	refund := walletRefundOperation{ID: refundID, Operation: walletAdjustmentOperation{
		RequestHash: requestHash, Phase: "before_balance", AccountID: operation.stringFact("accountId"), Sub2APIUserID: charge.UserID,
		Kind: "business_refund", AmountUSDMicros: remaining, AmountUSD: formatWalletUSD(remaining), Reason: operation.Closeout.Reason,
		RelatedOperationID: operation.ID, ActorUserID: operation.Closeout.AuthorizedBy,
		CanonicalRedeemCode: walletAdjustmentRedeemCode(refundID), RedeemCodeVersion: "v2", CreatedAt: now, UpdatedAt: now, Status: "pending",
	}}
	refunds = append(refunds, refund)
	return refunds, &refund, nil
}

func (app *controlPlaneServer) confirmWalletRefundSource(ctx context.Context, service *controlplane.Service, operation walletAdjustmentOperation) error {
	row, found, err := app.tables.GetRuntimeOperation(ctx, operation.RelatedOperationID)
	if err != nil {
		return errWalletAdjustmentState
	}
	if !found || stringValue(row["accountId"]) != operation.AccountID {
		return errWalletAdjustmentConflict
	}
	charge, err := refundableWalletOperationCharge(row)
	if err != nil || charge.UserID != operation.Sub2APIUserID {
		return errWalletAdjustmentConflict
	}
	history, err := service.FinancialBalanceHistoryByCodes(ctx, charge.UserID, []string{charge.Code})
	if err != nil {
		return errWalletAdjustmentUpstream
	}
	_, err = confirmWalletAdjustmentHistory(history, charge.Code, walletAdjustmentOperation{Kind: "debit", Sub2APIUserID: charge.UserID, AmountUSDMicros: charge.AmountUSDMicros})
	return err
}

func (app *controlPlaneServer) refundWorkspaceLaunchCloseout(ctx context.Context, service *controlplane.Service, operation workspaceLaunchReconcileOperation) (string, int64, bool, error) {
	if !workspaceLaunchRefundAuthorized(operation) {
		return "", 0, false, errWalletAdjustmentConflict
	}
	original, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		return "", 0, false, err
	}
	charge, err := refundableWalletOperationCharge(original)
	if err != nil {
		return "", 0, false, err
	}
	history, err := service.FinancialBalanceHistoryByCodes(ctx, charge.UserID, []string{charge.Code})
	if err != nil {
		return "", 0, false, err
	}
	if _, err := confirmWalletAdjustmentHistory(history, charge.Code, walletAdjustmentOperation{Kind: "debit", Sub2APIUserID: charge.UserID, AmountUSDMicros: charge.AmountUSDMicros}); err != nil {
		return "", 0, false, err
	}
	refunds, err := app.tables.ReserveWorkspaceLaunchCloseoutRefund(ctx, operation)
	if err != nil {
		return "", 0, false, err
	}
	refundID := workspaceLaunchRefundOperationID(operation.ID)
	var own *walletRefundOperation
	var confirmed int64
	priorComplete := true
	for _, refund := range refunds {
		if refund.Operation.Status == "failed" && !refund.Operation.AdjustmentAttempted {
			continue
		}
		if refund.ID == refundID {
			copy := refund
			own = &copy
			continue
		}
		// Only finish an already-dispatched manual payment. A pending manual
		// reservation is never authority for this worker to send another payment.
		if refund.Operation.Status != "succeeded" && refund.Operation.AdjustmentAttempted && refund.Operation.RedeemCodeVersion == "v2" {
			audit := walletAdjustmentAuditIdentity{Actor: auditActor{UserID: operation.Closeout.AuthorizedBy, Role: "operator"}}
			refund.Operation, err = app.runWalletAdjustment(ctx, service, refund.ID, refund.Operation, audit)
			if err != nil {
				return "", confirmed, false, err
			}
		}
		ok, err := confirmWorkspaceLaunchRefund(ctx, service, refund, operation.stringFact("accountId"), charge.UserID)
		if err != nil {
			return "", confirmed, false, err
		}
		if !ok {
			priorComplete = false
			continue
		}
		if refund.Operation.AmountUSDMicros > charge.AmountUSDMicros-confirmed {
			return "", confirmed, false, errWalletAdjustmentConflict
		}
		confirmed += refund.Operation.AmountUSDMicros
	}
	if !priorComplete {
		return "", confirmed, false, nil
	}
	if own != nil {
		if own.Operation.Status != "succeeded" {
			audit := walletAdjustmentAuditIdentity{Actor: auditActor{UserID: operation.Closeout.AuthorizedBy, Role: "operator"}}
			own.Operation, err = app.runWalletAdjustment(ctx, service, own.ID, own.Operation, audit)
			if err != nil {
				return refundID, confirmed, false, err
			}
		}
		ok, err := confirmWorkspaceLaunchRefund(ctx, service, *own, operation.stringFact("accountId"), charge.UserID)
		if err != nil || !ok {
			return refundID, confirmed, false, err
		}
		if own.Operation.AmountUSDMicros > charge.AmountUSDMicros-confirmed {
			return refundID, confirmed, false, errWalletAdjustmentConflict
		}
		confirmed += own.Operation.AmountUSDMicros
		return refundID, confirmed, confirmed == charge.AmountUSDMicros, nil
	}
	return "", confirmed, confirmed == charge.AmountUSDMicros, nil
}

func confirmWorkspaceLaunchRefund(ctx context.Context, service *controlplane.Service, refund walletRefundOperation, accountID string, userID int64) (bool, error) {
	operation := refund.Operation
	if operation.Kind != "business_refund" || operation.AccountID != accountID || operation.Sub2APIUserID != userID {
		return false, errWalletAdjustmentConflict
	}
	if operation.Status != "succeeded" || operation.ReceiptID == "" || operation.BalanceHistoryRef == "" {
		return false, nil
	}
	code := operation.CanonicalRedeemCode
	if operation.LegacySupersession == "legacy_history_confirmed" {
		code = legacyWalletAdjustmentRedeemCode(refund.ID)
	}
	history, err := service.FinancialBalanceHistoryByCodes(ctx, userID, []string{code})
	if err != nil {
		return false, err
	}
	entry, err := confirmWalletAdjustmentHistory(history, code, operation)
	if err != nil {
		return false, err
	}
	if operation.BalanceHistoryRef != walletAdjustmentBalanceHistoryRef(userID, entry) || operation.BalanceHistoryUsedAt != entry.UsedAt.UTC().Format(time.RFC3339Nano) {
		return false, errWalletAdjustmentConflict
	}
	receipt, err := service.BillingReceiptForAccount(ctx, accountID, "", operation.ReceiptID)
	if err != nil {
		return false, err
	}
	if receipt.ReceiptID != operation.ReceiptID || !workspaceLaunchReceiptInputMatches(receipt.ReceiptInput, walletAdjustmentReceipt(refund.ID, operation)) {
		return false, errWalletAdjustmentConflict
	}
	return true, nil
}

func walletAdjustmentDTO(operationID string, operation walletAdjustmentOperation) map[string]any {
	beforeReadAt, afterReadAt := operation.BeforeBalanceReadAt, operation.AfterBalanceReadAt
	if beforeReadAt == "" {
		beforeReadAt = operation.UpdatedAt
	}
	if afterReadAt == "" {
		afterReadAt = operation.UpdatedAt
	}
	result := map[string]any{
		"operationId": operationID, "status": operation.Status, "phase": operation.Phase, "accountId": operation.AccountID,
		"kind": operation.Kind, "amountUsd": operation.AmountUSD, "reason": operation.Reason,
		"beforeBalance": walletBalanceEnvelope(operation.BeforeBalanceKnown, operation.BeforeBalanceMicros, beforeReadAt),
		"afterBalance":  walletBalanceEnvelope(operation.AfterBalanceKnown, operation.AfterBalanceMicros, afterReadAt),
		"actor":         operation.ActorUserID, "createdAt": operation.CreatedAt, "updatedAt": operation.UpdatedAt,
	}
	for key, value := range map[string]string{
		"relatedOperationId": operation.RelatedOperationID, "balanceHistoryRef": operation.BalanceHistoryRef,
		"receiptId": operation.ReceiptID, "errorCode": operation.ErrorCode,
	} {
		if value != "" {
			result[key] = value
		}
	}
	if operation.UpstreamFailure != nil {
		result["upstreamFailure"] = operation.UpstreamFailure
	}
	if operation.Status == "manual_review" {
		result["allowedActions"] = []string{"recover_wallet_adjustment"}
	}
	return result
}

func walletBalanceEnvelope(known bool, micros int64, fetchedAt string) map[string]any {
	result := map[string]any{"source": "sub2api", "status": "unavailable", "available": false, "fetchedAt": fetchedAt}
	if known {
		result["status"], result["available"] = "available", true
		result["data"] = map[string]any{"currency": "USD", "usdMicros": strconv.FormatInt(micros, 10)}
	}
	return result
}

func walletAdjustmentRedeemCode(operationID string) string {
	return "opl:" + stableID("sub2api-wallet-adjustment-v2", operationID)[:28]
}

func legacyWalletAdjustmentRedeemCode(operationID string) string {
	return "opl:wallet-adjustment:" + stableID(operationID)[:24] + ":v1"
}

func formatWalletUSD(micros int64) string {
	whole, fraction := micros/1_000_000, micros%1_000_000
	decimal := strings.TrimRight(fmt.Sprintf("%06d", fraction), "0")
	if len(decimal) < 2 {
		decimal += strings.Repeat("0", 2-len(decimal))
	}
	return strconv.FormatInt(whole, 10) + "." + decimal
}

func writeWalletAdjustmentError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errIdempotencyConflict), errors.Is(err, errWalletAdjustmentConflict):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, errWalletAdjustmentAccount):
		writeError(w, http.StatusNotFound, "wallet_adjustment_account_not_found")
	case errors.Is(err, errWalletAdjustmentState):
		writeError(w, http.StatusInternalServerError, "state_persist_failed")
	default:
		writeError(w, http.StatusBadGateway, errWalletAdjustmentUpstream.Error())
	}
}
