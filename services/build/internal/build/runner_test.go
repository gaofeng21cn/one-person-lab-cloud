package build

import (
	"archive/tar"
	"bytes"
	"testing"
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
