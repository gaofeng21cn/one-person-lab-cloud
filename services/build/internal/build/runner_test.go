package build

import (
	"archive/tar"
	"bytes"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
)

func archiveEntry(t *testing.T, name string, kind byte, body string) []byte {
	t.Helper()
	var out bytes.Buffer
	w := tar.NewWriter(&out)
	if err := w.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(body)), Typeflag: kind}); err != nil {
		t.Fatal(err)
	}
	if kind == tar.TypeReg {
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestExtractRejectsTraversalAndLinks(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind byte
	}{
		{name: "../escape", kind: tar.TypeReg},
		{name: "link", kind: tar.TypeSymlink},
	} {
		dir := t.TempDir()
		if err := extract(archiveEntry(t, tc.name, tc.kind, "x"), dir, 1024, 10); err == nil {
			t.Fatalf("extract accepted unsafe archive entry %q", tc.name)
		}
	}
}

func TestExtractEnforcesLimits(t *testing.T) {
	if err := extract(archiveEntry(t, "package/file", tar.TypeReg, "0123456789"), t.TempDir(), 4, 10); err == nil {
		t.Fatal("extract accepted an archive over the expansion limit")
	}
}

func confirmedUserDestination() *api.UserExternalBindingReadback {
	at := timestamppb.New(time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC))
	return &api.UserExternalBindingReadback{
		ActorId: "user-1", Sub2ApiUserId: "101", RegistryHost: "uswccr.ccs.tencentyun.com", RegistryNamespace: "oplcloud", Repository: "testcloud4",
		Status: api.UserExternalBindingReadbackStatusEnum_USER_EXTERNAL_BINDING_READBACK_STATUS_ENUM_ACTIVE, Verification: api.UserExternalBindingReadbackVerificationEnum_USER_EXTERNAL_BINDING_READBACK_VERIFICATION_ENUM_CONFIRMED, VerificationEvidenceRef: "sub2api:user:101",
		VerifiedAt: at, DestinationVerifiedAt: at, CreatedAt: at, UpdatedAt: at,
	}
}

func TestDestinationRepositoryUsesInstallationNamespace(t *testing.T) {
	for _, tc := range []struct{ prefix, host, namespace string }{
		{"uswccr.ccs.tencentyun.com/oplcloud", "uswccr.ccs.tencentyun.com", "oplcloud"},
		{"127.0.0.1:5000/result", "127.0.0.1:5000", "result"},
	} {
		t.Run(tc.prefix, func(t *testing.T) {
			binding := confirmedUserDestination()
			binding.RegistryHost, binding.RegistryNamespace = tc.host, tc.namespace
			got, err := (&Runner{RegistryPrefix: tc.prefix}).DestinationRepository(binding)
			want := tc.host + "/" + tc.namespace + "/testcloud4"
			if err != nil || got != want {
				t.Fatalf("destination=%q, err=%v; want %q", got, err, want)
			}
		})
	}
}

func TestDestinationRepositoryRejectsUnverifiedOrUnsafeBinding(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*api.UserExternalBindingReadback)
	}{
		{"missing actor", func(b *api.UserExternalBindingReadback) { b.ActorId = "" }},
		{"missing upstream user", func(b *api.UserExternalBindingReadback) { b.Sub2ApiUserId = "" }},
		{"revoked", func(b *api.UserExternalBindingReadback) { b.Status = api.UserExternalBindingReadbackStatusEnum(999) }},
		{"unverified", func(b *api.UserExternalBindingReadback) {
			b.Verification = api.UserExternalBindingReadbackVerificationEnum_USER_EXTERNAL_BINDING_READBACK_VERIFICATION_ENUM_UNSPECIFIED
		}},
		{"rejected", func(b *api.UserExternalBindingReadback) {
			b.Verification = api.UserExternalBindingReadbackVerificationEnum(999)
		}},
		{"missing evidence", func(b *api.UserExternalBindingReadback) { b.VerificationEvidenceRef = "" }},
		{"missing identity time", func(b *api.UserExternalBindingReadback) { b.VerifiedAt = nil }},
		{"missing destination time", func(b *api.UserExternalBindingReadback) { b.DestinationVerifiedAt = nil }},
		{"missing creation time", func(b *api.UserExternalBindingReadback) { b.CreatedAt = nil }},
		{"missing update time", func(b *api.UserExternalBindingReadback) { b.UpdatedAt = nil }},
		{"invalid time", func(b *api.UserExternalBindingReadback) { b.VerifiedAt = &timestamppb.Timestamp{Nanos: -1} }},
		{"zero time", func(b *api.UserExternalBindingReadback) { b.DestinationVerifiedAt = timestamppb.New(time.Time{}) }},
		{"host override", func(b *api.UserExternalBindingReadback) { b.RegistryHost = "other.example.test" }},
		{"namespace override", func(b *api.UserExternalBindingReadback) { b.RegistryNamespace = "other" }},
		{"host path", func(b *api.UserExternalBindingReadback) { b.RegistryHost += "/extra" }},
		{"host credentials", func(b *api.UserExternalBindingReadback) { b.RegistryHost = "user@uswccr.ccs.tencentyun.com" }},
		{"host scheme", func(b *api.UserExternalBindingReadback) { b.RegistryHost = "https://" + b.RegistryHost }},
		{"namespace path", func(b *api.UserExternalBindingReadback) { b.RegistryNamespace += "/extra" }},
		{"namespace traversal", func(b *api.UserExternalBindingReadback) { b.RegistryNamespace = "../oplcloud" }},
		{"repository path", func(b *api.UserExternalBindingReadback) { b.Repository = "other/testcloud4" }},
		{"repository traversal", func(b *api.UserExternalBindingReadback) { b.Repository = "../testcloud4" }},
		{"repository parent", func(b *api.UserExternalBindingReadback) { b.Repository = ".." }},
		{"repository encoded traversal", func(b *api.UserExternalBindingReadback) { b.Repository = "%2e%2e" }},
		{"repository tag", func(b *api.UserExternalBindingReadback) { b.Repository += ":tag" }},
		{"repository digest", func(b *api.UserExternalBindingReadback) { b.Repository += "@sha256:abcd" }},
		{"repository empty", func(b *api.UserExternalBindingReadback) { b.Repository = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			binding := proto.Clone(confirmedUserDestination()).(*api.UserExternalBindingReadback)
			tc.change(binding)
			if got, err := (&Runner{RegistryPrefix: "uswccr.ccs.tencentyun.com/oplcloud"}).DestinationRepository(binding); err == nil || got != "" {
				t.Fatalf("unsafe binding accepted: destination=%q err=%v", got, err)
			}
		})
	}
	if _, err := (&Runner{RegistryPrefix: "uswccr.ccs.tencentyun.com/oplcloud"}).DestinationRepository(nil); err == nil {
		t.Fatal("nil binding accepted")
	}
}

func TestDestinationRepositoryRejectsInvalidInstallationPrefix(t *testing.T) {
	for _, prefix := range []string{"", "uswccr.ccs.tencentyun.com", "https://uswccr.ccs.tencentyun.com/oplcloud", "uswccr.ccs.tencentyun.com/oplcloud/", "uswccr.ccs.tencentyun.com/oplcloud/../escape", "uswccr.ccs.tencentyun.com/oplcloud//escape", "registry.example.test/installation/output"} {
		t.Run(prefix, func(t *testing.T) {
			if _, err := (&Runner{RegistryPrefix: prefix}).DestinationRepository(confirmedUserDestination()); err == nil {
				t.Fatal("unsafe installation prefix accepted")
			}
		})
	}
}
