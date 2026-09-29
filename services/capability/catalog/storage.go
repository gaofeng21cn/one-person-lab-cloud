package catalog

import (
	"context"
	"errors"
	"io"
	"os"
	"time"
)

// Storage is the Capability byte-plane seam between the Package metadata owner
// and the instance-approved Storage Provider. Capability still owns upload
// sessions, signed-part authorization, archive policy and admission; the
// provider only stores and returns exact bytes.
//
// Two implementations exist: a single-node filesystem provider that receives
// bytes through Capability's restricted data plane, and a Tencent COS provider
// that hands the browser a scoped presigned URL so bytes never transit
// Capability. Both persist provider_upload_ref / provider_part_ref and both
// verify the assembled object's exact size and digest before it becomes
// immutable.
type Storage interface {
	// Ready verifies the provider is reachable and writable at process start.
	Ready(ctx context.Context) error
	// BeginUpload opens a provider upload session for one Capability upload id
	// and returns the provider upload reference to persist in
	// capability.upload_sessions.provider_upload_ref. Providers that stream
	// through Capability may return an empty reference.
	BeginUpload(ctx context.Context, upload, digest string, size int64) (providerUploadRef string, err error)
	// AuthorizePart issues exactly one scoped write authorization binding the
	// upload, part number, size and digest. The URL is short-lived and cannot
	// read or address another object.
	AuthorizePart(ctx context.Context, upload, providerUploadRef string, part int, size int64, digest string, expires time.Time) (UploadAuthorization, error)
	// PutPart stores one part for the restricted local data plane. Direct-to-
	// storage providers are written by the browser and return an error here.
	PutPart(ctx context.Context, upload string, part int, body io.Reader, size int64, digest string) (etag string, err error)
	// ListParts reports the provider's current view of one upload's shards, so
	// GetUpload can reconcile the owner's readback with the real storage state
	// and a resumed upload only re-sends the shards the provider does not hold.
	// It never verifies a customer-declared digest; the assembled object is
	// still verified end to end in Assemble.
	ListParts(ctx context.Context, upload, providerUploadRef, digest string) (ProviderParts, error)
	// Assemble finalizes the provider upload, verifies the assembled size and
	// digest against the admitted facts, and stores the immutable object keyed
	// by digest. It returns a readable copy of the exact admitted bytes for
	// archive policy validation. The immutable object already exists on success.
	Assemble(ctx context.Context, upload, providerUploadRef, digest string, size int64, parts []ConfirmedPart) (*AssembledObject, error)
	// OpenImmutable streams the stored immutable object keyed by digest.
	OpenImmutable(ctx context.Context, digest string) (*ImmutableObject, error)
	// DeleteUpload removes provider upload artifacts for a cancelled or expired
	// Capability upload. It never touches confirmed immutable objects.
	DeleteUpload(ctx context.Context, upload, providerUploadRef string) error
}

// ConfirmedPart is one owner-readback part fact from capability.upload_chunks.
// Confirmed records whether Capability's own data plane observed the part; a
// direct-to-storage part stays unconfirmed until the provider validates it
// during assembly.
type ConfirmedPart struct {
	PartNumber int32
	SizeBytes  int64
	Sha256     string
	Etag       string
	Confirmed  bool
}

// ProviderParts is the provider's current view of one Capability upload.
//
// Complete is true when every shard is already present because the provider
// upload was finalized: the multipart was completed, or the assembled staging
// object (or content-addressed immutable object) exists. Parts is then empty,
// because individual shard identities are no longer addressable, and the owner
// finalizes through CompleteUpload instead of expecting further parts.
type ProviderParts struct {
	Complete bool
	Parts    map[int32]ProviderPart
}

// ProviderPart is one shard the provider currently holds. SizeBytes and Etag
// are the provider's own authority; a shard whose size differs from the
// registered part identity is not the shard the owner admitted.
type ProviderPart struct {
	SizeBytes int64
	Etag      string
}

// UploadAuthorization is one short-lived scoped write permit returned to the
// browser. ChecksumName/Value carry the provider integrity requirement; both are
// empty when the provider cannot validate a client-declared digest.
type UploadAuthorization struct {
	URL           string
	ContentType   string
	ChecksumName  string
	ChecksumValue string
}

// AssembledObject is the verified immutable bytes plus a validation temp file.
type AssembledObject struct {
	File *os.File
}

// Cleanup releases the validation temp file; the immutable object remains.
func (a *AssembledObject) Cleanup() {
	if a != nil && a.File != nil {
		name := a.File.Name()
		a.File.Close()
		os.Remove(name)
	}
}

// ImmutableObject is an open stream over stored immutable bytes.
type ImmutableObject struct {
	Body     io.ReadCloser
	Size     int64
	Filename string
}

func (o *ImmutableObject) Close() {
	if o != nil && o.Body != nil {
		o.Body.Close()
	}
}

var (
	// ErrDigestMismatch means the assembled bytes did not match the admitted
	// size/digest. It is a customer-facing validation failure, not an outage.
	ErrDigestMismatch = errors.New("assembled object digest mismatch")
	// ErrStorageUnavailable means the provider could not be read or written.
	ErrStorageUnavailable = errors.New("storage provider unavailable")
	// ErrObjectIntegrity means a part or object failed size/integrity checks.
	ErrObjectIntegrity = errors.New("object integrity failed")
)
