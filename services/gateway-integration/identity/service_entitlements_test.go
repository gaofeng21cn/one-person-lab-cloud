package identity

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	api "opl-cloud/packages/contracts/go/api"
)

func TestParseServiceCatalogJSONValidation(t *testing.T) {
	valid := `{
		"netfleet_cloud": {
			"default_origin": "https://netfleet.example.test",
			"audience": "opl-netfleet-cloud",
			"admission_mode": "active_gateway_default_with_overrides",
			"default_limits": {"max_sites": 10}
		},
		"link_relay": {
			"default_origin": "https://link.example.test",
			"audience": "opl-link-relay",
			"admission_mode": "allowlist_only"
		}
	}`
	catalog, err := ParseServiceCatalogJSON(valid)
	if err != nil {
		t.Fatalf("valid catalog rejected: %v", err)
	}
	if len(catalog) != 2 || catalog["netfleet_cloud"].Audience != "opl-netfleet-cloud" {
		t.Fatalf("unexpected catalog: %+v", catalog)
	}

	for _, bad := range []string{
		`{"bad-id":{"default_origin":"https://a.test","audience":"aud","admission_mode":"allowlist_only"}}`,
		`{"netfleet_cloud":{"default_origin":"http://a.test","audience":"aud","admission_mode":"allowlist_only"}}`,
		`{"netfleet_cloud":{"default_origin":"https://a.test/path","audience":"aud","admission_mode":"allowlist_only"}}`,
		`{"netfleet_cloud":{"default_origin":"https://a.test/","audience":"aud","admission_mode":"allowlist_only"}}`,
		`{"netfleet_cloud":{"default_origin":"https://a.test","audience":"aud","admission_mode":"unknown"}}`,
	} {
		if _, err := ParseServiceCatalogJSON(bad); err == nil {
			t.Fatalf("expected error for invalid catalog %s", bad)
		}
	}
}

func TestGrantSignerSignsVerifiableEd25519JWTAndJWKS(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	pemText := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8}))
	extraPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	extrasJSON, _ := json.Marshal([]map[string]string{
		{"kid": "prev-key", "x": base64.RawURLEncoding.EncodeToString(extraPub)},
	})

	signer, err := NewGrantSigner(pemText, "active-key", "https://cloud.example.test", string(extrasJSON), 2*time.Minute)
	if err != nil {
		t.Fatalf("NewGrantSigner failed: %v", err)
	}
	token, err := signer.signJWT(map[string]any{
		"iss":     signer.issuer,
		"sub":     "1042",
		"email":   "user@example.test",
		"aud":     "opl-netfleet-cloud",
		"service": "netfleet_cloud",
		"scope":   "device:enroll",
		"role":    "user",
		"limits":  map[string]any{"max_sites": 10},
		"iat":     1700000000,
		"exp":     1700000120,
		"jti":     "grant-1",
	})
	if err != nil {
		t.Fatalf("signJWT failed: %v", err)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("expected 3 JWT segments, got %d", len(parts))
	}
	headerRaw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatal(err)
	}
	var header map[string]string
	if err := json.Unmarshal(headerRaw, &header); err != nil {
		t.Fatal(err)
	}
	if header["alg"] != "EdDSA" || header["typ"] != "JWT" || header["kid"] != "active-key" {
		t.Fatalf("unexpected JWT header: %+v", header)
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	if !ed25519.Verify(pub, []byte(parts[0]+"."+parts[1]), sig) {
		t.Fatal("Ed25519 signature did not verify")
	}

	jwks := signer.jwks()
	if len(jwks.GetKeys()) != 2 || jwks.Keys[0].GetKid() != "active-key" || jwks.Keys[0].GetCrv() != "Ed25519" || jwks.Keys[1].GetKid() != "prev-key" {
		t.Fatalf("unexpected JWKS: %+v", jwks.GetKeys())
	}

	// Also verify 32-byte base64url seed loading works without PEM.
	seedSigner, err := NewGrantSigner(base64.RawURLEncoding.EncodeToString(priv.Seed()), "", "https://cloud.example.test", "", 0)
	if err != nil || seedSigner.kid == "" || !seedSigner.pub.Equal(pub) {
		t.Fatalf("seed signer failed: err=%v signer=%+v", err, seedSigner)
	}
}

func TestGatewayLoginForServiceGrantHandles2FAAndBearer(t *testing.T) {
	logoutCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/login":
			var req map[string]string
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			switch req["email"] {
			case "normal@example.test":
				if req["password"] != "good-pass" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"code": 0,
					"data": map[string]any{
						"access_token":  "gw-access-normal",
						"refresh_token": "gw-refresh-normal",
						"user":          map[string]any{"id": 1042, "email": "Normal@Example.Test", "status": "active"},
					},
				})
			case "twofa@example.test":
				if req["password"] != "good-pass" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"code": 0,
					"data": map[string]any{
						"requires_2fa": true,
						"temp_token":   "tmp-2fa-token",
					},
				})
			default:
				w.WriteHeader(http.StatusUnauthorized)
			}
		case "/api/v1/auth/login/2fa":
			var req map[string]string
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req["temp_token"] != "tmp-2fa-token" || req["totp_code"] != "123456" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{
					"access_token": "gw-access-2fa",
					"user":         map[string]any{"id": 2048, "email": "twofa@example.test", "status": "active"},
				},
			})
		case "/api/v1/auth/me":
			if r.Header.Get("Authorization") != "Bearer live-bearer-1" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{"id": 3001, "email": "BearerUser@Example.Test", "status": "active"},
			})
		case "/api/v1/auth/logout":
			logoutCalls++
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"ok": true}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	gw, err := NewGateway(srv.URL)
	if err != nil {
		t.Fatal(err)
	}

	id, req2fa, err := gw.LoginForServiceGrant(context.Background(), " Normal@Example.test ", "good-pass", "")
	if err != nil || req2fa || id.ID != 1042 || id.Email != "normal@example.test" || logoutCalls != 1 {
		t.Fatalf("normal login failed: id=%+v req2fa=%v logout=%d err=%v", id, req2fa, logoutCalls, err)
	}

	id, req2fa, err = gw.LoginForServiceGrant(context.Background(), "twofa@example.test", "good-pass", "")
	if err != nil || !req2fa || id.ID != 0 {
		t.Fatalf("expected 2FA challenge, got id=%+v req2fa=%v err=%v", id, req2fa, err)
	}

	id, req2fa, err = gw.LoginForServiceGrant(context.Background(), "twofa@example.test", "good-pass", "123456")
	if err != nil || req2fa || id.ID != 2048 || id.Email != "twofa@example.test" {
		t.Fatalf("2FA completion failed: id=%+v req2fa=%v err=%v", id, req2fa, err)
	}

	if _, _, err := gw.LoginForServiceGrant(context.Background(), "twofa@example.test", "good-pass", "000000"); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("expected Unauthenticated on wrong TOTP, got %v", err)
	}

	bearerID, err := gw.AuthenticateBearer(context.Background(), "live-bearer-1")
	if err != nil || bearerID.ID != 3001 || bearerID.Email != "beareruser@example.test" {
		t.Fatalf("AuthenticateBearer failed: id=%+v err=%v", bearerID, err)
	}
}

func TestServiceFailClosedWhenGrantSignerMissing(t *testing.T) {
	svc := &Service{}
	if _, err := svc.GetServiceGrantJWKS(context.Background(), &api.GetServiceGrantJWKSRpcRequest{}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected peer check refusal without mTLS context, got %v", err)
	}
}
