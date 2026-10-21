package catalog

import (
	"context"
	"google.golang.org/grpc"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/packages/contracts/go/publicjson"
	"opl-cloud/services/internal/ownerservice"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
)

// This owner-local check uses the real uploaded Package, publisher and WebUI
// rows. The livebuild owner chain separately exercises the real Runtime and
// CloudIdentity services; its accepted grant must also survive this boundary.
func TestRegistrationRequiresOriginalBuildContinuationAndReplaysCommittedEvent(t *testing.T) {
	o := gapNewOwner(t)
	o.seedPublisher(t)
	runtimeContract, webuiContract := gapRuntimeContract(t), gapWebuiContract(t)
	archive := candidateZIPBytes(t)
	packageID, sha := o.uploadedPackage(t, "tenant-gap", "gap-ns", "gap-pkg", archive)
	webuiID := o.seedWebui(t, webuiContract, "approved")
	runtime := gapApprovedRuntime(t, runtimeContract, "runtime-gap")
	artifact := gapArtifact("registry.example/official/agent", runtimeContract.GetImage().GetPlatform())

	for _, test := range []struct {
		name   string
		change func(*api.BuildArtifactReadback)
	}{
		{"missing", func(r *api.BuildArtifactReadback) { r.RegistrationContext = nil }},
		{"missing-actor", func(r *api.BuildArtifactReadback) { r.RegistrationContext.ActorId = "" }},
		{"missing-grant", func(r *api.BuildArtifactReadback) { r.RegistrationContext.AcceptedOperationGrantId = nil }},
		{"browser-session", func(r *api.BuildArtifactReadback) {
			r.RegistrationContext.SessionId = proto.String("expired-browser-session")
		}},
		{"interactive-context", func(r *api.BuildArtifactReadback) {
			r.RegistrationContext.AuthorizationContextId = "interactive-decision"
		}},
		{"other-tenant", func(r *api.BuildArtifactReadback) { r.RegistrationContext.Scope.GetTenant().TenantId = "other-tenant" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			job := "job-" + test.name
			input := o.gapSnapshot(t, runtime, runtimeContract, webuiID, webuiContract, "gap-pkg", packageID, int64(len(archive)), sha)
			o.gapSeedInputs(t, job, "tenant-gap", "gap-ns", "gap-pkg", packageID, webuiID, runtime.Id, input)
			if _, err := o.gapDeliver(t, job, input, gapDescriptorFor(input, artifact), artifact, runtime, "event-"+job, test.change); status.Code(err) != codes.Unauthenticated {
				t.Fatalf("invalid registration continuation admitted: %v", err)
			}
			if count := o.registeredVersionCount(t); count != 0 {
				t.Fatalf("rejected continuation registered %d versions", count)
			}
		})
	}

	input := o.gapSnapshot(t, runtime, runtimeContract, webuiID, webuiContract, "gap-pkg", packageID, int64(len(archive)), sha)
	o.gapSeedInputs(t, "job-accepted", "tenant-gap", "gap-ns", "gap-pkg", packageID, webuiID, runtime.Id, input)
	descriptor := gapDescriptorFor(input, artifact)
	ack, err := o.gapDeliver(t, "job-accepted", input, descriptor, artifact, runtime, "event-accepted", nil)
	if err != nil || !ack.GetCommitted() || ack.GetDuplicate() {
		t.Fatalf("original registration: ack=%v error=%v", ack, err)
	}
	var persistedGrant bool
	if err := o.db.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM capability.capability_versions WHERE provenance_evidence ? 'registrationContext')`).Scan(&persistedGrant); err != nil || persistedGrant {
		t.Fatalf("transient authorization entered artifact provenance: present=%v error=%v", persistedGrant, err)
	}

	if _, err = o.db.ExecContext(t.Context(), `UPDATE capability.webui_versions SET status='revoked' WHERE id=$1`, webuiID); err != nil {
		t.Fatal(err)
	}
	runtime.Status = api.RuntimeVersionStatusEnum_RUNTIME_VERSION_STATUS_ENUM_REVOKED
	// A duplicate that lost its ACK is already committed. It needs neither a
	// fresh Runtime approval nor another registration authorization/readback.
	ack, err = o.gapDeliver(t, "job-accepted", input, descriptor, artifact, runtime, "event-accepted", func(r *api.BuildArtifactReadback) {
		r.RegistrationContext = nil
		r.Outcome = api.Observation_OBSERVATION_UNKNOWN
	})
	if err != nil || !ack.GetCommitted() || !ack.GetDuplicate() || o.registeredVersionCount(t) != 1 {
		t.Fatalf("committed registration did not replay: ack=%v error=%v", ack, err)
	}
	// The same event id with changed immutable payload must still conflict.
	_, err = o.gapDeliver(t, "job-accepted", input, descriptor, artifact, runtime, "event-accepted", func(r *api.BuildArtifactReadback) {
		r.ArtifactReceiptId = "different-artifact-receipt"
	})
	if status.Code(err) != codes.AlreadyExists {
		t.Fatalf("conflicting replay accepted: %v", err)
	}
}

// The remote owner blocks at a deterministic barrier. Both duplicate deliveries
// must reach it even with one local connection; no transaction may hold that
// connection while waiting for owner readback.
type registrationBarrier struct {
	api.BuildCoordinationClient
	entered  chan struct{}
	release  <-chan struct{}
	artifact *api.BuildArtifactReadback
}

func (b *registrationBarrier) ReadArtifact(ctx context.Context, _ *api.ReadBuildArtifactRequest, _ ...grpc.CallOption) (*api.BuildArtifactReadback, error) {
	b.entered <- struct{}{}
	select {
	case <-b.release:
		return proto.Clone(b.artifact).(*api.BuildArtifactReadback), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestConcurrentRegistrationDoesNotHoldPoolAcrossOwnerReadback(t *testing.T) {
	o := gapNewOwner(t)
	o.seedPublisher(t)
	runtimeContract, webuiContract := gapRuntimeContract(t), gapWebuiContract(t)
	archive := candidateZIPBytes(t)
	packageVersionID, sha := o.uploadedPackage(t, "tenant-gap", "gap-ns", "gap-pkg", archive)
	webuiID := o.seedWebui(t, webuiContract, "approved")
	runtime := gapApprovedRuntime(t, runtimeContract, "runtime-concurrent")
	artifact := gapArtifact("registry.example/official/agent", runtimeContract.GetImage().GetPlatform())
	input := o.gapSnapshot(t, runtime, runtimeContract, webuiID, webuiContract, "gap-pkg", packageVersionID, int64(len(archive)), sha)
	const job = "job-concurrent"
	o.gapSeedInputs(t, job, "tenant-gap", "gap-ns", "gap-pkg", packageVersionID, webuiID, runtime.Id, input)
	descriptor := gapDescriptorFor(input, artifact)
	encoded, err := publicjson.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	registration := gapTenantCall("tenant-gap", "registration")
	registration.SessionId = nil
	registration.AcceptedOperationGrantId = proto.String("accepted-build-grant")
	readback := &api.BuildArtifactReadback{BuildJobId: job, Input: input, Artifact: artifact, ArtifactReceiptId: "artifact-concurrent", VersionLabel: job,
		Outcome: api.Observation_OBSERVATION_CONFIRMED, DeploymentDescriptor: descriptor, DeploymentDescriptorDigest: digest(encoded), DeploymentDescriptorObjectRef: "descriptor-concurrent", RegistrationContext: registration}
	request := &api.DeliverEventRequest{AuthenticatedProducer: "build", Event: &api.EventEnvelope{
		EventId: "event-concurrent", EventType: "build.artifact_confirmed.v1", SchemaVersion: 1, Owner: "build", TenantId: "tenant-gap", Scope: "tenant", AggregateId: job, AggregateVersion: 1, RequestId: "request-concurrent",
		Payload: &api.EventEnvelope_BuildArtifactConfirmed{BuildArtifactConfirmed: &api.BuildArtifactConfirmedEvent{BuildJobId: job, PackageVersionId: packageVersionID, RuntimeVersionId: runtime.Id, WebuiVersionId: webuiID, ArtifactDigest: artifact.Digest, ArtifactReceiptId: readback.ArtifactReceiptId, DeploymentDescriptorDigest: readback.DeploymentDescriptorDigest}}}}
	release := make(chan struct{})
	barrier := &registrationBarrier{entered: make(chan struct{}, 2), release: release, artifact: readback}
	o.service.Build = barrier
	o.service.Runtime = &gapReadback{runtime: runtime}
	o.db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(ownerservice.WithPeerOwner(t.Context(), owneridentity.Build.Service()), 4*time.Second)
	defer cancel()
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	type result struct {
		ack *api.InboxAck
		err error
	}
	results := make(chan result, 2)
	for i := 0; i < 2; i++ {
		go func() { ack, err := o.service.Deliver(ctx, request); results <- result{ack, err} }()
	}
	for i := 0; i < 2; i++ {
		select {
		case <-barrier.entered:
		case <-ctx.Done():
			t.Fatal("duplicate delivery retained the local connection across owner readback")
		}
	}
	// Releasing both owner calls proves that the final transaction recheck, rather
	// than a lock held across RPC, gives one mutation and one duplicate ACK.
	releaseOnce.Do(func() { close(release) })
	duplicates := 0
	for i := 0; i < 2; i++ {
		select {
		case r := <-results:
			if r.err != nil || !r.ack.GetCommitted() {
				t.Fatalf("concurrent registration ack=%v err=%v", r.ack, r.err)
			}
			if r.ack.GetDuplicate() {
				duplicates++
			}
		case <-ctx.Done():
			t.Fatal("concurrent registration did not commit")
		}
	}
	if duplicates != 1 || o.registeredVersionCount(t) != 1 {
		t.Fatalf("duplicates=%d, expected one committed Capability version", duplicates)
	}
}
