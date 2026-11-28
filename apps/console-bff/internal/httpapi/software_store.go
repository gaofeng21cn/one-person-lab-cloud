package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	softwarePackagesEnv         = "OPL_SOFTWARE_PACKAGES_JSON"
	softwareRegistryUsernameEnv = "OPL_SOFTWARE_REGISTRY_USERNAME"
	softwareRegistryTokenEnv    = "OPL_SOFTWARE_REGISTRY_TOKEN"
	softwareCacheRootEnv        = "OPL_SOFTWARE_CACHE_ROOT"

	softwareDefaultRegistryHost = "ghcr.io"
	softwareMaxCatalogBytes     = 256 << 10
	softwareMaxBlobBytes        = 2 << 30
	softwareMaxChallengeBytes   = 64 << 10
)

var (
	softwarePackageIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	softwareSHA256Pattern    = regexp.MustCompile(`^[a-f0-9]{64}$`)
	bearerParamPattern       = regexp.MustCompile(`([a-zA-Z0-9_]+)="([^"]*)"`)

	errSoftwareCatalogInvalid     = errors.New("software package catalog configuration is invalid")
	errSoftwareCacheUnconfigured  = errors.New("software cache directory is not configured")
	errSoftwareUpstreamFailed     = errors.New("software registry upstream request failed")
	errSoftwareDigestMismatch     = errors.New("software archive digest verification failed")
	errSoftwareSourceUnconfigured = errors.New("software package download source is not configured")
)

type softwareCatalogEntry struct {
	PackageID    string
	DisplayName  string
	Summary      string
	Version      string
	SHA256       string
	SizeBytes    int64
	Filename     string
	RegistryHost string
	Repository   string
	BlobDigest   string
	BlobURL      string
}

type rawSoftwareCatalogEntry struct {
	PackageID    string `json:"packageId"`
	DisplayName  string `json:"displayName"`
	Summary      string `json:"summary"`
	Version      string `json:"version"`
	SHA256       string `json:"sha256"`
	SizeBytes    int64  `json:"sizeBytes"`
	Filename     string `json:"filename"`
	RegistryHost string `json:"registryHost"`
	Repository   string `json:"repository"`
	BlobDigest   string `json:"blobDigest"`
	BlobURL      string `json:"blobUrl"`
}

func (e softwareCatalogEntry) publicMetadata() map[string]any {
	return map[string]any{
		"packageId":   e.PackageID,
		"displayName": e.DisplayName,
		"summary":     e.Summary,
		"version":     e.Version,
		"sha256":      e.SHA256,
		"sizeBytes":   e.SizeBytes,
		"filename":    e.Filename,
	}
}

type softwareBlobStore struct {
	mu      sync.Mutex
	flights map[string]*softwareFetchFlight
}

type softwareFetchFlight struct {
	done chan struct{}
	path string
	err  error
}

func validSoftwarePackageID(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed != raw || !softwarePackageIDPattern.MatchString(trimmed) {
		return "", false
	}
	return trimmed, true
}

func sanitizeSoftwareFilename(raw, packageID, version string) (string, bool) {
	name := strings.TrimSpace(raw)
	if name == "" {
		safeVer := strings.Map(func(r rune) rune {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
				return r
			default:
				return '-'
			}
		}, version)
		safeVer = strings.Trim(safeVer, ".-_")
		if safeVer == "" {
			safeVer = "release"
		}
		name = packageID + "-" + safeVer + ".tar.gz"
	}
	if len(name) > 128 || strings.ContainsAny(name, `/\"`+"\r\n\t") || strings.Contains(name, "..") || name != filepath.Base(name) {
		return "", false
	}
	return name, true
}

func (s *Server) allowInsecureSoftwareTestURL(parsed *url.URL) bool {
	if parsed == nil {
		return false
	}
	if parsed.Scheme == "https" {
		return true
	}
	return s != nil && s.softwareHTTPClient != nil && parsed.Scheme == "http"
}

func (s *Server) loadSoftwareCatalog() (configured bool, entries []softwareCatalogEntry, err error) {
	raw := strings.TrimSpace(s.env(softwarePackagesEnv))
	if raw == "" {
		return false, nil, nil
	}
	if len(raw) > softwareMaxCatalogBytes {
		return true, nil, errSoftwareCatalogInvalid
	}

	var items []rawSoftwareCatalogEntry
	if strings.HasPrefix(raw, "[") {
		if decodeErr := json.Unmarshal([]byte(raw), &items); decodeErr != nil {
			return true, nil, errSoftwareCatalogInvalid
		}
	} else {
		var envelope struct {
			Packages []rawSoftwareCatalogEntry `json:"packages"`
		}
		if decodeErr := json.Unmarshal([]byte(raw), &envelope); decodeErr != nil || envelope.Packages == nil {
			return true, nil, errSoftwareCatalogInvalid
		}
		items = envelope.Packages
	}

	seen := make(map[string]struct{}, len(items))
	out := make([]softwareCatalogEntry, 0, len(items))
	for _, item := range items {
		packageID, ok := validSoftwarePackageID(item.PackageID)
		if !ok {
			return true, nil, errSoftwareCatalogInvalid
		}
		if _, dup := seen[packageID]; dup {
			return true, nil, errSoftwareCatalogInvalid
		}
		seen[packageID] = struct{}{}

		version := strings.TrimSpace(item.Version)
		sha := strings.ToLower(strings.TrimSpace(item.SHA256))
		if version == "" || !softwareSHA256Pattern.MatchString(sha) || item.SizeBytes < 0 {
			return true, nil, errSoftwareCatalogInvalid
		}
		filename, ok := sanitizeSoftwareFilename(item.Filename, packageID, version)
		if !ok {
			return true, nil, errSoftwareCatalogInvalid
		}

		displayName := strings.TrimSpace(item.DisplayName)
		if displayName == "" {
			displayName = packageID
		}
		registryHost := strings.TrimSpace(item.RegistryHost)
		if registryHost == "" {
			registryHost = softwareDefaultRegistryHost
		}
		repository := strings.Trim(strings.TrimSpace(item.Repository), "/")
		blobDigest := strings.ToLower(strings.TrimSpace(item.BlobDigest))
		if blobDigest == "" {
			blobDigest = "sha256:" + sha
		} else if !strings.HasPrefix(blobDigest, "sha256:") || !softwareSHA256Pattern.MatchString(strings.TrimPrefix(blobDigest, "sha256:")) {
			return true, nil, errSoftwareCatalogInvalid
		}
		blobURL := strings.TrimSpace(item.BlobURL)
		if blobURL != "" {
			parsed, parseErr := url.Parse(blobURL)
			if parseErr != nil || parsed.Host == "" || !s.allowInsecureSoftwareTestURL(parsed) {
				return true, nil, errSoftwareCatalogInvalid
			}
		} else if repository == "" {
			return true, nil, errSoftwareCatalogInvalid
		} else if strings.ContainsAny(registryHost, "/\\ \t\r\n") {
			return true, nil, errSoftwareCatalogInvalid
		}

		out = append(out, softwareCatalogEntry{
			PackageID:    packageID,
			DisplayName:  displayName,
			Summary:      strings.TrimSpace(item.Summary),
			Version:      version,
			SHA256:       sha,
			SizeBytes:    item.SizeBytes,
			Filename:     filename,
			RegistryHost: registryHost,
			Repository:   repository,
			BlobDigest:   blobDigest,
			BlobURL:      blobURL,
		})
	}
	return true, out, nil
}

func (s *Server) findSoftwareCatalogEntry(packageID string) (configured bool, entry softwareCatalogEntry, found bool, err error) {
	configured, entries, err := s.loadSoftwareCatalog()
	if err != nil {
		return configured, softwareCatalogEntry{}, false, err
	}
	for _, candidate := range entries {
		if candidate.PackageID == packageID {
			return configured, candidate, true, nil
		}
	}
	return configured, softwareCatalogEntry{}, false, nil
}

func (s *Server) softwareHTTP() *http.Client {
	if s != nil && s.softwareHTTPClient != nil {
		return s.softwareHTTPClient
	}
	return &http.Client{
		Timeout: 5 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("stopped after 10 redirects")
			}
			// Never leak GHCR registry tokens onto CDN/S3 object storage redirects.
			if len(via) > 0 && !strings.EqualFold(req.URL.Host, via[0].URL.Host) {
				req.Header.Del("Authorization")
			}
			return nil
		},
	}
}

func (s *Server) ensureSoftwareBlob(ctx context.Context, entry softwareCatalogEntry) (string, error) {
	cacheRoot := strings.TrimSpace(s.env(softwareCacheRootEnv))
	if cacheRoot == "" {
		return "", errSoftwareCacheUnconfigured
	}
	blobDir := filepath.Join(cacheRoot, "blobs", "sha256")
	finalPath := filepath.Join(blobDir, entry.SHA256)
	if cachedPath, ok := cachedBlobReady(finalPath, entry.SizeBytes); ok {
		return cachedPath, nil
	}

	s.softwareStore.mu.Lock()
	if cachedPath, ok := cachedBlobReady(finalPath, entry.SizeBytes); ok {
		s.softwareStore.mu.Unlock()
		return cachedPath, nil
	}
	if s.softwareStore.flights == nil {
		s.softwareStore.flights = make(map[string]*softwareFetchFlight)
	}
	if existing, active := s.softwareStore.flights[entry.SHA256]; active {
		s.softwareStore.mu.Unlock()
		select {
		case <-existing.done:
			return existing.path, existing.err
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	flight := &softwareFetchFlight{done: make(chan struct{})}
	s.softwareStore.flights[entry.SHA256] = flight
	s.softwareStore.mu.Unlock()

	path, err := s.fetchAndCacheSoftwareBlob(ctx, blobDir, finalPath, entry)
	flight.path = path
	flight.err = err
	close(flight.done)

	s.softwareStore.mu.Lock()
	delete(s.softwareStore.flights, entry.SHA256)
	s.softwareStore.mu.Unlock()
	return path, err
}

func cachedBlobReady(finalPath string, expectedSize int64) (string, bool) {
	info, err := os.Stat(finalPath)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	if expectedSize > 0 && info.Size() != expectedSize {
		return "", false
	}
	return finalPath, true
}

func (e softwareCatalogEntry) sourceBlobURL() (string, error) {
	if e.BlobURL != "" {
		return e.BlobURL, nil
	}
	if e.RegistryHost == "" || e.Repository == "" || e.BlobDigest == "" {
		return "", errSoftwareSourceUnconfigured
	}
	segments := strings.Split(e.Repository, "/")
	for i, seg := range segments {
		if seg == "" {
			return "", errSoftwareSourceUnconfigured
		}
		segments[i] = url.PathEscape(seg)
	}
	return (&url.URL{
		Scheme: "https",
		Host:   e.RegistryHost,
		Path:   "/v2/" + strings.Join(segments, "/") + "/blobs/" + url.PathEscape(e.BlobDigest),
	}).String(), nil
}

func (s *Server) fetchAndCacheSoftwareBlob(ctx context.Context, blobDir, finalPath string, entry softwareCatalogEntry) (string, error) {
	if err := os.MkdirAll(blobDir, 0o750); err != nil {
		return "", fmt.Errorf("create software blob cache dir: %w", err)
	}
	sourceURL, err := entry.sourceBlobURL()
	if err != nil {
		return "", err
	}

	username := strings.TrimSpace(s.env(softwareRegistryUsernameEnv))
	token := strings.TrimSpace(s.env(softwareRegistryTokenEnv))
	httpClient := s.softwareHTTP()

	resp, err := s.doSoftwareBlobRequest(ctx, httpClient, sourceURL, "", username, token)
	if err != nil {
		return "", err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		challenge := resp.Header.Get("WWW-Authenticate")
		_ = resp.Body.Close()
		bearerToken, challengeErr := s.exchangeBearerRegistryToken(ctx, httpClient, challenge, username, token)
		if challengeErr != nil {
			return "", challengeErr
		}
		resp, err = s.doSoftwareBlobRequest(ctx, httpClient, sourceURL, bearerToken, "", "")
		if err != nil {
			return "", err
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: HTTP %d", errSoftwareUpstreamFailed, resp.StatusCode)
	}

	tmpFile, err := os.CreateTemp(blobDir, ".tmp-*")
	if err != nil {
		return "", fmt.Errorf("create temporary blob file: %w", err)
	}
	tmpPath := tmpFile.Name()
	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	hasher := sha256.New()
	limited := io.LimitReader(resp.Body, softwareMaxBlobBytes+1)
	written, copyErr := io.Copy(io.MultiWriter(tmpFile, hasher), limited)
	if copyErr != nil {
		_ = tmpFile.Close()
		return "", fmt.Errorf("%w: %v", errSoftwareUpstreamFailed, copyErr)
	}
	if written > softwareMaxBlobBytes {
		_ = tmpFile.Close()
		return "", errSoftwareUpstreamFailed
	}
	actualDigest := hex.EncodeToString(hasher.Sum(nil))
	if actualDigest != entry.SHA256 || (entry.SizeBytes > 0 && written != entry.SizeBytes) {
		_ = tmpFile.Close()
		return "", errSoftwareDigestMismatch
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return "", err
	}
	if err := tmpFile.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return "", err
	}
	cleanupTmp = false
	return finalPath, nil
}

func (s *Server) doSoftwareBlobRequest(ctx context.Context, client *http.Client, targetURL, bearerToken, username, password string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, errSoftwareSourceUnconfigured
	}
	req.Header.Set("Accept", "application/octet-stream, application/vnd.oci.image.layer.v1.tar+gzip")
	switch {
	case bearerToken != "":
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	case username != "" || password != "":
		req.SetBasicAuth(username, password)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errSoftwareUpstreamFailed, err)
	}
	return resp, nil
}

func parseBearerChallenge(header string) (realm, service, scope string, ok bool) {
	trimmed := strings.TrimSpace(header)
	if len(trimmed) < 7 || !strings.EqualFold(trimmed[:7], "Bearer ") {
		return "", "", "", false
	}
	matches := bearerParamPattern.FindAllStringSubmatch(trimmed[7:], -1)
	for _, m := range matches {
		switch strings.ToLower(m[1]) {
		case "realm":
			realm = m[2]
		case "service":
			service = m[2]
		case "scope":
			scope = m[2]
		}
	}
	return realm, service, scope, realm != ""
}

func (s *Server) exchangeBearerRegistryToken(ctx context.Context, client *http.Client, challenge, username, password string) (string, error) {
	realm, service, scope, ok := parseBearerChallenge(challenge)
	if !ok {
		return "", errSoftwareUpstreamFailed
	}
	parsed, err := url.Parse(realm)
	if err != nil || parsed.Host == "" || !s.allowInsecureSoftwareTestURL(parsed) {
		return "", errSoftwareUpstreamFailed
	}
	query := parsed.Query()
	if service != "" {
		query.Set("service", service)
	}
	if scope != "" {
		query.Set("scope", scope)
	}
	parsed.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return "", errSoftwareUpstreamFailed
	}
	req.Header.Set("Accept", "application/json")
	if username != "" || password != "" {
		req.SetBasicAuth(username, password)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %v", errSoftwareUpstreamFailed, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: auth HTTP %d", errSoftwareUpstreamFailed, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, softwareMaxChallengeBytes))
	if err != nil {
		return "", fmt.Errorf("%w: %v", errSoftwareUpstreamFailed, err)
	}
	var payload struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", errSoftwareUpstreamFailed
	}
	token := strings.TrimSpace(payload.Token)
	if token == "" {
		token = strings.TrimSpace(payload.AccessToken)
	}
	if token == "" {
		return "", errSoftwareUpstreamFailed
	}
	return token, nil
}

func formatContentLengthHeader(size int64) string {
	return strconv.FormatInt(size, 10)
}
