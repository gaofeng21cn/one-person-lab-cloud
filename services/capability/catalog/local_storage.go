package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// localStorage is the single-node/filesystem Storage Provider. It preserves the
// pre-abstract data plane: owner-signed single-use permits, part files under
// parts/, immutable content-addressed objects under objects/, and hard links for
// idempotent immutable writes.
type localStorage struct {
	root, publicURL string
	signingKey      []byte
}

func newLocalStorage(root, publicURL string, signingKey []byte) (*localStorage, error) {
	u, e := url.Parse(publicURL)
	if e != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, fmt.Errorf("invalid public upload URL")
	}
	if u.Scheme == "http" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && u.Hostname() != "::1" {
		return nil, fmt.Errorf("unencrypted object endpoint is only allowed on loopback")
	}
	for _, dir := range []string{"objects", "parts", "pending"} {
		if e = os.MkdirAll(filepath.Join(root, dir), 0700); e != nil {
			return nil, e
		}
	}
	// Part permits are signed by this provider and verified by the Capability data
	// plane from the same Objects.SigningKey, so the provider instance must retain
	// that exact key; signing with an empty key would make every part upload fail
	// its own authorization check.
	return &localStorage{root: root, publicURL: strings.TrimRight(publicURL, "/"), signingKey: signingKey}, nil
}

func (l *localStorage) objectPath(d string) (string, error) {
	if !digestRE.MatchString(d) {
		return "", fmt.Errorf("invalid object digest")
	}
	return filepath.Join(l.root, "objects", strings.TrimPrefix(d, "sha256:")), nil
}

func (l *localStorage) partPath(upload string, part int) string {
	return filepath.Join(l.root, "parts", upload, strconv.Itoa(part))
}

func (l *localStorage) BeginUpload(ctx context.Context, upload, digest string, size int64) (string, error) {
	return "", nil
}

func (l *localStorage) DeleteUpload(ctx context.Context, upload, providerUploadRef string) error {
	return os.RemoveAll(filepath.Join(l.root, "parts", upload))
}

func (l *localStorage) Ready(ctx context.Context) error {
	f, e := os.CreateTemp(filepath.Join(l.root, "pending"), "ready-")
	if e != nil {
		return e
	}
	name := f.Name()
	e = f.Close()
	os.Remove(name)
	return e
}

func (l *localStorage) AuthorizePart(ctx context.Context, upload, providerUploadRef string, part int, size int64, digest string, expires time.Time) (UploadAuthorization, error) {
	return UploadAuthorization{
		URL:           l.publicURL + "/parts?permit=" + permit(l.signingKey, partPermit{Upload: upload, Part: int32(part), Size: size, Digest: digest, Expires: expires.Unix()}),
		ContentType:   "application/octet-stream",
		ChecksumName:  "X-OPL-SHA256",
		ChecksumValue: digest,
	}, nil
}

// PutPart writes one verified part after the data plane validates its permit
// and the owner DB confirms the part identity.
func (l *localStorage) PutPart(ctx context.Context, upload string, part int, body io.Reader, size int64, digest string) (string, error) {
	dir := filepath.Join(l.root, "parts", upload)
	if e := os.MkdirAll(dir, 0700); e != nil {
		return "", ErrStorageUnavailable
	}
	f, e := os.CreateTemp(filepath.Join(l.root, "pending"), "part-")
	if e != nil {
		return "", ErrStorageUnavailable
	}
	defer os.Remove(f.Name())
	h := sha256.New()
	n, e := io.Copy(io.MultiWriter(f, h), io.LimitReader(body, size+1))
	closeErr := f.Close()
	if e != nil || closeErr != nil || n != size || "sha256:"+hex.EncodeToString(h.Sum(nil)) != digest {
		return "", ErrObjectIntegrity
	}
	if e = os.Rename(f.Name(), l.partPath(upload, part)); e != nil {
		return "", ErrStorageUnavailable
	}
	return strings.TrimPrefix(digest, "sha256:"), nil
}

// ListParts reports the part files the local provider holds. The local provider
// has no separate provider etag: its etag is the registered part digest, so it
// leaves Etag empty and the reconciler reuses the registered identity.
func (l *localStorage) ListParts(ctx context.Context, upload, providerUploadRef, digest string) (ProviderParts, error) {
	out := ProviderParts{Parts: map[int32]ProviderPart{}}
	entries, e := os.ReadDir(filepath.Join(l.root, "parts", upload))
	if e != nil {
		if os.IsNotExist(e) {
			return out, nil
		}
		return ProviderParts{}, ErrStorageUnavailable
	}
	for _, entry := range entries {
		number, e := strconv.Atoi(entry.Name())
		if e != nil || number <= 0 {
			continue
		}
		info, e := entry.Info()
		if e != nil {
			return ProviderParts{}, ErrStorageUnavailable
		}
		if !info.Mode().IsRegular() {
			continue
		}
		out.Parts[int32(number)] = ProviderPart{SizeBytes: info.Size()}
	}
	return out, nil
}

func (l *localStorage) Assemble(ctx context.Context, upload, providerUploadRef, digest string, size int64, parts []ConfirmedPart) (*AssembledObject, error) {
	f, e := os.CreateTemp(filepath.Join(l.root, "pending"), "complete-")
	if e != nil {
		return nil, ErrStorageUnavailable
	}
	h := sha256.New()
	var total int64
	for _, p := range parts {
		part, e := os.Open(l.partPath(upload, int(p.PartNumber)))
		if e != nil {
			f.Close()
			os.Remove(f.Name())
			return nil, ErrStorageUnavailable
		}
		n, e := io.Copy(io.MultiWriter(f, h), io.LimitReader(part, p.SizeBytes+1))
		part.Close()
		if e != nil || n != p.SizeBytes {
			f.Close()
			os.Remove(f.Name())
			return nil, ErrObjectIntegrity
		}
		total += n
	}
	if e = f.Close(); e != nil {
		os.Remove(f.Name())
		return nil, ErrStorageUnavailable
	}
	if total != size || "sha256:"+hex.EncodeToString(h.Sum(nil)) != digest {
		os.Remove(f.Name())
		return nil, ErrDigestMismatch
	}
	return &AssembledObject{File: f}, nil
}

func (l *localStorage) Promote(ctx context.Context, upload, digest string, assembled *AssembledObject) error {
	return l.putImmutable(assembled.File.Name(), digest)
}

func (l *localStorage) putImmutable(source, d string) error {
	target, e := l.objectPath(d)
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
			return ErrObjectIntegrity
		}
		return nil
	}
	if e == nil {
		e = os.Chmod(target, 0400)
	}
	return e
}

func (l *localStorage) OpenImmutable(ctx context.Context, digest string) (*ImmutableObject, error) {
	filename, e := l.objectPath(digest)
	if e != nil {
		return nil, e
	}
	f, e := os.Open(filename)
	if e != nil {
		return nil, ErrStorageUnavailable
	}
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, ErrObjectIntegrity
	}
	return &ImmutableObject{Body: f, Size: info.Size(), Filename: "package.zip"}, nil
}
