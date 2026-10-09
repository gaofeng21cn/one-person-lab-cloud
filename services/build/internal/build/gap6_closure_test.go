package build

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/publicjson"
)

const gapPublisherSchema = "../../../../docs/spec/target/contracts/publisher-contract.schema.json"

// gapContracts decodes the approved publisher-contract schema examples so the
// Build owner's combination checks are exercised against the contract the owner
// actually consumes, not a hand-written approximation.
func gapContracts(t *testing.T) (*api.RuntimePublisherContract, *api.WebuiPublisherContract) {
	t.Helper()
	raw, err := os.ReadFile(gapPublisherSchema)
	if err != nil {
		t.Fatal(err)
	}
	var document struct{ Examples []json.RawMessage }
	if err = json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Examples) < 2 {
		t.Fatalf("publisher schema examples=%d", len(document.Examples))
	}
	runtime := &api.RuntimePublisherContract{}
	webui := &api.WebuiPublisherContract{}
	if err = publicjson.Unmarshal(document.Examples[0], runtime); err != nil {
		t.Fatal(err)
	}
	if err = publicjson.Unmarshal(document.Examples[1], webui); err != nil {
		t.Fatal(err)
	}
	return runtime, webui
}

func gapSnapshot(t *testing.T, runtime *api.RuntimePublisherContract, webui *api.WebuiPublisherContract) *api.BuildInputSnapshot {
	t.Helper()
	d := "sha256:" + strings.Repeat("1", 64)
	in := &api.BuildInputSnapshot{
		PackageId:                "pkg",
		PackageVersionId:         "pv",
		PackageObject:            &api.SourceObjectReference{StorageObjectId: d, VersionId: d, Sha256: d, SizeBytes: 9},
		RuntimeVersionId:         "runtime",
		RuntimeArtifact:          runtime.GetImage(),
		WebuiVersionId:           "webui",
		WebuiArtifact:            webui.GetImage(),
		RuntimeContract:          runtime,
		WebuiContract:            webui,
		RuntimeContractReference: &api.PublisherContractReference{PublisherNamespaceId: runtime.GetPublisherNamespaceId(), VersionId: "runtime", Kind: api.PublisherContractReferenceKindEnum_PUBLISHER_CONTRACT_REFERENCE_KIND_ENUM_RUNTIME, DescriptorDigest: d, DescriptorObjectRef: "runtime-contract:runtime@" + d},
		WebuiContractReference:   &api.PublisherContractReference{PublisherNamespaceId: webui.GetPublisherNamespaceId(), VersionId: "webui", Kind: api.PublisherContractReferenceKindEnum_PUBLISHER_CONTRACT_REFERENCE_KIND_ENUM_WEBUI, DescriptorDigest: d, DescriptorObjectRef: "webui-contract:webui@" + d},
	}
	in.SnapshotDigest = digest(BuildInputDigestBytes(in))
	return in
}

func gapRunner() *Runner {
	return &Runner{MaxPackageBytes: 1 << 20, MaxExpandedBytes: 1 << 20, MaxFiles: 16}
}

// TestGap6BuildRejectsIncompatibleApprovedCombination reproduces GAP-6(c) at the
// Build owner: individually approved Runtime Release and WebUI whose ABI sets do
// not intersect must be refused before an exporter can start.
func TestGap6BuildRejectsIncompatibleApprovedCombination(t *testing.T) {
	runtime, webui := gapContracts(t)
	webui.RuntimeAbiVersions = []string{"opl-runtime/v9"}
	in := gapSnapshot(t, runtime, webui)
	if err := gapRunner().ValidateInput(in); err == nil {
		t.Fatal("Build accepted an ABI-incompatible approved combination")
	}
	webui.RuntimeAbiVersions = []string{runtime.GetRuntimeAbiVersion()}
	in = gapSnapshot(t, runtime, webui)
	if err := gapRunner().ValidateInput(in); err != nil {
		t.Fatalf("Build rejected the compatible combination: %v", err)
	}
}

// TestGap6BuildRejectsUnapprovedPackageFormat is the second compatibility axis on
// the Build side: the Runtime Build recipe must select a Package format the
// Runtime Release itself approves.
func TestGap6BuildRejectsUnapprovedPackageFormat(t *testing.T) {
	runtime, webui := gapContracts(t)
	runtime.BuildRecipe.PackageInput.FormatVersion = "unapproved-format"
	in := gapSnapshot(t, runtime, webui)
	if err := gapRunner().ValidateInput(in); err == nil {
		t.Fatal("Build accepted a recipe selecting an unapproved Package format")
	}
	runtime.BuildRecipe.PackageInput.FormatVersion = runtime.GetPackageFormatVersions()[0]
	in = gapSnapshot(t, runtime, webui)
	if err := gapRunner().ValidateInput(in); err != nil {
		t.Fatalf("Build rejected the approved Package format: %v", err)
	}
}

// TestGap6BuildRejectsSnapshotDigestThatDoesNotDescribeItsBytes reproduces
// GAP-6(a)/(e): an assembled or edited snapshot cannot carry a digest that does
// not describe its own bytes, because the digest is the identity every later
// owner readback and claim binding compares against.
func TestGap6BuildRejectsSnapshotDigestThatDoesNotDescribeItsBytes(t *testing.T) {
	runtime, webui := gapContracts(t)
	in := gapSnapshot(t, runtime, webui)
	in.RuntimeArtifact = &api.ArtifactReference{Repository: runtime.GetImage().GetRepository(), Digest: "sha256:" + strings.Repeat("2", 64), Platform: runtime.GetImage().GetPlatform()}
	in.RuntimeContract.Image = in.RuntimeArtifact
	if err := gapRunner().ValidateInput(in); err == nil {
		t.Fatal("Build accepted a snapshot whose digest does not describe its bytes")
	}
	// Re-freezing the same facts produces a snapshot the owner accepts, so the
	// refusal is about the digest/content relationship rather than the edit.
	frozen := gapSnapshot(t, runtime, webui)
	if err := gapRunner().ValidateInput(frozen); err != nil {
		t.Fatalf("re-frozen snapshot=%v", err)
	}
}

// TestGap6SnapshotDigestIsReDerivableAcrossEncoders pins the encoder the frozen
// identity uses: protojson output is intentionally unstable, so the digest must
// come from the canonical public encoding and must be recomputable from the
// persisted snapshot bytes alone.
func TestGap6SnapshotDigestIsReDerivableAcrossEncoders(t *testing.T) {
	runtime, webui := gapContracts(t)
	in := gapSnapshot(t, runtime, webui)
	canonical := BuildInputDigestBytes(in)
	if digest(canonical) != in.GetSnapshotDigest() {
		t.Fatalf("canonical digest=%s frozen=%s", digest(canonical), in.GetSnapshotDigest())
	}
	persisted := &api.BuildInputSnapshot{}
	if err := publicjson.Unmarshal(canonical, persisted); err != nil {
		t.Fatal(err)
	}
	if digest(BuildInputDigestBytes(persisted)) != in.GetSnapshotDigest() {
		t.Fatalf("round-tripped digest=%s frozen=%s", digest(BuildInputDigestBytes(persisted)), in.GetSnapshotDigest())
	}
}

// TestGap6FrozenSnapshotAcceptsClaimBinding reproduces the live ordering
// breakpoint: Capability freezes the three-input digest before the Build worker
// acquires the input reference claims, then the worker records those claim IDs
// into the persisted snapshot and binds each claim to that same frozen digest.
// The claim IDs are Build-domain coordination bookkeeping, not one of the three
// immutable input facts, so adding them to the persisted snapshot must not
// change the identity every later owner recomputes.
func TestGap6FrozenSnapshotAcceptsClaimBinding(t *testing.T) {
	runtime, webui := gapContracts(t)
	in := gapSnapshot(t, runtime, webui)
	frozen := in.GetSnapshotDigest()
	in.PackageClaimId = "claim-package"
	in.RuntimeClaimId = "claim-runtime"
	in.WebuiClaimId = "claim-webui"
	if got := digest(BuildInputDigestBytes(in)); got != frozen {
		t.Fatalf("claim binding changed the frozen digest: %s want %s", got, frozen)
	}
	if err := gapRunner().ValidateInput(in); err != nil {
		t.Fatalf("Build rejected the claim-bound frozen snapshot it persisted: %v", err)
	}
}
