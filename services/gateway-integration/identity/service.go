package identity

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
)

type credential struct {
	token string
	until time.Time
}
type Service struct {
	api.UnimplementedTenantProductServiceServer
	api.UnimplementedCloudIdentityAuthorizationServer
	DB          *sql.DB
	Gateway     *Gateway
	BuildCommit api.OwnerCommitReadbackClient
	signing     []byte
	admins      map[string]bool
	mu          sync.Mutex
	credentials map[string]credential
}

// Platform administrators are explicit deployment-owned Gateway subject IDs,
// never inferred from Tenant membership or a browser-supplied role.
func New(db *sql.DB, gateway *Gateway, signing []byte, admins []string) (*Service, error) {
	if db == nil || gateway == nil || len(signing) < 32 {
		return nil, fmt.Errorf("tenant DB, Gateway and session signing secret required")
	}
	s := &Service{DB: db, Gateway: gateway, signing: append([]byte(nil), signing...), admins: map[string]bool{}, credentials: map[string]credential{}}
	for _, a := range admins {
		n, e := strconv.ParseInt(a, 10, 64)
		if e != nil || n <= 0 {
			return nil, fmt.Errorf("platform admins must be Gateway subject IDs")
		}
		s.admins[a] = true
	}
	return s, nil
}
func (s *Service) Register(server *ownerservice.Server) error {
	return server.RegisterGroup("TenantProductService", func(g *grpc.Server) {
		api.RegisterTenantProductServiceServer(g, s)
		api.RegisterCloudIdentityAuthorizationServer(g, s)
	})
}
func randomID() string {
	var b [32]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b[:])
}
func hash(v string) string { h := sha256.Sum256([]byte(v)); return hex.EncodeToString(h[:]) }
func (s *Service) mac(v string) string {
	h := hmac.New(sha256.New, s.signing)
	h.Write([]byte(v))
	return hex.EncodeToString(h.Sum(nil))
}
func denied() error {
	return status.Error(codes.PermissionDenied, "CloudIdentity authorization denied")
}
func persistence(e error) error {
	if e == nil {
		return nil
	}
	if e == sql.ErrNoRows {
		return status.Error(codes.Unauthenticated, "active Cloud session required")
	}
	return status.Error(codes.Unavailable, "CloudIdentity persistence unavailable")
}
func peer(ctx context.Context, want owneridentity.Service) error {
	p, ok := ownerservice.PeerOwner(ctx)
	if !ok || p != want {
		return denied()
	}
	return nil
}
func (s *Service) GetLoginContext(ctx context.Context, _ *api.GetLoginContextRpcRequest) (*api.LoginContext, error) {
	if e := peer(ctx, owneridentity.ConsoleBFF); e != nil {
		return nil, e
	}
	until := time.Now().Add(5 * time.Minute)
	value := strconv.FormatInt(until.Unix(), 10) + "." + randomID()
	token := value + "." + s.mac("login:"+value)
	if e := grpc.SetHeader(ctx, metadata.Pairs(owneridentity.SessionCookieHeader, token)); e != nil {
		return nil, e
	}
	return &api.LoginContext{CsrfToken: s.mac("csrf:" + token), ExpiresAt: timestamppb.New(until)}, nil
}
func (s *Service) Login(ctx context.Context, r *api.LoginRpcRequest) (*api.Session, error) {
	if e := peer(ctx, owneridentity.ConsoleBFF); e != nil {
		return nil, e
	}
	challenge := r.GetContext().GetSessionId()
	parts := strings.Split(challenge, ".")
	if len(parts) != 3 {
		return nil, denied()
	}
	until, e := strconv.ParseInt(parts[0], 10, 64)
	if e != nil || until < time.Now().Unix() || until > time.Now().Add(6*time.Minute).Unix() || !hmac.Equal([]byte(parts[2]), []byte(s.mac("login:"+parts[0]+"."+parts[1]))) {
		return nil, denied()
	}
	user, token, e := s.Gateway.Login(ctx, r.GetBody().GetUsername(), r.GetBody().GetPassword())
	if e != nil {
		return nil, e
	}
	actor := strconv.FormatInt(user.ID, 10)
	var tenant sql.NullString
	e = s.DB.QueryRowContext(ctx, `SELECT tenant_id FROM tenant.tenant_members m JOIN tenant.tenants t ON t.id=m.tenant_id WHERE m.actor_id=$1 AND m.revoked_at IS NULL AND t.status='active'`, actor).Scan(&tenant)
	if e != nil && e != sql.ErrNoRows {
		return nil, persistence(e)
	}
	cookie := randomID()
	ref := owneridentity.SessionReference(cookie)
	expires := time.Now().Add(12 * time.Hour)
	_, e = s.DB.ExecContext(ctx, `INSERT INTO tenant.sessions(id,session_hash,actor_id,tenant_id,gateway_session_ref,csrf_hash,expires_at) VALUES($1,$2,$3,$4,$1,$5,$6)`, ref, hash(cookie), actor, tenant, hash(s.mac("csrf:"+cookie)), expires)
	if e != nil {
		return nil, persistence(e)
	}
	s.mu.Lock()
	for k, v := range s.credentials {
		if !v.until.After(time.Now()) {
			delete(s.credentials, k)
		}
	}
	s.credentials[ref] = credential{token, expires}
	s.mu.Unlock()
	out, _, _, e := s.session(ctx, ref)
	if e != nil {
		return nil, e
	}
	out.CsrfToken = s.mac("csrf:" + cookie)
	if e = grpc.SetHeader(ctx, metadata.Pairs(owneridentity.SessionCookieHeader, cookie)); e != nil {
		return nil, e
	}
	return out, nil
}
func (s *Service) session(ctx context.Context, ref string) (*api.Session, int64, bool, error) {
	out := &api.Session{}
	var tenant, role sql.NullString
	var expires time.Time
	var version int64
	e := s.DB.QueryRowContext(ctx, `SELECT s.actor_id,s.tenant_id,s.expires_at,m.role,COALESCE(t.permission_version,0)+1 FROM tenant.sessions s LEFT JOIN tenant.tenants t ON t.id=s.tenant_id LEFT JOIN tenant.tenant_members m ON m.tenant_id=s.tenant_id AND m.actor_id=s.actor_id AND m.revoked_at IS NULL WHERE s.id=$1 AND s.revoked_at IS NULL AND s.expires_at>now() AND (s.tenant_id IS NULL OR (t.status='active' AND m.id IS NOT NULL))`, ref).Scan(&out.ActorId, &tenant, &expires, &role, &version)
	if e != nil {
		return nil, 0, false, persistence(e)
	}
	s.mu.Lock()
	c, ok := s.credentials[ref]
	s.mu.Unlock()
	if !ok || !c.until.After(time.Now()) {
		return nil, 0, false, status.Error(codes.Unauthenticated, "reauthentication required after credential expiry or process restart")
	}
	user, e := s.Gateway.Read(ctx, c.token, out.ActorId)
	if e != nil {
		return nil, 0, false, e
	}
	out.DisplayName = user.Email
	out.ExpiresAt = timestamppb.New(expires)
	if tenant.Valid {
		out.TenantId = &tenant.String
		v := api.TenantRoleEnum(api.TenantRoleEnum_value["TENANT_ROLE_ENUM_"+strings.ToUpper(role.String)])
		out.Role = &v
	}
	admin := s.admins[out.ActorId]
	for a, p := range actions {
		if permitted(p, role.String, admin) {
			out.Permissions = append(out.Permissions, a)
		}
	}
	sort.Slice(out.Permissions, func(i, j int) bool { return out.Permissions[i] < out.Permissions[j] })
	return out, version, admin, nil
}
func (s *Service) GetSession(ctx context.Context, r *api.GetSessionRpcRequest) (*api.Session, error) {
	if e := peer(ctx, owneridentity.ConsoleBFF); e != nil {
		return nil, e
	}
	raw := r.GetContext().GetSessionId()
	out, _, _, e := s.session(ctx, owneridentity.SessionReference(raw))
	if e != nil {
		return nil, e
	}
	out.CsrfToken = s.mac("csrf:" + raw)
	var expected string
	if e = s.DB.QueryRowContext(ctx, `SELECT csrf_hash FROM tenant.sessions WHERE id=$1`, owneridentity.SessionReference(raw)).Scan(&expected); e != nil {
		return nil, persistence(e)
	}
	if subtle.ConstantTimeCompare([]byte(expected), []byte(hash(out.CsrfToken))) != 1 {
		return nil, denied()
	}
	return out, nil
}
func (s *Service) Logout(ctx context.Context, r *api.LogoutRpcRequest) (*emptypb.Empty, error) {
	if e := peer(ctx, owneridentity.ConsoleBFF); e != nil {
		return nil, e
	}
	ref := r.GetContext().GetSessionId()
	if _, _, _, e := s.session(ctx, ref); e != nil {
		return nil, e
	}
	_, e := s.DB.ExecContext(ctx, `UPDATE tenant.sessions SET revoked_at=now(),updated_at=now() WHERE id=$1`, ref)
	if e != nil {
		return nil, persistence(e)
	}
	s.mu.Lock()
	delete(s.credentials, ref)
	s.mu.Unlock()
	return &emptypb.Empty{}, nil
}
