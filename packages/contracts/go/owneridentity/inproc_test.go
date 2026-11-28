package owneridentity

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

func TestInProcTransportGRPCAndAuthInfo(t *testing.T) {
	transport := NewInProcTransport()
	ln, err := transport.Listen(Tenant.Service())
	if err != nil {
		t.Fatalf("listen inproc: %v", err)
	}
	defer ln.Close()

	if _, err := transport.Listen(Tenant.Service()); err == nil {
		t.Fatal("expected duplicate inproc listener registration to fail")
	}

	serverCreds, err := (TLSConfig{AllowInsecureLocal: true, InProc: transport}).ServerCredentials(Tenant.Service())
	if err != nil {
		t.Fatalf("server credentials: %v", err)
	}

	var observedAuth InProcAuthInfo
	srv := grpc.NewServer(
		grpc.Creds(serverCreds),
		grpc.ChainUnaryInterceptor(func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			p, ok := peer.FromContext(ctx)
			if !ok {
				return nil, status.Error(codes.Unauthenticated, "missing peer")
			}
			ai, ok := p.AuthInfo.(InProcAuthInfo)
			if !ok {
				return nil, status.Error(codes.Unauthenticated, "missing inproc auth info")
			}
			observedAuth = ai
			return handler(ctx, req)
		}),
	)
	hs := health.NewServer()
	hs.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(srv, hs)
	go func() { _ = srv.Serve(ln) }()
	defer srv.Stop()

	dialOpts, err := (TLSConfig{InProc: transport}).DialOptions(Workspace.Service(), Tenant.Service(), "0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("dial options: %v", err)
	}
	conn, err := grpc.NewClient("127.0.0.1:8187", dialOpts...)
	if err != nil {
		t.Fatalf("grpc new client: %v", err)
	}
	defer conn.Close()

	resp, err := healthpb.NewHealthClient(conn).Check(context.Background(), &healthpb.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}
	if resp.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		t.Fatalf("status=%v want SERVING", resp.GetStatus())
	}
	if observedAuth.Caller != Workspace.Service() {
		t.Fatalf("observed caller=%s want %s", observedAuth.Caller, Workspace.Service())
	}

	// Verify an un-tagged TCP connection hitting the same ServerCredentials does NOT receive InProcAuthInfo.
	tcpLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("tcp listen: %v", err)
	}
	defer tcpLn.Close()
	go func() { _ = srv.Serve(tcpLn) }()

	tcpConn, err := grpc.NewClient(tcpLn.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("tcp dial: %v", err)
	}
	defer tcpConn.Close()
	if _, err := healthpb.NewHealthClient(tcpConn).Check(context.Background(), &healthpb.HealthCheckRequest{}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("expected Unauthenticated over raw insecure TCP without InProcAuthInfo, got %v", err)
	}
}

func TestInProcPipeLargeConcurrentTransferAndDeadlines(t *testing.T) {
	c1, c2 := newInProcPipePair(inprocAddr("inproc://a"), inprocAddr("inproc://b"))
	defer c1.Close()
	defer c2.Close()

	payload := bytes.Repeat([]byte("opl-cloud-inproc-"), 40*1024) // >640 KiB (> 256 KiB ring)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if _, err := c1.Write(payload); err != nil {
			t.Errorf("write large payload: %v", err)
		}
	}()

	got := make([]byte, len(payload))
	read := 0
	for read < len(payload) {
		n, err := c2.Read(got[read:])
		if err != nil {
			t.Fatalf("read failed at %d: %v", read, err)
		}
		read += n
	}
	wg.Wait()
	if !bytes.Equal(got, payload) {
		t.Fatal("large payload mismatch")
	}

	if err := c2.SetReadDeadline(time.Now().Add(20 * time.Millisecond)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	buf := make([]byte, 16)
	if _, err := c2.Read(buf); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
	_ = c2.SetReadDeadline(time.Time{})

	if err := c1.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := c1.Write([]byte("after-close")); err == nil {
		t.Fatal("expected error writing to closed conn")
	}
}
