//go:build livebuild

package catalog

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func testDigest(b []byte) string { h := sha256.Sum256(b); return "sha256:" + hex.EncodeToString(h[:]) }

func TestNativeOMACandidateTransportValidatesExactManifestAndContent(t *testing.T) {
	path := os.Getenv("OPL_OMA_CANDIDATE_TRANSPORT")
	if path == "" {
		t.Skip("OPL_OMA_CANDIDATE_TRANSPORT is not set")
	}
	objects := &Objects{Policy: UploadPolicy{MaxExpandedBytes: 8 << 20, MaxFiles: 128, ManifestPath: "manifest.json"}}
	r, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	manifest, err := readZipEntry(r, "manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := validateOMACandidateTransport(r, manifest, objects.Policy); err != nil {
		t.Fatal(err)
	}
}

func TestNativeOMACandidateTransportRejectsTamperedCandidate(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "candidate.zip")
	candidate := []byte("immutable candidate\n")
	index := map[string]any{"surface_kind": "opl-foundry-candidate-file-index", "version": "opl-foundry-candidate-index.v2", "files": []any{map[string]any{"path": "agent/agent-pack.json", "byte_size": len(candidate), "sha256": testDigest(candidate)[7:]}}}
	indexBytes, _ := json.Marshal(index)
	h := sha256.New()
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], uint64(len("agent/agent-pack.json")))
	h.Write(n[:])
	h.Write([]byte("agent/agent-pack.json"))
	binary.BigEndian.PutUint64(n[:], uint64(len(candidate)))
	h.Write(n[:])
	h.Write(candidate)
	envelope := map[string]any{"schema_version": 1, "surface_kind": "opl_foundry_candidate_transport.v1", "transport_format": "ZIP", "candidate_root": "candidate", "candidate_file_count": 1, "candidate_total_bytes": len(candidate), "candidate_digest": "sha256:" + hex.EncodeToString(make([]byte, 32)), "candidate_index_sha256": testDigest(indexBytes), "content_digest": "sha256:" + hex.EncodeToString(h.Sum(nil)), "manifest_digest": testDigest(candidate), "qualification_status": "not_qualified", "domain_quality_status": "not_evaluated", "admission": map[string]any{"status": "development_transport_ready"}}
	manifest, _ := json.Marshal(envelope)
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for name, data := range map[string][]byte{"manifest.json": manifest, "candidate/candidate-index.json": indexBytes, "candidate/agent/agent-pack.json": []byte("tampered\n")} {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write(data)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	m, _ := readZipEntry(r, "manifest.json")
	if err := validateOMACandidateTransport(r, m, UploadPolicy{MaxExpandedBytes: 1 << 20, MaxFiles: 20, ManifestPath: "manifest.json"}); err == nil {
		t.Fatal("tampered transport unexpectedly accepted")
	}
}

func readZipEntry(r *zip.ReadCloser, name string) ([]byte, error) {
	for _, f := range r.File {
		if f.Name == name {
			h, e := f.Open()
			if e != nil {
				return nil, e
			}
			defer h.Close()
			return io.ReadAll(h)
		}
	}
	return nil, os.ErrNotExist
}
