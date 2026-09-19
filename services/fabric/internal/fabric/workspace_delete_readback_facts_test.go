package fabric

import (
	"context"
	"errors"
	"slices"
	"testing"
)

// computeDestroyStatusStub reports the provider-authoritative absence facts and
// records whether a provider mutation was attempted.
type computeDestroyStatusStub struct {
	testProvider
	readback ComputeAllocation
	err      error
	reads    int
}

func (p *computeDestroyStatusStub) ReadComputeDestroyStatus(_ context.Context, allocation ComputeAllocation) (ComputeAllocation, error) {
	p.reads++
	if p.err != nil {
		return allocation, p.err
	}
	return p.readback, nil
}

func seedComputeAllocationForDeleteReadback(t *testing.T, service *Service, allocation ComputeAllocation) {
	t.Helper()
	service.mu.Lock()
	defer service.mu.Unlock()
	service.computes[allocation.ID] = allocation
}

func TestReadComputeDestroyStatusReturnsProviderAbsenceFactsWithoutMutation(t *testing.T) {
	absent := false
	provider := &computeDestroyStatusStub{}
	service := NewService(provider)
	allocation := ComputeAllocation{ID: "compute-alpha", AccountID: "acct-alpha", WorkspaceID: "ws-alpha", PackageID: "medium", Status: "destroying", CreatedAt: service.now()}
	seedComputeAllocationForDeleteReadback(t, service, allocation)
	provider.readback = allocation
	provider.readback.Status, provider.readback.ProviderRequestID = "external_deleted", "provider-readback-alpha"
	provider.readback.MachinePresent, provider.readback.TKEStatus, provider.readback.CVMStatus = &absent, "NOT_FOUND", "NOT_FOUND"

	readback, err := service.ReadComputeDestroyStatus(context.Background(), "compute-alpha")
	if err != nil || readback.Status != "external_deleted" || readback.MachinePresent == nil || *readback.MachinePresent ||
		readback.TKEStatus != "NOT_FOUND" || readback.CVMStatus != "NOT_FOUND" || provider.reads != 1 {
		t.Fatalf("readback=%#v err=%v reads=%d", readback, err, provider.reads)
	}
}

func TestReadComputeDestroyStatusFailsClosedWithoutProviderReadback(t *testing.T) {
	service := NewService(testProvider{})
	seedComputeAllocationForDeleteReadback(t, service, ComputeAllocation{ID: "compute-alpha", AccountID: "acct-alpha", WorkspaceID: "ws-alpha", CreatedAt: service.now()})
	if _, err := service.ReadComputeDestroyStatus(context.Background(), "compute-alpha"); err == nil {
		t.Fatal("missing provider readback capability was reported as success")
	}
	provider := &computeDestroyStatusStub{err: errors.New("provider readback unavailable")}
	unavailable := NewService(provider)
	seedComputeAllocationForDeleteReadback(t, unavailable, ComputeAllocation{ID: "compute-alpha", AccountID: "acct-alpha", WorkspaceID: "ws-alpha", CreatedAt: unavailable.now()})
	if _, err := unavailable.ReadComputeDestroyStatus(context.Background(), "compute-alpha"); err == nil {
		t.Fatal("unavailable provider readback was reported as success")
	}
}

func TestReadComputeDestroyStatusRejectsProviderIdentityDrift(t *testing.T) {
	provider := &computeDestroyStatusStub{}
	service := NewService(provider)
	allocation := ComputeAllocation{ID: "compute-alpha", AccountID: "acct-alpha", WorkspaceID: "ws-alpha", InstanceID: "ins-alpha", CreatedAt: service.now()}
	seedComputeAllocationForDeleteReadback(t, service, allocation)
	drifted := allocation
	drifted.Status, drifted.InstanceID = "external_deleted", "ins-other"
	provider.readback = drifted
	if _, err := service.ReadComputeDestroyStatus(context.Background(), "compute-alpha"); err == nil {
		t.Fatal("identity drift was reported as an authoritative readback")
	}
}

func TestTencentStorageDeleteReadbackReportsBindingAbsenceAsTypedFact(t *testing.T) {
	kubectlReads := [][]string{}
	provider := &TencentProvider{
		provision: func(_ context.Context, _ provisionerRequest) (provisionerResponse, error) {
			return provisionerResponse{
				OK: true, StorageVolumeID: "disk-alpha", CBSStatus: "NOT_FOUND", Status: "external_deleted",
				ProviderRequestID: "req-cbs-absent", ProviderData: map[string]string{"region": "ap-guangzhou"},
			}, nil
		},
		kubectl: func(_ context.Context, args []string, _ []byte) ([]byte, error) {
			kubectlReads = append(kubectlReads, args)
			return []byte(`{"kind":"List","items":[]}`), nil
		},
	}
	volume := StorageVolume{
		ID: "storage-alpha", AccountID: "acct-alpha", WorkspaceID: "ws-alpha", ProviderResourceID: "disk-alpha",
		SizeGB: 10, Zone: "ap-guangzhou-3", DiskType: "CLOUD_BASIC",
		ProviderData: map[string]string{"region": "ap-guangzhou", "storageDestroyPhase": "absence_confirmed", "storageDestroyMutationCount": "0"},
	}
	ctx := context.WithValue(context.Background(), workspaceStorageDeleteOwnerContextKey{}, volume)
	readback, err := provider.ReadStorageVolumeStatus(ctx, volume)
	if err != nil || readback.BindingPresent == nil || *readback.BindingPresent {
		t.Fatalf("readback=%#v err=%v", readback, err)
	}
	if readback.CBSStatus != "NOT_FOUND" || readback.Status != "external_deleted" {
		t.Fatalf("cbs readback=%#v", readback)
	}
	pvName, pvcName := storageBindingNames(volume)
	if len(kubectlReads) != 1 || !slices.Equal(kubectlReads[0], []string{"get", "pv/" + pvName, "pvc/" + pvcName, "--ignore-not-found", "-o", "json"}) {
		t.Fatalf("binding readback calls=%#v", kubectlReads)
	}
}
