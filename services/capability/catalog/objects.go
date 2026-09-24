package catalog

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"opl-cloud/packages/contracts/go/publisherjson"
)

// UploadPolicy is explicit instance configuration, not customer-controlled input.
type UploadPolicy struct {
	MaxBytes, PartBytes, MaxExpandedBytes  int64
	MaxFiles                               int
	TTL                                    time.Duration
	ManifestPath, SchemaPath, SchemaDigest string
}
type Objects struct {
	Root, PublicURL string
	SigningKey      []byte
	Policy          UploadPolicy
	schema          *jsonschema.Schema
}

func NewObjects(root, publicURL string, key []byte, p UploadPolicy) (*Objects, error) {
	if !filepath.IsAbs(root) || len(key) < 32 || p.MaxBytes <= 0 || p.PartBytes <= 0 || p.PartBytes > p.MaxBytes || p.MaxExpandedBytes <= 0 || p.MaxFiles <= 0 || p.TTL <= 0 || !safePath(p.ManifestPath) || !digestRE.MatchString(p.SchemaDigest) {
		return nil, fmt.Errorf("explicit object root, upload URL, signing key and bounded upload/schema policy are required")
	}
	u, e := url.Parse(publicURL)
	if e != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, fmt.Errorf("invalid public upload URL")
	}
	if u.Scheme == "http" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && u.Hostname() != "::1" {
		return nil, fmt.Errorf("unencrypted object endpoint is only allowed on loopback")
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
	compiler := publisherjson.NewSchemaCompiler()
	if e = compiler.AddResource("package-schema.json", raw); e != nil {
		return nil, e
	}
	schema, e := compiler.Compile("package-schema.json")
	if e != nil {
		return nil, e
	}
	for _, dir := range []string{"objects", "parts", "pending"} {
		if e = os.MkdirAll(filepath.Join(root, dir), 0700); e != nil {
			return nil, e
		}
	}
	return &Objects{Root: root, PublicURL: strings.TrimRight(publicURL, "/"), SigningKey: key, Policy: p, schema: schema}, nil
}
func safePath(v string) bool {
	return v != "" && !strings.Contains(v, "\\") && !strings.HasPrefix(v, "/") && path.Clean(v) == v && v != ".." && !strings.HasPrefix(v, "../") && !strings.Contains(v, ":")
}
func (o *Objects) objectPath(d string) (string, error) {
	if !digestRE.MatchString(d) {
		return "", fmt.Errorf("invalid object digest")
	}
	return filepath.Join(o.Root, "objects", strings.TrimPrefix(d, "sha256:")), nil
}
func (o *Objects) putImmutable(source, d string) error {
	target, e := o.objectPath(d)
	if e != nil {
		return e
	}
	e = os.Link(source, target)
	if os.IsExist(e) {
		f, x := os.Open(target)
		if x != nil {
			return x
		}
		defer f.Close()
		h := sha256.New()
		if _, x = io.Copy(h, f); x != nil {
			return x
		}
		if "sha256:"+hex.EncodeToString(h.Sum(nil)) != d {
			return fmt.Errorf("immutable object integrity failure")
		}
		return nil
	}
	if e == nil {
		e = os.Chmod(target, 0400)
	}
	return e
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
	if e = o.schema.Validate(v); e != nil {
		return nil, fmt.Errorf("package manifest does not match approved schema")
	}
	return manifest, nil
}

type partPermit struct {
	Upload  string `json:"upload"`
	Part    int32  `json:"part"`
	Size    int64  `json:"size"`
	Digest  string `json:"digest"`
	Expires int64  `json:"expires"`
}

func (o *Objects) permit(p partPermit) string {
	b, _ := json.Marshal(p)
	v := base64.RawURLEncoding.EncodeToString(b)
	h := hmac.New(sha256.New, o.SigningKey)
	h.Write([]byte(v))
	return v + "." + base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}
func (o *Objects) readPermit(v string) (partPermit, error) {
	var p partPermit
	v1, v2, ok := strings.Cut(v, ".")
	if !ok {
		return p, fmt.Errorf("invalid permit")
	}
	sig, e := base64.RawURLEncoding.DecodeString(v2)
	if e != nil {
		return p, e
	}
	h := hmac.New(sha256.New, o.SigningKey)
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

// UploadHandler is a restricted signed PUT data plane; it cannot list or read objects.
func (s *Service) UploadHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		p, e := s.Objects.readPermit(r.URL.Query().Get("permit"))
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
		f, e := os.CreateTemp(filepath.Join(s.Objects.Root, "pending"), "part-")
		if e != nil {
			w.WriteHeader(503)
			return
		}
		defer os.Remove(f.Name())
		h := sha256.New()
		n, e := io.Copy(io.MultiWriter(f, h), http.MaxBytesReader(w, r.Body, p.Size+1))
		closeErr := f.Close()
		if e != nil || closeErr != nil || n != p.Size || "sha256:"+hex.EncodeToString(h.Sum(nil)) != p.Digest {
			http.Error(w, "part digest or size mismatch", 400)
			return
		}
		dir := filepath.Join(s.Objects.Root, "parts", p.Upload)
		if e = os.MkdirAll(dir, 0700); e != nil {
			w.WriteHeader(503)
			return
		}
		target := filepath.Join(dir, strconv.Itoa(int(p.Part)))
		if e = os.Rename(f.Name(), target); e != nil {
			w.WriteHeader(503)
			return
		}
		etag := strings.TrimPrefix(p.Digest, "sha256:")
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
func (o *Objects) Ready(ctx context.Context) error {
	f, e := os.CreateTemp(filepath.Join(o.Root, "pending"), "ready-")
	if e != nil {
		return e
	}
	name := f.Name()
	e = f.Close()
	os.Remove(name)
	return e
}
