package delivery

// The Serve access entry is served by the Serve process itself. Binding it
// eagerly and reporting it as a readiness dependency means an installation whose
// entry cannot listen reports NOT_SERVING instead of accepting application
// traffic it will never answer.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"opl-cloud/services/internal/ownerservice"
)

// startAccessEntry binds and serves the Workspace application entry for the life
// of the process.
func startAccessEntry(server *ownerservice.Server, entry *AccessEntry, address string) error {
	address = strings.TrimSpace(address)
	if address == "" {
		address = accessDefaultAddress
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("serve access entry %s: %w", address, err)
	}
	httpServer := &http.Server{
		Handler: entry.Handler(),
		// A streaming application response is not bounded by a write deadline:
		// the entry must not cut a long-lived SSE or chunked response.
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       5 * time.Minute,
	}
	if err := server.TrackCloser(&accessEntryServer{server: httpServer, listener: listener}); err != nil {
		_ = listener.Close()
		return err
	}
	go func() {
		log.Printf("serve access entry listening on %s", listener.Addr())
		if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("serve access entry stopped: %v", err)
		}
	}()
	return server.AddReadinessCheck("access_entry", func(ctx context.Context) error {
		dialer := &net.Dialer{Timeout: 2 * time.Second}
		conn, err := dialer.DialContext(ctx, "tcp", listener.Addr().String())
		if err != nil {
			return fmt.Errorf("serve access entry %s does not accept connections: %w", listener.Addr(), err)
		}
		return conn.Close()
	})
}

// accessEntryServer closes the entry's listener and drains its requests when the
// owner bootstrap stops.
type accessEntryServer struct {
	server   *http.Server
	listener net.Listener
}

func (a *accessEntryServer) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = a.server.Shutdown(ctx)
	return a.listener.Close()
}
