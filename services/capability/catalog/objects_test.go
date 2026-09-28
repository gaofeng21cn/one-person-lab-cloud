package catalog

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func candidateArchive(t *testing.T, mutate func(*omaCandidateIndex, map[string][]byte, map[string]any)) string {
	t.Helper()
	files := map[string][]byte{"agent/agent-pack.json": []byte(`{"id":"agent"}`), "runtime/start.js": []byte("run()\n")}
	idx := omaCandidateIndex{SurfaceKind: "opl_foundry_candidate_file_index", Version: "opl-foundry-candidate-index.v2", BlueprintDigest: "sha256:" + strings.Repeat("1", 64)}
	for _, name := range []string{"agent/agent-pack.json", "runtime/start.js"} {
		idx.Files = append(idx.Files, omaCandidateFile{Path: name, ByteSize: int64(len(files[name])), SHA256: strings.TrimPrefix(digest(files[name]), "sha256:")})
	}
	envelope := map[string]any{"schema_version": 1, "surface_kind": "opl_foundry_candidate_transport.v1", "transport_format": "ZIP", "candidate_root": "candidate", "qualification_status": "not_qualified", "domain_quality_status": "not_evaluated", "admission": map[string]any{"status": "development_transport_ready"}}
	if mutate != nil {
		mutate(&idx, files, envelope)
	}
	idx.CandidateDigest = omaCandidateDigest(idx)
	index, err := json.Marshal(idx)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.New()
	var total int64
	for _, item := range idx.Files {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(item.Path)))
		h.Write(n[:])
		h.Write([]byte(item.Path))
		binary.BigEndian.PutUint64(n[:], uint64(len(files[item.Path])))
		h.Write(n[:])
		h.Write(files[item.Path])
		total += int64(len(files[item.Path]))
	}
	if _, override := envelope["candidate_digest"]; !override {
		envelope["candidate_digest"] = idx.CandidateDigest
	}
	envelope["candidate_file_count"] = len(idx.Files)
	envelope["candidate_total_bytes"] = total
	envelope["candidate_index_sha256"] = digest(index)
	envelope["content_digest"] = "sha256:" + hex.EncodeToString(h.Sum(nil))
	envelope["manifest_digest"] = digest(files["agent/agent-pack.json"])
	manifest, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string][]byte{"manifest.json": manifest, "candidate/candidate-index.json": index}
	for name, data := range files {
		entries["candidate/"+name] = data
	}
	archive := filepath.Join(t.TempDir(), "candidate.zip")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for name, data := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = entry.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return archive
}

func testCandidateObjects(t *testing.T) *Objects {
	t.Helper()
	root := t.TempDir()
	schema := []byte(`{"type":"object","required":["name"]}`)
	path := filepath.Join(root, "schema.json")
	if err := os.WriteFile(path, schema, 0600); err != nil {
		t.Fatal(err)
	}
	objects, err := NewObjects(root, "http://127.0.0.1:1", bytes.Repeat([]byte("x"), 32), UploadPolicy{MaxBytes: 1 << 20, PartBytes: 1 << 20, MaxExpandedBytes: 2 << 20, MaxFiles: 128, TTL: time.Minute, ManifestPath: "manifest.json", SchemaPath: path, SchemaDigest: digest(schema)})
	if err != nil {
		t.Fatal(err)
	}
	return objects
}

func TestNativeOMACandidateTransportInventoryAndIdentity(t *testing.T) {
	for _, test := range []struct {
		name      string
		mutate    func(*omaCandidateIndex, map[string][]byte, map[string]any)
		wantError string
	}{
		{name: "native exact inventory"},
		{name: "arbitrary candidate identity", mutate: func(_ *omaCandidateIndex, _ map[string][]byte, envelope map[string]any) {
			envelope["candidate_digest"] = "sha256:" + strings.Repeat("0", 64)
		}, wantError: "identity digest mismatch"},
		{name: "duplicate index hides unlisted payload", mutate: func(idx *omaCandidateIndex, _ map[string][]byte, _ map[string]any) { idx.Files[1] = idx.Files[0] }, wantError: "file digest mismatch"},
		{name: "tampered indexed content", mutate: func(_ *omaCandidateIndex, files map[string][]byte, _ map[string]any) {
			files["runtime/start.js"] = []byte("changed")
		}, wantError: "file digest mismatch"},
	} {
		t.Run(test.name, func(t *testing.T) {
			archive := candidateArchive(t, test.mutate)
			_, err := testCandidateObjects(t).validateArchive(archive)
			if test.wantError == "" && err != nil {
				t.Fatal(err)
			}
			if test.wantError != "" && (err == nil || !strings.Contains(err.Error(), test.wantError)) {
				t.Fatalf("error=%v, want %s", err, test.wantError)
			}
		})
	}
}

func TestNativeOMACandidateTransportValidatesExactManifestAndContent(t *testing.T) {
	path := os.Getenv("OPL_OMA_CANDIDATE_TRANSPORT")
	if path == "" {
		t.Skip("OPL_OMA_CANDIDATE_TRANSPORT is not set")
	}
	if _, err := testCandidateObjects(t).validateArchive(path); err != nil {
		t.Fatal(err)
	}
}

func TestOMACandidateIdentityMatchesFoundryCanonicalJSON(t *testing.T) {
	// Generated with Framework's canonicalDigest over the v2 file index; covers
	// JSON.stringify's non-escaped HTML, Unicode and line-separator behavior.
	idx := omaCandidateIndex{SurfaceKind: "opl_foundry_candidate_file_index", Version: "opl-foundry-candidate-index.v2", BlueprintDigest: "sha256:" + strings.Repeat("1", 64), Files: []omaCandidateFile{{Path: "agent/测<&\u2028.json", ByteSize: 12, SHA256: strings.Repeat("2", 64)}}}
	const want = "sha256:fb500a3b06f91f2dff71f718549f4deba7ce2354a8f65fc69ff192e6a4b9e42d"
	if got := omaCandidateDigest(idx); got != want {
		t.Fatalf("candidate digest=%s, want %s", got, want)
	}
}
