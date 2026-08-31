package fabric

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"regexp"
	"strings"
	"time"
)

const WorkspaceLaunchFabricSchemaVersion = 1

const workspaceLaunchPreflightPayloadKey = "workspaceLaunchPreflight"
const workspaceLaunchStageRecordPayloadKey = "workspaceLaunchStageRecord"
const workspaceLaunchStageDiagnosticPayloadKey = "workspaceLaunchStageDiagnostic"
const workspaceLaunchStageRecordSchemaVersion = 2

var workspaceLaunchStageDiagnosticFieldPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._-]{0,127}$`)

var (
	ErrWorkspaceLaunchInputInvalid                 = errors.New("workspace_launch_input_invalid")
	ErrWorkspaceLaunchUnavailable                  = errors.New("workspace_launch_unavailable")
	ErrWorkspaceLaunchPending                      = errors.New("workspace_launch_pending")
	ErrWorkspaceLaunchOwnershipPending             = errors.New("workspace_launch_ownership_pending")
	ErrWorkspaceLaunchRuntimeImageRevisionRequired = errors.New("workspace_launch_runtime_image_revision_required")
	ErrWorkspaceLaunchResourceAbsent               = errors.New("workspace_launch_resource_absent")
)

type WorkspaceLaunchPreflightInput struct {
	SchemaVersion        int    `json:"schemaVersion"`
	LaunchOperationID    string `json:"launchOperationId"`
	AccountID            string `json:"accountId"`
	WorkspaceID          string `json:"workspaceId"`
	PackageID            string `json:"packageId"`
	SizeGB               int    `json:"sizeGb"`
	WorkspaceImageDigest string `json:"workspaceImageDigest"`
	RequestHash          string `json:"requestHash"`
}

type WorkspaceLaunchPreflight struct {
	SchemaVersion      int    `json:"schemaVersion"`
	Available          bool   `json:"available"`
	Reason             string `json:"reason"`
	LaunchOperationID  string `json:"launchOperationId"`
	RequestHash        string `json:"requestHash"`
	ProviderProfileRef string `json:"providerProfileRef"`
	ProviderBindingRef string `json:"providerBindingRef"`
	SpecDigest         string `json:"specDigest"`
}

type WorkspaceLaunchPreflightReadInput struct {
	ProviderBindingRef string `json:"providerBindingRef"`
}

// WorkspaceLaunchPreflightBinding is the narrow, owner-authoritative identity
// projection used to verify a persisted provider plan without exposing it.
type WorkspaceLaunchPreflightBinding struct {
	SchemaVersion        int    `json:"schemaVersion"`
	LaunchOperationID    string `json:"launchOperationId"`
	AccountID            string `json:"accountId"`
	WorkspaceID          string `json:"workspaceId"`
	PackageID            string `json:"packageId"`
	SizeGB               int    `json:"sizeGb"`
	WorkspaceImageDigest string `json:"workspaceImageDigest"`
	RequestHash          string `json:"requestHash"`
	ProviderProfileRef   string `json:"providerProfileRef"`
	ProviderBindingRef   string `json:"providerBindingRef"`
	SpecDigest           string `json:"specDigest"`
}

type workspaceLaunchPreflightAdmission struct {
	SchemaVersion         int                           `json:"schemaVersion"`
	Input                 WorkspaceLaunchPreflightInput `json:"input"`
	ProviderProfileRef    string                        `json:"providerProfileRef"`
	ProviderBindingRef    string                        `json:"providerBindingRef"`
	CanonicalProviderPlan json.RawMessage               `json:"canonicalProviderPlan"`
	SpecDigest            string                        `json:"specDigest"`
}

type WorkspaceLaunchResources struct {
	ComputeAllocationID        string `json:"computeAllocationId,omitempty"`
	ComputeBindingRef          string `json:"computeBindingRef,omitempty"`
	StorageID                  string `json:"storageId,omitempty"`
	StorageBindingRef          string `json:"storageBindingRef,omitempty"`
	AttachmentID               string `json:"attachmentId,omitempty"`
	AttachmentBindingRef       string `json:"attachmentBindingRef,omitempty"`
	GatewaySecretRef           string `json:"gatewaySecretRef,omitempty"`
	GatewaySecretVersion       string `json:"gatewaySecretVersion,omitempty"`
	GatewaySecretFingerprint   string `json:"gatewaySecretFingerprint,omitempty"`
	SecretBindingRef           string `json:"secretBindingRef,omitempty"`
	RuntimeID                  string `json:"runtimeId,omitempty"`
	RuntimeServiceName         string `json:"runtimeServiceName,omitempty"`
	RuntimeUsername            string `json:"runtimeUsername,omitempty"`
	RuntimeURL                 string `json:"runtimeUrl,omitempty"`
	RuntimeCredentialStatus    string `json:"runtimeCredentialStatus,omitempty"`
	RuntimeCredentialVersion   string `json:"runtimeCredentialVersion,omitempty"`
	RuntimeCredentialSecretRef string `json:"runtimeCredentialSecretRef,omitempty"`
	RuntimeBindingRef          string `json:"runtimeBindingRef,omitempty"`
}

type WorkspaceLaunchGatewayCredential struct {
	KeyID int64  `json:"keyId"`
	Value string `json:"value"`
}

type WorkspaceLaunchRuntimeImageRevision struct {
	SchemaVersion          int    `json:"schemaVersion"`
	LaunchOperationID      string `json:"launchOperationId"`
	WorkspaceID            string `json:"workspaceId"`
	RuntimeOperationID     string `json:"runtimeOperationId"`
	AuthorizationDigest    string `json:"authorizationDigest"`
	PreviousImageDigest    string `json:"previousImageDigest"`
	ReplacementImageDigest string `json:"replacementImageDigest"`
}

type WorkspaceLaunchStageInput struct {
	Binding              WorkspaceLaunchStageBinding          `json:"binding"`
	ProviderProfileRef   string                               `json:"providerProfileRef"`
	ProviderBindingRef   string                               `json:"providerBindingRef"`
	SpecDigest           string                               `json:"specDigest"`
	PackageID            string                               `json:"packageId"`
	SizeGB               int                                  `json:"sizeGb"`
	WorkspaceImageDigest string                               `json:"workspaceImageDigest"`
	Resources            WorkspaceLaunchResources             `json:"resources"`
	GatewayCredential    *WorkspaceLaunchGatewayCredential    `json:"gatewayCredential,omitempty"`
	RuntimeImageRevision *WorkspaceLaunchRuntimeImageRevision `json:"runtimeImageRevision,omitempty"`
}

type WorkspaceLaunchStageResult struct {
	SchemaVersion int                             `json:"schemaVersion"`
	State         string                          `json:"state"`
	Reason        string                          `json:"reason"`
	Binding       WorkspaceLaunchStageBinding     `json:"binding"`
	Resources     WorkspaceLaunchResources        `json:"resources"`
	Diagnostic    *WorkspaceLaunchStageDiagnostic `json:"diagnostic,omitempty"`
}

type WorkspaceLaunchStageDiagnostic struct {
	SchemaVersion int     `json:"schemaVersion"`
	Owner         string  `json:"owner"`
	BlockReason   string  `json:"blockReason"`
	ErrorCode     string  `json:"errorCode,omitempty"`
	Retryable     bool    `json:"retryable"`
	ObservedAt    string  `json:"observedAt"`
	Checks        []Check `json:"checks,omitempty"`
}

type persistedWorkspaceLaunchStageDiagnostic struct {
	Diagnostic WorkspaceLaunchStageDiagnostic `json:"diagnostic"`
	Digest     string                         `json:"digest"`
}

type workspaceLaunchStageRecord struct {
	SchemaVersion        int                                  `json:"schemaVersion"`
	ProviderProfileRef   string                               `json:"providerProfileRef"`
	ProviderBindingRef   string                               `json:"providerBindingRef"`
	SpecDigest           string                               `json:"specDigest"`
	RequestResources     WorkspaceLaunchResources             `json:"requestResources"`
	Resources            WorkspaceLaunchResources             `json:"resources"`
	GatewayKeyID         int64                                `json:"gatewayKeyId,omitempty"`
	RuntimeImageRevision *WorkspaceLaunchRuntimeImageRevision `json:"runtimeImageRevision,omitempty"`
	ProviderState        json.RawMessage                      `json:"providerState,omitempty"`
}

type persistedWorkspaceLaunchStageRecord struct {
	Record workspaceLaunchStageRecord `json:"record"`
	Digest string                     `json:"digest"`
}

type WorkspaceLaunchProviderRequest struct {
	Input        WorkspaceLaunchStageInput
	Current      workspaceLaunchStageRecord
	Prior        map[string]workspaceLaunchStageRecord
	ProviderPlan json.RawMessage
}

type WorkspaceLaunchProviderResult struct {
	Resources     WorkspaceLaunchResources
	ProviderState json.RawMessage
	Diagnostic    *WorkspaceLaunchStageDiagnostic
}

type workspaceLaunchProvider interface {
	EnsureWorkspaceLaunchStage(context.Context, WorkspaceLaunchProviderRequest) (WorkspaceLaunchProviderResult, error)
	ReadWorkspaceLaunchStage(context.Context, WorkspaceLaunchProviderRequest) (WorkspaceLaunchProviderResult, error)
}

type workspaceLaunchRuntimeImageRevisionProvider interface {
	WorkspaceLaunchRuntimeImageRevisionSupported() bool
}

func validWorkspaceLaunchPreflightInput(input WorkspaceLaunchPreflightInput) bool {
	if input.SchemaVersion != WorkspaceLaunchFabricSchemaVersion || input.SizeGB < 10 || input.SizeGB%10 != 0 {
		return false
	}
	for _, value := range []string{input.LaunchOperationID, input.AccountID, input.WorkspaceID, input.PackageID, input.WorkspaceImageDigest, input.RequestHash} {
		if value == "" || value != strings.TrimSpace(value) {
			return false
		}
	}
	return validWorkspaceLaunchHash(input.RequestHash)
}

func (s *Service) PreflightWorkspaceLaunch(ctx context.Context, input WorkspaceLaunchPreflightInput) (WorkspaceLaunchPreflight, error) {
	if !validWorkspaceLaunchPreflightInput(input) {
		return WorkspaceLaunchPreflight{}, ErrWorkspaceLaunchInputInvalid
	}
	providerProfileRef := s.providerDescriptor.Descriptor().Name
	rawPlan, planErr := s.workspaceLaunchPlans.ResolveWorkspacePlan(ctx, WorkspaceLaunchPlanInput{PackageID: input.PackageID, SizeGB: input.SizeGB})
	canonicalPlan, _, canonicalErr := canonicalProviderPlan(rawPlan)
	canonicalPlan, specDigest, envelopeErr := canonicalProviderPlanEnvelope(providerProfileRef, input.PackageID, canonicalPlan)
	if planErr != nil || canonicalErr != nil || envelopeErr != nil || !s.workspaceImagePolicy.ValidateWorkspaceImageReference(input.WorkspaceImageDigest) {
		return WorkspaceLaunchPreflight{
			SchemaVersion: 1, Available: false, Reason: "provider_profile_unavailable", LaunchOperationID: input.LaunchOperationID,
			RequestHash: input.RequestHash, ProviderProfileRef: providerProfileRef,
		}, nil
	}
	if _, err := s.providerReadiness.Readiness(ctx); err != nil {
		return WorkspaceLaunchPreflight{
			SchemaVersion: 1, Available: false, Reason: "provider_unavailable", LaunchOperationID: input.LaunchOperationID,
			RequestHash: input.RequestHash, ProviderProfileRef: providerProfileRef,
		}, nil
	}
	result := WorkspaceLaunchPreflight{
		SchemaVersion: 1, Available: true, Reason: "none", LaunchOperationID: input.LaunchOperationID,
		RequestHash: input.RequestHash, ProviderProfileRef: providerProfileRef, SpecDigest: specDigest,
	}
	admission := workspaceLaunchPreflightAdmission{
		SchemaVersion: 1, Input: input, ProviderProfileRef: result.ProviderProfileRef, CanonicalProviderPlan: canonicalPlan, SpecDigest: specDigest,
	}
	admission.ProviderBindingRef = workspaceLaunchPreflightBindingRef(admission)
	result.ProviderBindingRef = admission.ProviderBindingRef
	if err := s.persistWorkspaceLaunchPreflight(ctx, admission); err != nil {
		return WorkspaceLaunchPreflight{}, err
	}
	return result, nil
}

func (s *Service) persistWorkspaceLaunchPreflight(ctx context.Context, admission workspaceLaunchPreflightAdmission) error {
	now := s.now()
	operation := newOperation(
		"admit_workspace_launch", "workspace_launch_preflight", admission.Input.LaunchOperationID,
		admission.Input.AccountID, admission.Input.WorkspaceID, admission.ProviderBindingRef, hashInput(admission), now,
	)
	operation.ID, operation.OperationID = admission.ProviderBindingRef, admission.ProviderBindingRef
	operation.Provider = admission.ProviderProfileRef
	operation.Status, operation.CreatedAt, operation.FinishedAt = "succeeded", now, now
	operation.RedactedProviderPayload = map[string]any{workspaceLaunchPreflightPayloadKey: admission}
	stored, claimed, err := s.workspaceLaunchPreflights.ClaimStageOperation(ctx, operation)
	if err != nil {
		return err
	}
	if persisted, ok := decodeWorkspaceLaunchPreflight(stored); !ok || hashInput(persisted) != hashInput(admission) || (!claimed && stored.RequestHash != operation.RequestHash) {
		return ErrLaunchStageBindingConflict
	}
	return nil
}

func workspaceLaunchPreflightBindingRef(admission workspaceLaunchPreflightAdmission) string {
	admission.ProviderBindingRef = ""
	return "fabric-provider-binding:" + hashInput(admission)
}

func decodeWorkspaceLaunchPreflight(operation FabricOperation) (workspaceLaunchPreflightAdmission, bool) {
	value, ok := operation.RedactedProviderPayload[workspaceLaunchPreflightPayloadKey]
	if !ok {
		return workspaceLaunchPreflightAdmission{}, false
	}
	var admission workspaceLaunchPreflightAdmission
	body, err := json.Marshal(value)
	if err != nil || json.Unmarshal(body, &admission) != nil || admission.SchemaVersion != 1 ||
		!validWorkspaceLaunchPreflightInput(admission.Input) || admission.ProviderProfileRef == "" ||
		admission.ProviderBindingRef != workspaceLaunchPreflightBindingRef(admission) || !validWorkspaceLaunchHash(admission.SpecDigest) {
		return workspaceLaunchPreflightAdmission{}, false
	}
	canonicalPlan, digest, canonicalErr := canonicalProviderPlan(admission.CanonicalProviderPlan)
	if canonicalErr != nil || string(canonicalPlan) != string(admission.CanonicalProviderPlan) || digest != admission.SpecDigest {
		return workspaceLaunchPreflightAdmission{}, false
	}
	var envelope struct {
		PackageID          string         `json:"packageId"`
		ProviderProfileRef string         `json:"providerProfileRef"`
		SchemaVersion      int            `json:"schemaVersion"`
		Spec               map[string]any `json:"spec"`
	}
	if json.Unmarshal(canonicalPlan, &envelope) != nil || envelope.SchemaVersion != 1 || envelope.PackageID != admission.Input.PackageID ||
		envelope.ProviderProfileRef != admission.ProviderProfileRef || len(envelope.Spec) == 0 {
		return workspaceLaunchPreflightAdmission{}, false
	}
	if operation.ID != admission.ProviderBindingRef || operation.OperationID != admission.ProviderBindingRef || operation.Action != "admit_workspace_launch" ||
		operation.ResourceKind != "workspace_launch_preflight" || operation.ResourceID != admission.Input.LaunchOperationID ||
		operation.AccountID != admission.Input.AccountID || operation.WorkspaceID != admission.Input.WorkspaceID ||
		operation.Provider != admission.ProviderProfileRef || operation.IdempotencyKey != admission.ProviderBindingRef ||
		operation.RequestHash != hashInput(admission) || operation.Status != "succeeded" {
		return workspaceLaunchPreflightAdmission{}, false
	}
	return admission, true
}

func (s *Service) workspaceLaunchPreflight(ctx context.Context, ref string) (workspaceLaunchPreflightAdmission, error) {
	operation, err := s.workspaceLaunchPreflights.Get(ctx, ref)
	if errors.Is(err, ErrOperationNotFound) {
		return workspaceLaunchPreflightAdmission{}, ErrLaunchStageBindingNotFound
	}
	if err != nil {
		return workspaceLaunchPreflightAdmission{}, err
	}
	admission, ok := decodeWorkspaceLaunchPreflight(operation)
	if !ok {
		return workspaceLaunchPreflightAdmission{}, ErrLaunchStageBindingConflict
	}
	return admission, nil
}

func (s *Service) ReadWorkspaceLaunchPreflight(ctx context.Context, input WorkspaceLaunchPreflightReadInput) (WorkspaceLaunchPreflightBinding, error) {
	const prefix = "fabric-provider-binding:"
	digest, found := strings.CutPrefix(input.ProviderBindingRef, prefix)
	if !found || !validWorkspaceLaunchHash(digest) {
		return WorkspaceLaunchPreflightBinding{}, ErrWorkspaceLaunchInputInvalid
	}
	admission, err := s.workspaceLaunchPreflight(ctx, input.ProviderBindingRef)
	if err != nil {
		return WorkspaceLaunchPreflightBinding{}, err
	}
	return WorkspaceLaunchPreflightBinding{
		SchemaVersion:        WorkspaceLaunchFabricSchemaVersion,
		LaunchOperationID:    admission.Input.LaunchOperationID,
		AccountID:            admission.Input.AccountID,
		WorkspaceID:          admission.Input.WorkspaceID,
		PackageID:            admission.Input.PackageID,
		SizeGB:               admission.Input.SizeGB,
		WorkspaceImageDigest: admission.Input.WorkspaceImageDigest,
		RequestHash:          admission.Input.RequestHash,
		ProviderProfileRef:   admission.ProviderProfileRef,
		ProviderBindingRef:   admission.ProviderBindingRef,
		SpecDigest:           admission.SpecDigest,
	}, nil
}

func validWorkspaceLaunchRuntimeImageRevision(input WorkspaceLaunchStageInput, imagePolicy workspaceImagePolicy, revisionProvider workspaceLaunchRuntimeImageRevisionProvider) bool {
	revision := input.RuntimeImageRevision
	if revision == nil {
		return true
	}
	if input.Binding.Stage != "runtime" || revisionProvider == nil || !revisionProvider.WorkspaceLaunchRuntimeImageRevisionSupported() ||
		revision.SchemaVersion != 1 || revision.LaunchOperationID != input.Binding.LaunchOperationID ||
		revision.WorkspaceID != input.Binding.WorkspaceID || revision.RuntimeOperationID != input.Binding.FabricOperationID ||
		revision.PreviousImageDigest == revision.ReplacementImageDigest ||
		!imagePolicy.ValidateWorkspaceImageReference(revision.PreviousImageDigest) || !imagePolicy.ValidateWorkspaceImageReference(revision.ReplacementImageDigest) {
		return false
	}
	if !strings.HasPrefix(revision.AuthorizationDigest, "sha256:") || len(revision.AuthorizationDigest) != len("sha256:")+64 {
		return false
	}
	return validWorkspaceLaunchHash(strings.TrimPrefix(revision.AuthorizationDigest, "sha256:"))
}

func validWorkspaceLaunchHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func workspaceLaunchStageRequestHash(input WorkspaceLaunchStageInput, launchRequestHash string) string {
	return hashInput(struct {
		LaunchRequestHash string                   `json:"launchRequestHash"`
		Action            string                   `json:"action"`
		PackageID         string                   `json:"packageId"`
		SizeGB            int                      `json:"sizeGb"`
		ImageDigest       string                   `json:"imageDigest"`
		Resources         WorkspaceLaunchResources `json:"resources"`
	}{launchRequestHash, input.Binding.Action, input.PackageID, input.SizeGB, input.WorkspaceImageDigest, input.Resources})
}

func workspaceLaunchCurrentStageBinding(stage string, resources WorkspaceLaunchResources) string {
	return map[string]string{
		"ensure_compute_allocation": resources.ComputeBindingRef,
		"storage":                   resources.StorageBindingRef,
		"attachment":                resources.AttachmentBindingRef,
		"secret":                    resources.SecretBindingRef,
		"runtime":                   resources.RuntimeBindingRef,
	}[stage]
}

func workspaceLaunchResourcesContain(actual, expected WorkspaceLaunchResources) bool {
	actualBody, _ := json.Marshal(actual)
	expectedBody, _ := json.Marshal(expected)
	actualFields, expectedFields := map[string]string{}, map[string]string{}
	if json.Unmarshal(actualBody, &actualFields) != nil || json.Unmarshal(expectedBody, &expectedFields) != nil {
		return false
	}
	for field, expectedValue := range expectedFields {
		if expectedValue != "" && actualFields[field] != expectedValue {
			return false
		}
	}
	return true
}

func setWorkspaceLaunchStageRecord(operation *FabricOperation, record workspaceLaunchStageRecord) {
	if operation.RedactedProviderPayload == nil {
		operation.RedactedProviderPayload = map[string]any{}
	}
	digest := hashInput(record)
	if record.SchemaVersion == workspaceLaunchStageRecordSchemaVersion {
		digest = workspaceLaunchStageRecordDigest(record)
	}
	operation.RedactedProviderPayload[workspaceLaunchStageRecordPayloadKey] = persistedWorkspaceLaunchStageRecord{
		Record: record, Digest: digest,
	}
}

func workspaceLaunchStageRecordDigest(record workspaceLaunchStageRecord) string {
	if len(record.ProviderState) == 0 {
		return hashInput(record)
	}
	var state map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(record.ProviderState)))
	decoder.UseNumber()
	if decoder.Decode(&state) != nil || state == nil {
		return ""
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return ""
	}
	canonical, err := json.Marshal(state)
	if err != nil {
		return ""
	}
	record.ProviderState = canonical
	return hashInput(record)
}

func legacyWorkspaceLaunchStageRecordDigest(record workspaceLaunchStageRecord) string {
	if len(record.ProviderState) == 0 {
		return hashInput(record)
	}
	switch record.ProviderProfileRef {
	case "local-docker":
		var state localDockerWorkspaceLaunchState
		if json.Unmarshal(record.ProviderState, &state) != nil {
			return ""
		}
		providerState, err := encodeLocalDockerWorkspaceLaunchState(state)
		if err != nil {
			return ""
		}
		record.ProviderState = providerState
	case "tencent-tke":
		var state tencentWorkspaceLaunchState
		if json.Unmarshal(record.ProviderState, &state) != nil {
			return ""
		}
		providerState, err := encodeTencentWorkspaceLaunchState(state)
		if err != nil {
			return ""
		}
		record.ProviderState = providerState
	default:
		return ""
	}
	return hashInput(record)
}

func decodeWorkspaceLaunchStageRecord(operation FabricOperation) (workspaceLaunchStageRecord, bool) {
	value, ok := operation.RedactedProviderPayload[workspaceLaunchStageRecordPayloadKey]
	if !ok {
		return workspaceLaunchStageRecord{}, false
	}
	body, err := json.Marshal(value)
	if err != nil {
		return workspaceLaunchStageRecord{}, false
	}
	var persisted persistedWorkspaceLaunchStageRecord
	if json.Unmarshal(body, &persisted) != nil {
		return workspaceLaunchStageRecord{}, false
	}
	expectedDigest := legacyWorkspaceLaunchStageRecordDigest(persisted.Record)
	if persisted.Record.SchemaVersion == workspaceLaunchStageRecordSchemaVersion {
		expectedDigest = workspaceLaunchStageRecordDigest(persisted.Record)
	}
	if (persisted.Record.SchemaVersion != 1 && persisted.Record.SchemaVersion != workspaceLaunchStageRecordSchemaVersion) ||
		persisted.Digest == "" || persisted.Digest != expectedDigest ||
		persisted.Record.ProviderProfileRef == "" || persisted.Record.ProviderBindingRef == "" || !validWorkspaceLaunchHash(persisted.Record.SpecDigest) {
		return workspaceLaunchStageRecord{}, false
	}
	return persisted.Record, true
}

func workspaceLaunchStageCredentialKeyID(input WorkspaceLaunchStageInput) int64 {
	if input.GatewayCredential == nil {
		return 0
	}
	return input.GatewayCredential.KeyID
}

func newWorkspaceLaunchStageOperation(input WorkspaceLaunchStageInput, provider string, now func() time.Time) (FabricOperation, workspaceLaunchStageRecord, error) {
	binding := input.Binding
	record := workspaceLaunchStageRecord{
		SchemaVersion: workspaceLaunchStageRecordSchemaVersion, ProviderProfileRef: provider, ProviderBindingRef: input.ProviderBindingRef, SpecDigest: input.SpecDigest,
		RequestResources: input.Resources, Resources: input.Resources, GatewayKeyID: workspaceLaunchStageCredentialKeyID(input),
	}
	operation := newOperation(binding.Action, "workspace_launch_stage", binding.FabricOperationID, binding.AccountID, binding.WorkspaceID, binding.IdempotencyKey, binding.RequestHash, now())
	operation.ID, operation.OperationID = binding.FabricOperationID, binding.FabricOperationID
	operation.Provider, operation.Status, operation.CreatedAt = provider, "started", now()
	setWorkspaceLaunchStageRecord(&operation, record)
	if err := bindLaunchStageOperation(&operation, &binding); err != nil {
		return FabricOperation{}, workspaceLaunchStageRecord{}, err
	}
	return operation, record, nil
}

func workspaceLaunchStageOperationMatches(operation FabricOperation, input WorkspaceLaunchStageInput, provider string) (workspaceLaunchStageRecord, bool) {
	binding, bindingOK := decodeLaunchStageBinding(operation)
	record, recordOK := decodeWorkspaceLaunchStageRecord(operation)
	if !bindingOK || !recordOK || binding != input.Binding || operation.ID != input.Binding.FabricOperationID ||
		operation.OperationID != input.Binding.FabricOperationID || operation.Action != input.Binding.Action ||
		operation.ResourceKind != "workspace_launch_stage" || operation.ResourceID != input.Binding.FabricOperationID ||
		operation.Provider != provider || record.ProviderProfileRef != provider || record.ProviderBindingRef != input.ProviderBindingRef || record.SpecDigest != input.SpecDigest ||
		record.RequestResources != input.Resources || !workspaceLaunchRuntimeImageRevisionMatches(operation, record, input) {
		return workspaceLaunchStageRecord{}, false
	}
	keyID := workspaceLaunchStageCredentialKeyID(input)
	if input.Binding.Stage == "secret" {
		if record.GatewayKeyID <= 0 || keyID > 0 && record.GatewayKeyID != keyID {
			return workspaceLaunchStageRecord{}, false
		}
	} else if record.GatewayKeyID != 0 {
		return workspaceLaunchStageRecord{}, false
	}
	return record, true
}

func workspaceLaunchRuntimeImageRevisionMatches(operation FabricOperation, record workspaceLaunchStageRecord, input WorkspaceLaunchStageInput) bool {
	if record.RuntimeImageRevision != nil {
		return input.RuntimeImageRevision != nil && *record.RuntimeImageRevision == *input.RuntimeImageRevision
	}
	return input.RuntimeImageRevision == nil || input.Binding.Stage == "runtime" && (operation.Status == "started" || operation.Status == "failed")
}

func workspaceLaunchRequiredPriorStages(stage string) []string {
	switch stage {
	case "storage":
		return []string{"ensure_compute_allocation"}
	case "attachment":
		return []string{"ensure_compute_allocation", "storage"}
	case "secret":
		return []string{"ensure_compute_allocation", "storage", "attachment"}
	case "runtime":
		return []string{"ensure_compute_allocation", "storage", "attachment", "secret"}
	default:
		return nil
	}
}

func workspaceLaunchStageBindingRef(stage string, resources WorkspaceLaunchResources) string {
	return map[string]string{
		"ensure_compute_allocation": resources.ComputeBindingRef,
		"storage":                   resources.StorageBindingRef,
		"attachment":                resources.AttachmentBindingRef,
		"secret":                    resources.SecretBindingRef,
		"runtime":                   resources.RuntimeBindingRef,
	}[stage]
}

func workspaceLaunchComputeID(binding WorkspaceLaunchStageBinding) string {
	return "ca_" + stableSuffix("create_compute_allocation", binding.IdempotencyKey)[:18]
}

func workspaceLaunchStorageID(binding WorkspaceLaunchStageBinding) string {
	return "vol_" + stableSuffix("create_storage_volume", binding.IdempotencyKey)[:16]
}

func workspaceLaunchAttachmentID(binding WorkspaceLaunchStageBinding) string {
	return "att_" + stableSuffix(binding.IdempotencyKey)[:18]
}

func validWorkspaceLaunchProviderResult(input WorkspaceLaunchStageInput, result WorkspaceLaunchProviderResult) bool {
	if !workspaceLaunchResourcesContain(result.Resources, input.Resources) || workspaceLaunchStageBindingRef(input.Binding.Stage, result.Resources) != input.Binding.FabricOperationID {
		return false
	}
	switch input.Binding.Stage {
	case "ensure_compute_allocation":
		return result.Resources.ComputeAllocationID != ""
	case "storage":
		return result.Resources.StorageID != ""
	case "attachment":
		return result.Resources.AttachmentID != ""
	case "secret":
		return result.Resources.GatewaySecretRef != "" && result.Resources.GatewaySecretVersion != "" &&
			result.Resources.GatewaySecretFingerprint == input.Resources.GatewaySecretFingerprint
	case "runtime":
		return result.Resources.RuntimeID != "" && result.Resources.RuntimeServiceName != "" && result.Resources.RuntimeURL != ""
	default:
		return false
	}
}

func workspaceLaunchStageDiagnosticAt(diagnostic *WorkspaceLaunchStageDiagnostic, observedAt time.Time) (*WorkspaceLaunchStageDiagnostic, error) {
	if diagnostic == nil {
		return nil, nil
	}
	body, err := json.Marshal(diagnostic)
	if err != nil {
		return nil, ErrWorkspaceLaunchUnavailable
	}
	var result WorkspaceLaunchStageDiagnostic
	if json.Unmarshal(body, &result) != nil {
		return nil, ErrWorkspaceLaunchUnavailable
	}
	result.ObservedAt = observedAt.UTC().Format(time.RFC3339Nano)
	if !validWorkspaceLaunchStageDiagnostic(result) {
		return nil, ErrWorkspaceLaunchUnavailable
	}
	return &result, nil
}

func validWorkspaceLaunchStageDiagnostic(diagnostic WorkspaceLaunchStageDiagnostic) bool {
	if diagnostic.SchemaVersion != 1 || diagnostic.Owner != "fabric.tencent_tke" ||
		!workspaceLaunchStageDiagnosticFieldPattern.MatchString(diagnostic.BlockReason) || len(diagnostic.Checks) > 32 {
		return false
	}
	if _, err := time.Parse(time.RFC3339Nano, diagnostic.ObservedAt); err != nil {
		return false
	}
	if diagnostic.ErrorCode != "" && !workspaceLaunchStageDiagnosticFieldPattern.MatchString(diagnostic.ErrorCode) {
		return false
	}
	for _, check := range diagnostic.Checks {
		if !workspaceLaunchStageDiagnosticFieldPattern.MatchString(check.Name) {
			return false
		}
		body, err := json.Marshal(check.Details)
		if err != nil || len(body) > 16*1024 {
			return false
		}
	}
	return true
}

func setWorkspaceLaunchStageDiagnostic(operation *FabricOperation, diagnostic *WorkspaceLaunchStageDiagnostic) {
	if diagnostic == nil {
		return
	}
	operation.RedactedProviderPayload = maps.Clone(operation.RedactedProviderPayload)
	if operation.RedactedProviderPayload == nil {
		operation.RedactedProviderPayload = map[string]any{}
	}
	value := *diagnostic
	operation.RedactedProviderPayload[workspaceLaunchStageDiagnosticPayloadKey] = persistedWorkspaceLaunchStageDiagnostic{
		Diagnostic: value,
		Digest:     hashInput(value),
	}
}

func decodeWorkspaceLaunchStageDiagnostic(operation FabricOperation) (WorkspaceLaunchStageDiagnostic, bool) {
	value, ok := operation.RedactedProviderPayload[workspaceLaunchStageDiagnosticPayloadKey]
	if !ok {
		return WorkspaceLaunchStageDiagnostic{}, false
	}
	body, err := json.Marshal(value)
	if err != nil {
		return WorkspaceLaunchStageDiagnostic{}, false
	}
	var persisted persistedWorkspaceLaunchStageDiagnostic
	if json.Unmarshal(body, &persisted) != nil || !validWorkspaceLaunchStageDiagnostic(persisted.Diagnostic) ||
		persisted.Digest == "" || persisted.Digest != hashInput(persisted.Diagnostic) {
		return WorkspaceLaunchStageDiagnostic{}, false
	}
	return persisted.Diagnostic, true
}

type workspaceLaunchStageDiagnosticConverger interface {
	ConvergeWorkspaceLaunchStageDiagnostic(context.Context, FabricOperation, FabricOperation) error
}

func pendingWorkspaceLaunchStageResult(input WorkspaceLaunchStageInput, reason string, diagnostic *WorkspaceLaunchStageDiagnostic) WorkspaceLaunchStageResult {
	if reason == "" {
		reason = "operation_pending"
	}
	return WorkspaceLaunchStageResult{
		SchemaVersion: 1, State: "pending", Reason: reason, Binding: input.Binding, Resources: input.Resources,
		Diagnostic: diagnostic,
	}
}

func observedWorkspaceLaunchStageResult(input WorkspaceLaunchStageInput, state, reason string, diagnostic *WorkspaceLaunchStageDiagnostic) WorkspaceLaunchStageResult {
	return WorkspaceLaunchStageResult{
		SchemaVersion: WorkspaceLaunchFabricSchemaVersion, State: state, Reason: reason, Binding: input.Binding, Resources: input.Resources,
		Diagnostic: diagnostic,
	}
}

func workspaceLaunchStageMayContinueEnsure(input WorkspaceLaunchStageInput, result WorkspaceLaunchStageResult) bool {
	return result.State == "absent" || result.State == "pending" &&
		(result.Reason == "ownership_pending" || result.Reason == "runtime_image_revision_required" ||
			input.RuntimeImageRevision != nil && result.Reason == "provider_provisioning")
}

func (s *Service) EnsureWorkspaceLaunchStage(ctx context.Context, input WorkspaceLaunchStageInput) (WorkspaceLaunchStageResult, error) {
	return s.launchStages.EnsureWorkspaceLaunchStage(ctx, input)
}

func (s *Service) ReadWorkspaceLaunchStage(ctx context.Context, input WorkspaceLaunchStageInput) (WorkspaceLaunchStageResult, error) {
	return s.launchStages.ReadWorkspaceLaunchStage(ctx, input)
}

func (s *Service) ObserveWorkspaceLaunchStage(ctx context.Context, input WorkspaceLaunchStageInput) (WorkspaceLaunchStageResult, error) {
	return s.launchStages.ObserveWorkspaceLaunchStage(ctx, input)
}
