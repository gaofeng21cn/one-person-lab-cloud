//go:build livebuild

package build

import (
	"context"
	"database/sql"
	"net"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/ledger/eventconsumer"
)

func liveLedger(t *testing.T, ctx context.Context) (*sql.DB, *lostInboxAck, *lostInboxAck) {
	t.Helper()
	db, err := sql.Open("postgres", startLivePostgres(t, ctx))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	receiver, err := eventconsumer.New(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := receiver.Install(ctx); err != nil {
		t.Fatal(err)
	}
	tls := owneridentity.TLSConfig{AllowInsecureLocal: true}
	tokens := map[owneridentity.Service]string{owneridentity.Build.Service(): strings.Repeat("b", 32), owneridentity.Capability.Service(): strings.Repeat("c", 32)}
	server, err := receiver.NewGRPC(ownerservice.Config{Owner: owneridentity.Ledger, TLS: tls, Peers: tokens})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go server.Serve(listener)
	t.Cleanup(server.Stop)
	connect := func(peer owneridentity.Service) *lostInboxAck {
		opts, err := tls.DialOptions(peer, owneridentity.Ledger.Service(), tokens[peer])
		if err != nil {
			t.Fatal(err)
		}
		conn, err := grpc.NewClient(listener.Addr().String(), opts...)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		return &lostInboxAck{DomainInboxClient: api.NewDomainInboxClient(conn)}
	}
	return db, connect(owneridentity.Build.Service()), connect(owneridentity.Capability.Service())
}

func verifyLedgerEvidence(t *testing.T, ctx context.Context, db *sql.DB, job string, build, capability *lostInboxAck) {
	t.Helper()
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM evidence_receipts WHERE job_id=$1 AND receipt_type IN ('build.artifact_confirmed.v1','capability.version_registered.v1')`, job).Scan(&count); err != nil || count != 2 {
		t.Fatalf("Ledger evidence count=%d: %v", count, err)
	}
	if !build.lost.Load() || !capability.lost.Load() {
		t.Fatal("Ledger lost post-commit acknowledgements not exercised")
	}
	if _, err := capability.DomainInboxClient.Deliver(ctx, build.last); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("Ledger accepted forged producer: %v", err)
	}
	conflicting := proto.Clone(build.last).(*api.DeliverEventRequest)
	conflicting.Event.GetBuildArtifactConfirmed().ArtifactDigest = "sha256:" + strings.Repeat("f", 64)
	if _, err := build.DomainInboxClient.Deliver(ctx, conflicting); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("Ledger replaced immutable event evidence: %v", err)
	}
	t.Log("Ledger persisted both original domain events; lost acknowledgements replay without duplicates; forged producers and conflicting event bytes rejected")
}
