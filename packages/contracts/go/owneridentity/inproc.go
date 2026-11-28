package owneridentity

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

const inprocPipeBufferSize = 256 * 1024

// InProcAuthInfo records the verified calling service identity attached to an
// in-process gRPC connection without going through TCP or TLS handshakes.
type InProcAuthInfo struct {
	Caller Service
}

// AuthType identifies in-process authenticated credentials.
func (InProcAuthInfo) AuthType() string { return "inproc" }

type inprocServerConn struct {
	net.Conn
	caller Service
}

type inprocAddr string

func (a inprocAddr) Network() string { return "inproc" }
func (a inprocAddr) String() string  { return string(a) }

// InProcTransport routes intra-process owner gRPC calls directly through
// buffered in-memory pipes while preserving caller identity propagation and
// leaving external TCP+mTLS listeners active.
type InProcTransport struct {
	mu        sync.Mutex
	listeners map[Service]*inprocListener
}

// NewInProcTransport returns an empty in-process gRPC transport registry.
func NewInProcTransport() *InProcTransport {
	return &InProcTransport{listeners: make(map[Service]*inprocListener)}
}

// HasTarget reports whether target currently accepts in-process connections.
func (t *InProcTransport) HasTarget(target Service) bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	_, ok := t.listeners[target]
	t.mu.Unlock()
	return ok
}

// Listen registers an active in-process listener for target.
func (t *InProcTransport) Listen(target Service) (net.Listener, error) {
	if t == nil {
		return nil, errors.New("in-process transport is nil")
	}
	if !target.Valid() {
		return nil, fmt.Errorf("unknown service identity %q", target)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, exists := t.listeners[target]; exists {
		return nil, fmt.Errorf("in-process listener already registered for %s", target)
	}
	ln := &inprocListener{
		transport: t,
		target:    target,
		ch:        make(chan net.Conn, 64),
		done:      make(chan struct{}),
	}
	t.listeners[target] = ln
	return ln, nil
}

// Dial opens an in-process bidirectional connection tagged with the verified
// caller identity to target's registered listener.
func (t *InProcTransport) Dial(ctx context.Context, caller, target Service) (net.Conn, error) {
	if t == nil {
		return nil, errors.New("in-process transport is nil")
	}
	if !caller.Valid() || !target.Valid() {
		return nil, errors.New("known caller and target service identities required")
	}
	t.mu.Lock()
	ln := t.listeners[target]
	t.mu.Unlock()
	if ln == nil {
		return nil, fmt.Errorf("inproc target %s not listening", target)
	}

	clientAddr := inprocAddr("inproc://" + caller.String())
	serverAddr := inprocAddr("inproc://" + target.String())
	clientConn, serverConn := newInProcPipePair(clientAddr, serverAddr)
	tagged := &inprocServerConn{Conn: serverConn, caller: caller}

	select {
	case <-ln.done:
		_ = clientConn.Close()
		return nil, net.ErrClosed
	case <-ctx.Done():
		_ = clientConn.Close()
		return nil, ctx.Err()
	default:
	}

	select {
	case ln.ch <- tagged:
		return clientConn, nil
	case <-ln.done:
		_ = clientConn.Close()
		return nil, net.ErrClosed
	case <-ctx.Done():
		_ = clientConn.Close()
		return nil, ctx.Err()
	}
}

type inprocListener struct {
	transport *InProcTransport
	target    Service
	ch        chan net.Conn
	done      chan struct{}
	closeOnce sync.Once
}

func (l *inprocListener) Accept() (net.Conn, error) {
	select {
	case <-l.done:
		return nil, net.ErrClosed
	default:
	}
	select {
	case conn := <-l.ch:
		return conn, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *inprocListener) Close() error {
	l.closeOnce.Do(func() {
		close(l.done)
		l.transport.mu.Lock()
		if l.transport.listeners[l.target] == l {
			delete(l.transport.listeners, l.target)
		}
		l.transport.mu.Unlock()
		for {
			select {
			case conn := <-l.ch:
				_ = conn.Close()
			default:
				return
			}
		}
	})
	return nil
}

func (l *inprocListener) Addr() net.Addr {
	return inprocAddr("inproc://" + l.target.String())
}

type inprocAwareCreds struct {
	fallback credentials.TransportCredentials
}

func newInProcAwareCreds(fallback credentials.TransportCredentials) credentials.TransportCredentials {
	if fallback == nil {
		fallback = insecure.NewCredentials()
	}
	return &inprocAwareCreds{fallback: fallback}
}

func (c *inprocAwareCreds) ServerHandshake(raw net.Conn) (net.Conn, credentials.AuthInfo, error) {
	if sc, ok := raw.(*inprocServerConn); ok {
		return sc, InProcAuthInfo{Caller: sc.caller}, nil
	}
	return c.fallback.ServerHandshake(raw)
}

func (c *inprocAwareCreds) ClientHandshake(ctx context.Context, authority string, raw net.Conn) (net.Conn, credentials.AuthInfo, error) {
	return c.fallback.ClientHandshake(ctx, authority, raw)
}

func (c *inprocAwareCreds) Info() credentials.ProtocolInfo {
	return c.fallback.Info()
}

func (c *inprocAwareCreds) Clone() credentials.TransportCredentials {
	return &inprocAwareCreds{fallback: c.fallback.Clone()}
}

func (c *inprocAwareCreds) OverrideServerName(name string) error {
	return c.fallback.OverrideServerName(name)
}

func newInProcPipePair(clientAddr, serverAddr net.Addr) (net.Conn, net.Conn) {
	c2s := newPipeHalf(inprocPipeBufferSize)
	s2c := newPipeHalf(inprocPipeBufferSize)
	return &memConn{in: s2c, out: c2s, local: clientAddr, remote: serverAddr},
		&memConn{in: c2s, out: s2c, local: serverAddr, remote: clientAddr}
}

type pipeHalf struct {
	mu            sync.Mutex
	cond          *sync.Cond
	buf           []byte
	head          int
	size          int
	closed        bool
	readDeadline  time.Time
	writeDeadline time.Time
	readTimer     *time.Timer
	writeTimer    *time.Timer
}

func newPipeHalf(capacity int) *pipeHalf {
	h := &pipeHalf{buf: make([]byte, capacity)}
	h.cond = sync.NewCond(&h.mu)
	return h
}

func (h *pipeHalf) Read(p []byte) (int, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(p) == 0 {
		if h.closed {
			return 0, io.EOF
		}
		if err := h.readDeadlineErrLocked(); err != nil {
			return 0, err
		}
		return 0, nil
	}
	for h.size == 0 {
		if h.closed {
			return 0, io.EOF
		}
		if err := h.readDeadlineErrLocked(); err != nil {
			return 0, err
		}
		h.cond.Wait()
	}
	if err := h.readDeadlineErrLocked(); err != nil {
		return 0, err
	}
	n := h.popLocked(p)
	h.cond.Broadcast()
	return n, nil
}

func (h *pipeHalf) Write(p []byte) (int, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(p) == 0 {
		if h.closed {
			return 0, net.ErrClosed
		}
		if err := h.writeDeadlineErrLocked(); err != nil {
			return 0, err
		}
		return 0, nil
	}
	total := 0
	for total < len(p) {
		if h.closed {
			return total, net.ErrClosed
		}
		if err := h.writeDeadlineErrLocked(); err != nil {
			return total, err
		}
		if h.size == cap(h.buf) {
			h.cond.Wait()
			continue
		}
		n := h.pushLocked(p[total:])
		total += n
		h.cond.Broadcast()
	}
	return total, nil
}

func (h *pipeHalf) pushLocked(p []byte) int {
	avail := cap(h.buf) - h.size
	if avail == 0 || len(p) == 0 {
		return 0
	}
	n := len(p)
	if n > avail {
		n = avail
	}
	tail := (h.head + h.size) % cap(h.buf)
	first := cap(h.buf) - tail
	if first >= n {
		copy(h.buf[tail:tail+n], p[:n])
	} else {
		copy(h.buf[tail:], p[:first])
		copy(h.buf[:n-first], p[first:n])
	}
	h.size += n
	return n
}

func (h *pipeHalf) popLocked(p []byte) int {
	if h.size == 0 || len(p) == 0 {
		return 0
	}
	n := len(p)
	if n > h.size {
		n = h.size
	}
	first := cap(h.buf) - h.head
	if first >= n {
		copy(p[:n], h.buf[h.head:h.head+n])
	} else {
		copy(p[:first], h.buf[h.head:])
		copy(p[first:n], h.buf[:n-first])
	}
	h.head = (h.head + n) % cap(h.buf)
	h.size -= n
	return n
}

func (h *pipeHalf) readDeadlineErrLocked() error {
	if !h.readDeadline.IsZero() && !time.Now().Before(h.readDeadline) {
		return os.ErrDeadlineExceeded
	}
	return nil
}

func (h *pipeHalf) writeDeadlineErrLocked() error {
	if !h.writeDeadline.IsZero() && !time.Now().Before(h.writeDeadline) {
		return os.ErrDeadlineExceeded
	}
	return nil
}

func (h *pipeHalf) setReadDeadline(t time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.readTimer != nil {
		h.readTimer.Stop()
		h.readTimer = nil
	}
	h.readDeadline = t
	if !t.IsZero() {
		d := time.Until(t)
		if d <= 0 {
			h.cond.Broadcast()
			return
		}
		var timer *time.Timer
		timer = time.AfterFunc(d, func() {
			h.mu.Lock()
			if h.readTimer == timer {
				h.cond.Broadcast()
			}
			h.mu.Unlock()
		})
		h.readTimer = timer
	}
	h.cond.Broadcast()
}

func (h *pipeHalf) setWriteDeadline(t time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.writeTimer != nil {
		h.writeTimer.Stop()
		h.writeTimer = nil
	}
	h.writeDeadline = t
	if !t.IsZero() {
		d := time.Until(t)
		if d <= 0 {
			h.cond.Broadcast()
			return
		}
		var timer *time.Timer
		timer = time.AfterFunc(d, func() {
			h.mu.Lock()
			if h.writeTimer == timer {
				h.cond.Broadcast()
			}
			h.mu.Unlock()
		})
		h.writeTimer = timer
	}
	h.cond.Broadcast()
}

func (h *pipeHalf) close() {
	h.mu.Lock()
	h.closed = true
	if h.readTimer != nil {
		h.readTimer.Stop()
		h.readTimer = nil
	}
	if h.writeTimer != nil {
		h.writeTimer.Stop()
		h.writeTimer = nil
	}
	h.cond.Broadcast()
	h.mu.Unlock()
}

type memConn struct {
	in     *pipeHalf
	out    *pipeHalf
	local  net.Addr
	remote net.Addr
}

func (c *memConn) Read(b []byte) (int, error)         { return c.in.Read(b) }
func (c *memConn) Write(b []byte) (int, error)        { return c.out.Write(b) }
func (c *memConn) Close() error                       { c.in.close(); c.out.close(); return nil }
func (c *memConn) LocalAddr() net.Addr                { return c.local }
func (c *memConn) RemoteAddr() net.Addr               { return c.remote }
func (c *memConn) SetDeadline(t time.Time) error      { c.in.setReadDeadline(t); c.out.setWriteDeadline(t); return nil }
func (c *memConn) SetReadDeadline(t time.Time) error  { c.in.setReadDeadline(t); return nil }
func (c *memConn) SetWriteDeadline(t time.Time) error { c.out.setWriteDeadline(t); return nil }
