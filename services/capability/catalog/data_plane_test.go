package catalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// The same-origin entry forwards a request's path verbatim, so the path a
// signed upload URL carries must be a path the Capability data plane itself
// serves. The read pattern GET /objects/{digest} matches every single-segment
// path under /objects, so an ingest route outside that namespace is shadowed
// by it and the upload is refused with 405 instead of reaching the handler.
func TestObjectDataPlaneServesTheSignedUploadPath(t *testing.T) {
	key := []byte(strings.Repeat("k", 32))
	publicURL := "https://localhost:8443/objects"
	store, err := newLocalStorage(t.TempDir(), publicURL, key)
	if err != nil {
		t.Fatal(err)
	}
	objects := &Objects{SigningKey: key, Policy: UploadPolicy{MaxBytes: 1 << 20, PartBytes: 1 << 20}, store: store}
	handler, err := (&Service{Objects: objects}).DataHandler(strings.Repeat("t", 32))
	if err != nil {
		t.Fatal(err)
	}

	// The signed URL is the provider's own authorization; its path is the one
	// the browser will PUT to.
	authorization, err := objects.store.AuthorizePart(context.Background(), "upload-1", "provider-1", 1, 16, "sha256:"+strings.Repeat("a", 64), time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	signed, err := url.Parse(authorization.URL)
	if err != nil {
		t.Fatal(err)
	}

	preflight := httptest.NewRecorder()
	handler.ServeHTTP(preflight, httptest.NewRequest(http.MethodOptions, signed.RequestURI(), nil))
	if preflight.Code != http.StatusNoContent || preflight.Header().Get("Access-Control-Allow-Methods") != "PUT" {
		t.Fatalf("signed upload preflight returned %d with methods %q", preflight.Code, preflight.Header().Get("Access-Control-Allow-Methods"))
	}

	// The signed path must be the ingest route, not the read route: an
	// unsigned PUT proves the request reached the ingest handler, because that
	// handler refuses it with 403 while the read pattern refuses it with 405.
	request := httptest.NewRequest(http.MethodPut, signed.Path, nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("the signed upload path %q reached status %d; the ingest route does not serve it (405 means the read pattern shadowed it)", signed.Path, recorder.Code)
	}
}

// The read route stays method-scoped: only GET reads an object. This is the
// pattern that shadows any ingest route registered outside the /objects
// namespace, so the guard above depends on it.
func TestObjectDataPlaneReadRouteStaysReadOnly(t *testing.T) {
	key := []byte(strings.Repeat("k", 32))
	store, err := newLocalStorage(t.TempDir(), "https://localhost:8443/objects", key)
	if err != nil {
		t.Fatal(err)
	}
	objects := &Objects{SigningKey: key, Policy: UploadPolicy{MaxBytes: 1 << 20, PartBytes: 1 << 20}, store: store}
	handler, err := (&Service{Objects: objects}).DataHandler(strings.Repeat("t", 32))
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPut, "/objects/"+strings.Repeat("a", 64), nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("a PUT to the read route returned %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
}
