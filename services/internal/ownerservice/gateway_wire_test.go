package ownerservice

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// wireGatewayProduct is a Gateway owner double that reports the authenticated
// caller of the wallet read, so the test can prove which peer identity the
// deployment unit admitted.
type wireGatewayProduct struct {
	api.UnimplementedGatewayProductServiceServer
	peers chan string
}

func (w *wireGatewayProduct) GetWallet(ctx context.Context, _ *api.GetWalletRpcRequest) (*api.Wallet, error) {
	peer, _ := PeerOwner(ctx)
	w.peers <- peer.String()
	return &api.Wallet{}, nil
}

// TestGatewayOwnerIsReachedThroughItsDeploymentUnitPrincipal proves the wire
// contract on a production-shaped mTLS configuration. The process that serves
// the Gateway owner listens with the deployment unit's identity (tenant), so a
// caller that dials the service name `gateway` cannot complete the handshake,
// while a caller that resolves the deployment unit's principal connects and is
// admitted as itself. The Gateway owner stays the data boundary: the admitted
// caller is `console_bff`, and the authorization audience remains the Gateway
// owner.
func TestGatewayOwnerIsReachedThroughItsDeploymentUnitPrincipal(t *testing.T) {
	certs := testCertificates(t)
	unit, err := NewServer(Config{Owner: OwnerTenant, TLS: certs(OwnerTenant.Service()), Peers: map[Service]string{owneridentity.ConsoleBFF: wireToken}})
	if err != nil {
		t.Fatal(err)
	}
	gateway := &wireGatewayProduct{peers: make(chan string, 1)}
	if err = unit.Register(func(server *grpc.Server) { api.RegisterGatewayProductServiceServer(server, gateway) }); err != nil {
		t.Fatal(err)
	}
	address := startWireServer(t, unit)

	dial := func(target Service) *grpc.ClientConn {
		t.Helper()
		options, err := certs(owneridentity.ConsoleBFF).DialOptions(owneridentity.ConsoleBFF, target, wireToken)
		if err != nil {
			t.Fatal(err)
		}
		conn, err := grpc.NewClient(address, options...)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		return conn
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := api.NewGatewayProductServiceClient(dial(owneridentity.DeploymentUnitTransportPrincipal(owneridentity.Gateway))).GetWallet(ctx, &api.GetWalletRpcRequest{}); err != nil {
		t.Fatalf("dialing the deployment unit principal must reach the Gateway owner: %v", err)
	}
	select {
	case peer := <-gateway.peers:
		if peer != owneridentity.ConsoleBFF.String() {
			t.Fatalf("admitted peer = %q, want %q", peer, owneridentity.ConsoleBFF)
		}
	default:
		t.Fatal("the Gateway owner did not observe its admitted caller")
	}

	if _, err := api.NewGatewayProductServiceClient(dial(owneridentity.Gateway.Service())).GetWallet(ctx, &api.GetWalletRpcRequest{}); status.Code(err) != codes.Unavailable {
		t.Fatalf("dialing the service name gateway must fail the handshake, got %v", err)
	}
}
