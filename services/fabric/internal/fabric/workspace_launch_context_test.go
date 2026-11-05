package fabric

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestLaunchDeleteProjectionPreservesPreComputeKeyFingerprint(t *testing.T) {
	// The retained full Launch runs Key before Compute. Its real CP adapter
	// carries that fingerprint through every Fabric resource-stage request.
	fingerprint := "sha256:" + strings.Repeat("a", 64)
	service, store, _, resources := workspaceLaunchDeleteProjectionFixture(t, WorkspaceLaunchResources{GatewaySecretFingerprint: fingerprint})
	before, err := store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	computes, volumes, attachments, err := projectWorkspaceLaunchDeleteResources(context.Background(), store)
	if err != nil || len(computes) != 1 || len(volumes) != 1 || len(attachments) != 1 {
		t.Fatalf("retained full Launch lost canonical resources: compute=%d storage=%d attachment=%d err=%v", len(computes), len(volumes), len(attachments), err)
	}
	if err := service.hydrateMissingResourceState(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, found := service.GetComputeAllocation(context.Background(), resources.ComputeAllocationID); !found {
		t.Fatal("original Compute not readable")
	}
	after, err := store.List(context.Background())
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("read-only projection mutated original history")
	}
}

func TestLaunchDeleteProjectionRejectsAlteredOrExtraKeyContext(t *testing.T) {
	fingerprint := "sha256:" + strings.Repeat("a", 64)
	for _, tc := range []struct {
		name, stage                                string
		mutate                                     func(*workspaceLaunchStageRecord)
		wantComputes, wantVolumes, wantAttachments int
	}{
		{"changed result", "ensure_compute_allocation", func(r *workspaceLaunchStageRecord) {
			r.Resources.GatewaySecretFingerprint = "sha256:" + strings.Repeat("b", 64)
		}, 0, 0, 0},
		{"missing result", "ensure_compute_allocation", func(r *workspaceLaunchStageRecord) { r.Resources.GatewaySecretFingerprint = "" }, 0, 0, 0},
		{"unrequested result", "ensure_compute_allocation", func(r *workspaceLaunchStageRecord) { r.RequestResources.GatewaySecretFingerprint = "" }, 0, 0, 0},
		{"invalid fingerprint", "ensure_compute_allocation", func(r *workspaceLaunchStageRecord) {
			r.RequestResources.GatewaySecretFingerprint = "invalid"
			r.Resources.GatewaySecretFingerprint = "invalid"
		}, 0, 0, 0},
		{"secret reference", "ensure_compute_allocation", func(r *workspaceLaunchStageRecord) {
			r.RequestResources.GatewaySecretRef = "secret-ref"
			r.Resources.GatewaySecretRef = "secret-ref"
		}, 0, 0, 0},
		{"runtime credential", "ensure_compute_allocation", func(r *workspaceLaunchStageRecord) {
			r.RequestResources.RuntimeCredentialStatus = "ready"
			r.Resources.RuntimeCredentialStatus = "ready"
		}, 0, 0, 0},
		{"storage sequence changed", "storage", func(r *workspaceLaunchStageRecord) {
			r.RequestResources.GatewaySecretFingerprint = "sha256:" + strings.Repeat("b", 64)
			r.Resources.GatewaySecretFingerprint = r.RequestResources.GatewaySecretFingerprint
		}, 1, 0, 0},
		{"attachment sequence changed", "attachment", func(r *workspaceLaunchStageRecord) {
			r.RequestResources.GatewaySecretFingerprint = "sha256:" + strings.Repeat("b", 64)
			r.Resources.GatewaySecretFingerprint = r.RequestResources.GatewaySecretFingerprint
		}, 1, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, store, provider, _ := workspaceLaunchDeleteProjectionFixture(t, WorkspaceLaunchResources{GatewaySecretFingerprint: fingerprint})
			for i := range store.operation {
				binding, ok := decodeLaunchStageBinding(store.operation[i])
				if !ok || binding.Stage != tc.stage {
					continue
				}
				record, ok := decodeWorkspaceLaunchStageRecord(store.operation[i])
				if !ok {
					t.Fatal("fixture record invalid")
				}
				tc.mutate(&record)
				// A valid digest does not make conflicting semantic facts valid.
				setWorkspaceLaunchStageRecord(&store.operation[i], record)
			}
			computes, volumes, attachments, err := projectWorkspaceLaunchDeleteResources(context.Background(), store)
			if err != nil || len(computes) != tc.wantComputes || len(volumes) != tc.wantVolumes || len(attachments) != tc.wantAttachments {
				t.Fatalf("invalid context projected: compute=%d storage=%d attachment=%d err=%v", len(computes), len(volumes), len(attachments), err)
			}
			if provider.computeDeleteCalls.Load() != 0 || provider.storageDeleteCalls.Load() != 0 || provider.detachCalls.Load() != 0 {
				t.Fatal("projection mutated provider")
			}
		})
	}
}
