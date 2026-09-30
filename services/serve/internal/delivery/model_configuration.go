package delivery

// This file resolves the frozen publisher model configuration interface one
// deployment was admitted against. The interface is a fact of the approved Runtime
// Release the deployment froze, so Serve reads it back from that exact immutable
// identity instead of accepting the interface from a caller or storing a second
// copy of it: a release that declares none leaves the capability absent, and a
// deployment whose frozen release cannot be resolved is refused rather than
// served with an invented interface.

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/serve/internal/tkeapply"
)

// ReasonModelConfigurationUnavailable names the one fact a reload needs and the
// frozen release does not declare: an executable publisher model configuration
// interface. A caller that asked for a model configuration version is refused with
// this reason rather than answered with a success it cannot prove.
const ReasonModelConfigurationUnavailable = "model_configuration_unavailable"

// ReasonModelConfigurationSourceUnresolved names a deployment whose frozen release
// identity cannot be resolved (an Agent whose admitted CapabilityVersion names no
// approved Runtime Release, or a deployment whose source is neither combination).
// The interface is then unprovable, so the reload is refused instead of falling
// back to another release.
const ReasonModelConfigurationSourceUnresolved = "model_configuration_source_unresolved"

// deploymentApplicationSource is the frozen application source one deployment was
// admitted against: exactly one of the two product combinations.
type deploymentApplicationSource struct {
	ApplicationKind     string
	CapabilityVersionID string
	RuntimeVersionID    string
	ArtifactDigest      string
}

// frozenModelConfiguration resolves the publisher's declared model configuration
// interface for the exact deployment a command names. The interface belongs to the
// approved Runtime Release: the default App names it directly, and a built Agent
// names the Dynamic Release its CapabilityVersion was built against.
func (s *Service) frozenModelConfiguration(ctx context.Context, call *api.CallContext, command *api.RuntimeDeployCommand) (tkeapply.ModelConfiguration, error) {
	if command.GetDeploymentId() == "" {
		return tkeapply.ModelConfiguration{}, status.Error(codes.InvalidArgument, "deployment is required to resolve the publisher model configuration")
	}
	source, err := s.deploymentApplicationSource(ctx, command.GetDeploymentId())
	if err != nil {
		return tkeapply.ModelConfiguration{}, err
	}
	releaseID := source.RuntimeVersionID
	if releaseID == "" {
		if source.ApplicationKind != "agent" || source.CapabilityVersionID == "" {
			return tkeapply.ModelConfiguration{}, status.Errorf(codes.FailedPrecondition, "%s: the deployment names no approved Runtime Release", ReasonModelConfigurationSourceUnresolved)
		}
		if s.Capability == nil {
			return tkeapply.ModelConfiguration{}, status.Error(codes.Unavailable, "Capability is not configured")
		}
		version, err := s.Capability.GetCapabilityVersion(ctx, &api.GetCapabilityVersionRpcRequest{Context: call, CapabilityVersionId: source.CapabilityVersionID})
		if err != nil {
			return tkeapply.ModelConfiguration{}, err
		}
		if version.GetId() != source.CapabilityVersionID || version.GetArtifact().GetDigest() != source.ArtifactDigest {
			return tkeapply.ModelConfiguration{}, status.Error(codes.FailedPrecondition, "Capability did not confirm the exact deployed version")
		}
		releaseID = strings.TrimSpace(version.GetRuntimeVersionId())
		if releaseID == "" {
			return tkeapply.ModelConfiguration{}, status.Errorf(codes.FailedPrecondition, "%s: the admitted Agent names no approved Runtime Release", ReasonModelConfigurationSourceUnresolved)
		}
	}
	release, err := s.runtimeRelease(ctx, call, releaseID)
	if err != nil {
		return tkeapply.ModelConfiguration{}, err
	}
	contract := release.GetPublisherContract().GetModelConfiguration()
	if contract == nil {
		return tkeapply.ModelConfiguration{}, nil
	}
	protocol := modelConfigurationProtocol(contract.GetProtocol())
	if protocol == "" {
		// A release that declares a protocol this Cloud has no wire for has no
		// executable interface; the reload reports the absence rather than a guess.
		return tkeapply.ModelConfiguration{}, nil
	}
	return tkeapply.ModelConfiguration{Declared: true, Contract: modelConfigurationContract(contract)}, nil
}

// deploymentApplicationSource reads the frozen application source from Serve's own
// deployment row. The row is the same fact Reserve wrote, so a reload never
// re-derives which of the two combinations this runtime is.
func (s *Service) deploymentApplicationSource(ctx context.Context, deploymentID string) (deploymentApplicationSource, error) {
	var source deploymentApplicationSource
	var capabilityVersion, runtimeVersion sql.NullString
	err := s.DB.QueryRowContext(ctx, `SELECT application_kind, capability_version_id, runtime_version_id, artifact_digest FROM serve.agent_deployments WHERE id=$1`, deploymentID).
		Scan(&source.ApplicationKind, &capabilityVersion, &runtimeVersion, &source.ArtifactDigest)
	if errors.Is(err, sql.ErrNoRows) {
		return deploymentApplicationSource{}, status.Error(codes.NotFound, "deployment not found")
	}
	if err != nil {
		return deploymentApplicationSource{}, dbError(err)
	}
	source.CapabilityVersionID = strings.TrimSpace(capabilityVersion.String)
	source.RuntimeVersionID = strings.TrimSpace(runtimeVersion.String)
	return source, nil
}

// modelConfigurationContract projects the contract's generated binding onto the
// executor's own interface. Only the declared facts travel: the projection adds no
// default and drops no declared field.
func modelConfigurationContract(contract *api.ModelConfigurationContract) tkeapply.ModelConfigurationContract {
	return tkeapply.ModelConfigurationContract{
		Protocol:                     modelConfigurationProtocol(contract.GetProtocol()),
		PortName:                     contract.GetPortName(),
		ApplyPath:                    contract.GetApplyPath(),
		ReadbackPath:                 contract.GetReadbackPath(),
		RequestFields:                modelConfigurationFields(contract.GetRequestFields()),
		ReadbackFields:               modelConfigurationReadbackFields(contract.GetReadbackFields()),
		AuthorizationSecretInputName: contract.GetAuthorizationSecretInputName(),
	}
}

// modelConfigurationProtocol maps the declared protocol to the one wire this Cloud
// executes. A protocol outside the contract's own enumeration projects nothing.
func modelConfigurationProtocol(protocol api.ModelConfigurationContractProtocolEnum) string {
	if protocol == api.ModelConfigurationContractProtocolEnum_MODEL_CONFIGURATION_CONTRACT_PROTOCOL_ENUM_OPL_MODEL_CONFIG_V1 {
		return tkeapply.ModelConfigurationProtocol
	}
	return ""
}

func modelConfigurationFields(fields []api.ModelConfigurationContractRequestFieldsEnum) []string {
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		switch field {
		case api.ModelConfigurationContractRequestFieldsEnum_MODEL_CONFIGURATION_CONTRACT_REQUEST_FIELDS_ENUM_VERSION:
			out = append(out, "version")
		case api.ModelConfigurationContractRequestFieldsEnum_MODEL_CONFIGURATION_CONTRACT_REQUEST_FIELDS_ENUM_SELECTIONS:
			out = append(out, "selections")
		}
	}
	return out
}

func modelConfigurationReadbackFields(fields []api.ModelConfigurationContractReadbackFieldsEnum) []string {
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		switch field {
		case api.ModelConfigurationContractReadbackFieldsEnum_MODEL_CONFIGURATION_CONTRACT_READBACK_FIELDS_ENUM_APPLIEDVERSION:
			out = append(out, "appliedVersion")
		case api.ModelConfigurationContractReadbackFieldsEnum_MODEL_CONFIGURATION_CONTRACT_READBACK_FIELDS_ENUM_SELECTIONS:
			out = append(out, "selections")
		}
	}
	return out
}
