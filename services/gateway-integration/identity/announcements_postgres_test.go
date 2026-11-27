package identity_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/gateway-integration/identity"
	"opl-cloud/services/gateway-integration/migrations"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore/ownerstoretest"
)

const announcementBFFToken = "isolated-announcement-bff-token-00001"

type announcementSystem struct {
	db        *sql.DB
	bffClient api.TenantProductServiceClient
}

// newAnnouncementSystem starts the real CloudIdentity owner over a real
// PostgreSQL database, so the announcement table, the read receipt and the
// idempotency record are the ones the running owner uses.
func newAnnouncementSystem(t *testing.T) announcementSystem {
	t.Helper()
	ctx := t.Context()
	dsn := ownerstoretest.EnsureAdminDSNOrSkip(os.Getenv, t.Skip)
	h, err := ownerstoretest.Setup(ctx, ownerstoretest.Config{
		AdminDSN: dsn, Owner: "tenant", Database: "opl_tenant",
		SchemaOwnerRole: "opl_tenant_owner", WriterRole: "opl_tenant_writer", RuntimeRole: "opl_tenant_runtime",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Close(context.Background()) })
	source, err := migrations.Source()
	if err != nil {
		t.Fatal(err)
	}
	if err = h.Install(ctx, h.OwnerDSN, h.DatabaseName(), source); err != nil {
		t.Fatal(err)
	}
	db, err := h.Open(ctx, h.RuntimeDSN, h.DatabaseName())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	actors := map[string]int64{
		"admin@example.test":     103,
		"customer@example.test":  201,
		"customer2@example.test": 202,
		"directory@example.test": 900,
	}
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/login" {
			var body struct {
				Email    string `json:"email"`
				Password string `json:"password"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil || body.Password != "password" {
				w.WriteHeader(401)
				return
			}
			actor, ok := actors[body.Email]
			if !ok {
				w.WriteHeader(401)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{
					"access_token": "isolated-gateway-" + body.Email,
					"user":         identity.GatewayIdentity{ID: actor, Email: body.Email, Status: "active"},
				},
			})
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/v1/admin/users/") {
			if r.Header.Get("Authorization") != "Bearer isolated-gateway-directory@example.test" {
				w.WriteHeader(401)
				return
			}
			id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/api/v1/admin/users/"), 10, 64)
			if err != nil {
				w.WriteHeader(404)
				return
			}
			for email, candidate := range actors {
				if candidate == id && email != "directory@example.test" {
					_ = json.NewEncoder(w).Encode(map[string]any{
						"code": 0,
						"data": identity.GatewayIdentity{ID: id, Email: email, Status: "active"},
					})
					return
				}
			}
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(404)
	}))
	t.Cleanup(gateway.Close)

	g, err := identity.NewGatewayWithDirectory(gateway.URL, &identity.GatewayDirectory{Email: "directory@example.test", Password: "password"})
	if err != nil {
		t.Fatal(err)
	}
	svc, err := identity.New(db, g, bytes.Repeat([]byte("e"), 32), []string{"103"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	svc.GatewayStore = gatewayStoreSystem(t)

	config := ownerservice.Config{
		Owner: owneridentity.Tenant,
		TLS:   owneridentity.TLSConfig{AllowInsecureLocal: true},
		Peers: map[owneridentity.Service]string{owneridentity.ConsoleBFF: announcementBFFToken},
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server, err := ownerservice.NewServer(config)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.Register(server); err != nil {
		t.Fatal(err)
	}
	go server.ServeOn(listener)
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithUnaryInterceptor(owneridentity.OutboundInterceptor(owneridentity.ConsoleBFF, announcementBFFToken)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })

	return announcementSystem{db: db, bffClient: api.NewTenantProductServiceClient(conn)}
}

// TestAnnouncementOwnerPostgresLifecycle proves the owner owns the publication
// and its receipt: an administrator publishes, a customer reads the announcement,
// records one receipt, and the repeated mark keeps the original timestamp
// instead of writing a second row. The withdrawal then removes the
// announcement from the reader's page and from the readable set.
func TestAnnouncementOwnerPostgresLifecycle(t *testing.T) {
	sys := newAnnouncementSystem(t)
	ctx := t.Context()

	adminCookie, adminSession := memberLogin(t, sys.bffClient, "admin@example.test")
	customerCookie, customerSession := memberLogin(t, sys.bffClient, "customer@example.test")
	adminCall := memberCall(adminSession, adminCookie, "idem-publish-1")
	customerCall := memberCall(customerSession, customerCookie, "idem-read-1")

	// The reader sees nothing before the publication.
	before, err := sys.bffClient.ListAnnouncements(ctx, &api.ListAnnouncementsRpcRequest{Context: customerCall, Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("empty read failed: %v", err)
	}
	if before.GetTotal() != 0 || len(before.GetItems()) != 0 {
		t.Fatalf("unpublished announcement leaked: %+v", before)
	}

	// The administrator publishes one announcement.
	published, err := sys.bffClient.PublishAnnouncement(ctx, &api.PublishAnnouncementRpcRequest{
		Context: adminCall,
		Body:    &api.PublishAnnouncementRequest{Title: "Scheduled maintenance", Body: "The console is unavailable tonight."},
	})
	if err != nil {
		t.Fatalf("publish failed: %v", err)
	}
	if published.GetId() == "" || published.GetStatus() != "published" || published.GetTitle() != "Scheduled maintenance" || published.GetPublishedAt() == "" {
		t.Fatalf("unexpected publication: %+v", published)
	}

	// The same idempotency key publishes no second announcement.
	replay, err := sys.bffClient.PublishAnnouncement(ctx, &api.PublishAnnouncementRpcRequest{
		Context: adminCall,
		Body:    &api.PublishAnnouncementRequest{Title: "Scheduled maintenance", Body: "The console is unavailable tonight."},
	})
	if err != nil {
		t.Fatalf("idempotent publish failed: %v", err)
	}
	if replay.GetId() != published.GetId() {
		t.Fatalf("the same idempotency key published a second announcement: %+v vs %+v", replay, published)
	}
	var announcementRows int
	if err := sys.db.QueryRowContext(ctx, `SELECT count(*) FROM tenant.announcements`).Scan(&announcementRows); err != nil || announcementRows != 1 {
		t.Fatalf("announcement rows=%d err=%v", announcementRows, err)
	}

	// The customer reads the announcement the owner published.
	page, err := sys.bffClient.ListAnnouncements(ctx, &api.ListAnnouncementsRpcRequest{Context: customerCall, Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("customer read failed: %v", err)
	}
	if page.GetTotal() != 1 || len(page.GetItems()) != 1 || page.GetItems()[0].GetId() != published.GetId() || page.GetItems()[0].GetRead() {
		t.Fatalf("customer read did not publish the announcement: %+v", page)
	}

	// The customer records one receipt, and the repeated mark keeps it.
	receipt, err := sys.bffClient.MarkAnnouncementRead(ctx, &api.MarkAnnouncementReadRpcRequest{Context: customerCall, AnnouncementId: published.GetId()})
	if err != nil {
		t.Fatalf("read receipt failed: %v", err)
	}
	if receipt.GetAnnouncementId() != published.GetId() || receipt.GetReadAt() == "" {
		t.Fatalf("unexpected receipt: %+v", receipt)
	}
	repeat, err := sys.bffClient.MarkAnnouncementRead(ctx, &api.MarkAnnouncementReadRpcRequest{Context: customerCall, AnnouncementId: published.GetId()})
	if err != nil {
		t.Fatalf("repeated read receipt failed: %v", err)
	}
	if repeat.GetReadAt() != receipt.GetReadAt() {
		t.Fatalf("the repeated mark changed the receipt timestamp: %q vs %q", repeat.GetReadAt(), receipt.GetReadAt())
	}
	var readRows int
	if err := sys.db.QueryRowContext(ctx, `SELECT count(*) FROM tenant.announcement_reads WHERE announcement_id=$1 AND actor_id=$2`, published.GetId(), customerSession.GetActorId()).Scan(&readRows); err != nil || readRows != 1 {
		t.Fatalf("receipt rows=%d err=%v", readRows, err)
	}

	// The reader now sees the announcement as read.
	page, err = sys.bffClient.ListAnnouncements(ctx, &api.ListAnnouncementsRpcRequest{Context: customerCall, Page: 1, PageSize: 20})
	if err != nil || len(page.GetItems()) != 1 || !page.GetItems()[0].GetRead() {
		t.Fatalf("the receipt did not reach the reader's page: %+v err=%v", page, err)
	}

	// The administrator withdraws the announcement.
	withdrawn, err := sys.bffClient.WithdrawAnnouncement(ctx, &api.WithdrawAnnouncementRpcRequest{
		Context: memberCall(adminSession, adminCookie, "idem-withdraw-1"), AnnouncementId: published.GetId(),
	})
	if err != nil {
		t.Fatalf("withdraw failed: %v", err)
	}
	if withdrawn.GetStatus() != "withdrawn" {
		t.Fatalf("unexpected withdrawal: %+v", withdrawn)
	}

	// The reader no longer sees it and can no longer record a receipt for it.
	page, err = sys.bffClient.ListAnnouncements(ctx, &api.ListAnnouncementsRpcRequest{Context: customerCall, Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("read after withdrawal failed: %v", err)
	}
	if page.GetTotal() != 0 || len(page.GetItems()) != 0 {
		t.Fatalf("the withdrawn announcement is still published: %+v", page)
	}
	if _, err := sys.bffClient.MarkAnnouncementRead(ctx, &api.MarkAnnouncementReadRpcRequest{Context: customerCall, AnnouncementId: published.GetId()}); status.Code(err) != codes.NotFound {
		t.Fatalf("expected NotFound for a withdrawn announcement, got %v", err)
	}
	// The receipt the reader already recorded is preserved.
	var kept int
	if err := sys.db.QueryRowContext(ctx, `SELECT count(*) FROM tenant.announcement_reads WHERE announcement_id=$1 AND actor_id=$2`, published.GetId(), customerSession.GetActorId()).Scan(&kept); err != nil || kept != 1 {
		t.Fatalf("the withdrawal dropped the recorded receipt: rows=%d err=%v", kept, err)
	}
}

// TestAnnouncementOwnerRejectsUnprivilegedPublication proves a customer without
// the platform administrator privilege cannot publish an announcement, and that
// a refused publication leaves no announcement behind.
func TestAnnouncementOwnerRejectsUnprivilegedPublication(t *testing.T) {
	sys := newAnnouncementSystem(t)
	ctx := t.Context()

	customerCookie, customerSession := memberLogin(t, sys.bffClient, "customer@example.test")
	if _, err := sys.bffClient.PublishAnnouncement(ctx, &api.PublishAnnouncementRpcRequest{
		Context: memberCall(customerSession, customerCookie, "idem-customer-publish"),
		Body:    &api.PublishAnnouncementRequest{Title: "Not mine", Body: "A customer may not publish."},
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied for a customer publication, got %v", err)
	}
	var announcementRows int
	if err := sys.db.QueryRowContext(ctx, `SELECT count(*) FROM tenant.announcements`).Scan(&announcementRows); err != nil || announcementRows != 0 {
		t.Fatalf("the refused publication left an announcement: rows=%d err=%v", announcementRows, err)
	}

	// The administrative read is refused for the same reason.
	if _, err := sys.bffClient.ListOperatorAnnouncements(ctx, &api.ListAnnouncementsRpcRequest{
		Context: memberCall(customerSession, customerCookie, ""),
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied for the administrative read, got %v", err)
	}
}

// TestAnnouncementOwnerRejectsAForeignReader proves the receipt belongs to the
// live session's own actor: a call that names another actor is refused, and a
// session that cannot be resolved never reads a receipt.
func TestAnnouncementOwnerRejectsAForeignReader(t *testing.T) {
	sys := newAnnouncementSystem(t)
	ctx := t.Context()

	adminCookie, adminSession := memberLogin(t, sys.bffClient, "admin@example.test")
	customerCookie, customerSession := memberLogin(t, sys.bffClient, "customer@example.test")
	published, err := sys.bffClient.PublishAnnouncement(ctx, &api.PublishAnnouncementRpcRequest{
		Context: memberCall(adminSession, adminCookie, "idem-publish-foreign"),
		Body:    &api.PublishAnnouncementRequest{Title: "Owner bound", Body: "Only the session's own actor reads it."},
	})
	if err != nil {
		t.Fatalf("publish failed: %v", err)
	}

	forged := memberCall(customerSession, customerCookie, "idem-forged")
	forged.ActorId = "999"
	if _, err := sys.bffClient.MarkAnnouncementRead(ctx, &api.MarkAnnouncementReadRpcRequest{Context: forged, AnnouncementId: published.GetId()}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied for a forged actor, got %v", err)
	}
	unknown := &api.CallContext{ActorId: customerSession.GetActorId(), SessionId: proto.String(owneridentity.SessionReference("session_" + strings.Repeat("a", 64))), RequestId: "req-unknown"}
	if _, err := sys.bffClient.ListAnnouncements(ctx, &api.ListAnnouncementsRpcRequest{Context: unknown, Page: 1, PageSize: 20}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("expected Unauthenticated for an unknown session, got %v", err)
	}
	var readRows int
	if err := sys.db.QueryRowContext(ctx, `SELECT count(*) FROM tenant.announcement_reads`).Scan(&readRows); err != nil || readRows != 0 {
		t.Fatalf("a refused reader wrote a receipt: rows=%d err=%v", readRows, err)
	}
}
