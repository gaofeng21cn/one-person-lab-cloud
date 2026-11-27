package httpapi

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestConsoleStaticHosting(t *testing.T) {
	dist := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dist, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte("<!doctype html><html><body>console-bff-spa</body></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dist, "assets", "app.js"), []byte("console.log('ok');"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dist, "favicon.ico"), []byte("icon"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPL_CONSOLE_DIST_DIR", dist)

	handler := NewServer(nil, nil).Handler()

	t.Run("serves immutable gzipped assets", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if rec.Header().Get("Cache-Control") != "public,max-age=31536000,immutable" {
			t.Fatalf("Cache-Control = %q", rec.Header().Get("Cache-Control"))
		}
		if rec.Header().Get("Content-Encoding") != "gzip" {
			t.Fatalf("Content-Encoding = %q, want gzip", rec.Header().Get("Content-Encoding"))
		}
		reader, err := gzip.NewReader(rec.Body)
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Close()
		body, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != "console.log('ok');" {
			t.Fatalf("body = %q", string(body))
		}
	})

	t.Run("serves root static file", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/favicon.ico", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || rec.Body.String() != "icon" {
			t.Fatalf("got (%d, %q)", rec.Code, rec.Body.String())
		}
		if rec.Header().Get("Cache-Control") != "public,max-age=86400" {
			t.Fatalf("Cache-Control = %q", rec.Header().Get("Cache-Control"))
		}
	})

	t.Run("falls back SPA routes to index.html", func(t *testing.T) {
		for _, target := range []string{"/", "/workspaces/ws-123", "/operator/accounts"} {
			req := httptest.NewRequest(http.MethodGet, target, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-cache" {
				t.Fatalf("%s: status=%d cache=%q", target, rec.Code, rec.Header().Get("Cache-Control"))
			}
			if rec.Body.String() != "<!doctype html><html><body>console-bff-spa</body></html>" {
				t.Fatalf("%s: body=%q", target, rec.Body.String())
			}
		}
	})

	t.Run("rejects path traversal and missing files", func(t *testing.T) {
		for _, target := range []string{"/assets/../index.html", "/../etc/passwd", "/missing.js"} {
			req := httptest.NewRequest(http.MethodGet, target, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("%s: status=%d, want 404", target, rec.Code)
			}
		}
	})

	t.Run("never serves HTML fallback for unknown api or internal paths", func(t *testing.T) {
		for _, target := range []string{"/api", "/api/missing", "/api/v2/missing", "/internal", "/internal/missing"} {
			req := httptest.NewRequest(http.MethodGet, target, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("%s: status=%d, want 404", target, rec.Code)
			}
		}
	})

	t.Run("rejects non-GET/HEAD requests to root", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/workspaces", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status=%d, want 404", rec.Code)
		}
	})
}
