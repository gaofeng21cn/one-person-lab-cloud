package ownerservice

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

func TestInProcWireAuthAndDualTCPHealth(t *testing.T) {
	certs := testCertificates(t)
	transport := owneridentity.NewInProcTransport()

	idTLS := certs(OwnerTenant.Service())
	idTLS.InProc = transport
	identity, err := NewServer(Config{
		Owner: OwnerTenant,
		TLS:   idTLS,
		Peers: map[Service]string{OwnerServe.Service(): wireToken},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = identity.RegisterGroup("CloudIdentityAuthorization", func(s *grpc.Server) {
		api.RegisterCloudIdentityAuthorizationServer(s, &wireIdentity{})
	})
	idAddr := startWireServer(t, identity)
	if err := identity.MarkServing(context.Background()); err != nil {
		t.Fatalf("mark identity serving: %v", err)
	}

	serveClientTLS := certs(OwnerServe.Service())
	serveClientTLS.InProc = transport
	auth, idConn, err := AuthorizerFromConfig(Config{
		Owner:              OwnerServe,
		TLS:                serveClientTLS,
		CloudIdentityAddr:  idAddr,
		CloudIdentityToken: wireToken,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = idConn.Close() })

	serveTLS := certs(OwnerServe.Service())
	serveTLS.InProc = transport
	owner, err := NewServer(Config{
		Owner: OwnerServe,
		TLS:   serveTLS,
		Peers: map[Service]string{
			owneridentity.ConsoleBFF: wireToken,
			OwnerBuild.Service():     wireToken,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = owner.RegisterGroup("ServeProductService", func(s *grpc.Server) {
		api.RegisterOwnerOperationsServer(s, &guardedOperations{auth: auth})
	})
	ownerAddr := startWireServer(t, owner)
	if err := owner.MarkServing(context.Background()); err != nil {
		t.Fatalf("mark serve serving: %v", err)
	}

	req := func(tenant string) *api.OwnerOperationRequest {
		session := "session-a"
		return &api.OwnerOperationRequest{
			OperationId: "op-inproc",
			Context: &api.CallContext{
				ActorId:   "actor-a",
				SessionId: &session,
				RequestId: "req-inproc",
				Scope:     &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: tenant}}},
			},
		}
	}

	// Authorized in-process call succeeds and reaches handler + in-proc CloudIdentity authorizer.
	bffTLS := certs(owneridentity.ConsoleBFF)
	bffTLS.InProc = transport
	dialOpts, err := bffTLS.DialOptions(owneridentity.ConsoleBFF, OwnerServe.Service(), wireToken)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := grpc.NewClient(ownerAddr, dialOpts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := api.NewOwnerOperationsClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := client.Read(ctx, req("tenant-a")); err != nil {
		t.Fatalf("authorized in-proc read failed: %v", err)
	}
	if _, err := client.Read(ctx, req("tenant-b")); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("cross tenant code=%s want PermissionDenied", status.Code(err))
	}

	// Header impersonation over in-proc fails closed (inproc.Caller != header owner).
	spoofOpts, err := bffTLS.DialOptions(owneridentity.ConsoleBFF, OwnerServe.Service(), wireToken)
	if err != nil {
		t.Fatal(err)
	}
	spoofOpts = append(spoofOpts, grpc.WithChainUnaryInterceptor(owneridentity.OutboundInterceptor(OwnerBuild.Service(), wireToken)))
	spoofConn, err := grpc.NewClient(ownerAddr, spoofOpts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = spoofConn.Close() })
	if _, err := api.NewOwnerOperationsClient(spoofConn).Read(ctx, req("tenant-a")); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("spoofed header code=%s want Unauthenticated", status.Code(err))
	}

	// Wrong peer token over in-proc is refused.
	badTokOpts, err := bffTLS.DialOptions(owneridentity.ConsoleBFF, OwnerServe.Service(), "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz")
	if err != nil {
		t.Fatal(err)
	}
	badTokConn, err := grpc.NewClient(ownerAddr, badTokOpts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = badTokConn.Close() })
	if _, err := api.NewOwnerOperationsClient(badTokConn).Read(ctx, req("tenant-a")); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("wrong token code=%s want Unauthenticated", status.Code(err))
	}

	// External TCP + mTLS health check still works simultaneously on the TCP listener.
	healthOpts, err := certs(OwnerServe.Service()).HealthDialOptions(OwnerServe.Service(), OwnerServe.Service())
	if err != nil {
		t.Fatal(err)
	}
	healthConn, err := grpc.NewClient(ownerAddr, healthOpts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = healthConn.Close() })
	hResp, err := healthpb.NewHealthClient(healthConn).Check(ctx, &healthpb.HealthCheckRequest{})
	if err != nil || hResp.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		t.Fatalf("TCP mTLS health check failed: resp=%v err=%v", hResp, err)
	}
}
