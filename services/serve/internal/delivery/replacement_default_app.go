package delivery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/publicjson"
)

func replacementSelection(body *api.UpdateWorkspaceVersionRequest) (*api.WorkspaceApplicationSelection, error) {
	if body == nil {
		return nil, status.Error(codes.InvalidArgument, "an update version body is required")
	}
	selection := body.GetApplicationSelection()
	if selection != nil {
		if body.CapabilityVersionId != nil {
			return nil, status.Error(codes.InvalidArgument, "application selection and capability version are mutually exclusive")
		}
	} else if body.CapabilityVersionId != nil {
		selection = &api.WorkspaceApplicationSelection{Kind: api.WorkspaceApplicationSelectionKindEnum_WORKSPACE_APPLICATION_SELECTION_KIND_ENUM_AGENT, CapabilityVersionId: body.CapabilityVersionId}
	}
	if err := contracts.ValidateWorkspaceApplicationSelection(selection); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return selection, nil
}

func (s *Service) replacementSource(ctx context.Context, call *api.CallContext, selection *api.WorkspaceApplicationSelection) (replacementPlan, error) {
	var plan replacementPlan
	if contracts.IsDefaultOPLAppSelection(selection) {
		release, err := s.runtimeRelease(ctx, nextOwnerCall(call), selection.GetRuntimeVersionId())
		if err != nil {
			return plan, err
		}
		contract := release.GetPublisherContract()
		// This path executes compatible application replacements. Publisher migrations
		// and backups need their own actual execution evidence, never a boolean claim.
		if contract.GetData().GetUpgrade().GetMode() != api.DataUpgradeContractModeEnum_DATA_UPGRADE_CONTRACT_MODE_ENUM_COMPATIBLE || contract.GetData().GetUpgrade().GetBackupRequired() {
			return plan, status.Errorf(codes.FailedPrecondition, "%s: publisher migration or backup execution is required", ReasonDataCompatibilityUnproven)
		}
		descriptor := &api.DeploymentDescriptor{
			SchemaVersion: api.DeploymentDescriptorSchemaVersionEnum_DEPLOYMENT_DESCRIPTOR_SCHEMA_VERSION_ENUM_OPL_DEPLOYMENT_DESCRIPTOR_V1,
			Artifact:      contract.GetImage(), Provenance: api.DeploymentDescriptorProvenanceEnum_DEPLOYMENT_DESCRIPTOR_PROVENANCE_ENUM_RUNTIME_RELEASE,
			ApplicationSelection: selection, ApplicationRevision: contract.GetApplicationRevisionTemplate(), RuntimeContract: contract,
		}
		if _, err = deploymentRevision(&api.RuntimeDeployCommand{DeploymentDescriptor: descriptor}); err != nil {
			return plan, err
		}
		if descriptor.GetApplicationRevision().GetImage() != contract.GetImage().GetRepository()+"@"+contract.GetImage().GetDigest() || contract.GetData().GetSchemaVersion() == "" {
			return plan, status.Error(codes.FailedPrecondition, "Runtime Release has inconsistent image or missing data contract")
		}
		digest, err := descriptorDigest(descriptor)
		if err != nil {
			return plan, err
		}
		compatibility, err := runtimeReplacementCompatibility(contract)
		return replacementPlan{runtimeVersionID: release.GetId(), descriptor: descriptor, descriptorDigest: digest,
			descriptorRef: "runtime-release:" + release.GetId() + "@" + digest, compatibility: compatibility}, err
	}
	if s.Capability == nil {
		return plan, status.Error(codes.Unavailable, "Capability is not configured")
	}
	version, err := s.Capability.GetCapabilityVersion(ctx, &api.GetCapabilityVersionRpcRequest{Context: nextOwnerCall(call), CapabilityVersionId: selection.GetCapabilityVersionId()})
	if err != nil {
		return plan, err
	}
	if version.GetId() != selection.GetCapabilityVersionId() || version.GetStatus() != api.CapabilityVersionStatusEnum_CAPABILITY_VERSION_STATUS_ENUM_READY || version.GetDeploymentDescriptor() == nil {
		return plan, status.Errorf(codes.FailedPrecondition, "%s: Capability did not confirm the requested version as deployable", ReasonReplacementUnavailable)
	}
	compatibility, err := compatibilityJSON(version.GetDataCompatibility())
	return replacementPlan{capabilityVersionID: version.GetId(), descriptor: version.GetDeploymentDescriptor(), descriptorDigest: version.GetDeploymentDescriptorDigest(), descriptorRef: version.GetDeploymentDescriptorObjectRef(), compatibility: compatibility}, err
}

func runtimeReplacementCompatibility(contract *api.RuntimePublisherContract) ([]byte, error) {
	data := contract.GetData()
	if data.GetSchemaVersion() == "" {
		return nil, status.Errorf(codes.FailedPrecondition, "%s: Runtime Release declares no data schema", ReasonDataCompatibilityUnproven)
	}
	return compatibilityJSON(&api.DataCompatibility{DataSchemaVersion: data.GetSchemaVersion(), CompatibleFromVersions: data.GetUpgrade().GetCompatibleFromSchemaVersions(), RollbackSafe: data.GetRollback().GetSafe(), MigrationRequired: data.GetUpgrade().GetMode() == api.DataUpgradeContractModeEnum_DATA_UPGRADE_CONTRACT_MODE_ENUM_PUBLISHER_MIGRATION})
}

// Historical default App starts omitted RuntimeContract from the descriptor and
// recorded empty DataCompatibility. Resolve only that exact immutable source;
// its current admission status does not change the schema it already wrote.
func (s *Service) replacementCurrentData(ctx context.Context, plan *replacementPlan, current *currentDelivery) error {
	descriptor := &api.DeploymentDescriptor{}
	if err := publicjson.Unmarshal(current.Descriptor, descriptor); err != nil {
		return status.Error(codes.DataLoss, "current application descriptor is invalid")
	}
	if plan.runtimeVersionID != "" && !contracts.IsDefaultOPLAppSelection(descriptor.GetApplicationSelection()) {
		return status.Errorf(codes.FailedPrecondition, "%s: a Default App upgrade requires an existing Default App", ReasonReplacementUnavailable)
	}
	if descriptor.GetProvenance() != api.DeploymentDescriptorProvenanceEnum_DEPLOYMENT_DESCRIPTOR_PROVENANCE_ENUM_RUNTIME_RELEASE {
		return nil
	}
	contract := descriptor.GetRuntimeContract()
	if contract == nil {
		if s.RuntimeReleases == nil {
			return status.Error(codes.Unavailable, "Runtime Control is not configured")
		}
		sourceID, cursor := descriptor.GetApplicationSelection().GetRuntimeVersionId(), ""
		if sourceID == "" {
			return status.Error(codes.DataLoss, "current App has no frozen Runtime Release identity")
		}
		for contract == nil {
			page, err := s.RuntimeReleases.ListRuntimeVersions(ctx, &api.ListRuntimeVersionsRpcRequest{Context: nextOwnerCall(plan.call), QueryCursor: &cursor})
			if err != nil {
				return err
			}
			for _, release := range page.GetItems() {
				if release.GetId() == sourceID {
					contract = release.GetPublisherContract()
					break
				}
			}
			if contract != nil {
				break
			}
			if page.GetNextCursor() == "" {
				return status.Errorf(codes.FailedPrecondition, "%s: original Runtime Release data contract is unavailable", ReasonDataCompatibilityUnproven)
			}
			cursor = page.GetNextCursor()
		}
	}
	if !proto.Equal(contract.GetImage(), descriptor.GetArtifact()) || !proto.Equal(contract.GetApplicationRevisionTemplate(), descriptor.GetApplicationRevision()) {
		return status.Error(codes.DataLoss, "original Runtime Release differs from the frozen application")
	}
	compatibility, err := runtimeReplacementCompatibility(contract)
	if err != nil {
		return err
	}
	descriptor.RuntimeContract = contract
	current.Descriptor, err = publicjson.Marshal(descriptor)
	if err != nil {
		return err
	}
	current.Compatibility = compatibility
	if plan.runtimeVersionID != "" && descriptor.GetApplicationRevision().GetApplicationId() != plan.descriptor.GetApplicationRevision().GetApplicationId() {
		return status.Errorf(codes.FailedPrecondition, "%s: a Default App update must retain its application data identity", ReasonDataCompatibilityUnproven)
	}
	if plan.runtimeVersionID != "" {
		// The credential generation is inherited by the existing runtime lineage.
		// Its names, paths and login identity must remain the same ABI, otherwise
		// the retained password/session files would no longer name the same facts.
		old := descriptor.GetApplicationRevision().GetCredentials()
		next := plan.descriptor.GetApplicationRevision().GetCredentials()
		if len(old) != len(next) {
			return status.Error(codes.FailedPrecondition, "Default App upgrade must retain the original credential declarations")
		}
		byName := map[string]*api.WorkspaceApplicationCredential{}
		for _, credential := range old {
			byName[credential.GetName()] = credential
		}
		for _, credential := range next {
			if !proto.Equal(byName[credential.GetName()], credential) {
				return status.Error(codes.FailedPrecondition, "Default App upgrade must retain credential names, kinds, targets and login identity")
			}
		}
	}
	return nil
}

// prepareReplacementState uses only the exact predecessor's frozen execution and
// confirmed reload record. It neither creates a Key nor binds a new Secret.
func (s *Service) prepareReplacementState(ctx context.Context, q rowQueryer, plan *replacementPlan, current *currentDelivery) error {
	if err := s.replacementCurrentData(ctx, plan, current); err != nil {
		return err
	}
	var raw []byte
	if err := q.QueryRowContext(ctx, `SELECT input_snapshot FROM serve.agent_runtime_actions WHERE command_id=$1 AND runtime_instance_id=$2 AND expected_deployment_id=$3`, stableID("start_", current.DeploymentID), current.RuntimeID, current.DeploymentID).Scan(&raw); err != nil {
		return status.Error(codes.FailedPrecondition, "original application execution identity is unavailable")
	}
	command, _, err := decodeRuntimeStartSnapshot(raw)
	if err != nil {
		return err
	}
	if command.GetWorkspaceId() != plan.workspaceID || command.GetDeploymentId() != current.DeploymentID || command.GetRuntimeInstanceId() != current.RuntimeID || command.GetResourceSetId() != current.ResourceSetID || command.GetDataAttachmentId() != current.DataAttachment {
		return status.Error(codes.DataLoss, "original application resource identity mismatch")
	}
	inherited := proto.Clone(command).(*api.RuntimeDeployCommand)
	var applied int64
	if err = q.QueryRowContext(ctx, `SELECT applied_model_configuration_version FROM serve.agent_runtime_instances WHERE id=$1 AND deployment_id=$2`, current.RuntimeID, current.DeploymentID).Scan(&applied); err != nil {
		return dbError(err)
	}
	var reload map[string]string
	err = q.QueryRowContext(ctx, `SELECT input_snapshot FROM serve.agent_runtime_actions WHERE runtime_instance_id=$1 AND expected_deployment_id=$2 AND action='reload' AND observation_result='confirmed' ORDER BY created_at DESC,id DESC LIMIT 1`, current.RuntimeID, current.DeploymentID).Scan(&raw)
	if err == nil {
		if json.Unmarshal(raw, &reload) != nil {
			return status.Error(codes.DataLoss, "confirmed predecessor reload is invalid")
		}
		version, parseErr := strconv.ParseInt(reload["targetVersion"], 10, 64)
		if parseErr != nil || version <= 0 || version != applied {
			return status.Error(codes.DataLoss, "confirmed predecessor reload does not match its applied model version")
		}
		// Existing reload records retain a digest, not the model selections. Never
		// silently revert a customer's later models to the initial start values.
		if reload["selections"] != reloadSelectionDigest(inherited.GetModelSelections()) {
			return status.Error(codes.FailedPrecondition, "current model selections cannot be reconstructed from the stored reload digest")
		}
		inherited.ModelConfigurationVersion = version
	} else if !errors.Is(err, sql.ErrNoRows) {
		return dbError(err)
	} else if applied != inherited.GetModelConfigurationVersion() {
		return status.Error(codes.DataLoss, "predecessor applied model version has no confirmed execution snapshot")
	}
	credential, declared, err := declaredGatewayCredential(&api.RuntimeDeployCommand{DeploymentDescriptor: plan.descriptor})
	if err != nil {
		return err
	}
	if !declared {
		inherited.ManagedKeyBinding = nil
	} else {
		binding := inherited.GetManagedKeyBinding()
		if binding == nil || binding.GetKeyBindingId() == "" || binding.GetSecretBindingId() == "" || binding.GetSecretVersion() == "" || binding.GetFingerprint() == "" || binding.GetSecretDeliveryReference() != contracts.WorkspaceGatewaySecretRef(plan.workspaceID) || binding.GetTargetSlot() != credential.Name {
			return status.Errorf(codes.FailedPrecondition, "%s: original confirmed Gateway binding does not match the target declaration", ReasonManagedKeyUnavailable)
		}
		if reload != nil {
			if strings.TrimSpace(reload["keyBinding"]) == "" || strings.TrimSpace(reload["fingerprint"]) == "" || strings.TrimSpace(reload["secretBindingId"]) == "" || strings.TrimSpace(reload["secretVersion"]) == "" {
				return status.Error(codes.DataLoss, "confirmed predecessor reload has no complete Gateway binding")
			}
			binding.KeyBindingId, binding.Fingerprint = reload["keyBinding"], reload["fingerprint"]
			binding.SecretBindingId, binding.SecretVersion = reload["secretBindingId"], reload["secretVersion"]
		}
	}
	plan.inherited = inherited
	return nil
}

func replacementPredecessor(ctx context.Context, q rowQueryer, deploymentID, workspaceID string) (*currentDelivery, error) {
	var previous string
	if err := q.QueryRowContext(ctx, `SELECT previous_deployment_id FROM serve.agent_deployments WHERE id=$1 AND workspace_id=$2`, deploymentID, workspaceID).Scan(&previous); err != nil {
		return nil, dbError(err)
	}
	current, err := existingReplacementTx(ctx, q, previous, workspaceID)
	if err == nil && current == nil {
		err = status.Error(codes.DataLoss, "replacement predecessor is unavailable")
	}
	return current, err
}

func (s *Service) validateReplacementReplay(ctx context.Context, q rowQueryer, plan replacementPlan, replay *currentDelivery) error {
	var previous, capability, runtime string
	if err := q.QueryRowContext(ctx, `SELECT previous_deployment_id,capability_version_id,COALESCE(runtime_version_id,'') FROM serve.agent_deployments WHERE id=$1 AND workspace_id=$2`, replay.DeploymentID, plan.workspaceID).Scan(&previous, &capability, &runtime); err != nil {
		return dbError(err)
	}
	descriptor := &api.DeploymentDescriptor{}
	if publicjson.Unmarshal(replay.Descriptor, descriptor) != nil {
		return status.Error(codes.DataLoss, "replacement descriptor is invalid")
	}
	if previous != plan.expectedCurrent || capability != plan.capabilityVersionID || runtime != plan.runtimeVersionID || !proto.Equal(descriptor, plan.descriptor) {
		return status.Error(codes.AlreadyExists, "replacement input differs from its original command")
	}
	return nil
}

// The predecessor decision is part of this accepted operation, before provider
// calls. Recovery never reselects credentials from a later predecessor reload.
type replacementInput struct {
	reservationInput
	ReplacementState json.RawMessage `json:"replacementState"`
	ExclusiveWriter  bool            `json:"exclusiveWriter"`
}

func restoreReplacementState(ctx context.Context, q rowQueryer, operationID string, plan *replacementPlan, current *currentDelivery) error {
	var raw []byte
	if err := q.QueryRowContext(ctx, `SELECT accepted_input FROM serve.operations WHERE id=$1`, operationID).Scan(&raw); err != nil {
		return dbError(err)
	}
	var input replacementInput
	if json.Unmarshal(raw, &input) != nil || len(input.ReplacementState) == 0 {
		return status.Error(codes.DataLoss, "replacement has no frozen predecessor state")
	}
	command := &api.RuntimeDeployCommand{}
	if protojson.Unmarshal(input.ReplacementState, command) != nil || command.GetWorkspaceId() != plan.workspaceID || command.GetDeploymentId() != current.DeploymentID || command.GetRuntimeInstanceId() != current.RuntimeID || command.GetResourceSetId() != current.ResourceSetID || command.GetDataAttachmentId() != current.DataAttachment {
		return status.Error(codes.DataLoss, "replacement predecessor state does not match its resource identity")
	}
	plan.inherited, plan.exclusive = command, input.ExclusiveWriter
	return nil
}

func (s *Service) restoreReplacementSource(ctx context.Context, plan *replacementPlan) (bool, error) {
	var capability, runtime, previous string
	var raw []byte
	err := s.DB.QueryRowContext(ctx, `SELECT d.capability_version_id,COALESCE(d.runtime_version_id,''),d.previous_deployment_id,i.deployment_descriptor,i.deployment_descriptor_digest,i.deployment_descriptor_object_ref,d.data_compatibility
 FROM serve.operations o JOIN serve.agent_deployments d ON d.id=o.resource_id JOIN serve.agent_runtime_instances i ON i.deployment_id=d.id
 WHERE o.id=$1 AND d.workspace_id=$2`, replacementOperationID(*plan), plan.workspaceID).Scan(&capability, &runtime, &previous, &raw, &plan.descriptorDigest, &plan.descriptorRef, &plan.compatibility)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, dbError(err)
	}
	if capability != plan.capabilityVersionID || runtime != plan.runtimeVersionID || previous != plan.expectedCurrent {
		return true, status.Error(codes.AlreadyExists, "replacement input differs from its original command")
	}
	plan.descriptor = &api.DeploymentDescriptor{}
	if publicjson.Unmarshal(raw, plan.descriptor) != nil {
		return true, status.Error(codes.DataLoss, "frozen replacement descriptor is invalid")
	}
	digest, err := descriptorDigest(plan.descriptor)
	if err != nil || digest != plan.descriptorDigest {
		return true, status.Error(codes.DataLoss, "frozen replacement descriptor digest mismatch")
	}
	return true, nil
}
