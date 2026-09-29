package build

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func mustRequest(t *testing.T, raw string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

// A real registry redirects blob reads to a separate storage host, so the worker
// must follow an approved redirect instead of rejecting every one.
func TestRunnerRedirectPolicyIsBounded(t *testing.T) {
	strict := (&Runner{}).client()
	if err := strict.CheckRedirect(mustRequest(t, "https://registry.example/blob"), nil); err != nil {
		t.Fatalf("https redirect must be followed: %v", err)
	}
	if err := strict.CheckRedirect(mustRequest(t, "http://registry.example/blob"), nil); err == nil {
		t.Fatal("plain http redirect must be refused when http is not allowed")
	}

	permissive := (&Runner{AllowHTTP: true}).client()
	if err := permissive.CheckRedirect(mustRequest(t, "http://127.0.0.1:5000/blob"), nil); err != nil {
		t.Fatalf("http redirect must be followed when http is allowed: %v", err)
	}

	via := make([]*http.Request, 5)
	if err := permissive.CheckRedirect(mustRequest(t, "https://registry.example/blob"), via); err == nil {
		t.Fatal("an over-long redirect chain must be refused")
	}
}

// The registry credential is a bearer token. When the redirect leaves the
// registry host the storage service must receive the presigned URL alone, never
// the registry's own Authorization header.
func TestRunnerFollowsBlobRedirectWithoutRegistryCredential(t *testing.T) {
	const blob = "immutable blob bytes"
	blobDigest := digest([]byte(blob))
	var leakedAuthorization string
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leakedAuthorization = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(blob))
	}))
	defer storage.Close()
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, storage.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer registry.Close()

	runner := &Runner{AllowHTTP: true}
	host := strings.TrimPrefix(registry.URL, "http://")
	got, err := runner.readBlob(context.Background(), host+"/oplcloud/tenant-abc", blobDigest, 1<<20)
	if err != nil {
		t.Fatalf("redirected blob read failed: %v", err)
	}
	if string(got) != blob {
		t.Fatalf("blob body=%q", got)
	}
	if leakedAuthorization != "" {
		t.Fatalf("registry credential reached the storage host: %q", leakedAuthorization)
	}
}
