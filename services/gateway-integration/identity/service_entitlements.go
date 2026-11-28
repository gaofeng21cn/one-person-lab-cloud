package identity

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

const (
	serviceGrantDefaultTTL          = 120 * time.Second
	maxServiceEntitlementReasonRune = 200
	maxServiceEntitlementBatchSize  = 100
	AdmissionAllowlistOnly          = "allowlist_only"
	AdmissionActiveGatewayDefault   = "active_gateway_default_with_overrides"
)

var (
	serviceIDPattern   = regexp.MustCompile(`^[a-z][a-z0-9_]{1,63}$`)
	serviceRolePattern = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)
)

// ServiceCatalogEntry defines one companion service registered on this OPL
// Cloud instance. It contains no secrets and binds the public service endpoint,
// downstream JWT audience, and admission policy.
type ServiceCatalogEntry struct {
	DefaultOrigin string         `json:"default_origin"`
	Audience      string         `json:"audience"`
	AdmissionMode string         `json:"admission_mode"`
	DefaultLimits map[string]any `json:"default_limits,omitempty"`
}

// GrantSigner signs short-lived Ed25519 service grants and exposes public JWKs
// for stateless offline verification by downstream companion services.
type GrantSigner struct {
	priv      ed25519.PrivateKey
	pub       ed25519.PublicKey
	kid       string
	issuer    string
	ttl       time.Duration
	extraJWKs []*api.ServiceGrantJWK
}

type additionalGrantKeyEntry struct {
	Kid       string `json:"kid"`
	X         string `json:"x"`
	PublicKey string `json:"public_key"`
}

func validCanonicalHTTPSOrigin(raw string) bool {
	if raw == "" || len(raw) > 253 {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return false
	}
	return raw == "https://"+u.Host
}

func validGrantIssuerOrigin(raw string) bool {
	if validCanonicalHTTPSOrigin(raw) {
		return true
	}
	if raw == "" || len(raw) > 253 {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return false
	}
	host := u.Hostname()
	return (host == "127.0.0.1" || host == "localhost") && raw == "http://"+u.Host
}

// ParseServiceCatalogJSON validates and decodes OPL_SERVICE_CATALOG_JSON.
func ParseServiceCatalogJSON(raw string) (map[string]ServiceCatalogEntry, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return map[string]ServiceCatalogEntry{}, nil
	}
	var decoded map[string]ServiceCatalogEntry
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return nil, fmt.Errorf("invalid OPL_SERVICE_CATALOG_JSON: %w", err)
	}
	catalog := make(map[string]ServiceCatalogEntry, len(decoded))
	for serviceID, entry := range decoded {
		if !serviceIDPattern.MatchString(serviceID) {
			return nil, fmt.Errorf("invalid service_id %q in service catalog", serviceID)
		}
		entry.DefaultOrigin = strings.TrimSpace(entry.DefaultOrigin)
		if !validCanonicalHTTPSOrigin(entry.DefaultOrigin) {
			return nil, fmt.Errorf("service %q default_origin must be a canonical HTTPS origin without path or trailing slash", serviceID)
		}
		entry.Audience = strings.TrimSpace(entry.Audience)
		if entry.Audience == "" || len(entry.Audience) > 128 {
			return nil, fmt.Errorf("service %q requires a valid audience", serviceID)
		}
		switch entry.AdmissionMode {
		case AdmissionAllowlistOnly, AdmissionActiveGatewayDefault:
		default:
			return nil, fmt.Errorf("service %q has unsupported admission_mode %q", serviceID, entry.AdmissionMode)
		}
		if entry.DefaultLimits != nil {
			if _, err := structpb.NewStruct(entry.DefaultLimits); err != nil {
				return nil, fmt.Errorf("service %q default_limits invalid: %w", serviceID, err)
			}
		}
		catalog[serviceID] = entry
	}
	return catalog, nil
}

func parseEd25519PrivateKey(raw string) (ed25519.PrivateKey, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, errors.New("empty grant signing private key")
	}
	if strings.Contains(trimmed, "-----BEGIN ") {
		block, _ := pem.Decode([]byte(trimmed))
		if block == nil {
			return nil, errors.New("invalid PEM block for grant signing private key")
		}
		parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse PKCS8 grant signing private key: %w", err)
		}
		key, ok := parsed.(ed25519.PrivateKey)
		if !ok || len(key) != ed25519.PrivateKeySize {
			return nil, errors.New("PKCS8 grant signing key must be Ed25519")
		}
		return append(ed25519.PrivateKey(nil), key...), nil
	}
	for _, enc := range []*base64.Encoding{base64.RawURLEncoding, base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding} {
		decoded, err := enc.DecodeString(trimmed)
		if err != nil {
			continue
		}
		switch len(decoded) {
		case ed25519.SeedSize:
			return ed25519.NewKeyFromSeed(decoded), nil
		case ed25519.PrivateKeySize:
			return append(ed25519.PrivateKey(nil), decoded...), nil
		}
	}
	return nil, errors.New("grant signing private key must be PKCS8 PEM or base64url 32-byte/64-byte Ed25519 key")
}

func parseEd25519PublicKey(raw string) (ed25519.PublicKey, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, errors.New("empty Ed25519 public key")
	}
	if strings.Contains(trimmed, "-----BEGIN ") {
		block, _ := pem.Decode([]byte(trimmed))
		if block == nil {
			return nil, errors.New("invalid PEM block for grant public key")
		}
		parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse PKIX grant public key: %w", err)
		}
		pub, ok := parsed.(ed25519.PublicKey)
		if !ok || len(pub) != ed25519.PublicKeySize {
			return nil, errors.New("PKIX grant public key must be Ed25519")
		}
		return append(ed25519.PublicKey(nil), pub...), nil
	}
	for _, enc := range []*base64.Encoding{base64.RawURLEncoding, base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding} {
		decoded, err := enc.DecodeString(trimmed)
		if err == nil && len(decoded) == ed25519.PublicKeySize {
			return append(ed25519.PublicKey(nil), decoded...), nil
		}
	}
	return nil, errors.New("grant public key must be PKIX PEM or base64url 32-byte Ed25519 key")
}

// NewGrantSigner constructs an Ed25519 service-grant signer and JWK view.
func NewGrantSigner(privateKeyRaw, kidRaw, issuerRaw, additionalPublicKeysRaw string, ttl time.Duration) (*GrantSigner, error) {
	priv, err := parseEd25519PrivateKey(privateKeyRaw)
	if err != nil {
		return nil, err
	}
	pub := priv.Public().(ed25519.PublicKey)
	issuer := strings.TrimRight(strings.TrimSpace(issuerRaw), "/")
	if !validGrantIssuerOrigin(issuer) {
		return nil, errors.New("OPL_GRANT_ISSUER (or OPL_PUBLIC_URL) must be a canonical origin")
	}
	kid := strings.TrimSpace(kidRaw)
	if kid == "" {
		sum := sha256.Sum256(pub)
		kid = "opl-grant-" + hex.EncodeToString(sum[:6])
	}
	if ttl <= 0 {
		ttl = serviceGrantDefaultTTL
	}
	var extras []*api.ServiceGrantJWK
	if trimmedExtra := strings.TrimSpace(additionalPublicKeysRaw); trimmedExtra != "" {
		var parsed []additionalGrantKeyEntry
		if err := json.Unmarshal([]byte(trimmedExtra), &parsed); err != nil {
			return nil, fmt.Errorf("invalid OPL_GRANT_ADDITIONAL_PUBLIC_KEYS_JSON: %w", err)
		}
		for _, item := range parsed {
			material := strings.TrimSpace(item.PublicKey)
			if material == "" {
				material = strings.TrimSpace(item.X)
			}
			extraPub, err := parseEd25519PublicKey(material)
			if err != nil {
				return nil, fmt.Errorf("invalid additional grant public key: %w", err)
			}
			extraKid := strings.TrimSpace(item.Kid)
			if extraKid == "" {
				sum := sha256.Sum256(extraPub)
				extraKid = "opl-grant-" + hex.EncodeToString(sum[:6])
			}
			extras = append(extras, &api.ServiceGrantJWK{
				Kty: "OKP",
				Crv: "Ed25519",
				Alg: "EdDSA",
				Use: "sig",
				Kid: extraKid,
				X:   base64.RawURLEncoding.EncodeToString(extraPub),
			})
		}
	}
	return &GrantSigner{
		priv:      priv,
		pub:       pub,
		kid:       kid,
		issuer:    issuer,
		ttl:       ttl,
		extraJWKs: extras,
	}, nil
}

// ConfigureServiceGrantsFromEnv loads the instance service catalog and optional
// Ed25519 signing key from environment variables. An absent key leaves the
// signer nil so existing deployments without grant issuance still boot safely
// while failing closed on grant/JWKS requests.
func ConfigureServiceGrantsFromEnv(getenv func(string) string) (map[string]ServiceCatalogEntry, *GrantSigner, error) {
	catalog, err := ParseServiceCatalogJSON(getenv("OPL_SERVICE_CATALOG_JSON"))
	if err != nil {
		return nil, nil, err
	}
	privateKey := strings.TrimSpace(getenv("OPL_GRANT_SIGNING_PRIVATE_KEY"))
	if privateKey == "" {
		if path := strings.TrimSpace(getenv("OPL_GRANT_SIGNING_PRIVATE_KEY_FILE")); path != "" {
			rawBytes, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil, nil, fmt.Errorf("read OPL_GRANT_SIGNING_PRIVATE_KEY_FILE: %w", readErr)
			}
			privateKey = string(rawBytes)
		}
	}
	if strings.TrimSpace(privateKey) == "" {
		return catalog, nil, nil
	}
	issuer := strings.TrimSpace(getenv("OPL_GRANT_ISSUER"))
	if issuer == "" {
		issuer = strings.TrimSpace(getenv("OPL_PUBLIC_URL"))
	}
	signer, err := NewGrantSigner(
		privateKey,
		getenv("OPL_GRANT_SIGNING_KEY_ID"),
		issuer,
		getenv("OPL_GRANT_ADDITIONAL_PUBLIC_KEYS_JSON"),
		serviceGrantDefaultTTL,
	)
	if err != nil {
		return nil, nil, err
	}
	return catalog, signer, nil
}

// ConfigureServiceGrants installs the instance Service Catalog and Ed25519 grant
// signer onto CloudIdentity.
func (s *Service) ConfigureServiceGrants(catalog map[string]ServiceCatalogEntry, signer *GrantSigner) {
	copied := make(map[string]ServiceCatalogEntry, len(catalog))
	for k, v := range catalog {
		copied[k] = v
	}
	s.serviceCatalog = copied
	s.grantSigner = signer
}

func (g *GrantSigner) signJWT(claims map[string]any) (string, error) {
	headerBytes, err := json.Marshal(map[string]string{
		"alg": "EdDSA",
		"typ": "JWT",
		"kid": g.kid,
	})
	if err != nil {
		return "", err
	}
	payloadBytes, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signingInput := base64.RawURLEncoding.EncodeToString(headerBytes) + "." + base64.RawURLEncoding.EncodeToString(payloadBytes)
	sig := ed25519.Sign(g.priv, []byte(signingInput))
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func (g *GrantSigner) jwks() *api.ServiceGrantJWKSResponse {
	keys := make([]*api.ServiceGrantJWK, 0, 1+len(g.extraJWKs))
	keys = append(keys, &api.ServiceGrantJWK{
		Kty: "OKP",
		Crv: "Ed25519",
		Alg: "EdDSA",
		Use: "sig",
		Kid: g.kid,
		X:   base64.RawURLEncoding.EncodeToString(g.pub),
	})
	for _, extra := range g.extraJWKs {
		if extra.GetKid() == g.kid {
			continue
		}
		keys = append(keys, proto.Clone(extra).(*api.ServiceGrantJWK))
	}
	return &api.ServiceGrantJWKSResponse{Keys: keys}
}

func cloneAndMergeLimits(base map[string]any, overlay map[string]any) map[string]any {
	if len(base) == 0 && len(overlay) == 0 {
		return nil
	}
	out := make(map[string]any, len(base)+len(overlay))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range overlay {
		out[k] = v
	}
	return out
}

func limitsToStruct(limits map[string]any) (*structpb.Struct, error) {
	if len(limits) == 0 {
		return nil, nil
	}
	return structpb.NewStruct(limits)
}

func limitsFromRawJSON(raw []byte) (map[string]any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) sortedCatalogServiceIDs() []string {
	ids := make([]string, 0, len(s.serviceCatalog))
	for id := range s.serviceCatalog {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// GetServiceGrantJWKS returns active and overlapping Ed25519 OKP public keys
// for verifying short-lived companion service grants.
func (s *Service) GetServiceGrantJWKS(ctx context.Context, _ *api.GetServiceGrantJWKSRpcRequest) (*api.ServiceGrantJWKSResponse, error) {
	if err := peer(ctx, owneridentity.ConsoleBFF); err != nil {
		return nil, err
	}
	if s.grantSigner == nil {
		return nil, status.Error(codes.FailedPrecondition, "service grant signer is not configured")
	}
	return s.grantSigner.jwks(), nil
}

// IssueServiceGrant verifies live Gateway credentials (or Gateway Bearer token),
// evaluates instance Service Catalog + tenant.service_entitlements admission, and
// mints a short-lived Ed25519 grant JWT containing only authorized origin and limits.
func (s *Service) IssueServiceGrant(ctx context.Context, r *api.IssueServiceGrantRpcRequest) (*api.ServiceGrantResponse, error) {
	if err := peer(ctx, owneridentity.ConsoleBFF); err != nil {
		return nil, err
	}
	if len(s.serviceCatalog) == 0 || s.grantSigner == nil {
		return nil, status.Error(codes.FailedPrecondition, "service grant issuer is not configured")
	}
	body := r.GetBody()
	if body == nil {
		return nil, status.Error(codes.InvalidArgument, "service grant body is required")
	}
	serviceID := strings.TrimSpace(body.GetService())
	if !serviceIDPattern.MatchString(serviceID) {
		return nil, status.Error(codes.InvalidArgument, "valid service id is required")
	}
	entry, ok := s.serviceCatalog[serviceID]
	if !ok {
		return nil, status.Error(codes.NotFound, "unknown companion service")
	}
	scope := strings.TrimSpace(body.GetScope())
	if scope == "" {
		scope = "device:enroll"
	}
	if scope != "device:enroll" && scope != "console:login" {
		return nil, status.Error(codes.InvalidArgument, "unsupported service grant scope")
	}

	bearer := strings.TrimSpace(r.GetBearerToken())
	email := strings.ToLower(strings.TrimSpace(body.GetEmail()))
	password := body.GetPassword()
	totpCode := strings.TrimSpace(body.GetTotpCode())
	hasPasswordCreds := email != "" || password != "" || totpCode != ""
	if bearer != "" && hasPasswordCreds {
		return nil, status.Error(codes.InvalidArgument, "supply either Gateway bearer token or email/password credentials")
	}
	if bearer == "" && (email == "" || password == "") {
		return nil, status.Error(codes.InvalidArgument, "Gateway credentials required")
	}

	var user GatewayIdentity
	var err error
	if bearer != "" {
		user, err = s.Gateway.AuthenticateBearer(ctx, bearer)
		if err != nil {
			return nil, err
		}
	} else {
		var requires2FA bool
		user, requires2FA, err = s.Gateway.LoginForServiceGrant(ctx, email, password, totpCode)
		password = ""
		body.Password = ""
		if err != nil {
			return nil, err
		}
		if requires2FA {
			return &api.ServiceGrantResponse{Requires_2Fa: true}, nil
		}
	}

	actorID := strconv.FormatInt(user.ID, 10)
	if user.ID <= 0 || user.Email == "" {
		return nil, status.Error(codes.Unauthenticated, "Gateway identity rejected")
	}
	if s.GatewayStore != nil {
		if _, bindErr := s.GatewayStore.ReadUserExternalBinding(ctx, actorID); bindErr != nil {
			switch status.Code(bindErr) {
			case codes.NotFound:
			case codes.FailedPrecondition:
				return nil, status.Error(codes.PermissionDenied, "service_not_entitled")
			default:
				return nil, status.Error(codes.Unavailable, "Gateway User binding persistence unavailable")
			}
		}
	}

	now := time.Now().UTC()
	var rowEnabled bool
	var rowRole string
	var rowOrigin sql.NullString
	var rowLimitsRaw []byte
	var rowExpiresAt sql.NullTime
	scanErr := s.DB.QueryRowContext(ctx, `
		SELECT enabled, role, origin_override, limits_json, expires_at
		FROM tenant.service_entitlements
		WHERE actor_id=$1 AND service_id=$2`, actorID, serviceID).
		Scan(&rowEnabled, &rowRole, &rowOrigin, &rowLimitsRaw, &rowExpiresAt)

	origin := entry.DefaultOrigin
	role := "user"
	limits := cloneAndMergeLimits(entry.DefaultLimits, nil)

	switch {
	case errors.Is(scanErr, sql.ErrNoRows):
		if entry.AdmissionMode != AdmissionActiveGatewayDefault {
			return nil, status.Error(codes.PermissionDenied, "service_not_entitled")
		}
	case scanErr != nil:
		return nil, persistence(scanErr)
	default:
		if !rowEnabled || (rowExpiresAt.Valid && !rowExpiresAt.Time.After(now)) {
			return nil, status.Error(codes.PermissionDenied, "service_not_entitled")
		}
		if rowRole = strings.TrimSpace(rowRole); rowRole != "" {
			role = rowRole
		}
		if rowOrigin.Valid && strings.TrimSpace(rowOrigin.String) != "" {
			override := strings.TrimSpace(rowOrigin.String)
			if !validCanonicalHTTPSOrigin(override) {
				return nil, status.Error(codes.FailedPrecondition, "service entitlement origin_override is invalid")
			}
			origin = override
		}
		overrideLimits, limitErr := limitsFromRawJSON(rowLimitsRaw)
		if limitErr != nil {
			return nil, status.Error(codes.FailedPrecondition, "service entitlement limits_json is invalid")
		}
		limits = cloneAndMergeLimits(limits, overrideLimits)
	}

	expiresAt := now.Add(s.grantSigner.ttl)
	claims := map[string]any{
		"iss":     s.grantSigner.issuer,
		"sub":     actorID,
		"email":   user.Email,
		"aud":     entry.Audience,
		"service": serviceID,
		"scope":   scope,
		"role":    role,
		"iat":     now.Unix(),
		"exp":     expiresAt.Unix(),
		"jti":     randomID()[:32],
	}
	if len(limits) > 0 {
		claims["limits"] = limits
	}
	token, signErr := s.grantSigner.signJWT(claims)
	if signErr != nil {
		return nil, status.Error(codes.Internal, "failed to sign service grant token")
	}
	limitsStruct, structErr := limitsToStruct(limits)
	if structErr != nil {
		return nil, status.Error(codes.Internal, "failed to encode service limits")
	}
	return &api.ServiceGrantResponse{
		ActorId:    actorID,
		Email:      user.Email,
		Service:    serviceID,
		Enabled:    true,
		Origin:     origin,
		GrantToken: token,
		ExpiresAt:  timestamppb.New(expiresAt),
		Role:       role,
		Limits:     limitsStruct,
	}, nil
}

// SetServiceEntitlement is the sole canonical writer for per-user companion
// service entitlements in tenant.service_entitlements.
func (s *Service) SetServiceEntitlement(ctx context.Context, r *api.SetServiceEntitlementRpcRequest) (*api.ServiceEntitlementCommand, error) {
	if err := peer(ctx, owneridentity.ConsoleBFF); err != nil {
		return nil, err
	}
	c := r.GetContext()
	if c == nil || c.GetScope().GetPlatform() == nil || strings.TrimSpace(c.GetActorId()) == "" || c.GetSessionId() == "" || strings.TrimSpace(c.GetRequestId()) == "" {
		return nil, denied()
	}
	targetActorID := strings.TrimSpace(r.GetTargetActorId())
	parsedID, parseErr := strconv.ParseInt(targetActorID, 10, 64)
	if parseErr != nil || parsedID <= 0 || targetActorID != r.GetTargetActorId() || strconv.FormatInt(parsedID, 10) != targetActorID {
		return nil, status.Error(codes.InvalidArgument, "a valid Gateway subject id is required")
	}
	serviceID := strings.TrimSpace(r.GetServiceId())
	if !serviceIDPattern.MatchString(serviceID) || serviceID != r.GetServiceId() {
		return nil, status.Error(codes.InvalidArgument, "a valid service id is required")
	}
	if len(s.serviceCatalog) == 0 {
		return nil, status.Error(codes.FailedPrecondition, "service catalog is not configured")
	}
	entry, ok := s.serviceCatalog[serviceID]
	if !ok {
		return nil, status.Error(codes.NotFound, "unknown companion service")
	}

	body := r.GetBody()
	if body == nil || strings.TrimSpace(body.GetConfirmationAccountId()) != targetActorID {
		return nil, status.Error(codes.InvalidArgument, "confirmationAccountId must match the target account")
	}
	reason := strings.TrimSpace(body.GetReason())
	if reason == "" || utf8.RuneCountInString(reason) > maxServiceEntitlementReasonRune {
		return nil, status.Error(codes.InvalidArgument, "reason must be between 1 and 200 characters")
	}
	if body.Role != nil {
		roleTrim := strings.TrimSpace(*body.Role)
		if !serviceRolePattern.MatchString(roleTrim) {
			return nil, status.Error(codes.InvalidArgument, "invalid role")
		}
	}
	if body.GetClearOriginOverride() && body.OriginOverride != nil && strings.TrimSpace(*body.OriginOverride) != "" {
		return nil, status.Error(codes.InvalidArgument, "cannot set and clear originOverride simultaneously")
	}
	if body.OriginOverride != nil {
		override := strings.TrimSpace(*body.OriginOverride)
		if override == "" || !validCanonicalHTTPSOrigin(override) {
			return nil, status.Error(codes.InvalidArgument, "originOverride must be a canonical HTTPS origin")
		}
	}
	if body.GetClearExpiresAt() && body.GetExpiresAt() != nil {
		return nil, status.Error(codes.InvalidArgument, "cannot set and clear expiresAt simultaneously")
	}
	if body.GetExpiresAt() != nil && !body.GetExpiresAt().IsValid() {
		return nil, status.Error(codes.InvalidArgument, "invalid expiresAt timestamp")
	}

	authReq := &api.AuthorizationRequest{
		Scope:         c.GetScope(),
		ActorId:       c.GetActorId(),
		SessionId:     c.SessionId,
		AudienceOwner: api.OwnerEnum_OWNER_ENUM_TENANT,
		Action:        api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_SETSERVICEENTITLEMENT,
		Resource:      &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PLATFORM, Id: &targetActorID},
		RequestId:     c.GetRequestId(),
	}
	if c.GetAuthorizationContextId() != "" {
		id := c.GetAuthorizationContextId()
		authReq.AuthorizationContextId = &id
	}
	decision, err := s.AuthorizeAction(ctx, authReq)
	if err != nil {
		return nil, err
	}
	if decision.GetResult() != api.AuthorizationResult_AUTHORIZATION_RESULT_ALLOWED {
		return nil, status.Error(codes.PermissionDenied, "platform administrator authorization required")
	}
	if _, err := s.Gateway.directoryIdentity(ctx, targetActorID); err != nil {
		return nil, err
	}

	canonicalBody := proto.Clone(body).(*api.SetServiceEntitlementRequest)
	canonicalBody.ConfirmationAccountId = targetActorID
	canonicalBody.Reason = reason
	canonicalReq := &api.SetServiceEntitlementRpcRequest{
		TargetActorId: targetActorID,
		ServiceId:     serviceID,
		Body:          canonicalBody,
	}
	out := &api.ServiceEntitlementCommand{}
	err = s.command(ctx, c, "platform", "SetServiceEntitlement", canonicalReq, out, func(tx *sql.Tx) error {
		var prevExists bool
		var prevEnabled bool
		var prevRole string
		var prevOrigin sql.NullString
		var prevLimitsRaw []byte
		var prevExpires sql.NullTime
		scanErr := tx.QueryRowContext(ctx, `
			SELECT enabled, role, origin_override, limits_json, expires_at
			FROM tenant.service_entitlements
			WHERE actor_id=$1 AND service_id=$2
			FOR UPDATE`, targetActorID, serviceID).
			Scan(&prevEnabled, &prevRole, &prevOrigin, &prevLimitsRaw, &prevExpires)
		switch {
		case scanErr == nil:
			prevExists = true
		case errors.Is(scanErr, sql.ErrNoRows):
			prevEnabled = entry.AdmissionMode == AdmissionActiveGatewayDefault
			prevRole = "user"
		default:
			return persistence(scanErr)
		}

		nextEnabled := body.GetEnabled()
		nextRole := prevRole
		if body.Role != nil {
			nextRole = strings.TrimSpace(*body.Role)
		}
		if nextRole == "" {
			nextRole = "user"
		}

		var nextOrigin sql.NullString
		if body.GetClearOriginOverride() {
			nextOrigin = sql.NullString{}
		} else if body.OriginOverride != nil {
			nextOrigin = sql.NullString{String: strings.TrimSpace(*body.OriginOverride), Valid: true}
		} else {
			nextOrigin = prevOrigin
		}

		var nextLimitsMap map[string]any
		if body.GetLimits() != nil {
			nextLimitsMap = body.GetLimits().AsMap()
		} else {
			var limitErr error
			nextLimitsMap, limitErr = limitsFromRawJSON(prevLimitsRaw)
			if limitErr != nil {
				return status.Error(codes.FailedPrecondition, "existing limits_json is invalid")
			}
		}
		if nextLimitsMap == nil {
			nextLimitsMap = map[string]any{}
		}
		nextLimitsBytes, marshalErr := json.Marshal(nextLimitsMap)
		if marshalErr != nil {
			return status.Error(codes.InvalidArgument, "limits must be a JSON object")
		}

		var nextExpires sql.NullTime
		if body.GetClearExpiresAt() {
			nextExpires = sql.NullTime{}
		} else if body.GetExpiresAt() != nil {
			nextExpires = sql.NullTime{Time: body.GetExpiresAt().AsTime().UTC(), Valid: true}
		} else {
			nextExpires = prevExpires
		}

		if _, execErr := tx.ExecContext(ctx, `
			INSERT INTO tenant.service_entitlements(actor_id, service_id, enabled, role, origin_override, limits_json, expires_at, reason, updated_by, updated_at)
			VALUES($1, $2, $3, $4, $5, $6::jsonb, $7, $8, $9, now())
			ON CONFLICT (actor_id, service_id) DO UPDATE
			SET enabled=EXCLUDED.enabled,
			    role=EXCLUDED.role,
			    origin_override=EXCLUDED.origin_override,
			    limits_json=EXCLUDED.limits_json,
			    expires_at=EXCLUDED.expires_at,
			    reason=EXCLUDED.reason,
			    updated_by=EXCLUDED.updated_by,
			    updated_at=now()`,
			targetActorID, serviceID, nextEnabled, nextRole, nextOrigin, string(nextLimitsBytes), nextExpires, reason, c.GetActorId()); execErr != nil {
			return persistence(execErr)
		}

		action := "account.service_entitlement.update"
		if nextEnabled && (!prevExists || !prevEnabled) {
			action = "account.service_entitlement.grant"
		} else if !nextEnabled {
			action = "account.service_entitlement.revoke"
		}
		if auditErr := s.recordAudit(ctx, tx, auditEvent{
			actorID:      c.GetActorId(),
			action:       action,
			resourceType: "account",
			resourceID:   targetActorID,
			requestID:    c.GetRequestId(),
			details: map[string]any{
				"serviceId":          serviceID,
				"beforeEnabled":      prevEnabled,
				"enabled":            nextEnabled,
				"role":               nextRole,
				"hasOriginOverride":  nextOrigin.Valid,
				"hasLimits":          len(nextLimitsMap) > 0,
				"hasExpiresAt":       nextExpires.Valid,
				"reason":             reason,
			},
			outcome: "succeeded",
		}); auditErr != nil {
			return auditErr
		}

		now := time.Now().UTC()
		effectiveEnabled := nextEnabled && (!nextExpires.Valid || nextExpires.Time.After(now))
		effectiveOrigin := entry.DefaultOrigin
		if nextOrigin.Valid {
			effectiveOrigin = nextOrigin.String
		}
		mergedLimits := cloneAndMergeLimits(entry.DefaultLimits, nextLimitsMap)
		limitsStruct, structErr := limitsToStruct(mergedLimits)
		if structErr != nil {
			return status.Error(codes.Internal, "failed to encode service limits")
		}
		cmd := &api.ServiceEntitlementCommand{
			OperationId:      "service-entitlement-" + hash(c.GetIdempotencyKey()+":"+targetActorID+":"+serviceID)[:18],
			AccountId:        targetActorID,
			ServiceId:        serviceID,
			Status:           "succeeded",
			Enabled:          nextEnabled,
			EffectiveEnabled: effectiveEnabled,
			Role:             nextRole,
			Origin:           effectiveOrigin,
			Limits:           limitsStruct,
		}
		if nextOrigin.Valid {
			cmd.OriginOverride = proto.String(nextOrigin.String)
		}
		if nextExpires.Valid {
			cmd.ExpiresAt = timestamppb.New(nextExpires.Time)
		}
		proto.Merge(out, cmd)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ListServiceEntitlements returns effective companion service entitlements for
// a bounded page of Gateway subjects to support operator account projection.
func (s *Service) ListServiceEntitlements(ctx context.Context, r *api.ListServiceEntitlementsRpcRequest) (*api.ServiceEntitlementList, error) {
	if err := peer(ctx, owneridentity.ConsoleBFF); err != nil {
		return nil, err
	}
	c := r.GetContext()
	if c == nil || c.GetScope().GetPlatform() == nil || strings.TrimSpace(c.GetActorId()) == "" || c.GetSessionId() == "" || strings.TrimSpace(c.GetRequestId()) == "" {
		return nil, denied()
	}
	authReq := &api.AuthorizationRequest{
		Scope:         c.GetScope(),
		ActorId:       c.GetActorId(),
		SessionId:     c.SessionId,
		AudienceOwner: api.OwnerEnum_OWNER_ENUM_TENANT,
		Action:        api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTTENANTS,
		Resource:      &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT},
		RequestId:     c.GetRequestId(),
	}
	if c.GetAuthorizationContextId() != "" {
		id := c.GetAuthorizationContextId()
		authReq.AuthorizationContextId = &id
	}
	decision, err := s.AuthorizeAction(ctx, authReq)
	if err != nil {
		return nil, err
	}
	if decision.GetResult() != api.AuthorizationResult_AUTHORIZATION_RESULT_ALLOWED {
		return nil, status.Error(codes.PermissionDenied, "platform administrator authorization required")
	}
	rawIDs := r.GetActorIds()
	if len(rawIDs) > maxServiceEntitlementBatchSize {
		return nil, status.Error(codes.InvalidArgument, "too many actor ids requested")
	}
	if len(rawIDs) == 0 || len(s.serviceCatalog) == 0 {
		return &api.ServiceEntitlementList{}, nil
	}
	seen := make(map[string]bool, len(rawIDs))
	unique := make([]string, 0, len(rawIDs))
	args := make([]any, 0, len(rawIDs))
	placeholders := make([]string, 0, len(rawIDs))
	for _, raw := range rawIDs {
		id := strings.TrimSpace(raw)
		if id == "" || id != raw {
			return nil, status.Error(codes.InvalidArgument, "actor ids must be non-empty trimmed identifiers")
		}
		if !seen[id] {
			seen[id] = true
			unique = append(unique, id)
			args = append(args, id)
			placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
		}
	}

	type storedRow struct {
		enabled        bool
		role           string
		originOverride sql.NullString
		limits         map[string]any
		expiresAt      sql.NullTime
		reason         string
		updatedBy      string
		updatedAt      time.Time
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT actor_id, service_id, enabled, role, origin_override, limits_json, expires_at, reason, updated_by, updated_at
		FROM tenant.service_entitlements
		WHERE actor_id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return nil, persistence(err)
	}
	defer rows.Close()
	stored := make(map[string]storedRow)
	for rows.Next() {
		var actorID, serviceID, role, reason, updatedBy string
		var enabled bool
		var originOverride sql.NullString
		var limitsRaw []byte
		var expiresAt sql.NullTime
		var updatedAt time.Time
		if scanErr := rows.Scan(&actorID, &serviceID, &enabled, &role, &originOverride, &limitsRaw, &expiresAt, &reason, &updatedBy, &updatedAt); scanErr != nil {
			return nil, persistence(scanErr)
		}
		limits, limitErr := limitsFromRawJSON(limitsRaw)
		if limitErr != nil {
			return nil, status.Error(codes.FailedPrecondition, "stored service entitlement limits_json is invalid")
		}
		stored[actorID+":"+serviceID] = storedRow{
			enabled:        enabled,
			role:           role,
			originOverride: originOverride,
			limits:         limits,
			expiresAt:      expiresAt,
			reason:         reason,
			updatedBy:      updatedBy,
			updatedAt:      updatedAt,
		}
	}
	if err := rows.Err(); err != nil {
		return nil, persistence(err)
	}

	now := time.Now().UTC()
	serviceIDs := s.sortedCatalogServiceIDs()
	items := make([]*api.ServiceEntitlementReadback, 0, len(unique)*len(serviceIDs))
	for _, actorID := range unique {
		for _, serviceID := range serviceIDs {
			entry := s.serviceCatalog[serviceID]
			row, found := stored[actorID+":"+serviceID]
			if !found {
				defaultEnabled := entry.AdmissionMode == AdmissionActiveGatewayDefault
				limitsStruct, structErr := limitsToStruct(entry.DefaultLimits)
				if structErr != nil {
					return nil, status.Error(codes.Internal, "failed to encode default service limits")
				}
				items = append(items, &api.ServiceEntitlementReadback{
					ActorId:             actorID,
					ServiceId:           serviceID,
					AdmissionMode:       entry.AdmissionMode,
					HasExplicitOverride: false,
					Enabled:             defaultEnabled,
					EffectiveEnabled:    defaultEnabled,
					Role:                "user",
					Origin:              entry.DefaultOrigin,
					Limits:              limitsStruct,
				})
				continue
			}
			origin := entry.DefaultOrigin
			if row.originOverride.Valid && strings.TrimSpace(row.originOverride.String) != "" {
				origin = strings.TrimSpace(row.originOverride.String)
			}
			role := strings.TrimSpace(row.role)
			if role == "" {
				role = "user"
			}
			mergedLimits := cloneAndMergeLimits(entry.DefaultLimits, row.limits)
			limitsStruct, structErr := limitsToStruct(mergedLimits)
			if structErr != nil {
				return nil, status.Error(codes.Internal, "failed to encode service limits")
			}
			effectiveEnabled := row.enabled && (!row.expiresAt.Valid || row.expiresAt.Time.After(now))
			readback := &api.ServiceEntitlementReadback{
				ActorId:             actorID,
				ServiceId:           serviceID,
				AdmissionMode:       entry.AdmissionMode,
				HasExplicitOverride: true,
				Enabled:             row.enabled,
				EffectiveEnabled:    effectiveEnabled,
				Role:                role,
				Origin:              origin,
				Limits:              limitsStruct,
				Reason:              proto.String(row.reason),
				UpdatedBy:           proto.String(row.updatedBy),
				UpdatedAt:           timestamppb.New(row.updatedAt),
			}
			if row.originOverride.Valid {
				readback.OriginOverride = proto.String(row.originOverride.String)
			}
			if row.expiresAt.Valid {
				readback.ExpiresAt = timestamppb.New(row.expiresAt.Time)
			}
			items = append(items, readback)
		}
	}
	return &api.ServiceEntitlementList{Items: items}, nil
}
