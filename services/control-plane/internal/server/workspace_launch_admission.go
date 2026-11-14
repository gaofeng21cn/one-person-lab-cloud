package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	contracts "opl-cloud/packages/contracts/go"
)

const (
	controlledBasicPilotEnabledEnv     = "OPL_CONTROLLED_BASIC_PILOT_ENABLED"
	controlledBasicPilotMaxInFlightEnv = "OPL_CONTROLLED_BASIC_PILOT_MAX_IN_FLIGHT"
	controlledBasicPilotDefaultLimit   = 50
)

const workspaceLaunchOperationDiagnosticLimit = 20

var productionAcceptanceBApprovalIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,199}$`)
var productionAcceptanceBReleaseSHAPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)
var productionAcceptanceBIdentityDigestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type workspaceLaunchAcceptanceBIdentityDigestSet struct {
	AccountIdentitySHA256   string `json:"accountIdentitySha256"`
	OperationIdentitySHA256 string `json:"operationIdentitySha256"`
	WorkspaceIdentitySHA256 string `json:"workspaceIdentitySha256"`
	KeyIdentitySHA256       string `json:"keyIdentitySha256"`
	DebitIdentitySHA256     string `json:"debitIdentitySha256"`
	QuoteIdentitySHA256     string `json:"quoteIdentitySha256"`
	ProviderIdentitySHA256  string `json:"providerIdentitySha256"`
}

type workspaceLaunchAcceptanceBResumeExistingBinding struct {
	SchemaVersion            int                                         `json:"schemaVersion"`
	ApprovalID               string                                      `json:"approvalId"`
	ApprovalSHA256           string                                      `json:"approvalSha256"`
	CanonicalCloudSHA        string                                      `json:"canonicalCloudSha"`
	CanonicalCloudTree       string                                      `json:"canonicalCloudTree"`
	DeployedCloudImageDigest string                                      `json:"deployedCloudImageDigest"`
	AuthoritativeState       string                                      `json:"authoritativeState"`
	IdentityDigests          workspaceLaunchAcceptanceBIdentityDigestSet `json:"identityDigests"`
}

type controlledBasicPilotAdmission struct {
	Enabled     bool
	Configured  bool
	MaxInFlight int
}

func controlledBasicPilotAdmissionFromEnv() controlledBasicPilotAdmission {
	admission := controlledBasicPilotAdmission{
		MaxInFlight: controlledBasicPilotDefaultLimit,
	}
	valid := true
	switch strings.TrimSpace(os.Getenv(controlledBasicPilotEnabledEnv)) {
	case "", "0":
	case "1":
		admission.Enabled = true
	default:
		valid = false
	}
	if raw := strings.TrimSpace(os.Getenv(controlledBasicPilotMaxInFlightEnv)); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit <= 0 {
			valid = false
		} else {
			admission.MaxInFlight = limit
		}
	}
	admission.Configured = valid
	return admission
}

func workspacePurchaseEnabled(account map[string]any) bool {
	return boolValue(account["workspacePurchaseEnabled"])
}

func validWorkspaceLaunchAcceptanceBIdentityDigests(value workspaceLaunchAcceptanceBIdentityDigestSet) bool {
	return productionAcceptanceBIdentityDigestPattern.MatchString(value.AccountIdentitySHA256) &&
		productionAcceptanceBIdentityDigestPattern.MatchString(value.OperationIdentitySHA256) &&
		productionAcceptanceBIdentityDigestPattern.MatchString(value.WorkspaceIdentitySHA256) &&
		productionAcceptanceBIdentityDigestPattern.MatchString(value.KeyIdentitySHA256) &&
		productionAcceptanceBIdentityDigestPattern.MatchString(value.DebitIdentitySHA256) &&
		productionAcceptanceBIdentityDigestPattern.MatchString(value.QuoteIdentitySHA256) &&
		productionAcceptanceBIdentityDigestPattern.MatchString(value.ProviderIdentitySHA256)
}

func controlledBasicPilotMetrics(ctx context.Context, store controlPlaneTableStore) (map[string]any, error) {
	admission := controlledBasicPilotAdmissionFromEnv()
	rows, err := queryRuntimeOperations(ctx, store, runtimeOperationQuery{})
	if err != nil {
		return nil, err
	}
	stages, failures := map[string]int{}, map[string]int{}
	inFlight, manualReview := 0, 0
	for _, row := range rows {
		action := stringValue(row["action"])
		if !isWorkspaceLaunchAction(action) {
			continue
		}
		if action == "workspace.launch" {
			if !terminalWorkspaceLaunchStatus(stringValue(row["status"])) {
				inFlight++
				stages["legacy_operation"]++
			}
			continue
		}
		operation, decodeErr := decodeWorkspaceLaunchReconcileOperation(row)
		if decodeErr != nil {
			failures["operation_decode_failed"]++
			continue
		}
		if !terminalWorkspaceLaunchStatus(string(operation.Status)) {
			inFlight++
			stages[safeControlledPilotMetricCode(string(operation.Stage))]++
			if operation.Status == contracts.StatusManualReview {
				manualReview++
			}
		}
	}
	availableCapacity := admission.MaxInFlight - inFlight
	if availableCapacity < 0 {
		availableCapacity = 0
	}
	alerts := make([]any, 0, 3)
	if !admission.Configured {
		alerts = append(alerts, map[string]any{"code": "controlled_pilot_config_invalid", "severity": "error", "action": "disable_new_purchases"})
	}
	if len(failures) > 0 || manualReview > 0 {
		alerts = append(alerts, map[string]any{"code": "controlled_pilot_first_failure", "severity": "error", "action": "disable_new_purchases"})
	}
	if inFlight >= admission.MaxInFlight {
		alerts = append(alerts, map[string]any{"code": "controlled_pilot_capacity_exhausted", "severity": "warning", "action": "wait_for_authoritative_terminal_state"})
	}
	sort.Slice(alerts, func(i, j int) bool {
		return stringValue(alerts[i].(map[string]any)["code"]) < stringValue(alerts[j].(map[string]any)["code"])
	})
	return map[string]any{
		"enabled": admission.Enabled, "configured": admission.Configured, "accountEligibilityAuthority": "control-plane-account",
		"maxInFlight": admission.MaxInFlight, "inFlight": inFlight, "availableCapacity": availableCapacity, "manualReview": manualReview,
		"stages": stages, "failures": failures, "disableRequired": len(failures) > 0 || manualReview > 0 || !admission.Configured,
		"alerts": alerts,
	}, nil
}

func workspaceLaunchOperationDiagnostics(ctx context.Context, store controlPlaneTableStore) (map[string]any, error) {
	rows, err := queryRuntimeOperations(ctx, store, runtimeOperationQuery{Action: workspaceLaunchAction})
	if err != nil {
		return nil, err
	}
	operations := make([]any, 0)
	failedOperationCount := 0
	for _, row := range rows {
		if _, decodeErr := decodeWorkspaceLaunchReconcileOperation(row); decodeErr == nil {
			continue
		} else {
			failedOperationCount++
			if len(operations) < workspaceLaunchOperationDiagnosticLimit {
				operations = append(operations, workspaceLaunchOperationDiagnostic(row, decodeErr))
			}
		}
	}
	return map[string]any{
		"schemaVersion": 1, "failedOperationCount": failedOperationCount,
		"truncated": failedOperationCount > len(operations), "operations": operations,
	}, nil
}

func workspaceLaunchOperationDiagnostic(row map[string]any, decodeErr error) map[string]any {
	identity := firstNonEmpty(stringValue(row["operationId"]), stringValue(row["id"]))
	digest := sha256.Sum256([]byte(identity))
	diagnostic := map[string]any{
		"operationIdentityDigest": fmt.Sprintf("sha256:%x", digest),
		"action":                  workspaceLaunchAction,
		"failureCategory":         workspaceLaunchDecodeFailureCategory(decodeErr),
		"attemptsKeys":            []string{},
		"attemptsKeyCount":        0,
		"attemptsSummary":         map[string]int{},
		"observationsKeys":        []string{},
		"observationsKeyCount":    0,
		"observationsSummary":     map[string]int{},
		"missingCanonicalKeys":    []string{},
		"forbiddenLegacyKeys":     []string{},
	}
	if status := stringValue(row["status"]); validWorkspaceLaunchDiagnosticStatus(status) {
		diagnostic["status"] = status
	}
	for _, field := range []string{"createdAt", "updatedAt"} {
		if value := stringValue(row[field]); validWorkspaceLaunchDiagnosticTimestamp(value) {
			diagnostic[field] = value
		}
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal([]byte(stringValue(row["result"])), &raw) != nil || raw == nil {
		return diagnostic
	}
	for _, field := range []string{"schemaVersion", "version"} {
		var value int
		if json.Unmarshal(raw[field], &value) == nil {
			diagnostic[field] = value
		}
	}
	var stage contracts.Stage
	if json.Unmarshal(raw["stage"], &stage) == nil && workspaceLaunchReconcileStageValid(stage) {
		diagnostic["stage"] = string(stage)
	}
	diagnostic["attemptsKeys"], diagnostic["attemptsKeyCount"], diagnostic["attemptsSummary"] = workspaceLaunchDiagnosticObjectSummary(
		raw["attempts"], "status", func(value string) bool { return workspaceLaunchReconcileStageValid(contracts.Stage(value)) }, validWorkspaceLaunchDiagnosticAttemptStatus,
	)
	diagnostic["observationsKeys"], diagnostic["observationsKeyCount"], diagnostic["observationsSummary"] = workspaceLaunchDiagnosticObjectSummary(
		raw["observations"], "state", func(value string) bool { return workspaceLaunchReconcileStageValid(contracts.Stage(value)) }, validWorkspaceLaunchDiagnosticObservationState,
	)
	diagnostic["missingCanonicalKeys"] = workspaceLaunchMissingCanonicalKeys(raw)
	forbidden := make([]string, 0)
	for _, field := range workspaceLaunchReconcileForbiddenFields {
		if _, exists := raw[field]; exists {
			forbidden = append(forbidden, field)
		}
	}
	sort.Strings(forbidden)
	diagnostic["forbiddenLegacyKeys"] = forbidden
	return diagnostic
}

func workspaceLaunchDiagnosticObjectSummary(encoded json.RawMessage, summaryField string, validKey, validCategory func(string) bool) ([]string, int, map[string]int) {
	var values map[string]json.RawMessage
	if len(encoded) == 0 || json.Unmarshal(encoded, &values) != nil || values == nil {
		return []string{}, 0, map[string]int{}
	}
	keys := make([]string, 0, len(values))
	summary := map[string]int{}
	for key, value := range values {
		if validKey(key) {
			keys = append(keys, key)
		}
		var item map[string]json.RawMessage
		var category string
		if json.Unmarshal(value, &item) == nil && json.Unmarshal(item[summaryField], &category) == nil && validCategory(category) {
			summary[category]++
		}
	}
	sort.Strings(keys)
	return keys, len(values), summary
}

func validWorkspaceLaunchDiagnosticTimestamp(value string) bool {
	_, err := time.Parse(time.RFC3339Nano, value)
	return err == nil
}

func validWorkspaceLaunchDiagnosticStatus(value string) bool {
	switch value {
	case "pending", "manual_review", "succeeded", "failed", "refunded",
		"running", "debit_pending", "provisioning", "completed":
		return true
	default:
		return false
	}
}

func validWorkspaceLaunchDiagnosticAttemptStatus(value string) bool {
	return value == "" || value == "reserved" || value == "confirmed" || value == "unknown"
}

func validWorkspaceLaunchDiagnosticObservationState(value string) bool {
	state := contracts.StageState(value)
	return state == workspaceLaunchStageAbsent || state == workspaceLaunchStagePending || state == workspaceLaunchStageReady || state == workspaceLaunchStageUnknown
}

func workspaceLaunchMissingCanonicalKeys(raw map[string]json.RawMessage) []string {
	required := []string{
		"schemaVersion", "version", "stage", "attempts", "requestHash", "accountId", "ownerUserId", "sub2apiUserId", "workspaceKeyGroupId",
		"workspaceId", "name", "packageId", "sizeGb", "priceVersion", "totalChargeUsdMicros", "providerProfileRef", "preflightBindingRef",
		"specDigest", "workspaceImageDigest", "sub2apiRedeemCode",
	}
	missing := make([]string, 0)
	for _, field := range required {
		if value, exists := raw[field]; !exists || len(value) == 0 || string(value) == "null" || string(value) == `""` {
			missing = append(missing, field)
		}
	}
	return missing
}

func safeControlledPilotMetricCode(value string) string {
	if value == "" {
		return "unknown"
	}
	for _, char := range value {
		if char != '_' && char != '-' && (char < 'a' || char > 'z') && (char < '0' || char > '9') {
			return "unknown"
		}
	}
	return value
}
