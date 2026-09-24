package build

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

type zipEntry struct {
	name, body string
	mode       os.FileMode
}

func packageZIP(t *testing.T, entries ...zipEntry) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Store}
		h.SetMode(e.mode)
		f, err := w.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func TestPackageZIPPreservesContentsAndExecutableMode(t *testing.T) {
	data := packageZIP(t, zipEntry{"manifest.json", `{"name":"sample"}`, 0644}, zipEntry{"scripts/start", "#!/bin/sh\n", 0755})
	dir := t.TempDir()
	if err := extractPackage(data, dir, 1024, 4); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil || string(b) != `{"name":"sample"}` {
		t.Fatalf("manifest: %s, %v", b, err)
	}
	info, err := os.Stat(filepath.Join(dir, "scripts/start"))
	if err != nil || info.Mode().Perm() != 0755 {
		t.Fatalf("executable permission lost: %v, %v", info, err)
	}
}
func TestPackageZIPRejectsUnsafeAndCorruptArchives(t *testing.T) {
	for _, tc := range []struct {
		name  string
		data  []byte
		max   int64
		files int
	}{
		{"parent", packageZIP(t, zipEntry{"../escape", "x", 0644}), 100, 10},
		{"absolute", packageZIP(t, zipEntry{"/escape", "x", 0644}), 100, 10},
		{"backslash", packageZIP(t, zipEntry{`a\b`, "x", 0644}), 100, 10},
		{"symlink", packageZIP(t, zipEntry{"link", "outside", os.ModeSymlink | 0777}), 100, 10},
		{"duplicate", packageZIP(t, zipEntry{"file", "x", 0644}, zipEntry{"file", "y", 0644}), 100, 10},
		{"expansion", packageZIP(t, zipEntry{"file", "12345", 0644}), 4, 10},
		{"entries", packageZIP(t, zipEntry{"file", "x", 0644}), 100, 0},
		{"tar", archiveEntry(t, "file", '0', "x"), 100, 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := extractPackage(tc.data, t.TempDir(), tc.max, tc.files); err == nil {
				t.Fatal("unsafe ZIP accepted")
			}
		})
	}
	data := packageZIP(t, zipEntry{"file", "unique-content", 0644})
	data[bytes.Index(data, []byte("unique-content"))] ^= 1
	if err := extractPackage(data, t.TempDir(), 100, 10); err == nil {
		t.Fatal("ZIP checksum corruption accepted")
	}
}
