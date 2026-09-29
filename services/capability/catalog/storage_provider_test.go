package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// The byte-plane seam must be satisfied by both the single-node filesystem
// provider and the Tencent COS provider.
var (
	_ Storage = (*localStorage)(nil)
	_ Storage = (*cosStorage)(nil)
)

func TestLocalProviderStreamsThroughCapability(t *testing.T) {
	root := t.TempDir()
	store, err := newLocalStorage(root, "https://capability.example.test")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := interface{}(store).(interface{ DirectUploadOnly() bool }); ok {
		t.Fatal("local provider must expose its restricted ingest data plane")
	}
}

func TestLocalProviderPromotesOnlyAfterValidationAndRetainsRetryInput(t *testing.T) {
	ctx := context.Background()
	store, err := newLocalStorage(t.TempDir(), "http://127.0.0.1:8281")
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("package bytes")
	hash := sha256.Sum256(data)
	digest := "sha256:" + hex.EncodeToString(hash[:])
	if _, err := store.PutPart(ctx, "upload-1", 1, strings.NewReader(string(data)), int64(len(data)), digest); err != nil {
		t.Fatal(err)
	}
	parts := []ConfirmedPart{{PartNumber: 1, SizeBytes: int64(len(data)), Sha256: digest}}
	assembled, err := store.Assemble(ctx, "upload-1", "", digest, int64(len(data)), parts)
	if err != nil {
		t.Fatal(err)
	}
	defer assembled.Cleanup()
	if _, err := os.Stat(store.root + "/objects/" + strings.TrimPrefix(digest, "sha256:")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("assembly must not publish bytes before archive validation: %v", err)
	}
	if err := store.Promote(ctx, "upload-1", digest, assembled); err != nil {
		t.Fatal(err)
	}
	object, err := store.OpenImmutable(ctx, digest)
	if err != nil {
		t.Fatal(err)
	}
	defer object.Close()
	read, err := io.ReadAll(object.Body)
	if err != nil || string(read) != string(data) {
		t.Fatalf("promoted bytes differ: %q, %v", read, err)
	}
	retry, err := store.Assemble(ctx, "upload-1", "", digest, int64(len(data)), parts)
	if err != nil {
		t.Fatalf("assembly must remain retryable until owner commit: %v", err)
	}
	retry.Cleanup()
}

func TestLocalProviderRejectsUnencryptedPublicEndpoint(t *testing.T) {
	if _, err := newLocalStorage(t.TempDir(), "http://objects.example.test"); err == nil {
		t.Fatal("non-loopback http upload endpoint must be rejected")
	}
	if _, err := newLocalStorage(t.TempDir(), "http://127.0.0.1:8281"); err != nil {
		t.Fatalf("loopback http must be admitted for local development: %v", err)
	}
}

func TestCOSProviderIsDirectUploadOnly(t *testing.T) {
	store, err := newCOSStorage(COSConfig{Bucket: "opl-example-1410708315", Region: "na-siliconvalley", SecretID: "AKIDEXAMPLE", SecretKey: "SECRETEXAMPLE"})
	if err != nil {
		t.Fatal(err)
	}
	if !store.DirectUploadOnly() {
		t.Fatal("COS provider must never accept part bytes through Capability")
	}
}

func TestCOSProviderRequiresExplicitBinding(t *testing.T) {
	for name, cfg := range map[string]COSConfig{
		"missing bucket":     {Region: "na-siliconvalley", SecretID: "a", SecretKey: "b"},
		"missing region":     {Bucket: "opl-example-1410708315", SecretID: "a", SecretKey: "b"},
		"missing secret id":  {Bucket: "opl-example-1410708315", Region: "na-siliconvalley", SecretKey: "b"},
		"missing key":        {Bucket: "opl-example-1410708315", Region: "na-siliconvalley", SecretID: "a"},
		"plaintext endpoint": {Bucket: "opl-example-1410708315", Region: "na-siliconvalley", SecretID: "a", SecretKey: "b", Endpoint: "http://cos.example.test"},
	} {
		if _, err := newCOSStorage(cfg); err == nil {
			t.Fatalf("%s must be rejected", name)
		}
	}
}

func TestCOSAuthorizePartBindsExactScopedPart(t *testing.T) {
	store, err := newCOSStorage(COSConfig{Bucket: "opl-example-1410708315", Region: "na-siliconvalley", SecretID: "AKIDEXAMPLE", SecretKey: "SECRETEXAMPLE"})
	if err != nil {
		t.Fatal(err)
	}
	digest := "sha256:" + strings.Repeat("ab", 32)
	auth, err := store.AuthorizePart(context.Background(), "upload-abc", "uploadid-xyz", 3, 16<<20, digest, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(auth.URL)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "https" || !strings.HasSuffix(u.Host, ".cos.na-siliconvalley.myqcloud.com") {
		t.Fatalf("presigned URL must address the instance bucket over TLS: %s", auth.URL)
	}
	if u.Path != "/staging/upload-abc" {
		t.Fatalf("presigned part must target the upload staging key, got %s", u.Path)
	}
	q := u.Query()
	if q.Get("partNumber") != "3" || q.Get("uploadId") != "uploadid-xyz" {
		t.Fatalf("presigned part must bind part number and provider upload id: %s", u.RawQuery)
	}
	if q.Get("q-signature") == "" || q.Get("q-sign-algorithm") == "" {
		t.Fatalf("presigned URL must carry a COS signature: %s", u.RawQuery)
	}
	if auth.ContentType != "application/octet-stream" || auth.ChecksumName != "X-OPL-SHA256" || auth.ChecksumValue != digest {
		t.Fatalf("presigned part must declare the owner identity, got %+v", auth)
	}
}

func TestCOSAuthorizePartRequiresLiveSessionAndExpiry(t *testing.T) {
	store, err := newCOSStorage(COSConfig{Bucket: "opl-example-1410708315", Region: "na-siliconvalley", SecretID: "AKIDEXAMPLE", SecretKey: "SECRETEXAMPLE"})
	if err != nil {
		t.Fatal(err)
	}
	digest := "sha256:" + strings.Repeat("ab", 32)
	if _, err := store.AuthorizePart(context.Background(), "upload-abc", "", 1, 1<<20, digest, time.Now().Add(time.Hour)); err != ErrStorageUnavailable {
		t.Fatalf("missing provider upload id must fail closed: %v", err)
	}
	if _, err := store.AuthorizePart(context.Background(), "upload-abc", "uploadid-xyz", 1, 1<<20, digest, time.Now().Add(-time.Minute)); err != ErrStorageUnavailable {
		t.Fatalf("expired part must fail closed: %v", err)
	}
}

func TestCOSImmutableObjectsAreContentAddressed(t *testing.T) {
	digest := "sha256:" + strings.Repeat("cd", 32)
	key, err := immutableKey(digest)
	if err != nil {
		t.Fatal(err)
	}
	if key != "objects/sha256/"+strings.Repeat("cd", 32) {
		t.Fatalf("immutable key must be the content digest, got %s", key)
	}
	if _, err := immutableKey("sha256:nothex"); err == nil {
		t.Fatal("non-digest object identity must be rejected")
	}
}
