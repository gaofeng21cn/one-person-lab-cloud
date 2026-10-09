package catalog

import (
	"archive/zip"
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"opl-cloud/packages/contracts/go/publicjson"
)

// UploadPolicy is explicit instance configuration, not customer-controlled input.
type UploadPolicy struct {
	MaxBytes, PartBytes, MaxExpandedBytes  int64
	MaxFiles                               int
	TTL                                    time.Duration
	ManifestPath, SchemaPath, SchemaDigest string
}
type Objects struct {
	SigningKey []byte
	Policy     UploadPolicy
	store      Storage
	schema     *jsonschema.Schema
}

// NewObjects builds the local-filesystem Storage Provider configuration used by
// development and single-node instances. A COS-backed instance uses
// NewObjectsWithStorage with the same policy and signing key.
func NewObjects(root, publicURL string, key []byte, p UploadPolicy) (*Objects, error) {
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("explicit object root is required")
	}
	local, err := newLocalStorage(root, publicURL, key)
	if err != nil {
		return nil, err
	}
	return NewObjectsWithStorage(local, key, p)
}

// NewObjectsWithStorage binds an instance-approved Storage Provider to the
// Capability upload policy. The signing key authenticates local permits; it is
// required regardless of provider so the object data plane stays owner-signed.
func NewObjectsWithStorage(store Storage, key []byte, p UploadPolicy) (*Objects, error) {
	direct := false
	if d, ok := store.(interface{ DirectUploadOnly() bool }); ok {
		direct = d.DirectUploadOnly()
	}
	if store == nil || (!direct && len(key) < 32) || p.MaxBytes <= 0 || p.PartBytes <= 0 || p.PartBytes > p.MaxBytes || p.MaxExpandedBytes <= 0 || p.MaxFiles <= 0 || p.TTL <= 0 || !safePath(p.ManifestPath) || !digestRE.MatchString(p.SchemaDigest) {
		return nil, fmt.Errorf("storage provider, signing key and bounded upload/schema policy are required")
	}
	b, e := os.ReadFile(p.SchemaPath)
	if e != nil {
		return nil, e
	}
	if digest(b) != p.SchemaDigest {
		return nil, fmt.Errorf("package schema digest mismatch")
	}
	var raw any
	if e = json.Unmarshal(b, &raw); e != nil {
		return nil, e
	}
	compiler := publicjson.NewSchemaCompiler()
	if e = compiler.AddResource("package-schema.json", raw); e != nil {
		return nil, e
	}
	schema, e := compiler.Compile("package-schema.json")
	if e != nil {
		return nil, e
	}
	return &Objects{SigningKey: key, Policy: p, store: store, schema: schema}, nil
}

func safePath(v string) bool {
	return v != "" && !strings.Contains(v, "\\") && !strings.HasPrefix(v, "/") && path.Clean(v) == v && v != ".." && !strings.HasPrefix(v, "../") && !strings.Contains(v, ":")
}
func (o *Objects) validateArchive(filename string) ([]byte, error) {
	r, e := zip.OpenReader(filename)
	if e != nil {
		return nil, fmt.Errorf("package must be a valid ZIP archive")
	}
	defer r.Close()
	if len(r.File) > o.Policy.MaxFiles {
		return nil, fmt.Errorf("too many package entries")
	}
	var total uint64
	var manifest []byte
	seen := map[string]bool{}
	for _, f := range r.File {
		n := strings.TrimSuffix(f.Name, "/")
		if !safePath(n) || seen[n] || f.Mode()&os.ModeSymlink != 0 || (!f.FileInfo().IsDir() && !f.Mode().IsRegular()) {
			return nil, fmt.Errorf("unsafe package entry")
		}
		seen[n] = true
		total += f.UncompressedSize64
		if total > uint64(o.Policy.MaxExpandedBytes) {
			return nil, fmt.Errorf("expanded package exceeds policy")
		}
		if f.FileInfo().IsDir() {
			continue
		}
		reader, e := f.Open()
		if e != nil {
			return nil, e
		}
		limit := int64(f.UncompressedSize64) + 1
		var count int64
		if f.Name == o.Policy.ManifestPath {
			if f.UncompressedSize64 > 1<<20 {
				reader.Close()
				return nil, fmt.Errorf("manifest too large")
			}
			manifest, e = io.ReadAll(io.LimitReader(reader, limit))
			count = int64(len(manifest))
		} else {
			count, e = io.Copy(io.Discard, io.LimitReader(reader, limit))
		}
		reader.Close()
		if e != nil || uint64(count) != f.UncompressedSize64 {
			return nil, fmt.Errorf("package entry integrity failed")
		}
	}
	if manifest == nil {
		return nil, fmt.Errorf("approved manifest is missing")
	}
	var v any
	d := json.NewDecoder(bytes.NewReader(manifest))
	d.UseNumber()
	if e = d.Decode(&v); e != nil {
		return nil, fmt.Errorf("invalid manifest JSON")
	}
	if d.Decode(new(any)) != io.EOF {
		return nil, fmt.Errorf("manifest has trailing data")
	}
	if e = o.schema.Validate(v); e == nil {
		return manifest, nil
	}
	if e = validateOMACandidateTransport(r, manifest); e != nil {
		return nil, e
	}
	return manifest, nil
}

// validateOMACandidateTransport admits the native Foundry transport without
// rewriting its candidate bytes. The outer transport digest is the upload
// digest; candidate/content/manifest digests remain distinct provenance facts.
func validateOMACandidateTransport(r *zip.ReadCloser, transportManifest []byte) error {
	var envelope struct {
		SchemaVersion        int    `json:"schema_version"`
		SurfaceKind          string `json:"surface_kind"`
		TransportFormat      string `json:"transport_format"`
		CandidateRoot        string `json:"candidate_root"`
		CandidateFileCount   int    `json:"candidate_file_count"`
		CandidateTotalBytes  int64  `json:"candidate_total_bytes"`
		CandidateDigest      string `json:"candidate_digest"`
		CandidateIndexSHA256 string `json:"candidate_index_sha256"`
		ContentDigest        string `json:"content_digest"`
		ManifestDigest       string `json:"manifest_digest"`
		QualificationStatus  string `json:"qualification_status"`
		DomainQualityStatus  string `json:"domain_quality_status"`
		Admission            struct {
			Status string `json:"status"`
		} `json:"admission"`
	}
	if json.Unmarshal(transportManifest, &envelope) != nil || envelope.SchemaVersion != 1 ||
		envelope.SurfaceKind != "opl_foundry_candidate_transport.v1" || envelope.TransportFormat != "ZIP" ||
		envelope.CandidateRoot != "candidate" || envelope.CandidateFileCount <= 0 || envelope.CandidateTotalBytes <= 0 ||
		!digestRE.MatchString(envelope.CandidateDigest) || !digestRE.MatchString(envelope.CandidateIndexSHA256) ||
		!digestRE.MatchString(envelope.ContentDigest) || !digestRE.MatchString(envelope.ManifestDigest) ||
		envelope.QualificationStatus != "not_qualified" || envelope.DomainQualityStatus != "not_evaluated" ||
		envelope.Admission.Status != "development_transport_ready" {
		return fmt.Errorf("invalid OMA candidate transport manifest")
	}
	files := map[string][]byte{}
	for _, entry := range r.File {
		name := strings.TrimSuffix(entry.Name, "/")
		if name == "" || name == "manifest.json" || entry.FileInfo().IsDir() {
			continue
		}
		if !strings.HasPrefix(name, "candidate/") || strings.HasSuffix(name, "/") || strings.Contains(path.Base(name), "._") {
			return fmt.Errorf("OMA candidate transport contains non-candidate content")
		}
		f, err := entry.Open()
		if err != nil {
			return fmt.Errorf("OMA candidate entry unreadable")
		}
		b, err := io.ReadAll(io.LimitReader(f, int64(entry.UncompressedSize64)+1))
		f.Close()
		if err != nil || uint64(len(b)) != entry.UncompressedSize64 {
			return fmt.Errorf("OMA candidate entry integrity failed")
		}
		files[strings.TrimPrefix(name, "candidate/")] = b
	}
	index, ok := files["candidate-index.json"]
	if !ok {
		return fmt.Errorf("OMA candidate index is missing")
	}
	if digest(index) != envelope.CandidateIndexSHA256 {
		return fmt.Errorf("OMA candidate index digest mismatch")
	}
	agentManifest, ok := files["agent/agent-pack.json"]
	if !ok || digest(agentManifest) != envelope.ManifestDigest {
		return fmt.Errorf("OMA agent manifest digest mismatch")
	}
	var idx omaCandidateIndex
	if json.Unmarshal(index, &idx) != nil || idx.SurfaceKind != "opl_foundry_candidate_file_index" || idx.Version != "opl-foundry-candidate-index.v2" || !digestRE.MatchString(idx.BlueprintDigest) || len(idx.Files) != envelope.CandidateFileCount {
		return fmt.Errorf("invalid OMA candidate index")
	}
	if idx.CandidateDigest != envelope.CandidateDigest || omaCandidateDigest(idx) != envelope.CandidateDigest {
		return fmt.Errorf("OMA candidate identity digest mismatch")
	}
	seen := make(map[string]bool, len(idx.Files))
	var total int64
	h := sha256.New()
	for _, item := range idx.Files {
		b, ok := files[item.Path]
		if !ok || !safePath(item.Path) || item.Path == "candidate-index.json" || seen[item.Path] || !hex64.MatchString(item.SHA256) || int64(len(b)) != item.ByteSize || strings.TrimPrefix(digest(b), "sha256:") != item.SHA256 {
			return fmt.Errorf("OMA candidate file digest mismatch")
		}
		seen[item.Path] = true
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len([]byte(item.Path))))
		h.Write(n[:])
		h.Write([]byte(item.Path))
		binary.BigEndian.PutUint64(n[:], uint64(len(b)))
		h.Write(n[:])
		h.Write(b)
		total += int64(len(b))
	}
	if total != envelope.CandidateTotalBytes || "sha256:"+hex.EncodeToString(h.Sum(nil)) != envelope.ContentDigest {
		return fmt.Errorf("OMA candidate content digest mismatch")
	}
	if len(files) != envelope.CandidateFileCount+1 {
		return fmt.Errorf("OMA candidate file inventory mismatch")
	}
	return nil
}

// These fields follow Foundry's opl-foundry-candidate-index.v2 identity. The
// candidate_digest is the canonical JSON digest of the other four fields;
// candidate-index.json itself is not part of its file inventory.
type omaCandidateFile struct {
	Path     string `json:"path"`
	ByteSize int64  `json:"byte_size"`
	SHA256   string `json:"sha256"`
}
type omaCandidateIndex struct {
	SurfaceKind     string             `json:"surface_kind"`
	Version         string             `json:"version"`
	BlueprintDigest string             `json:"blueprint_digest"`
	CandidateDigest string             `json:"candidate_digest"`
	Files           []omaCandidateFile `json:"files"`
}

func omaCandidateDigest(index omaCandidateIndex) string {
	// Object keys use the upstream canonical order; array order is preserved.
	// JSON.stringify leaves Unicode and HTML characters unescaped.
	var out strings.Builder
	out.WriteString(`{"blueprint_digest":`)
	writeOMAJSONString(&out, index.BlueprintDigest)
	out.WriteString(`,"files":[`)
	for i, file := range index.Files {
		if i > 0 {
			out.WriteByte(',')
		}
		out.WriteString(`{"byte_size":`)
		out.WriteString(strconv.FormatInt(file.ByteSize, 10))
		out.WriteString(`,"path":`)
		writeOMAJSONString(&out, file.Path)
		out.WriteString(`,"sha256":`)
		writeOMAJSONString(&out, file.SHA256)
		out.WriteByte('}')
	}
	out.WriteString(`],"surface_kind":`)
	writeOMAJSONString(&out, index.SurfaceKind)
	out.WriteString(`,"version":`)
	writeOMAJSONString(&out, index.Version)
	out.WriteByte('}')
	return digest([]byte(out.String()))
}

func writeOMAJSONString(out *strings.Builder, value string) {
	out.WriteByte('"')
	for _, char := range value {
		switch char {
		case '"', '\\':
			out.WriteByte('\\')
			out.WriteRune(char)
		case '\b':
			out.WriteString(`\b`)
		case '\f':
			out.WriteString(`\f`)
		case '\n':
			out.WriteString(`\n`)
		case '\r':
			out.WriteString(`\r`)
		case '\t':
			out.WriteString(`\t`)
		default:
			if char < 0x20 {
				fmt.Fprintf(out, `\u%04x`, char)
			} else {
				out.WriteRune(char)
			}
		}
	}
	out.WriteByte('"')
}

type partPermit struct {
	Upload  string `json:"upload"`
	Part    int32  `json:"part"`
	Size    int64  `json:"size"`
	Digest  string `json:"digest"`
	Expires int64  `json:"expires"`
}

func permit(key []byte, p partPermit) string {
	b, _ := json.Marshal(p)
	v := base64.RawURLEncoding.EncodeToString(b)
	h := hmac.New(sha256.New, key)
	h.Write([]byte(v))
	return v + "." + base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

func readPermitWithKey(key []byte, v string) (partPermit, error) {
	var p partPermit
	v1, v2, ok := strings.Cut(v, ".")
	if !ok {
		return p, fmt.Errorf("invalid permit")
	}
	sig, e := base64.RawURLEncoding.DecodeString(v2)
	if e != nil {
		return p, e
	}
	h := hmac.New(sha256.New, key)
	h.Write([]byte(v1))
	if !hmac.Equal(sig, h.Sum(nil)) {
		return p, fmt.Errorf("invalid permit")
	}
	b, e := base64.RawURLEncoding.DecodeString(v1)
	if e != nil {
		return p, e
	}
	if e = json.Unmarshal(b, &p); e != nil {
		return p, e
	}
	if p.Expires <= time.Now().Unix() || !nameRE.MatchString(p.Upload) || p.Part <= 0 || p.Size <= 0 || !digestRE.MatchString(p.Digest) {
		return p, fmt.Errorf("expired or invalid permit")
	}
	return p, nil
}

// UploadHandler is a restricted signed PUT data plane; it cannot list or read
// objects. It is only used by storage providers whose upload URL points back at
// Capability (local filesystem). Direct-to-storage providers are written by the
// browser against their own scoped presigned URL.
func (s *Service) UploadHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// This endpoint is authorized solely by its signed, bounded permit, not
		// cookies. Browser uploads omit credentials; CORS grants no object reads.
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Expose-Headers", "ETag")
		if r.Method == http.MethodOptions {
			if _, err := readPermitWithKey(s.Objects.SigningKey, r.URL.Query().Get("permit")); err != nil {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			w.Header().Set("Access-Control-Allow-Methods", "PUT")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-OPL-SHA256")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != "PUT" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		p, e := readPermitWithKey(s.Objects.SigningKey, r.URL.Query().Get("permit"))
		if e != nil || r.Header.Get("X-OPL-SHA256") != p.Digest {
			http.Error(w, "invalid upload authorization", http.StatusForbidden)
			return
		}
		if p.Size > s.Objects.Policy.PartBytes {
			http.Error(w, "part too large", http.StatusRequestEntityTooLarge)
			return
		}
		ctx := r.Context()
		tx, e := s.DB.BeginTx(ctx, nil)
		if e != nil {
			w.WriteHeader(503)
			return
		}
		defer tx.Rollback()
		var size int64
		var sha, st string
		var expires time.Time
		e = tx.QueryRowContext(ctx, `SELECT c.size_bytes,c.sha256,u.status,u.expires_at FROM capability.upload_chunks c JOIN capability.upload_sessions u ON u.id=c.upload_session_id WHERE u.id=$1 AND c.part_number=$2 FOR UPDATE OF c,u`, p.Upload, p.Part).Scan(&size, &sha, &st, &expires)
		if e != nil || size != p.Size || sha != p.Digest || st != "uploading" || !expires.After(time.Now()) {
			http.Error(w, "upload unavailable", 409)
			return
		}
		etag, e := s.Objects.store.PutPart(ctx, p.Upload, int(p.Part), http.MaxBytesReader(w, r.Body, p.Size+1), p.Size, p.Digest)
		if e == ErrObjectIntegrity {
			http.Error(w, "part digest or size mismatch", 400)
			return
		}
		if e != nil {
			w.WriteHeader(503)
			return
		}
		if _, e = tx.ExecContext(ctx, `UPDATE capability.upload_chunks SET observation_result='confirmed',etag=$1 WHERE upload_session_id=$2 AND part_number=$3`, etag, p.Upload, p.Part); e != nil {
			w.WriteHeader(503)
			return
		}
		if e = tx.Commit(); e != nil {
			w.WriteHeader(503)
			return
		}
		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusNoContent)
	})
}
