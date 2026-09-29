package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/tencentyun/cos-go-sdk-v5"
)

// COSConfig is the instance-approved Tencent COS Storage Provider binding. The
// secret pair is a bucket-scoped sub-account credential held only in the
// instance Secret store; it is never sent to a browser. Browser uploads use
// short-lived presigned URLs derived from it.
type COSConfig struct {
	Bucket, Region, SecretID, SecretKey, Endpoint string
}

// cosStorage stores Capability package bytes in Tencent COS. Immutable objects
// are content-addressed by SHA-256; staging parts live under the upload id.
// Configuration is validated here so a misconfigured instance fails at start
// rather than on the first customer upload.
type cosStorage struct {
	client *cos.Client
	cfg    COSConfig
}

func newCOSStorage(cfg COSConfig) (*cosStorage, error) {
	if cfg.Bucket == "" || cfg.Region == "" || cfg.SecretID == "" || cfg.SecretKey == "" {
		return nil, fmt.Errorf("COS bucket, region, secret id and secret key are required")
	}
	bucketURL, err := cos.NewBucketURL(cfg.Bucket, cfg.Region, true)
	if err != nil {
		return nil, fmt.Errorf("invalid COS bucket or region: %w", err)
	}
	if cfg.Endpoint != "" {
		u, err := url.Parse(cfg.Endpoint)
		if err != nil || u.Host == "" || u.Scheme != "https" {
			return nil, fmt.Errorf("COS endpoint must be an https URL")
		}
		bucketURL = u
	}
	client := cos.NewClient(&cos.BaseURL{BucketURL: bucketURL}, &http.Client{
		Timeout: 15 * time.Minute,
		Transport: &cos.AuthorizationTransport{
			SecretID:  cfg.SecretID,
			SecretKey: cfg.SecretKey,
		},
	})
	return &cosStorage{client: client, cfg: cfg}, nil
}

// NewCOSObjects binds the Tencent COS Storage Provider to the Capability upload
// policy. It validates the binding and the bucket on start so a misconfigured
// instance fails before the first customer upload.
func NewCOSObjects(ctx context.Context, cfg COSConfig, p UploadPolicy) (*Objects, error) {
	store, err := newCOSStorage(cfg)
	if err != nil {
		return nil, err
	}
	if err := store.Ready(ctx); err != nil {
		return nil, err
	}
	return NewObjectsWithStorage(store, nil, p)
}

// DirectUploadOnly marks this provider as writing parts from the browser
// straight to COS; Capability never proxies part bytes.
func (c *cosStorage) DirectUploadOnly() bool { return true }

func stagingKey(upload string) string { return "staging/" + upload }

func immutableKey(digest string) (string, error) {
	if !digestRE.MatchString(digest) {
		return "", fmt.Errorf("invalid object digest")
	}
	return "objects/sha256/" + strings.TrimPrefix(digest, "sha256:"), nil
}

func (c *cosStorage) Ready(ctx context.Context) error {
	// A reachable, authorized bucket answers a HEAD on an absent probe key with
	// 404. Authentication failures and network errors must fail startup.
	_, err := c.client.Object.Head(ctx, "probe/ready", nil)
	if err == nil {
		return nil
	}
	var cosErr *cos.ErrorResponse
	if errors.As(err, &cosErr) && cosErr.Response != nil && cosErr.Response.StatusCode == http.StatusNotFound {
		return nil
	}
	return ErrStorageUnavailable
}

// objectExists reports whether the bucket holds the key. An unreadable provider
// is not absence: the caller must fail closed rather than treat it as a missing
// object.
func (c *cosStorage) objectExists(ctx context.Context, key string) bool {
	_, err := c.client.Object.Head(ctx, key, nil)
	return err == nil
}

func (c *cosStorage) BeginUpload(ctx context.Context, upload, digest string, size int64) (string, error) {
	res, _, err := c.client.Object.InitiateMultipartUpload(ctx, stagingKey(upload), nil)
	if err != nil || res == nil || res.UploadID == "" {
		return "", ErrStorageUnavailable
	}
	return res.UploadID, nil
}

func (c *cosStorage) AuthorizePart(ctx context.Context, upload, providerUploadRef string, part int, size int64, digest string, expires time.Time) (UploadAuthorization, error) {
	if providerUploadRef == "" {
		return UploadAuthorization{}, ErrStorageUnavailable
	}
	ttl := time.Until(expires)
	if ttl <= 0 {
		return UploadAuthorization{}, ErrStorageUnavailable
	}
	query := url.Values{}
	query.Set("partNumber", strconv.Itoa(part))
	query.Set("uploadId", providerUploadRef)
	signed, err := c.client.Object.GetPresignedURL(ctx, http.MethodPut, stagingKey(upload), c.cfg.SecretID, c.cfg.SecretKey, ttl, &cos.PresignedURLOptions{Query: &query})
	if err != nil {
		return UploadAuthorization{}, ErrStorageUnavailable
	}
	return UploadAuthorization{URL: signed.String(), ContentType: "application/octet-stream", ChecksumName: "X-OPL-SHA256", ChecksumValue: digest}, nil
}

// PutPart is never used: COS parts are written by the browser through the
// presigned URL. The local data-plane route is not registered for this provider.
func (c *cosStorage) PutPart(ctx context.Context, upload string, part int, body io.Reader, size int64, digest string) (string, error) {
	return "", ErrStorageUnavailable
}

// ListParts reports the provider's current view of one upload's shards. A
// multipart still open reports its parts; a multipart that is gone means the
// upload was finalized, so every registered shard is present and the owner
// finalizes through CompleteUpload instead of expecting further parts.
func (c *cosStorage) ListParts(ctx context.Context, upload, providerUploadRef, digest string) (ProviderParts, error) {
	if providerUploadRef == "" {
		return ProviderParts{}, ErrStorageUnavailable
	}
	key := stagingKey(upload)
	remote, err := c.listParts(ctx, key, providerUploadRef)
	if err != nil {
		target, keyErr := immutableKey(digest)
		if keyErr != nil {
			return ProviderParts{}, ErrStorageUnavailable
		}
		// The multipart is gone: the assembled staging object or the copied
		// content-addressed immutable object must still be present, otherwise
		// the provider has lost the upload and the readback fails closed.
		if !c.objectExists(ctx, key) && !c.objectExists(ctx, target) {
			return ProviderParts{}, ErrStorageUnavailable
		}
		return ProviderParts{Complete: true}, nil
	}
	out := ProviderParts{Parts: make(map[int32]ProviderPart, len(remote))}
	for number, part := range remote {
		out.Parts[number] = ProviderPart{SizeBytes: part.Size, Etag: part.ETag}
	}
	return out, nil
}

func (c *cosStorage) Assemble(ctx context.Context, upload, providerUploadRef, digest string, size int64, parts []ConfirmedPart) (*AssembledObject, error) {
	key := stagingKey(upload)
	target, err := immutableKey(digest)
	if err != nil {
		return nil, err
	}
	remote, err := c.listParts(ctx, key, providerUploadRef)
	if err != nil {
		// The multipart is gone: the upload was finalized, either by a repeated
		// Complete after a lost response or by an earlier Complete whose owner
		// transaction did not commit. When the admitted bytes were already
		// copied to their content-addressed immutable key, recovery reads them
		// back and re-verifies the exact size and SHA-256 rather than failing
		// the retry or starting a second upload identity. Otherwise the
		// finalized staging object must still exist and the same verification
		// runs below.
		if c.objectExists(ctx, target) {
			return c.downloadVerified(ctx, target, digest, size)
		}
		if !c.objectExists(ctx, key) {
			return nil, ErrStorageUnavailable
		}
	} else {
		complete := make([]cos.Object, 0, len(parts))
		for _, p := range parts {
			found, ok := remote[p.PartNumber]
			if !ok || found.Size != p.SizeBytes {
				return nil, ErrObjectIntegrity
			}
			complete = append(complete, cos.Object{PartNumber: int(p.PartNumber), ETag: found.ETag})
		}
		if _, _, err := c.client.Object.CompleteMultipartUpload(ctx, key, providerUploadRef, &cos.CompleteMultipartUploadOptions{Parts: complete}); err != nil {
			return nil, ErrStorageUnavailable
		}
	}
	assembled, err := c.downloadVerified(ctx, key, digest, size)
	if err != nil {
		if err == ErrDigestMismatch {
			c.client.Object.Delete(ctx, key)
		}
		return nil, err
	}
	return assembled, nil
}

func (c *cosStorage) Promote(ctx context.Context, upload, digest string, assembled *AssembledObject) error {
	target, err := immutableKey(digest)
	if err != nil {
		return err
	}
	if _, _, err := c.client.Object.Copy(ctx, target, c.cfg.Bucket+"/"+stagingKey(upload), nil); err != nil {
		return ErrStorageUnavailable
	}
	return nil
}

// listParts returns every uploaded part keyed by number, following pagination.
func (c *cosStorage) listParts(ctx context.Context, key, uploadID string) (map[int32]cos.Object, error) {
	out := map[int32]cos.Object{}
	marker := ""
	for {
		res, _, err := c.client.Object.ListParts(ctx, key, uploadID, &cos.ObjectListPartsOptions{PartNumberMarker: marker})
		if err != nil || res == nil {
			return nil, ErrStorageUnavailable
		}
		for _, p := range res.Parts {
			out[int32(p.PartNumber)] = p
		}
		if !res.IsTruncated || res.NextPartNumberMarker == "" {
			return out, nil
		}
		marker = res.NextPartNumberMarker
	}
}

// downloadVerified streams the stored object to a local temp file and verifies
// its exact size and SHA-256 against the admitted facts. The temp file is the
// copy handed to archive policy validation.
func (c *cosStorage) downloadVerified(ctx context.Context, key, digest string, size int64) (*AssembledObject, error) {
	resp, err := c.client.Object.Get(ctx, key, nil)
	if err != nil {
		return nil, ErrStorageUnavailable
	}
	defer resp.Body.Close()
	f, err := os.CreateTemp("", "opl-cos-object-")
	if err != nil {
		return nil, ErrStorageUnavailable
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, size+1))
	if err != nil {
		f.Close()
		os.Remove(f.Name())
		return nil, ErrStorageUnavailable
	}
	if n != size || "sha256:"+hex.EncodeToString(h.Sum(nil)) != digest {
		f.Close()
		os.Remove(f.Name())
		return nil, ErrDigestMismatch
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		f.Close()
		os.Remove(f.Name())
		return nil, ErrStorageUnavailable
	}
	return &AssembledObject{File: f}, nil
}

func (c *cosStorage) OpenImmutable(ctx context.Context, digest string) (*ImmutableObject, error) {
	key, err := immutableKey(digest)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Object.Get(ctx, key, nil)
	if err != nil {
		return nil, ErrStorageUnavailable
	}
	return &ImmutableObject{Body: resp.Body, Size: resp.ContentLength, Filename: "package.zip"}, nil
}

func (c *cosStorage) DeleteUpload(ctx context.Context, upload, providerUploadRef string) error {
	key := stagingKey(upload)
	if providerUploadRef != "" {
		c.client.Object.AbortMultipartUpload(ctx, key, providerUploadRef)
	}
	c.client.Object.Delete(ctx, key)
	return nil
}
