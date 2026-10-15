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
	"opl-cloud/services/internal/ownerstore"
)

type Service struct {
	api.UnimplementedTenantProductServiceServer
	api.UnimplementedCloudIdentityAuthorizationServer
	api.UnimplementedGatewayProductServiceServer
	api.UnimplementedGatewayCoordinationServer
	DB              *sql.DB
	store           *ownerstore.Store
	Gateway         *Gateway
	BuildCommit     api.OwnerCommitReadbackClient
	WorkspaceCommit api.OwnerCommitReadbackClient
	// InvitationTTL is how long a pending invitation may be accepted. The
	// canonical contract fixes Invitation.expiresAt but names no window, so the
	// deployment supplies it explicitly here rather than this owner baking a
	// product decision into code.
	InvitationTTL time.Duration
	signing       []byte
	admins        map[string]bool
	mu            sync.Mutex
	// RegistryHost / RegistryNamespace are installation facts used to reserve a
	// User's application OCI destination. They carry no credential.
	registryHost      string
	registryNamespace string

	// GatewayStore is required before service registration or authentication.
	// Secret delivery remains separately configured for managed-key operations.
	GatewayStore *GatewayStore
	SecretStore  SecretStore
	KeyIssuer    ManagedKeyIssuer
	// WorkspaceKeyGroup is the approved Sub2API group a Workspace-managed key is
	// placed in. It is a deployment fact (OPL_GATEWAY_WORKSPACE_KEY_GROUP): the
	// group carries the approved model allowlist the issuer resolves for a
	// command whose declared selection names no model.
	WorkspaceKeyGroup string
}

// Platform administrators are explicit deployment-owned Gateway subject IDs,
// never inferred from Tenant membership or a browser-supplied role.
func New(db *sql.DB, gateway *Gateway, signing []byte, admins []string, invitationTTL time.Duration) (*Service, error) {
	if db == nil || gateway == nil || len(signing) < 32 {
		return nil, fmt.Errorf("tenant DB, Gateway and session signing secret required")
	}
	if !gateway.DirectoryConfigured() {
		return nil, fmt.Errorf("Gateway directory identity required for session authorization")
	}
	if invitationTTL <= 0 {
		return nil, fmt.Errorf("an explicit positive invitation validity window is required")
	}
	store, e := ownerstore.New(db, "tenant")
	if e != nil {
		return nil, e
	}
	s := &Service{DB: db, store: store, Gateway: gateway, InvitationTTL: invitationTTL, signing: append([]byte(nil), signing...), admins: map[string]bool{}}
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
	if s.GatewayStore == nil {
		return fmt.Errorf("Gateway User binding persistence is required before registering handlers")
	}
	if err := server.AddReadinessCheck("gateway_bindings", s.GatewayStore.Ready); err != nil {
		return err
	}
	required := []string{"TenantProductService", "GatewayProductService", "GatewayCoordination"}
	if err := server.RequireProductGroups(required...); err != nil {
		return err
	}
	if err := server.RegisterGroup("TenantProductService", func(g *grpc.Server) {
		api.RegisterTenantProductServiceServer(g, s)
		api.RegisterCloudIdentityAuthorizationServer(g, s)
	}); err != nil {
		return err
	}
	if err := server.RegisterGroup("GatewayCoordination", func(g *grpc.Server) {
		api.RegisterGatewayCoordinationServer(g, s)
	}); err != nil {
		return err
	}
	return server.RegisterGroup("GatewayProductService", func(g *grpc.Server) {
		api.RegisterGatewayProductServiceServer(g, s)
	})
}

// ConfigureRegistry records the installation's registry facts once at startup.
// Both are supplied together; a half-configured pair is refused by the caller.
func (s *Service) ConfigureRegistry(host, namespace string) {
	s.registryHost = strings.ToLower(strings.TrimSpace(host))
	s.registryNamespace = strings.ToLower(strings.TrimSpace(namespace))
}
func stampOf(v time.Time) *timestamppb.Timestamp { return timestamppb.New(v) }

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
	user, _, e := s.Gateway.Login(ctx, r.GetBody().GetUsername(), r.GetBody().GetPassword())
	if e != nil {
		return nil, e
	}
	actor := strconv.FormatInt(user.ID, 10)
	if s.GatewayStore == nil {
		return nil, status.Error(codes.Unavailable, "Gateway User binding persistence is not configured")
	}
	host, namespace, repository, e := s.authenticatedUserDestination(ctx, actor, user.Email)
	if e != nil {
		return nil, e
	}
	if _, e = s.GatewayStore.bindAuthenticatedUser(ctx, actor, strconv.FormatInt(user.ID, 10), host, namespace, repository); e != nil {
		return nil, e
	}
	var tenant sql.NullString
	e = s.DB.QueryRowContext(ctx, `SELECT tenant_id FROM tenant.tenant_members m JOIN tenant.tenants t ON t.id=m.tenant_id WHERE m.actor_id=$1 AND m.revoked_at IS NULL AND t.status='active'`, actor).Scan(&tenant)
	if e != nil && e != sql.ErrNoRows {
		return nil, persistence(e)
	}
	cookie := randomID()
	ref := owneridentity.SessionReference(cookie)
	expires := time.Now().Add(12 * time.Hour)
	// The display name is captured here so any replica can answer this session
	// from the row alone. It is the email the identity owner just returned, not
	// a secret, and it keeps the member's Gateway credential out of every request
	// path. A row written by an image that predates the column resolves the name
	// from the actor id instead.
	_, e = s.DB.ExecContext(ctx, `INSERT INTO tenant.sessions(id,session_hash,actor_id,tenant_id,gateway_session_ref,csrf_hash,expires_at,display_name) VALUES($1,$2,$3,$4,$1,$5,$6,$7)`, ref, hash(cookie), actor, tenant, hash(s.mac("csrf:"+cookie)), expires, user.Email)
	if e != nil {
		return nil, persistence(e)
	}
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

// activeSubject is the authoritative check that the actor behind a session row may
// still act. The row proves that a login happened; only the Gateway owner knows
// whether the subject behind it is still active, so an account disabled there must
// not obtain new interactive authorization through an unexpired Cloud session.
//
// The read authenticates as this service's own configured directory identity rather
// than as the member credential the login returned. That is what keeps the check
// cross-replica: the directory credential is a deployment fact every replica holds,
// so no per-login credential has to be kept in process memory or written into the
// session table for the check to be possible. Anything the Gateway cannot answer
// fails closed rather than falling back to the row.
func (s *Service) activeSubject(ctx context.Context, actor string) error {
	binding, e := s.userBinding(ctx, actor)
	if e != nil {
		return e
	}
	subject, e := s.Gateway.directoryIdentity(ctx, binding.Sub2APIUserID)
	if e != nil {
		return e
	}
	if subject.Status != "active" {
		return status.Error(codes.Unauthenticated, "Gateway identity is no longer active")
	}
	return nil
}
func (s *Service) session(ctx context.Context, ref string) (*api.Session, int64, bool, error) {
	out := &api.Session{}
	var tenant, role sql.NullString
	var expires time.Time
	var version int64
	e := s.DB.QueryRowContext(ctx, `SELECT s.actor_id,s.tenant_id,s.expires_at,COALESCE(s.display_name,s.actor_id),m.role,COALESCE(t.permission_version,0)+1 FROM tenant.sessions s LEFT JOIN tenant.tenants t ON t.id=s.tenant_id LEFT JOIN tenant.tenant_members m ON m.tenant_id=s.tenant_id AND m.actor_id=s.actor_id AND m.revoked_at IS NULL WHERE s.id=$1 AND s.revoked_at IS NULL AND s.expires_at>now() AND (s.tenant_id IS NULL OR (t.status='active' AND m.id IS NOT NULL))`, ref).Scan(&out.ActorId, &tenant, &expires, &out.DisplayName, &role, &version)
	if e != nil {
		return nil, 0, false, persistence(e)
	}
	if e = s.activeSubject(ctx, out.ActorId); e != nil {
		return nil, 0, false, e
	}
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
	return &emptypb.Empty{}, nil
}

// GetWallet returns the balance read directly from Sub2API's authoritative admin
// user endpoint. No balance is copied into CloudIdentity and an unavailable
// directory credential fails closed.
func (s *Service) GetWallet(ctx context.Context, r *api.GetWalletRpcRequest) (*api.Wallet, error) {
	if err := peer(ctx, owneridentity.ConsoleBFF); err != nil {
		return nil, err
	}
	if r == nil || r.GetContext() == nil || r.GetContext().GetSessionId() == "" {
		return nil, status.Error(codes.Unauthenticated, "active Cloud session required")
	}
	out, _, _, err := s.session(ctx, r.GetContext().GetSessionId())
	if err != nil {
		return nil, err
	}
	binding, err := s.userBinding(ctx, out.GetActorId())
	if err != nil {
		return nil, err
	}
	balance, err := s.Gateway.Balance(ctx, binding.Sub2APIUserID)
	if err != nil {
		return nil, err
	}
	return &api.Wallet{
		Source:           api.WalletSourceEnum_WALLET_SOURCE_ENUM_GATEWAY,
		Status:           api.WalletStatusEnum_WALLET_STATUS_ENUM_AVAILABLE,
		BalanceUsdMicros: balance.USDMicros,
		Currency:         api.WalletCurrencyEnum_WALLET_CURRENCY_ENUM_USD,
		FetchedAt:        timestamppb.Now(),
	}, nil
}

// GetUsageSummary reads the caller's usage totals from Sub2API through the Gateway owner.
// The BFF never aggregates or fabricates usage facts.
func (s *Service) GetUsageSummary(ctx context.Context, r *api.GetUsageSummaryRpcRequest) (*api.UsageSummary, error) {
	if err := peer(ctx, owneridentity.ConsoleBFF); err != nil {
		return nil, err
	}
	if r == nil || r.GetContext() == nil || r.GetContext().GetSessionId() == "" {
		return nil, status.Error(codes.Unauthenticated, "active Cloud session required")
	}
	out, _, _, err := s.session(ctx, r.GetContext().GetSessionId())
	if err != nil {
		return nil, err
	}
	binding, err := s.userBinding(ctx, out.GetActorId())
	if err != nil {
		return nil, err
	}
	stats, err := s.Gateway.UsageStats(ctx, binding.Sub2APIUserID, r.GetPeriod())
	if err != nil {
		return nil, err
	}
	return &api.UsageSummary{
		TotalRequests:            stats.TotalRequests,
		TotalInputTokens:         stats.TotalInputTokens,
		TotalOutputTokens:        stats.TotalOutputTokens,
		TotalTokens:              stats.TotalTokens,
		TotalActualCostUsdMicros: stats.TotalActualCostUSDMicros,
	}, nil
}

// ListModels reads the caller's own Codex model catalog from the Gateway owner.
// The catalog is read with the caller's active Gateway API key and carries no
// token price; a missing key or unresolvable catalog fails closed rather than
// returning an empty or fabricated catalog.
func (s *Service) ListModels(ctx context.Context, r *api.ListModelsRpcRequest) (*api.ModelPage, error) {
	if err := peer(ctx, owneridentity.ConsoleBFF); err != nil {
		return nil, err
	}
	if r == nil || r.GetContext() == nil || r.GetContext().GetSessionId() == "" {
		return nil, status.Error(codes.Unauthenticated, "active Cloud session required")
	}
	out, _, _, err := s.session(ctx, r.GetContext().GetSessionId())
	if err != nil {
		return nil, err
	}
	binding, err := s.userBinding(ctx, out.GetActorId())
	if err != nil {
		return nil, err
	}
	models, err := s.Gateway.Models(ctx, binding.Sub2APIUserID)
	if err != nil {
		return nil, err
	}
	page := &api.ModelPage{}
	for _, model := range models {
		page.Items = append(page.Items, &api.Model{
			Id:           model.ID,
			Name:         model.Name,
			Capabilities: model.Capabilities,
			Available:    model.Available,
			FetchedAt:    timestamppb.Now(),
		})
	}
	return page, nil
}
