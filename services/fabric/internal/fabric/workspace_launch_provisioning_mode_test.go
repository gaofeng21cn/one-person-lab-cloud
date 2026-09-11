package fabric

import (
	"context"
	"errors"
	"strings"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
)

func TestWorkspaceLaunchResourceOnlyPreflightAndStageAdmitWithoutImage(t *testing.T) {
	store := NewMemoryOperationStore()
	provider := &workspaceLaunchRecordingProvider{}
	service := NewServiceWithOperationStore(provider, store)
	launchHash := strings.Repeat("c", 64)
	preflight, err := service.PreflightWorkspaceLaunch(context.Background(), WorkspaceLaunchPreflightInput{
		SchemaVersion: 1, LaunchOperationID: "launch-resource-only", AccountID: "acct-alpha", WorkspaceID: "ws-alpha",
		PackageID: "basic", SizeGB: 10, ProvisioningMode: string(contracts.WorkspaceProvisioningResourceOnly), RequestHash: launchHash,
	})
	if err != nil || !preflight.Available || preflight.Reason != "none" || preflight.ProviderBindingRef == "" {
		t.Fatalf("resource-only preflight=%#v err=%v", preflight, err)
	}

	input := WorkspaceLaunchStageInput{
		Binding: WorkspaceLaunchStageBinding{
			SchemaVersion: 1, LaunchOperationID: "launch-resource-only", AccountID: "acct-alpha", WorkspaceID: "ws-alpha",
			Stage: string(contracts.StageCompute), Action: string(contracts.StageCompute),
			FabricOperationID: "launch-resource-only:ensure_compute_allocation", IdempotencyKey: "launch-resource-only:ensure_compute_allocation",
		},
		ProviderProfileRef: preflight.ProviderProfileRef, ProviderBindingRef: preflight.ProviderBindingRef, SpecDigest: preflight.SpecDigest,
		PackageID: "basic", SizeGB: 10, ProvisioningMode: string(contracts.WorkspaceProvisioningResourceOnly), Resources: WorkspaceLaunchResources{},
	}
	input.Binding.RequestHash = workspaceLaunchStageRequestHash(input, launchHash)
	provider.ensureResult = &WorkspaceLaunchProviderResult{Resources: WorkspaceLaunchResources{
		ComputeAllocationID: workspaceLaunchComputeID(input.Binding), ComputeBindingRef: input.Binding.FabricOperationID,
	}}
	result, ensureErr := service.EnsureWorkspaceLaunchStage(context.Background(), input)
	if ensureErr != nil || result.State != string(contracts.StageStateReady) || provider.ensureCalls != 1 {
		t.Fatalf("resource-only stage result=%#v ensures=%d err=%v", result, provider.ensureCalls, ensureErr)
	}

	dirty := input
	dirty.WorkspaceImageDigest = "repo.example/workspace@sha256:" + strings.Repeat("d", 64)
	dirty.Binding.RequestHash = workspaceLaunchStageRequestHash(dirty, launchHash)
	if _, err := service.EnsureWorkspaceLaunchStage(context.Background(), dirty); !errors.Is(err, ErrWorkspaceLaunchInputInvalid) {
		t.Fatalf("resource-only stage with image digest error=%v, want %v", err, ErrWorkspaceLaunchInputInvalid)
	}

	_, preflightErr := service.PreflightWorkspaceLaunch(context.Background(), WorkspaceLaunchPreflightInput{
		SchemaVersion: 1, LaunchOperationID: "launch-resource-only-image", AccountID: "acct-alpha", WorkspaceID: "ws-image",
		PackageID: "basic", SizeGB: 10, ProvisioningMode: string(contracts.WorkspaceProvisioningResourceOnly),
		WorkspaceImageDigest: "repo.example/workspace@sha256:" + strings.Repeat("d", 64), RequestHash: strings.Repeat("e", 64),
	})
	if !errors.Is(preflightErr, ErrWorkspaceLaunchInputInvalid) {
		t.Fatalf("resource-only preflight with image digest error=%v, want %v", preflightErr, ErrWorkspaceLaunchInputInvalid)
	}
}
