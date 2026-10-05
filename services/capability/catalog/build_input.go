package catalog

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/publicjson"
)

// canonicalContractDigest re-derives the identity of one publisher contract from
// the canonical public encoding, so a stored digest is proven against its own
// bytes instead of being trusted as an opaque string.
func canonicalContractDigest(m proto.Message) (string, error) {
	encoded, err := publicjson.Marshal(m)
	if err != nil {
		return "", status.Error(codes.DataLoss, "publisher contract is not encodable")
	}
	return digest(encoded), nil
}

// buildInputDigest names one frozen three-input Build snapshot. The canonical
// public encoding is used, not protojson: protojson output carries intentional
// cross-build whitespace instability, so a digest computed from it cannot be
// re-derived by any independent reader of the persisted snapshot. The canonical
// encoding depends only on message content and sorts its keys, so Capability,
// Build, PostgreSQL jsonb round-trips and a later auditor all recompute the same
// digest from the same facts. The three reference-claim IDs are Build-domain
// coordination fields: they are acquired after the freeze and are validated
// against this digest by validateFrozenClaims instead of hashing into it.
func buildInputDigest(in *api.BuildInputSnapshot) (string, error) {
	probe := proto.Clone(in).(*api.BuildInputSnapshot)
	// The digest describes the three immutable input facts, never itself and
	// never the Build owner's claim bookkeeping: the snapshot is frozen before
	// the worker acquires and binds the three reference claims, so those claim
	// IDs join the persisted message afterwards and cannot be part of an
	// identity every owner must be able to recompute unchanged.
	probe.SnapshotDigest = ""
	probe.PackageClaimId = ""
	probe.RuntimeClaimId = ""
	probe.WebuiClaimId = ""
	encoded, err := publicjson.Marshal(probe)
	if err != nil {
		return "", status.Error(codes.DataLoss, "Build input snapshot is not encodable")
	}
	return digest(encoded), nil
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// validateBuildCombination enforces the compatibility facts the three-input
// combination must satisfy even when each part is individually approved: the
// Runtime Release must approve the Package format its own Build recipe selects,
// and the selected WebUI must declare the Runtime Release ABI as supported. An
// incompatible combination is refused before any snapshot exists.
func validateBuildCombination(runtime *api.RuntimePublisherContract, webui *api.WebuiPublisherContract) error {
	if runtime == nil || webui == nil {
		return status.Error(codes.InvalidArgument, "Runtime Release and WebUI publisher contracts are required")
	}
	recipe := runtime.GetBuildRecipe()
	if recipe == nil || runtime.GetImage() == nil || webui.GetImage() == nil {
		return status.Error(codes.DataLoss, "Runtime publisher contract is incomplete")
	}
	formats := runtime.GetPackageFormatVersions()
	if len(formats) == 0 {
		return status.Error(codes.FailedPrecondition, "Runtime Release declares no approved Package format")
	}
	format := recipe.GetPackageInput().GetFormatVersion()
	if format == "" || !containsString(formats, format) {
		return status.Error(codes.FailedPrecondition, "Runtime Build recipe selects a Package format the Runtime Release does not approve")
	}
	abi := runtime.GetRuntimeAbiVersion()
	if abi == "" {
		return status.Error(codes.FailedPrecondition, "Runtime Release declares no Runtime ABI")
	}
	if len(webui.GetRuntimeAbiVersions()) == 0 {
		return status.Error(codes.FailedPrecondition, "selected WebUI declares no Runtime ABI")
	}
	if !containsString(webui.GetRuntimeAbiVersions(), abi) {
		return status.Error(codes.FailedPrecondition, "selected WebUI does not support the Runtime Release ABI")
	}
	return nil
}

// validateFrozenInput proves the three-input provenance the single READY writer
// must hold before it inserts a CapabilityVersion:
//
//  1. the frozen snapshot describes its own bytes and the DeploymentDescriptor
//     binds that exact snapshot, package version and built artifact;
//  2. the Package identity preserved through intake still matches the
//     PackageVersion row (tenant, digest, size, immutable object) that Capability
//     owns, so the same uploaded bytes are what the build consumed;
//  3. the WebUI is re-read from Capability's own record and must still be
//     approved with the frozen artifact and contract identity, the Runtime
//     Release's publisher namespace must still be admitted, and the three inputs
//     must still be a compatible combination.
//
// Registration also requires the three inputs to be claim-bound to this exact
// snapshot digest by the Build owner's committed operation: AcquireReference and
// BindReference re-read the Runtime Release from the Runtime owner with the
// caller's authorization context and refuse an unapproved or revoked release, and
// BindReference records the accepted input digest. A snapshot that no owner
// bound, or one whose digest differs from the bound input, is not registrable.
//
// An admission receipt string is never consulted as approval. Approval is the
// owning record's current status; a fixture receipt cannot substitute for it.
func (s *Service) validateFrozenInput(ctx context.Context, event *api.EventEnvelope, readback *api.BuildArtifactReadback) error {
	in := readback.GetInput()
	if in == nil {
		return status.Error(codes.FailedPrecondition, "Build readback carries no frozen input")
	}
	if in.GetPackageId() == "" || in.GetPackageVersionId() == "" || in.GetRuntimeVersionId() == "" || in.GetWebuiVersionId() == "" || in.GetPackageObject().GetStorageObjectId() == "" {
		return status.Error(codes.FailedPrecondition, "frozen Build input is incomplete")
	}
	derived, e := buildInputDigest(in)
	if e != nil {
		return e
	}
	if derived != in.GetSnapshotDigest() {
		return status.Error(codes.FailedPrecondition, "Build input snapshot digest does not describe its bytes")
	}
	descriptor := readback.GetDeploymentDescriptor()
	if descriptor == nil || descriptor.GetBuildInputDigest() != in.GetSnapshotDigest() || descriptor.GetPackageVersionId() != in.GetPackageVersionId() {
		return status.Error(codes.FailedPrecondition, "Build descriptor does not bind the frozen Build input")
	}
	if !proto.Equal(descriptor.GetArtifact(), readback.GetArtifact()) {
		return status.Error(codes.FailedPrecondition, "Build descriptor artifact differs from the built artifact")
	}
	if e := validateBuildCombination(in.GetRuntimeContract(), in.GetWebuiContract()); e != nil {
		return e
	}

	var packageName, objectRef, sha, packageStatus, packageTenant string
	var size int64
	if e := s.DB.QueryRowContext(ctx, `SELECT p.id,v.object_ref,v.sha256,v.size_bytes,v.status,n.tenant_id FROM capability.package_versions v JOIN capability.packages p ON p.id=v.package_id JOIN capability.namespaces n ON n.id=p.namespace_id WHERE v.id=$1`, in.GetPackageVersionId()).Scan(&packageName, &objectRef, &sha, &size, &packageStatus, &packageTenant); e != nil {
		return dbError(e)
	}
	if packageName != in.GetPackageId() || packageTenant != event.GetTenantId() {
		return status.Error(codes.PermissionDenied, "frozen Package does not belong to this tenant or Package")
	}
	object := in.GetPackageObject()
	if packageStatus != "uploaded" || objectRef == "" || objectRef != object.GetStorageObjectId() || object.GetVersionId() != objectRef || sha != object.GetSha256() || size != object.GetSizeBytes() {
		return status.Error(codes.FailedPrecondition, "frozen Package bytes differ from the uploaded object")
	}

	var webuiRaw []byte
	var webuiStatus, webuiDigest, webuiRepository, webuiDigestValue, webuiNamespace string
	if e := s.DB.QueryRowContext(ctx, `SELECT publisher_contract,status,publisher_contract_digest,artifact_repository,artifact_digest,publisher_namespace_id FROM capability.webui_versions WHERE id=$1`, in.GetWebuiVersionId()).Scan(&webuiRaw, &webuiStatus, &webuiDigest, &webuiRepository, &webuiDigestValue, &webuiNamespace); e != nil {
		return dbError(e)
	}
	webui := &api.WebuiPublisherContract{}
	if webuiStatus != "approved" || publicjson.Unmarshal(webuiRaw, webui) != nil {
		return status.Error(codes.FailedPrecondition, "frozen WebUI version is not approved")
	}
	if !proto.Equal(webui.GetImage(), in.GetWebuiArtifact()) || webuiRepository != in.GetWebuiArtifact().GetRepository() || webuiDigestValue != in.GetWebuiArtifact().GetDigest() {
		return status.Error(codes.FailedPrecondition, "frozen WebUI artifact differs from the approved record")
	}
	if canonical, e := canonicalContractDigest(webui); e != nil || canonical != webuiDigest || webuiDigest != in.GetWebuiContractReference().GetDescriptorDigest() {
		return status.Error(codes.FailedPrecondition, "frozen WebUI contract differs from the approved record")
	}
	if !proto.Equal(webui, in.GetWebuiContract()) {
		return status.Error(codes.FailedPrecondition, "frozen WebUI contract bytes differ from the approved record")
	}
	// The Runtime Release is re-read from the Runtime owner, which is the sole
	// authority for its lifecycle status: an approved-and-frozen release that has
	// since been revoked or deprecated must not become a ready CapabilityVersion.
	if s.Runtime == nil {
		return status.Error(codes.Unavailable, "Runtime owner readback is unavailable")
	}
	runtime, e := s.runtimeVersion(ctx, eventCall(event), in.GetRuntimeVersionId())
	if e != nil {
		return e
	}
	if !proto.Equal(runtime.GetPublisherContract(), in.GetRuntimeContract()) || runtime.GetPublisherContractDigest() != in.GetRuntimeContractReference().GetDescriptorDigest() || runtime.GetArtifactDigest() != in.GetRuntimeArtifact().GetDigest() {
		return status.Error(codes.FailedPrecondition, "frozen Runtime Release differs from the approved record")
	}
	for _, publisherID := range []string{webuiNamespace, in.GetRuntimeContract().GetPublisherNamespaceId()} {
		var admitted bool
		if e := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM capability.publisher_namespaces WHERE id=$1 AND status='approved')`, publisherID).Scan(&admitted); e != nil {
			return dbError(e)
		}
		if !admitted {
			return status.Error(codes.FailedPrecondition, "frozen input publisher is no longer approved")
		}
	}
	return s.validateFrozenClaims(ctx, in)
}

// validateFrozenClaims requires one bound claim per frozen input, all recorded
// by the Build owner's committed operation at exactly this snapshot digest.
func (s *Service) validateFrozenClaims(ctx context.Context, in *api.BuildInputSnapshot) error {
	claimIDs := []string{in.GetPackageClaimId(), in.GetRuntimeClaimId(), in.GetWebuiClaimId()}
	if claimIDs[0] == "" || claimIDs[1] == "" || claimIDs[2] == "" {
		return status.Error(codes.FailedPrecondition, "frozen Build input is not bound by its three reference claims")
	}
	wants := []struct {
		kind, target string
	}{
		{"package_version", in.GetPackageVersionId()},
		{"runtime_version", in.GetRuntimeVersionId()},
		{"webui_version", in.GetWebuiVersionId()},
	}
	for i, want := range wants {
		var found bool
		if e := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM capability.reference_claims WHERE id=$1 AND target_type=$2 AND COALESCE(package_version_id,capability_version_id,runtime_version_id,webui_version_id)=$3 AND claimant_owner='build' AND purpose='build' AND released_at IS NULL AND bound_at IS NOT NULL AND bound_input_digest=$4)`, claimIDs[i], want.kind, want.target, in.GetSnapshotDigest()).Scan(&found); e != nil {
			return dbError(e)
		}
		if !found {
			return status.Error(codes.FailedPrecondition, "frozen Build input claim is not bound to this snapshot")
		}
	}
	return nil
}
