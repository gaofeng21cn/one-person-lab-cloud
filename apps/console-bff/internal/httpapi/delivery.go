// Package httpapi serves the BFF's same-origin REST surface. It aggregates the
// owner that wrote each fact and never stores or invents a business value.
package httpapi

import (
	"context"
	"fmt"
	"strings"

	"opl-cloud/apps/console-bff/internal/clients"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// DeliveryView is the composed product view of one Workspace's Agent delivery
// chain. Every layer names the owner that reported it, so the Console renders the
// same facts the owners persist rather than a BFF copy.
type DeliveryView struct {
	WorkspaceID string    `json:"workspaceId"`
	Workspace   OwnerFact `json:"workspace"`
	Serve       OwnerFact `json:"serve"`
	Capability  OwnerFact `json:"capabilityVersion"`
	Build       OwnerFact `json:"build"`
}

// OwnerFact names the reporting owner and its typed readback for one layer.
type OwnerFact struct {
	Owner   string         `json:"owner"`
	State   string         `json:"state"`
	Details map[string]any `json:"details"`
}

// DeliveryReader reads the typed owner facts the delivery view composes.
type DeliveryReader interface {
	Workspace(ctx context.Context, workspaceID string) (*api.Workspace, error)
	Deployments(ctx context.Context, workspaceID, cursor string, limit int32) (*api.DeploymentPage, error)
	WorkspaceAccess(ctx context.Context, workspaceID string) (*api.WorkspaceAccess, error)
	Build(ctx context.Context, buildID string) (*api.BuildJob, error)
	CapabilityVersion(ctx context.Context, capabilityVersionID string) (*api.CapabilityVersion, error)
}

// DeliveryView assembles the chain from the owners. A missing owner readback is a
// real error: the BFF never substitutes a default for a fact it did not read. The
// current deployment and access come from Serve, the pinned version from
// Capability, the build input/output from Build, and the Workspace identity and
// plan from Workspace.
func (s *Server) deliveryView(ctx context.Context, caller Caller, workspaceID string) (DeliveryView, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return DeliveryView{}, fmt.Errorf("workspace id is required")
	}
	if s.reader == nil {
		return DeliveryView{}, fmt.Errorf("delivery reader is not configured")
	}
	reader := s.reader

	view := DeliveryView{WorkspaceID: workspaceID}
	// Each decision is bound to its own action, audience and resource. A composed
	// view must obtain the corresponding context immediately before each owner call.
	authorize := func(owner owneridentity.Owner, action api.AuthorizationActionEnum, kind api.AuthorizationResourceKind, id string) error {
		call := clients.CallContext(ctx)
		call.AuthorizationContextId = ""
		return RequireAuthorizedAction(ctx, s.identity, caller, owner, action, &api.AuthorizationResource{Kind: kind, Id: &id}, call.GetRequestId())
	}
	if err := authorize(owneridentity.Workspace, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACE, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, workspaceID); err != nil {
		return DeliveryView{}, err
	}

	workspace, err := reader.Workspace(ctx, workspaceID)
	if err != nil {
		return DeliveryView{}, fmt.Errorf("workspace: %w", err)
	}
	if workspace == nil {
		return DeliveryView{}, fmt.Errorf("workspace: empty owner readback")
	}
	view.Workspace = OwnerFact{
		Owner: workspaceOwner,
		State: workspace.GetStatus().String(),
		Details: map[string]any{
			"computePlanId":     workspace.GetComputePlanId(),
			"storagePlanId":     workspace.GetStoragePlanId(),
			"resourceReadiness": workspace.GetResourceReadiness().String(),
			"capabilityVersion": workspace.GetCapabilityVersionId(),
		},
	}

	if err := authorize(owneridentity.Serve, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTDEPLOYMENTS, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, workspaceID); err != nil {
		return DeliveryView{}, err
	}
	deployments, err := reader.Deployments(ctx, workspaceID, "", 0)
	if err != nil {
		return DeliveryView{}, fmt.Errorf("serve deployments: %w", err)
	}
	current := currentDeployment(deployments)
	if current == nil {
		return DeliveryView{}, fmt.Errorf("serve: workspace %s has no deployment", workspaceID)
	}
	view.Serve = OwnerFact{
		Owner: serveOwner,
		State: current.GetStatus().String(),
		Details: map[string]any{
			"deploymentId":        current.GetId(),
			"capabilityVersionId": current.GetCapabilityVersionId(),
		},
	}

	if err := authorize(owneridentity.Serve, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACEACCESS, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, workspaceID); err != nil {
		return DeliveryView{}, err
	}
	access, err := reader.WorkspaceAccess(ctx, workspaceID)
	if err != nil {
		return DeliveryView{}, fmt.Errorf("serve access: %w", err)
	}
	if access == nil {
		return DeliveryView{}, fmt.Errorf("serve access: empty owner readback")
	}
	view.Serve.Details["accessUrl"] = access.GetUrl()
	view.Serve.Details["accessAuthenticationMode"] = access.GetAuthenticationMode().String()
	view.Serve.Details["applicationCredentialsAvailable"] = access.GetApplicationCredentialsAvailable()

	capabilityVersionID := current.GetCapabilityVersionId()
	if capabilityVersionID == "" {
		capabilityVersionID = workspace.GetCapabilityVersionId()
	}
	if capabilityVersionID == "" {
		// The default App is delivered from an approved Runtime Release and needs
		// no synthetic Package, Build or CapabilityVersion, so a deployment that
		// names none is the second supported combination rather than a broken one.
		// Report the Capability and Build layers as genuinely not applicable: the
		// view stays owner-attributed and honest without inventing a version,
		// calling an owner that has no such fact, or failing the whole chain.
		view.Capability = OwnerFact{Owner: capabilityOwner, State: defaultAppLayerState, Details: map[string]any{"reason": defaultAppLayerReason}}
		view.Build = OwnerFact{Owner: buildOwner, State: defaultAppLayerState, Details: map[string]any{"reason": defaultAppLayerReason}}
		return view, nil
	}
	if err := authorize(owneridentity.Capability, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETCAPABILITYVERSION, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_VERSION, capabilityVersionID); err != nil {
		return DeliveryView{}, err
	}
	version, err := reader.CapabilityVersion(ctx, capabilityVersionID)
	if err != nil {
		return DeliveryView{}, fmt.Errorf("capability version: %w", err)
	}
	if version == nil {
		return DeliveryView{}, fmt.Errorf("capability version: empty owner readback")
	}
	view.Capability = OwnerFact{
		Owner: capabilityOwner,
		State: version.GetStatus().String(),
		Details: map[string]any{
			"capabilityVersionId":        version.GetId(),
			"artifactDigest":             version.GetArtifactDigest(),
			"artifactRepository":         version.GetArtifact().GetRepository(),
			"deploymentDescriptorDigest": version.GetDeploymentDescriptorDigest(),
			"buildJobId":                 version.GetBuildJobId(),
		},
	}

	if buildID := version.GetBuildJobId(); buildID != "" {
		if err := authorize(owneridentity.Build, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETBUILD, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_BUILD, buildID); err != nil {
			return DeliveryView{}, err
		}
		job, err := reader.Build(ctx, buildID)
		if err != nil {
			return DeliveryView{}, fmt.Errorf("build: %w", err)
		}
		if job == nil {
			return DeliveryView{}, fmt.Errorf("build: empty owner readback")
		}
		view.Build = OwnerFact{
			Owner: buildOwner,
			State: job.GetStatus().String(),
			Details: map[string]any{
				"buildId":        job.GetId(),
				"stage":          job.GetStage(),
				"artifactDigest": job.GetArtifactDigest(),
				"operationId":    job.GetOperationId(),
			},
		}
	}
	return view, nil
}

func currentDeployment(page *api.DeploymentPage) *api.Deployment {
	if page == nil || len(page.GetItems()) == 0 {
		return nil
	}
	for _, item := range page.GetItems() {
		if item.GetStatus() == api.DeploymentStatusEnum_DEPLOYMENT_STATUS_ENUM_ACTIVE {
			return item
		}
	}
	// No active deployment yet: report the newest attempt the owner returned. Its
	// own status already distinguishes queued/deploying/verifying/failed.
	return page.GetItems()[0]
}

// The default App carries no CapabilityVersion and no Build. The delivery view
// reports those two layers as not applicable rather than as a failure or an
// invented value, matching the adopted delivery invariant that the default App
// needs no synthetic Package, Build or CapabilityVersion.
const (
	defaultAppLayerState  = "not_applicable"
	defaultAppLayerReason = "default_app_runtime_release"
)

// The owner names are the fixed Cloud owner identities, not a second vocabulary.
const (
	capabilityOwner = string(owneridentity.Capability)
	buildOwner      = string(owneridentity.Build)
	workspaceOwner  = string(owneridentity.Workspace)
	serveOwner      = string(owneridentity.Serve)
)
