package fabric

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"reflect"
	"strings"
	"time"
)

const providerMutationBindingPayloadKey = "providerMutationBinding"
const providerMutationStatePayloadKey = "providerMutationState"
const providerMutationReplayEpochPayloadKey = "providerMutationReplayEpoch"

const providerMutationReplayLease = 30 * time.Second
const providerMutationRuntimeImageRevisionReplayClass = "runtime_image_revision"

type providerMutationJournalContextKey struct{}

type providerMutationJournal struct {
	operations      OperationStore
	parent          WorkspaceLaunchStageBinding
	parentOperation FabricOperation
	provider        string
	now             func() time.Time
	readOnly        bool
}

type providerMutationBinding struct {
	SchemaVersion           int                         `json:"schemaVersion"`
	Parent                  WorkspaceLaunchStageBinding `json:"parent"`
	FabricOperationID       string                      `json:"fabricOperationId"`
	Action                  string                      `json:"action"`
	ResourceKind            string                      `json:"resourceKind"`
	ResourceID              string                      `json:"resourceId"`
	ExpectedResourceBinding string                      `json:"expectedResourceBinding"`
}

type providerMutationAttempt struct {
	journal   *providerMutationJournal
	operation FabricOperation
	Fresh     bool
	Replay    bool
}

type providerMutationReplayEpoch struct {
	SchemaVersion           int    `json:"schemaVersion"`
	ReplayID                string `json:"replayId"`
	ParentFabricOperationID string `json:"parentFabricOperationId"`
	ChildOperationID        string `json:"childOperationId"`
	IdempotencyKey          string `json:"idempotencyKey"`
	ReplayClass             string `json:"replayClass,omitempty"`
	AuthorityDigest         string `json:"authorityDigest,omitempty"`
	PreviousImageDigest     string `json:"previousImageDigest,omitempty"`
	ReplacementImageDigest  string `json:"replacementImageDigest,omitempty"`
	State                   string `json:"state"`
	LeaseGeneration         int    `json:"leaseGeneration"`
	LeaseExpiresAt          string `json:"leaseExpiresAt"`
	DispatchStartedAt       string `json:"dispatchStartedAt,omitempty"`
	CompletedAt             string `json:"completedAt,omitempty"`
}

type providerMutationReplayStore interface {
	SaveProviderMutationReplayEpoch(context.Context, FabricOperation, FabricOperation) error
	ConvergeProviderMutationReplay(context.Context, FabricOperation, FabricOperation) error
}

type persistedProviderMutationBinding struct {
	Binding providerMutationBinding `json:"binding"`
	Digest  string                  `json:"digest"`
}

type persistedProviderMutationState struct {
	Value  json.RawMessage `json:"value"`
	Digest string          `json:"digest"`
}

func providerMutationJournalFromContext(ctx context.Context) *providerMutationJournal {
	journal, _ := ctx.Value(providerMutationJournalContextKey{}).(*providerMutationJournal)
	return journal
}

func (s *Service) providerMutationContext(ctx context.Context, operation FabricOperation) context.Context {
	return s.providerOperationContext(ctx, operation, false)
}

func (s *Service) providerReadContext(ctx context.Context, operation FabricOperation) context.Context {
	return s.providerOperationContext(ctx, operation, true)
}

func (s *Service) providerOperationContext(ctx context.Context, operation FabricOperation, readOnly bool) context.Context {
	binding, ok := decodeLaunchStageBinding(operation)
	if !ok {
		binding, ok = directStorageProviderMutationBinding(operation)
		if !ok {
			return ctx
		}
	}
	return context.WithValue(ctx, providerMutationJournalContextKey{}, &providerMutationJournal{
		operations: s.operations, parent: binding, parentOperation: operation, provider: s.provider.Descriptor().Name, now: s.now, readOnly: readOnly,
	})
}

func directStorageProviderMutationBinding(operation FabricOperation) (WorkspaceLaunchStageBinding, bool) {
	if operation.CallerService != "control-plane" || operation.Action != "create_storage_volume" || operation.ResourceKind != "storage_volume" {
		return WorkspaceLaunchStageBinding{}, false
	}
	for _, value := range []string{
		operation.OperationID, operation.ResourceID, operation.AccountID, operation.WorkspaceID,
		operation.IdempotencyKey, operation.RequestHash,
	} {
		if value == "" || value != strings.TrimSpace(value) {
			return WorkspaceLaunchStageBinding{}, false
		}
	}
	binding := WorkspaceLaunchStageBinding{
		SchemaVersion: 1, LaunchOperationID: operation.OperationID,
		AccountID: operation.AccountID, WorkspaceID: operation.WorkspaceID,
		Stage: "storage", Action: "ensure_storage", FabricOperationID: operation.OperationID,
		IdempotencyKey: operation.IdempotencyKey, RequestHash: operation.RequestHash,
	}
	return binding, validWorkspaceLaunchStageBinding(binding)
}

func providerMutationOperationID(parent WorkspaceLaunchStageBinding, action, resourceKind, resourceID, expectedBinding string) string {
	return parent.FabricOperationID + ":provider:" + stableSuffix(action, resourceKind, resourceID, expectedBinding)[:16]
}

func beginProviderMutation(ctx context.Context, action, resourceKind, resourceID, expectedBinding string) (*providerMutationAttempt, error) {
	return beginProviderMutationWithState(ctx, action, resourceKind, resourceID, expectedBinding, nil)
}

func beginProviderMutationWithState(ctx context.Context, action, resourceKind, resourceID, expectedBinding string, state any) (*providerMutationAttempt, error) {
	journal := providerMutationJournalFromContext(ctx)
	if journal == nil {
		return nil, nil
	}
	if journal.readOnly {
		return nil, fmt.Errorf("provider_mutation_forbidden_in_read")
	}
	if action == "" || resourceKind == "" || resourceID == "" {
		return nil, fmt.Errorf("provider_mutation_binding_invalid")
	}
	binding := providerMutationBinding{
		SchemaVersion: 1, Parent: journal.parent, Action: action, ResourceKind: resourceKind,
		ResourceID: resourceID, ExpectedResourceBinding: expectedBinding,
	}
	binding.FabricOperationID = providerMutationOperationID(journal.parent, action, resourceKind, resourceID, expectedBinding)
	now := journal.now()
	operation := newOperation(action, resourceKind, resourceID, journal.parent.AccountID, journal.parent.WorkspaceID, binding.FabricOperationID, hashInput(binding), now)
	operation.ID, operation.OperationID = binding.FabricOperationID, binding.FabricOperationID
	operation.Provider, operation.Status, operation.CreatedAt = journal.provider, "started", now
	operation.RedactedProviderPayload = map[string]any{
		providerMutationBindingPayloadKey: persistedProviderMutationBinding{Binding: binding, Digest: hashInput(binding)},
	}
	if state != nil {
		body, err := json.Marshal(state)
		if err != nil {
			return nil, err
		}
		operation.RedactedProviderPayload[providerMutationStatePayloadKey] = persistedProviderMutationState{Value: body, Digest: hashInput(json.RawMessage(body))}
	}
	current, err := journal.operations.Get(ctx, operation.ID)
	if err == nil {
		persisted, ok := decodeProviderMutationBinding(current)
		if !ok || persisted != binding || !sameProviderMutationState(current, operation) {
			return nil, ErrLaunchStageBindingConflict
		}
		return &providerMutationAttempt{journal: journal, operation: current}, nil
	}
	if !errors.Is(err, ErrOperationNotFound) {
		return nil, err
	}
	if err := journal.operations.Append(ctx, operation); err != nil {
		concurrent, getErr := journal.operations.Get(ctx, operation.ID)
		if getErr != nil || concurrent.RequestHash != operation.RequestHash {
			return nil, err
		}
		return &providerMutationAttempt{journal: journal, operation: concurrent}, nil
	}
	return &providerMutationAttempt{journal: journal, operation: operation, Fresh: true}, nil
}

func sameProviderMutationState(current, expected FabricOperation) bool {
	currentState, currentOK := current.RedactedProviderPayload[providerMutationStatePayloadKey]
	expectedState, expectedOK := expected.RedactedProviderPayload[providerMutationStatePayloadKey]
	if currentOK != expectedOK {
		return false
	}
	if !currentOK {
		return true
	}
	currentPersisted, currentOK := decodePersistedProviderMutationState(currentState)
	expectedPersisted, expectedOK := decodePersistedProviderMutationState(expectedState)
	if !currentOK || !expectedOK || currentPersisted.Digest != expectedPersisted.Digest ||
		!sameJSONObject(currentPersisted.Value, expectedPersisted.Value) {
		return false
	}
	if currentPersisted.Digest == hashInput(currentPersisted.Value) || expectedPersisted.Digest == hashInput(expectedPersisted.Value) {
		return true
	}
	// A store transition can compare two identical JSONB-normalized copies whose
	// preserved typed digest no longer matches either reordered raw value.
	return bytes.Equal(currentPersisted.Value, expectedPersisted.Value)
}

func decodeProviderMutationBinding(operation FabricOperation) (providerMutationBinding, bool) {
	value, ok := operation.RedactedProviderPayload[providerMutationBindingPayloadKey]
	if !ok {
		return providerMutationBinding{}, false
	}
	body, err := json.Marshal(value)
	if err != nil {
		return providerMutationBinding{}, false
	}
	var persisted persistedProviderMutationBinding
	if json.Unmarshal(body, &persisted) != nil || persisted.Binding.SchemaVersion != 1 ||
		persisted.Digest == "" || persisted.Digest != hashInput(persisted.Binding) {
		return providerMutationBinding{}, false
	}
	binding := persisted.Binding
	if operation.ID != binding.FabricOperationID || operation.OperationID != binding.FabricOperationID ||
		operation.Action != binding.Action || operation.ResourceKind != binding.ResourceKind || operation.ResourceID != binding.ResourceID ||
		operation.AccountID != binding.Parent.AccountID || operation.WorkspaceID != binding.Parent.WorkspaceID ||
		operation.IdempotencyKey != binding.FabricOperationID || operation.RequestHash != hashInput(binding) {
		return providerMutationBinding{}, false
	}
	return binding, true
}

func providerMutationReplayID(operation FabricOperation, binding providerMutationBinding) string {
	return "replay_" + stableSuffix(binding.Parent.FabricOperationID, operation.ID, operation.IdempotencyKey)[:24]
}

func decodeProviderMutationReplayEpoch(operation FabricOperation) (providerMutationReplayEpoch, bool) {
	value, ok := operation.RedactedProviderPayload[providerMutationReplayEpochPayloadKey]
	if !ok {
		return providerMutationReplayEpoch{}, false
	}
	body, err := json.Marshal(value)
	if err != nil {
		return providerMutationReplayEpoch{}, false
	}
	var epoch providerMutationReplayEpoch
	binding, bindingOK := decodeProviderMutationBinding(operation)
	if json.Unmarshal(body, &epoch) != nil || !bindingOK || epoch.SchemaVersion != 1 || epoch.ReplayID != providerMutationReplayID(operation, binding) ||
		epoch.ParentFabricOperationID != binding.Parent.FabricOperationID || epoch.ChildOperationID != operation.ID ||
		epoch.IdempotencyKey != operation.IdempotencyKey || epoch.LeaseGeneration <= 0 {
		return providerMutationReplayEpoch{}, false
	}
	if epoch.ReplayClass == "" {
		if epoch.AuthorityDigest != "" || epoch.PreviousImageDigest != "" || epoch.ReplacementImageDigest != "" {
			return providerMutationReplayEpoch{}, false
		}
	} else if epoch.ReplayClass != providerMutationRuntimeImageRevisionReplayClass ||
		!strings.HasPrefix(epoch.AuthorityDigest, "sha256:") || len(epoch.AuthorityDigest) != len("sha256:")+64 ||
		!validWorkspaceLaunchHash(strings.TrimPrefix(epoch.AuthorityDigest, "sha256:")) ||
		!validWorkspaceRuntimeImageIdentity(epoch.PreviousImageDigest) || !validWorkspaceRuntimeImageIdentity(epoch.ReplacementImageDigest) ||
		epoch.PreviousImageDigest == epoch.ReplacementImageDigest {
		return providerMutationReplayEpoch{}, false
	}
	if _, err := time.Parse(time.RFC3339Nano, epoch.LeaseExpiresAt); err != nil {
		return providerMutationReplayEpoch{}, false
	}
	switch epoch.State {
	case "leased":
		if epoch.CompletedAt != "" {
			return providerMutationReplayEpoch{}, false
		}
		if epoch.DispatchStartedAt != "" {
			if _, err := time.Parse(time.RFC3339Nano, epoch.DispatchStartedAt); err != nil {
				return providerMutationReplayEpoch{}, false
			}
		}
	case "awaiting_readback":
		if epoch.DispatchStartedAt == "" || epoch.CompletedAt != "" {
			return providerMutationReplayEpoch{}, false
		}
		if _, err := time.Parse(time.RFC3339Nano, epoch.DispatchStartedAt); err != nil {
			return providerMutationReplayEpoch{}, false
		}
	case "succeeded":
		if epoch.CompletedAt == "" {
			return providerMutationReplayEpoch{}, false
		}
		if _, err := time.Parse(time.RFC3339Nano, epoch.CompletedAt); err != nil {
			return providerMutationReplayEpoch{}, false
		}
	case "blocked":
		if epoch.DispatchStartedAt != "" || epoch.CompletedAt == "" {
			return providerMutationReplayEpoch{}, false
		}
		if _, err := time.Parse(time.RFC3339Nano, epoch.CompletedAt); err != nil {
			return providerMutationReplayEpoch{}, false
		}
	default:
		return providerMutationReplayEpoch{}, false
	}
	return epoch, true
}

func (a *providerMutationAttempt) resource(target any) bool {
	return a != nil && decodeOperationResource(a.operation, target)
}

func (a *providerMutationAttempt) state(target any) bool {
	if a == nil {
		return false
	}
	return decodeProviderMutationState(a.operation, target)
}

func decodeProviderMutationState(operation FabricOperation, target any) bool {
	value, ok := operation.RedactedProviderPayload[providerMutationStatePayloadKey]
	if !ok {
		return false
	}
	persisted, ok := decodePersistedProviderMutationState(value)
	targetValue := reflect.ValueOf(target)
	if !ok || !targetValue.IsValid() || targetValue.Kind() != reflect.Pointer || targetValue.IsNil() {
		return false
	}
	decodedTarget := reflect.New(targetValue.Elem().Type())
	if !decodeStrictJSON(persisted.Value, decodedTarget.Interface()) {
		return false
	}
	typedValue, err := json.Marshal(decodedTarget.Interface())
	if err != nil || !sameJSONObject(persisted.Value, typedValue) || persisted.Digest != hashInput(json.RawMessage(typedValue)) {
		return false
	}
	targetValue.Elem().Set(decodedTarget.Elem())
	return true
}

func decodePersistedProviderMutationState(value any) (persistedProviderMutationState, bool) {
	body, err := json.Marshal(value)
	if err != nil {
		return persistedProviderMutationState{}, false
	}
	var persisted persistedProviderMutationState
	if !decodeStrictJSON(body, &persisted) || persisted.Digest == "" || len(persisted.Value) == 0 {
		return persistedProviderMutationState{}, false
	}
	return persisted, true
}

func decodeStrictJSON(body []byte, target any) bool {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return false
	}
	return decoder.Decode(&struct{}{}) == io.EOF
}

func sameJSONObject(left, right []byte) bool {
	leftCanonical, leftOK := canonicalJSONObject(left)
	rightCanonical, rightOK := canonicalJSONObject(right)
	return leftOK && rightOK && bytes.Equal(leftCanonical, rightCanonical)
}

func canonicalJSONObject(body []byte) ([]byte, bool) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return nil, false
	}
	if _, ok := value.(map[string]any); !ok {
		return nil, false
	}
	canonical, err := json.Marshal(value)
	return canonical, err == nil
}

func (a *providerMutationAttempt) complete(ctx context.Context, providerRequestID string, resource any, mutationErr error) error {
	if a == nil || (!a.Fresh && a.operation.Status == "succeeded" && !a.Replay) {
		return nil
	}
	if !a.Replay && mutationErr == nil {
		if epoch, ok := decodeProviderMutationReplayEpoch(a.operation); ok && epoch.State != "succeeded" {
			a.Replay = true
		}
	}
	next := a.operation
	next.ProviderRequestID = providerRequestID
	next.FinishedAt = a.journal.now()
	fillOperationResource(&next, resource)
	if a.Replay {
		epoch, ok := decodeProviderMutationReplayEpoch(a.operation)
		if !ok {
			return ErrLaunchStageBindingConflict
		}
		store, ok := a.journal.operations.(providerMutationReplayStore)
		if !ok {
			return ErrRuntimeOperationNotCurrent
		}
		if mutationErr != nil {
			if epoch.State == "leased" {
				if epoch.DispatchStartedAt == "" {
					epoch.State = "blocked"
					epoch.CompletedAt = a.journal.now().UTC().Format(time.RFC3339Nano)
				} else {
					epoch.State = "awaiting_readback"
				}
				next = a.operation
				next.RedactedProviderPayload = maps.Clone(a.operation.RedactedProviderPayload)
				next.RedactedProviderPayload[providerMutationReplayEpochPayloadKey] = epoch
				return store.SaveProviderMutationReplayEpoch(ctx, a.operation, next)
			}
			return nil
		}
		epoch.State = "succeeded"
		epoch.CompletedAt = a.journal.now().UTC().Format(time.RFC3339Nano)
		next.RedactedProviderPayload = maps.Clone(next.RedactedProviderPayload)
		next.RedactedProviderPayload[providerMutationReplayEpochPayloadKey] = epoch
		next.Status, next.ErrorCode = "succeeded", ""
		return store.ConvergeProviderMutationReplay(ctx, a.operation, next)
	}
	if mutationErr == nil {
		next.Status = "succeeded"
	} else {
		next.Status, next.ErrorCode = "failed", errorCode(mutationErr)
	}
	if !a.Fresh && a.operation.Status == "started" && mutationErr == nil {
		converger, ok := a.journal.operations.(runtimeReadbackConverger)
		if !ok {
			return ErrRuntimeOperationNotCurrent
		}
		return converger.ConvergeRuntimeReadback(ctx, a.operation, next)
	}
	if !a.Fresh && a.operation.Status == "failed" {
		if mutationErr != nil {
			return nil
		}
		converger, ok := a.journal.operations.(runtimeReadbackConverger)
		if !ok {
			return ErrRuntimeOperationNotCurrent
		}
		return converger.ConvergeRuntimeReadback(ctx, a.operation, next)
	}
	return a.journal.operations.SaveRuntime(ctx, next)
}

func (a *providerMutationAttempt) claimReplay(ctx context.Context) (bool, error) {
	return a.claimReplayWithClass(ctx, providerMutationReplayEpoch{}, false)
}

func (a *providerMutationAttempt) claimRuntimeImageRevision(ctx context.Context, revision WorkspaceLaunchRuntimeImageRevision, observed WorkspaceRuntime) (bool, error) {
	if a == nil || a.journal == nil || a.Fresh || a.operation.Status != "succeeded" {
		return false, nil
	}
	binding, bindingOK := decodeProviderMutationBinding(a.operation)
	var persisted WorkspaceRuntime
	if !bindingOK || binding.Action != "tencent_workspace_runtime_apply" || binding.ResourceKind != "workspace_runtime" ||
		binding.Parent.Stage != "runtime" || binding.Parent.Action != "ensure_runtime" ||
		binding.Parent.LaunchOperationID != revision.LaunchOperationID || binding.Parent.WorkspaceID != revision.WorkspaceID ||
		binding.Parent.FabricOperationID != revision.RuntimeOperationID || !decodeOperationResource(a.operation, &persisted) ||
		persisted.ID == "" || persisted.ID != binding.ResourceID || persisted.ID != observed.ID ||
		persisted.OperationID != revision.RuntimeOperationID || persisted.OperationID != observed.OperationID ||
		persisted.WorkspaceID != revision.WorkspaceID || persisted.WorkspaceID != observed.WorkspaceID ||
		persisted.ServiceName == "" || persisted.ServiceName != binding.ExpectedResourceBinding || persisted.ServiceName != observed.ServiceName {
		return false, ErrLaunchStageBindingConflict
	}
	directRevision := persisted.ImageID == revision.PreviousImageDigest &&
		(observed.ImageID == revision.PreviousImageDigest || observed.ImageID == revision.ReplacementImageDigest && !observed.Ready)
	supersession := a.runtimeImageRevisionSupersessionEligible(revision, persisted, observed)
	if !directRevision && !supersession {
		return false, ErrLaunchStageBindingConflict
	}
	template := providerMutationReplayEpoch{
		ReplayClass: providerMutationRuntimeImageRevisionReplayClass, AuthorityDigest: revision.AuthorizationDigest,
		PreviousImageDigest: revision.PreviousImageDigest, ReplacementImageDigest: revision.ReplacementImageDigest,
	}
	if supersession {
		return a.claimRuntimeImageRevisionSupersession(ctx, template, persisted, observed)
	}
	return a.claimReplayWithClass(ctx, template, true)
}

func (a *providerMutationAttempt) runtimeImageRevisionSupersessionEligible(revision WorkspaceLaunchRuntimeImageRevision, persisted, observed WorkspaceRuntime) bool {
	epoch, ok := decodeProviderMutationReplayEpoch(a.operation)
	if !ok || epoch.ReplayClass != providerMutationRuntimeImageRevisionReplayClass || epoch.State != "awaiting_readback" ||
		epoch.AuthorityDigest == revision.AuthorizationDigest || epoch.PreviousImageDigest != persisted.ImageID ||
		epoch.ReplacementImageDigest != observed.ImageID || observed.Ready || observed.Status != "unready" ||
		revision.PreviousImageDigest != epoch.ReplacementImageDigest || revision.ReplacementImageDigest == epoch.PreviousImageDigest ||
		revision.ReplacementImageDigest == epoch.ReplacementImageDigest || !sameWorkspaceRuntimeRevisionIdentity(persisted, observed) {
		return false
	}
	lease, err := time.Parse(time.RFC3339Nano, epoch.LeaseExpiresAt)
	return err == nil && !lease.After(a.journal.now().UTC())
}

func sameWorkspaceRuntimeRevisionIdentity(persisted, observed WorkspaceRuntime) bool {
	return persisted.ID == observed.ID && persisted.OperationID == observed.OperationID && persisted.WorkspaceID == observed.WorkspaceID &&
		persisted.URL == observed.URL && persisted.ServiceName == observed.ServiceName &&
		persisted.Access.Username == observed.Access.Username && persisted.Access.CredentialStatus == observed.Access.CredentialStatus &&
		persisted.Access.CredentialVersion == observed.Access.CredentialVersion && persisted.Access.SecretRef == observed.Access.SecretRef &&
		reflect.DeepEqual(persisted.CostTags, observed.CostTags)
}

func (a *providerMutationAttempt) claimRuntimeImageRevisionSupersession(
	ctx context.Context,
	template providerMutationReplayEpoch,
	persisted WorkspaceRuntime,
	observed WorkspaceRuntime,
) (bool, error) {
	store, ok := a.journal.operations.(providerMutationReplayStore)
	if !ok {
		return false, ErrRuntimeOperationNotCurrent
	}
	previous, ok := decodeProviderMutationReplayEpoch(a.operation)
	binding, bindingOK := decodeProviderMutationBinding(a.operation)
	if !ok || !bindingOK {
		return false, ErrLaunchStageBindingConflict
	}
	now := a.journal.now().UTC()
	baseline := observed
	baseline.ProviderRequestID = persisted.ProviderRequestID
	baseline.CreatedAt = persisted.CreatedAt
	next := a.operation
	fillOperationResource(&next, baseline)
	template.SchemaVersion = 1
	template.ReplayID = providerMutationReplayID(next, binding)
	template.ParentFabricOperationID = binding.Parent.FabricOperationID
	template.ChildOperationID = next.ID
	template.IdempotencyKey = next.IdempotencyKey
	template.State = "leased"
	template.LeaseGeneration = previous.LeaseGeneration + 1
	template.LeaseExpiresAt = now.Add(providerMutationReplayLease).Format(time.RFC3339Nano)
	template.DispatchStartedAt = ""
	template.CompletedAt = ""
	next.RedactedProviderPayload[providerMutationReplayEpochPayloadKey] = template
	if err := store.SaveProviderMutationReplayEpoch(ctx, a.operation, next); err != nil {
		return false, err
	}
	a.operation, a.Replay = next, true
	return true, nil
}

func (a *providerMutationAttempt) runtimeImageRevisionClaimed(revision WorkspaceLaunchRuntimeImageRevision) bool {
	if a == nil {
		return false
	}
	epoch, ok := decodeProviderMutationReplayEpoch(a.operation)
	return ok && epoch.ReplayClass == providerMutationRuntimeImageRevisionReplayClass && epoch.AuthorityDigest == revision.AuthorizationDigest &&
		epoch.PreviousImageDigest == revision.PreviousImageDigest && epoch.ReplacementImageDigest == revision.ReplacementImageDigest &&
		epoch.State != "blocked"
}

func convergeRuntimeImageRevisionReadback(ctx context.Context, revision WorkspaceLaunchRuntimeImageRevision, runtime WorkspaceRuntime) error {
	journal := providerMutationJournalFromContext(ctx)
	if journal == nil || journal.parent.Stage != "runtime" || journal.parent.LaunchOperationID != revision.LaunchOperationID ||
		journal.parent.WorkspaceID != revision.WorkspaceID || journal.parent.FabricOperationID != revision.RuntimeOperationID ||
		runtime.OperationID != revision.RuntimeOperationID || runtime.WorkspaceID != revision.WorkspaceID ||
		runtime.ImageID != revision.ReplacementImageDigest {
		return ErrLaunchStageBindingConflict
	}
	childID := providerMutationOperationID(journal.parent, "tencent_workspace_runtime_apply", "workspace_runtime", runtime.ID, runtime.ServiceName)
	operation, err := journal.operations.Get(ctx, childID)
	if err != nil {
		return err
	}
	attempt := &providerMutationAttempt{journal: journal, operation: operation}
	epoch, epochOK := decodeProviderMutationReplayEpoch(operation)
	if !attempt.runtimeImageRevisionClaimed(revision) || !epochOK {
		return ErrLaunchStageBindingConflict
	}
	if !runtime.Ready {
		return nil
	}
	if epoch.State == "succeeded" {
		var persisted WorkspaceRuntime
		if !decodeOperationResource(operation, &persisted) || persisted.ID != runtime.ID || persisted.OperationID != runtime.OperationID ||
			persisted.WorkspaceID != runtime.WorkspaceID || persisted.ServiceName != runtime.ServiceName || persisted.ImageID != runtime.ImageID {
			return ErrLaunchStageBindingConflict
		}
		return nil
	}
	if epoch.State != "awaiting_readback" || !correctableSucceededRuntimeImageRevision(operation, epoch) {
		return ErrLaunchStageBindingConflict
	}
	attempt.Replay = true
	return attempt.complete(ctx, runtime.ProviderRequestID, runtime, nil)
}

func (a *providerMutationAttempt) claimReplayWithClass(ctx context.Context, template providerMutationReplayEpoch, allowSucceeded bool) (bool, error) {
	if a == nil || a.journal == nil || a.Fresh ||
		a.operation.Status != "started" && a.operation.Status != "failed" && !correctableSucceededNodeClaim(a.operation) && !(allowSucceeded && a.operation.Status == "succeeded") {
		return false, nil
	}
	store, ok := a.journal.operations.(providerMutationReplayStore)
	if !ok {
		return false, ErrRuntimeOperationNotCurrent
	}
	now := a.journal.now().UTC()
	binding, validBinding := decodeProviderMutationBinding(a.operation)
	if !validBinding || binding.Parent.FabricOperationID == "" {
		return false, ErrLaunchStageBindingConflict
	}
	generation := 1
	dispatchStartedAt := ""
	if _, exists := a.operation.RedactedProviderPayload[providerMutationReplayEpochPayloadKey]; exists {
		epoch, valid := decodeProviderMutationReplayEpoch(a.operation)
		upgradeGenericSucceededRuntime := valid && allowSucceeded && a.operation.Status == "succeeded" &&
			epoch.State == "succeeded" && epoch.ReplayClass == "" &&
			template.ReplayClass == providerMutationRuntimeImageRevisionReplayClass
		if !valid || !upgradeGenericSucceededRuntime && (epoch.State == "succeeded" || epoch.State == "blocked" ||
			epoch.ReplayClass != template.ReplayClass || epoch.AuthorityDigest != template.AuthorityDigest ||
			epoch.PreviousImageDigest != template.PreviousImageDigest || epoch.ReplacementImageDigest != template.ReplacementImageDigest) {
			return false, ErrLaunchStageBindingConflict
		}
		if upgradeGenericSucceededRuntime {
			generation = epoch.LeaseGeneration + 1
		} else {
			lease, leaseErr := time.Parse(time.RFC3339Nano, epoch.LeaseExpiresAt)
			if leaseErr != nil {
				return false, ErrLaunchStageBindingConflict
			}
			if lease.After(now) {
				return false, nil
			}
			generation = epoch.LeaseGeneration + 1
			dispatchStartedAt = epoch.DispatchStartedAt
		}
	}
	next := a.operation
	next.RedactedProviderPayload = maps.Clone(a.operation.RedactedProviderPayload)
	template.SchemaVersion = 1
	template.ReplayID = providerMutationReplayID(next, binding)
	template.ParentFabricOperationID = binding.Parent.FabricOperationID
	template.ChildOperationID = next.ID
	template.IdempotencyKey = next.IdempotencyKey
	template.State = "leased"
	template.LeaseGeneration = generation
	template.LeaseExpiresAt = now.Add(providerMutationReplayLease).Format(time.RFC3339Nano)
	template.DispatchStartedAt = dispatchStartedAt
	next.RedactedProviderPayload[providerMutationReplayEpochPayloadKey] = template
	if err := store.SaveProviderMutationReplayEpoch(ctx, a.operation, next); err != nil {
		return false, err
	}
	a.operation, a.Replay = next, true
	return true, nil
}

func correctableSucceededNodeClaim(operation FabricOperation) bool {
	binding, ok := decodeProviderMutationBinding(operation)
	return ok && operation.Status == "succeeded" && binding.Action == "tencent_kubernetes_node_claim" &&
		binding.ResourceKind == "compute_binding" && binding.ExpectedResourceBinding != ""
}

func (a *providerMutationAttempt) markReplayDispatch(ctx context.Context) error {
	if a == nil || a.Fresh {
		return nil
	}
	if !a.Replay {
		return ErrRuntimeOperationNotCurrent
	}
	epoch, ok := decodeProviderMutationReplayEpoch(a.operation)
	if !ok || epoch.State != "leased" {
		return ErrLaunchStageBindingConflict
	}
	store, ok := a.journal.operations.(providerMutationReplayStore)
	if !ok {
		return ErrRuntimeOperationNotCurrent
	}
	next := a.operation
	next.RedactedProviderPayload = maps.Clone(a.operation.RedactedProviderPayload)
	epoch.State = "awaiting_readback"
	if epoch.DispatchStartedAt == "" {
		epoch.DispatchStartedAt = a.journal.now().UTC().Format(time.RFC3339Nano)
	}
	next.RedactedProviderPayload[providerMutationReplayEpochPayloadKey] = epoch
	if err := store.SaveProviderMutationReplayEpoch(ctx, a.operation, next); err != nil {
		return err
	}
	a.operation = next
	return nil
}
