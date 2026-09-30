package delivery

// ServeRuntimeAdapter is Serve's own execution-adapter surface.
//
// The typed contract decomposes Serve's delivery steps: Serve reserves delivery
// identity, then executes and observes it through this adapter. The adapter holds
// no delivery state of its own; it executes or observes the exact original
// command through the configured TKE execution adapter and reports only what the
// provider actually observed. Persisting a delivery fact stays with the delivery
// step that owns it, so this bridge cannot become a second writer of Serve's own
// runtime records.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	api "opl-cloud/packages/contracts/go/api"
)

// ReasonRuntimeAdapterCapabilityUnavailable names an execution-adapter capability
// this Serve slice does not implement. It is never reported as a success.
const ReasonRuntimeAdapterCapabilityUnavailable = "runtime_adapter_capability_unavailable"

// StartRuntime executes the already reserved deployment through the execution
// adapter. A successful start acknowledges the request; it never claims readiness,
// which only ObserveRuntime may report.
func (s *Service) StartRuntime(ctx context.Context, command *api.RuntimeDeployCommand) (*api.RuntimeReadback, error) {
	if err := requireServePeer(ctx); err != nil {
		return nil, err
	}
	observation, err := s.executeRuntimeStep(ctx, command, true)
	if err != nil {
		return nil, err
	}
	return runtimeObservationReadback(command, observation), nil
}

// ObserveRuntime reports the execution adapter's own whole-runtime observation for
// the exact original command. It never derives readiness from a persisted pointer.
func (s *Service) ObserveRuntime(ctx context.Context, request *api.RuntimeReadbackRequest) (*api.RuntimeReadback, error) {
	if err := requireServePeer(ctx); err != nil {
		return nil, err
	}
	return s.runtimeReadback(ctx, request.GetRuntimeInstanceId(), request.GetDeploymentId())
}

// StopRuntime retires the exact original runtime through the installation's
// lifecycle boundary. It resolves the persisted original command from Serve's own
// store, so a lost caller response retires the same runtime rather than a new one.
func (s *Service) StopRuntime(ctx context.Context, command *api.RuntimeStopCommand) (*api.Operation, error) {
	if err := requireServePeer(ctx); err != nil {
		return nil, err
	}
	if s.Runtime == nil {
		return nil, status.Error(codes.Unavailable, "runtime execution adapter is not configured")
	}
	runtimeID := strings.TrimSpace(command.GetRuntimeInstanceId())
	if runtimeID == "" {
		return nil, status.Error(codes.InvalidArgument, "runtime instance is required")
	}
	deploy, err := s.persistedRuntimeCommand(ctx, runtimeID, command.GetDeploymentId())
	if err != nil {
		return nil, err
	}
	target, err := s.confirmedRuntimeTarget(ctx, deploy.command)
	if err != nil {
		return nil, err
	}
	if err := s.applyRecordedLifecycle(ctx, deploy, "stop", map[string]string{"desired": "suspended"}, func() error {
		return s.Runtime.Lifecycle(ctx, deploy.command, target, "suspended")
	}); err != nil {
		return nil, err
	}
	// The provider confirmed the suspended state, so the retire action completes.
	return s.runtimeLifecycleOperation(ctx, deploy, "runtime_stop", "runtime", "succeeded")
}

// ReloadRuntime applies a new model configuration to the exact persisted runtime
// through the frozen publisher interface and records the version the application
// itself read back. The requested version is never written as the applied one: the
// stored applied version advances only by a compare-and-set that the readback
// confirmed, so a failed or unconfirmed reload leaves the last confirmed
// configuration exactly as it was.
func (s *Service) ReloadRuntime(ctx context.Context, command *api.RuntimeReloadCommand) (*api.Operation, error) {
	if err := requireServePeer(ctx); err != nil {
		return nil, err
	}
	if s.Runtime == nil {
		return nil, status.Error(codes.Unavailable, "runtime execution adapter is not configured")
	}
	runtimeID := strings.TrimSpace(command.GetRuntimeInstanceId())
	if runtimeID == "" || command.GetTargetVersion() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "runtime instance and a positive target version are required")
	}
	deploy, err := s.persistedRuntimeCommand(ctx, runtimeID, "")
	if err != nil {
		return nil, err
	}
	if current := deploy.appliedModelVersion; current != command.GetExpectedAppliedVersion() {
		return nil, status.Errorf(codes.FailedPrecondition, "runtime applied model configuration is %d, not the expected version", current)
	}
	reload := proto.Clone(deploy.command).(*api.RuntimeDeployCommand)
	reload.ModelConfigurationVersion = command.GetTargetVersion()
	reload.ModelSelections = command.GetSelections()
	target, err := s.confirmedRuntimeTarget(ctx, reload)
	if err != nil {
		return nil, err
	}
	return s.reloadModelConfiguration(ctx, deploy, reload, target, command)
}

// reloadModelConfiguration records the reload intent for one target version, applies
// it through the execution boundary, and records the version the application read
// back together with the operation that reports it. The action and the operation are
// keyed by the target version, so each configuration version has one durable intent
// and one durable result while a retry of the same version resumes its own.
//
// The applied version is written by the readback alone: the compare-and-set requires
// the exact version the caller expected, so a reload that the application did not
// confirm leaves the runtime's last confirmed configuration untouched.
func (s *Service) reloadModelConfiguration(ctx context.Context, deploy *persistedRuntime, reload *api.RuntimeDeployCommand, target ExecutionTarget, command *api.RuntimeReloadCommand) (*api.Operation, error) {
	targetVersion := strconv.FormatInt(command.GetTargetVersion(), 10)
	snapshot, err := json.Marshal(map[string]string{
		"desired": "running", "targetVersion": targetVersion,
		"selections": reloadSelectionDigest(reload.GetModelSelections()),
	})
	if err != nil {
		return nil, status.Error(codes.Internal, "cannot record the runtime reload")
	}
	actionID := stableID("act_", "reload", deploy.command.GetRuntimeInstanceId(), deploy.operationID, targetVersion)
	if _, err = s.recordRuntimeAction(ctx, actionID, deploy.command.GetRuntimeInstanceId(), "reload", deploy.command.GetDeploymentId(), snapshot); err != nil {
		return nil, err
	}
	appliedVersion, err := s.Runtime.Reload(ctx, reload, target)
	if err != nil {
		return nil, err
	}
	if appliedVersion <= 0 {
		return nil, status.Error(codes.Internal, "the execution boundary reported no applied model configuration version")
	}
	if err = s.recordAppliedModelConfiguration(ctx, deploy, actionID, command.GetTargetVersion(), appliedVersion); err != nil {
		return nil, err
	}
	return s.runtimeLifecycleOperation(ctx, deploy, "runtime_reload", "runtime", "succeeded", targetVersion)
}

// recordAppliedModelConfiguration advances the runtime's applied model
// configuration version by compare-and-set and records the confirmed reload action
// in one transaction. The readback version must be the requested one and the stored
// version must be the one the caller expected, so the applied fact is exactly what
// the application reported and never a version a caller asked for.
func (s *Service) recordAppliedModelConfiguration(ctx context.Context, deploy *persistedRuntime, actionID string, expectedVersion, appliedVersion int64) error {
	if appliedVersion != expectedVersion {
		return status.Errorf(codes.FailedPrecondition, "the application reported applied model configuration %d, not the requested %d", appliedVersion, expectedVersion)
	}
	runtimeID := deploy.command.GetRuntimeInstanceId()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return dbError(err)
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `UPDATE serve.agent_runtime_instances SET applied_model_configuration_version=$3,updated_at=now() WHERE id=$1 AND applied_model_configuration_version=$2`, runtimeID, deploy.appliedModelVersion, appliedVersion)
	if err != nil {
		return dbError(err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		return status.Errorf(codes.FailedPrecondition, "runtime applied model configuration is no longer %d", deploy.appliedModelVersion)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE serve.agent_runtime_actions SET observation_result='confirmed',evidence_ref=NULLIF($2,''),updated_at=now() WHERE command_id=$1`, actionID, modelConfigurationEvidence(actionID, appliedVersion)); err != nil {
		return dbError(err)
	}
	return dbError(tx.Commit())
}

// modelConfigurationEvidence is the reload action's evidence identity: the action
// and the version the application itself reported for it.
func modelConfigurationEvidence(actionID string, appliedVersion int64) string {
	return "serve-model-configuration://" + actionID + "/" + strconv.FormatInt(appliedVersion, 10)
}

// ReadApplicationCredentials returns the platform-issued WebUI credential for the
// exact persisted runtime through the installation's credential boundary. The
// value is never persisted, logged or written to an event.
func (s *Service) ReadApplicationCredentials(ctx context.Context, request *api.ReadApplicationCredentialsRequest) (*api.WorkspaceApplicationCredentials, error) {
	if err := requireServePeer(ctx); err != nil {
		return nil, err
	}
	if s.Runtime == nil {
		return nil, status.Error(codes.Unavailable, "runtime execution adapter is not configured")
	}
	runtimeID := strings.TrimSpace(request.GetRuntimeInstanceId())
	if runtimeID == "" {
		return nil, status.Error(codes.InvalidArgument, "runtime instance is required")
	}
	deploy, err := s.persistedRuntimeCommand(ctx, runtimeID, request.GetDeploymentId())
	if err != nil {
		return nil, err
	}
	target, err := s.confirmedRuntimeTarget(ctx, deploy.command)
	if err != nil {
		return nil, err
	}
	credentials, err := s.Runtime.Credentials(ctx, deploy.command, target)
	if err != nil {
		return nil, err
	}
	// The runtime's own credential for the same workspace is the only value served;
	// a mismatch is refused rather than returned.
	if credentials.GetRuntimeInstanceId() != runtimeID || credentials.GetWorkspaceId() != deploy.command.GetWorkspaceId() {
		return nil, status.Error(codes.FailedPrecondition, "runtime credentials do not match the exact runtime")
	}
	return credentials, nil
}

// executeRuntimeStep resolves the exact executable resource binding through the
// Fabric readback and executes or observes the original command. A binding that
// Fabric has not confirmed stops the step before any provider call.
func (s *Service) executeRuntimeStep(ctx context.Context, command *api.RuntimeDeployCommand, start bool) (RuntimeObservation, error) {
	if command == nil || s.Runtime == nil || s.Resources == nil {
		return RuntimeObservation{}, status.Error(codes.Unavailable, "runtime adapter and Fabric readback must be configured")
	}
	if command.GetWorkspaceId() == "" || command.GetResourceSetId() == "" || command.GetDataAttachmentId() == "" {
		return RuntimeObservation{}, status.Error(codes.InvalidArgument, "workspace, resource set and data attachment are required")
	}
	resources, err := s.Resources.ReadResources(ctx, &api.ResourceReadbackRequest{Context: nextOwnerCall(command.GetContext()), ResourceSetId: command.GetResourceSetId()})
	if err != nil {
		return RuntimeObservation{}, err
	}
	target, err := confirmedExecutionTarget(command, resources)
	if err != nil {
		return RuntimeObservation{}, err
	}
	if start {
		if _, err = s.Runtime.Start(ctx, command, target); err != nil {
			return RuntimeObservation{}, err
		}
	}
	return s.Runtime.Observe(ctx, command, target)
}

// runtimeObservationReadback projects one adapter observation into the contract's
// readback shape without persisting it: the caller observes, the delivery step
// records. Readiness is reported only for an observation that is truly ready.
func runtimeObservationReadback(command *api.RuntimeDeployCommand, observation RuntimeObservation) *api.RuntimeReadback {
	readback := &api.RuntimeReadback{
		RuntimeInstanceId: command.GetRuntimeInstanceId(), WorkspaceId: command.GetWorkspaceId(), DeploymentId: command.GetDeploymentId(),
		State: observation.State, Artifact: command.GetDeploymentDescriptor().GetArtifact(),
		AppliedModelConfigurationVersion: observation.AppliedModelConfigurationVersion,
		DeploymentDescriptorDigest:       command.GetDeploymentDescriptorDigest(), ExecutionEpoch: command.GetExecutionEpoch(),
		DeploymentDescriptorObjectRef: command.GetDeploymentDescriptorObjectRef(), Outcome: api.Observation_OBSERVATION_UNKNOWN,
	}
	if !observation.ObservedAt.IsZero() {
		readback.ObservedAt = timestamppb.New(observation.ObservedAt.UTC())
		readback.Outcome = api.Observation_OBSERVATION_CONFIRMED
	}
	if observation.ApplicationEntry != nil {
		entry := &api.WorkspaceApplicationEntry{}
		if observation.ApplicationEntry.ServiceName != "" {
			entry.ServiceName = proto.String(observation.ApplicationEntry.ServiceName)
		}
		if observation.ApplicationEntry.URL != "" {
			entry.Url = proto.String(observation.ApplicationEntry.URL)
		}
		if observation.ApplicationEntry.Port != 0 {
			entry.Port = proto.Int32(int32(observation.ApplicationEntry.Port))
		}
		readback.ApplicationEntry = entry
	}
	if observation.AccessURL != "" {
		readback.AccessUrl = observation.AccessURL
	}
	if observation.State == api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_READY && observation.AccessURL != "" {
		readback.ProcessReady = true
		readback.ApplicationAvailable = true
		readback.ReadinessReceiptId = observation.ReadinessEvidenceRef
	}
	return readback
}

// reloadSelectionDigest identifies the exact model selections one reload applies,
// so a retry of the same action is recognized while a retry that changes the
// selections is refused.
func reloadSelectionDigest(selections []*api.ModelSelection) string {
	raw, _ := json.Marshal(selections)
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}
