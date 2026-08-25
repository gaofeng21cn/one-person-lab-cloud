package fabric

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"strings"
	"time"
)

type launchStageEngine struct {
	stages               WorkspaceLaunchStageStore
	provider             workspaceLaunchProvider
	providerDescriptor   providerDescriptorReader
	imagePolicy          workspaceImagePolicy
	runtimeImageRevision workspaceLaunchRuntimeImageRevisionProvider
	providerMutations    ProviderMutationStore
	machineOwnership     MachineOwnershipStore
	now                  func() time.Time
}

func newLaunchStageEngine(
	stages WorkspaceLaunchStageStore,
	provider workspaceLaunchProvider,
	providerDescriptor providerDescriptorReader,
	imagePolicy workspaceImagePolicy,
	runtimeImageRevision workspaceLaunchRuntimeImageRevisionProvider,
	providerMutations ProviderMutationStore,
	machineOwnership MachineOwnershipStore,
	now func() time.Time,
) *launchStageEngine {
	return &launchStageEngine{
		stages:               stages,
		provider:             provider,
		providerDescriptor:   providerDescriptor,
		imagePolicy:          imagePolicy,
		runtimeImageRevision: runtimeImageRevision,
		providerMutations:    providerMutations,
		machineOwnership:     machineOwnership,
		now:                  now,
	}
}

func (e *launchStageEngine) workspaceLaunchPreflight(ctx context.Context, ref string) (workspaceLaunchPreflightAdmission, error) {
	operation, err := e.stages.Get(ctx, ref)
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

func (e *launchStageEngine) validateWorkspaceLaunchStageInput(ctx context.Context, input WorkspaceLaunchStageInput) error {
	if !validWorkspaceLaunchStageBinding(input.Binding) || strings.TrimSpace(input.ProviderProfileRef) == "" ||
		strings.TrimSpace(input.ProviderBindingRef) == "" || !validWorkspaceLaunchHash(input.SpecDigest) || strings.TrimSpace(input.PackageID) == "" ||
		input.SizeGB < 10 || input.SizeGB%10 != 0 || !e.imagePolicy.ValidateWorkspaceImageReference(input.WorkspaceImageDigest) {
		return ErrWorkspaceLaunchInputInvalid
	}
	if !validWorkspaceLaunchRuntimeImageRevision(input, e.imagePolicy, e.runtimeImageRevision) {
		return ErrWorkspaceLaunchInputInvalid
	}
	admission, err := e.workspaceLaunchPreflight(ctx, input.ProviderBindingRef)
	if err != nil {
		return err
	}
	preflight := admission.Input
	if admission.ProviderProfileRef != input.ProviderProfileRef || admission.ProviderBindingRef != input.ProviderBindingRef || admission.SpecDigest != input.SpecDigest ||
		preflight.LaunchOperationID != input.Binding.LaunchOperationID ||
		preflight.AccountID != input.Binding.AccountID || preflight.WorkspaceID != input.Binding.WorkspaceID ||
		preflight.PackageID != input.PackageID || preflight.SizeGB != input.SizeGB ||
		preflight.WorkspaceImageDigest != input.WorkspaceImageDigest {
		return ErrLaunchStageBindingConflict
	}
	if admission.ProviderProfileRef != e.providerDescriptor.Descriptor().Name || input.Binding.RequestHash != workspaceLaunchStageRequestHash(input, preflight.RequestHash) {
		return ErrLaunchStageBindingConflict
	}
	return e.validateWorkspaceLaunchExpectedBinding(ctx, input)
}

func (e *launchStageEngine) validateWorkspaceLaunchExpectedBinding(ctx context.Context, input WorkspaceLaunchStageInput) error {
	expected := workspaceLaunchCurrentStageBinding(input.Binding.Stage, input.Resources)
	if input.Binding.ExpectedResourceBinding != expected {
		return ErrLaunchStageBindingConflict
	}
	if expected == "" {
		return nil
	}
	operation, err := e.stages.Get(ctx, expected)
	if err != nil {
		return ErrLaunchStageBindingConflict
	}
	persisted, ok := decodeLaunchStageBinding(operation)
	record, recordOK := decodeWorkspaceLaunchStageRecord(operation)
	if !ok || !recordOK || operation.Status != "succeeded" || persisted.LaunchOperationID != input.Binding.LaunchOperationID ||
		persisted.AccountID != input.Binding.AccountID || persisted.WorkspaceID != input.Binding.WorkspaceID ||
		persisted.Stage != input.Binding.Stage || operation.ID != expected || record.ProviderProfileRef != input.ProviderProfileRef ||
		!workspaceLaunchResourcesContain(input.Resources, record.Resources) {
		return ErrLaunchStageBindingConflict
	}
	return nil
}

func (e *launchStageEngine) WorkspaceLaunchProviderRequest(ctx context.Context, input WorkspaceLaunchStageInput, current workspaceLaunchStageRecord) (WorkspaceLaunchProviderRequest, error) {
	admission, err := e.workspaceLaunchPreflight(ctx, input.ProviderBindingRef)
	if err != nil || admission.SpecDigest != input.SpecDigest || admission.ProviderProfileRef != input.ProviderProfileRef {
		return WorkspaceLaunchProviderRequest{}, ErrLaunchStageBindingConflict
	}
	request := WorkspaceLaunchProviderRequest{Input: input, Current: current, Prior: map[string]workspaceLaunchStageRecord{}, ProviderPlan: append(json.RawMessage(nil), admission.CanonicalProviderPlan...)}
	for _, stage := range workspaceLaunchRequiredPriorStages(input.Binding.Stage) {
		ref := workspaceLaunchStageBindingRef(stage, input.Resources)
		if ref == "" {
			return WorkspaceLaunchProviderRequest{}, ErrLaunchStageBindingConflict
		}
		operation, err := e.stages.Get(ctx, ref)
		if err != nil {
			return WorkspaceLaunchProviderRequest{}, ErrLaunchStageBindingConflict
		}
		binding, bindingOK := decodeLaunchStageBinding(operation)
		record, recordOK := decodeWorkspaceLaunchStageRecord(operation)
		if !bindingOK || !recordOK || operation.Status != "succeeded" || operation.ID != ref || binding.Stage != stage ||
			binding.LaunchOperationID != input.Binding.LaunchOperationID || binding.AccountID != input.Binding.AccountID ||
			binding.WorkspaceID != input.Binding.WorkspaceID || record.ProviderProfileRef != input.ProviderProfileRef || record.ProviderBindingRef != input.ProviderBindingRef || record.SpecDigest != input.SpecDigest ||
			workspaceLaunchStageBindingRef(stage, record.Resources) != ref || !workspaceLaunchResourcesContain(input.Resources, record.Resources) {
			return WorkspaceLaunchProviderRequest{}, ErrLaunchStageBindingConflict
		}
		request.Prior[stage] = record
	}
	return request, nil
}

func (e *launchStageEngine) providerOperationContext(ctx context.Context, operation FabricOperation, readOnly bool) context.Context {
	return withProviderOperationJournal(
		ctx,
		operation,
		readOnly,
		e.providerMutations,
		e.machineOwnership,
		e.providerDescriptor.Descriptor().Name,
		e.now,
	)
}

func (e *launchStageEngine) persistWorkspaceLaunchStageDiagnostic(ctx context.Context, operation FabricOperation, diagnostic *WorkspaceLaunchStageDiagnostic) (FabricOperation, error) {
	if diagnostic == nil {
		return operation, nil
	}
	if current, ok := decodeWorkspaceLaunchStageDiagnostic(operation); ok && hashInput(current) == hashInput(*diagnostic) {
		return operation, nil
	}
	next := operation
	setWorkspaceLaunchStageDiagnostic(&next, diagnostic)
	if err := e.stages.ConvergeStageDiagnostic(ctx, operation, next); err != nil {
		return FabricOperation{}, err
	}
	return next, nil
}

func (e *launchStageEngine) persistWorkspaceLaunchStageResult(ctx context.Context, input WorkspaceLaunchStageInput, current FabricOperation, record workspaceLaunchStageRecord, result WorkspaceLaunchProviderResult) error {
	next := current
	next.RedactedProviderPayload = maps.Clone(current.RedactedProviderPayload)
	next.Status, next.ErrorCode, next.Retryable, next.FinishedAt = "succeeded", "", false, e.now()
	record.Resources, record.ProviderState = result.Resources, append(json.RawMessage(nil), result.ProviderState...)
	if input.RuntimeImageRevision != nil {
		revision := *input.RuntimeImageRevision
		record.RuntimeImageRevision = &revision
	}
	setWorkspaceLaunchStageRecord(&next, record)
	setWorkspaceLaunchStageDiagnostic(&next, result.Diagnostic)
	if current.Status == "started" {
		return e.stages.SaveStageOutcome(ctx, next)
	}
	return e.stages.ConvergeStageReadback(ctx, current, next)
}

func (e *launchStageEngine) failWorkspaceLaunchStage(ctx context.Context, current FabricOperation, err error) {
	if current.Status != "started" {
		return
	}
	next := current
	next.Status, next.ErrorCode, next.Retryable, next.FinishedAt = "failed", errorCode(err), false, e.now()
	_ = e.stages.SaveStageOutcome(ctx, next)
}

func (e *launchStageEngine) EnsureWorkspaceLaunchStage(ctx context.Context, input WorkspaceLaunchStageInput) (WorkspaceLaunchStageResult, error) {
	if err := e.validateWorkspaceLaunchStageInput(ctx, input); err != nil {
		return WorkspaceLaunchStageResult{}, err
	}
	if input.Binding.Stage == "secret" && (input.GatewayCredential == nil || input.GatewayCredential.KeyID <= 0) {
		return WorkspaceLaunchStageResult{}, ErrWorkspaceLaunchInputInvalid
	}
	if e.provider == nil {
		return WorkspaceLaunchStageResult{}, ErrWorkspaceLaunchUnavailable
	}
	providerName := e.providerDescriptor.Descriptor().Name
	if input.RuntimeImageRevision != nil {
		existing, getErr := e.stages.Get(ctx, input.Binding.FabricOperationID)
		if getErr != nil {
			return WorkspaceLaunchStageResult{}, ErrLaunchStageBindingConflict
		}
		if _, matches := workspaceLaunchStageOperationMatches(existing, input, providerName); !matches {
			return WorkspaceLaunchStageResult{}, ErrLaunchStageBindingConflict
		}
	}
	operation, record, err := newWorkspaceLaunchStageOperation(input, providerName, e.now)
	if err != nil {
		return WorkspaceLaunchStageResult{}, err
	}
	if existing, found, lookupErr := e.stages.OperationByActionIdempotency(ctx, input.Binding.Action, input.Binding.IdempotencyKey); lookupErr != nil {
		return WorkspaceLaunchStageResult{}, lookupErr
	} else if found && existing.Status == "failed" {
		if existingRecord, matches := workspaceLaunchStageOperationMatches(existing, input, providerName); !matches {
			return WorkspaceLaunchStageResult{}, ErrLaunchStageBindingConflict
		} else {
			observed, readErr := e.readWorkspaceLaunchStage(ctx, input, existing, existingRecord)
			if readErr != nil || !workspaceLaunchStageMayContinueEnsure(input, observed) {
				return observed, readErr
			}
		}
	}
	stored, claimed, err := e.stages.ClaimStageOperation(ctx, operation)
	if err != nil {
		return WorkspaceLaunchStageResult{}, err
	}
	record, ok := workspaceLaunchStageOperationMatches(stored, input, providerName)
	if !ok {
		return WorkspaceLaunchStageResult{}, ErrLaunchStageBindingConflict
	}
	if stored.Status == "succeeded" {
		observed, readErr := e.readWorkspaceLaunchStage(ctx, input, stored, record)
		if readErr != nil || input.Binding.Stage != "ensure_compute_allocation" ||
			observed.State != "pending" || observed.Reason != "ownership_pending" {
			return observed, readErr
		}
	}
	if !claimed {
		observed, readErr := e.readWorkspaceLaunchStage(ctx, input, stored, record)
		if readErr != nil || !workspaceLaunchStageMayContinueEnsure(input, observed) {
			return observed, readErr
		}
		if observed.Diagnostic != nil {
			current, getErr := e.stages.Get(ctx, input.Binding.FabricOperationID)
			if getErr != nil {
				return WorkspaceLaunchStageResult{}, getErr
			}
			var matches bool
			if record, matches = workspaceLaunchStageOperationMatches(current, input, providerName); !matches {
				return WorkspaceLaunchStageResult{}, ErrLaunchStageBindingConflict
			}
			stored = current
		}
	}
	request, err := e.WorkspaceLaunchProviderRequest(ctx, input, record)
	if err != nil {
		return WorkspaceLaunchStageResult{}, err
	}
	providerResult, err := e.provider.EnsureWorkspaceLaunchStage(e.providerOperationContext(ctx, stored, false), request)
	diagnostic, diagnosticErr := workspaceLaunchStageDiagnosticAt(providerResult.Diagnostic, e.now())
	if diagnosticErr != nil {
		return WorkspaceLaunchStageResult{}, diagnosticErr
	}
	providerResult.Diagnostic = diagnostic
	if err != nil && providerResult.Diagnostic != nil {
		stored, diagnosticErr = e.persistWorkspaceLaunchStageDiagnostic(ctx, stored, providerResult.Diagnostic)
		if diagnosticErr != nil {
			return WorkspaceLaunchStageResult{}, diagnosticErr
		}
	}
	if errors.Is(err, ErrWorkspaceLaunchRuntimeImageRevisionRequired) {
		return pendingWorkspaceLaunchStageResult(input, "runtime_image_revision_required", providerResult.Diagnostic), nil
	}
	if errors.Is(err, ErrWorkspaceLaunchOwnershipPending) {
		return pendingWorkspaceLaunchStageResult(input, "ownership_pending", providerResult.Diagnostic), nil
	}
	if errors.Is(err, ErrWorkspaceLaunchPending) {
		if input.RuntimeImageRevision != nil {
			return pendingWorkspaceLaunchStageResult(input, "provider_provisioning", providerResult.Diagnostic), nil
		}
		return pendingWorkspaceLaunchStageResult(input, stored.ErrorCode, providerResult.Diagnostic), nil
	}
	if err != nil {
		e.failWorkspaceLaunchStage(ctx, stored, err)
		return WorkspaceLaunchStageResult{}, err
	}
	if !validWorkspaceLaunchProviderResult(input, providerResult) {
		err = ErrWorkspaceLaunchUnavailable
		e.failWorkspaceLaunchStage(ctx, stored, err)
		return WorkspaceLaunchStageResult{}, err
	}
	if err := e.persistWorkspaceLaunchStageResult(ctx, input, stored, record, providerResult); err != nil {
		latest, getErr := e.stages.Get(ctx, input.Binding.FabricOperationID)
		if getErr != nil || latest.Status != "succeeded" {
			return WorkspaceLaunchStageResult{}, err
		}
	}
	return WorkspaceLaunchStageResult{
		SchemaVersion: 1, State: "ready", Reason: "none", Binding: input.Binding, Resources: providerResult.Resources,
		Diagnostic: providerResult.Diagnostic,
	}, nil
}

func (e *launchStageEngine) ReadWorkspaceLaunchStage(ctx context.Context, input WorkspaceLaunchStageInput) (WorkspaceLaunchStageResult, error) {
	if err := e.validateWorkspaceLaunchStageInput(ctx, input); err != nil {
		return WorkspaceLaunchStageResult{}, err
	}
	operation, err := e.stages.Get(ctx, input.Binding.FabricOperationID)
	if errors.Is(err, ErrOperationNotFound) {
		return observedWorkspaceLaunchStageResult(input, "absent", "no_stage_record", nil), nil
	}
	if err != nil {
		return WorkspaceLaunchStageResult{}, err
	}
	record, ok := workspaceLaunchStageOperationMatches(operation, input, e.providerDescriptor.Descriptor().Name)
	if !ok {
		return WorkspaceLaunchStageResult{}, ErrLaunchStageBindingConflict
	}
	return e.readWorkspaceLaunchStage(ctx, input, operation, record)
}

func (e *launchStageEngine) readWorkspaceLaunchStage(ctx context.Context, input WorkspaceLaunchStageInput, operation FabricOperation, record workspaceLaunchStageRecord) (WorkspaceLaunchStageResult, error) {
	if e.provider == nil {
		return WorkspaceLaunchStageResult{}, ErrWorkspaceLaunchUnavailable
	}
	request, err := e.WorkspaceLaunchProviderRequest(ctx, input, record)
	if err != nil {
		return WorkspaceLaunchStageResult{}, err
	}
	providerResult, err := e.provider.ReadWorkspaceLaunchStage(e.providerOperationContext(ctx, operation, true), request)
	diagnostic, diagnosticErr := workspaceLaunchStageDiagnosticAt(providerResult.Diagnostic, e.now())
	if diagnosticErr != nil {
		return WorkspaceLaunchStageResult{}, diagnosticErr
	}
	providerResult.Diagnostic = diagnostic
	if err != nil && providerResult.Diagnostic != nil {
		operation, diagnosticErr = e.persistWorkspaceLaunchStageDiagnostic(ctx, operation, providerResult.Diagnostic)
		if diagnosticErr != nil {
			return WorkspaceLaunchStageResult{}, diagnosticErr
		}
	}
	if errors.Is(err, ErrWorkspaceLaunchRuntimeImageRevisionRequired) {
		return pendingWorkspaceLaunchStageResult(input, "runtime_image_revision_required", providerResult.Diagnostic), nil
	}
	if errors.Is(err, ErrWorkspaceLaunchOwnershipPending) {
		return pendingWorkspaceLaunchStageResult(input, "ownership_pending", providerResult.Diagnostic), nil
	}
	if errors.Is(err, ErrWorkspaceLaunchResourceAbsent) {
		if operation.Status == "started" {
			return observedWorkspaceLaunchStageResult(input, "absent", "started_no_resource", providerResult.Diagnostic), nil
		}
		if operation.Status == "failed" {
			return observedWorkspaceLaunchStageResult(input, "absent", "failed_no_resource", providerResult.Diagnostic), nil
		}
		return observedWorkspaceLaunchStageResult(input, "unknown", "resource_absence_status_conflict", providerResult.Diagnostic), nil
	}
	if errors.Is(err, ErrWorkspaceLaunchPending) {
		if input.RuntimeImageRevision != nil {
			return pendingWorkspaceLaunchStageResult(input, "provider_provisioning", providerResult.Diagnostic), nil
		}
		if operation.Status == "started" {
			return pendingWorkspaceLaunchStageResult(input, "provider_provisioning", providerResult.Diagnostic), nil
		}
		return observedWorkspaceLaunchStageResult(input, "unknown", "failed_no_resource_unproven", providerResult.Diagnostic), nil
	}
	if err != nil {
		return WorkspaceLaunchStageResult{}, err
	}
	if !validWorkspaceLaunchProviderResult(input, providerResult) {
		return WorkspaceLaunchStageResult{}, ErrWorkspaceLaunchUnavailable
	}
	if operation.Status != "succeeded" {
		if err := e.persistWorkspaceLaunchStageResult(ctx, input, operation, record, providerResult); err != nil {
			return WorkspaceLaunchStageResult{}, err
		}
	} else if !workspaceLaunchResourcesContain(providerResult.Resources, record.Resources) || !workspaceLaunchResourcesContain(record.Resources, providerResult.Resources) {
		return WorkspaceLaunchStageResult{}, ErrLaunchStageBindingConflict
	} else if providerResult.Diagnostic != nil {
		if _, err := e.persistWorkspaceLaunchStageDiagnostic(ctx, operation, providerResult.Diagnostic); err != nil {
			return WorkspaceLaunchStageResult{}, err
		}
	}
	return WorkspaceLaunchStageResult{
		SchemaVersion: 1, State: "ready", Reason: "none", Binding: input.Binding, Resources: providerResult.Resources,
		Diagnostic: providerResult.Diagnostic,
	}, nil
}
