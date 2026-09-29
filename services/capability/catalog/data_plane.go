package catalog

import (
	"crypto/subtle"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// DataHandler serves signed upload permits and Build's existing immutable object
// read contract. The read token is a service-only credential, never an upload
// permit or a browser session; only confirmed Package objects can be read.
func (s *Service) DataHandler(readToken string) (http.Handler, error) {
	if len(readToken) < 32 {
		return nil, fmt.Errorf("a dedicated Build object-read token of at least 32 bytes is required")
	}
	mux := http.NewServeMux()
	// Direct-to-storage providers hand the browser a scoped presigned URL, so
	// Capability never accepts part bytes and does not expose the ingest route.
	if d, ok := s.Objects.store.(interface{ DirectUploadOnly() bool }); !ok || !d.DirectUploadOnly() {
		mux.Handle("/parts", s.UploadHandler())
	}
	mux.HandleFunc("GET /objects/{digest}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+readToken)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		d := "sha256:" + r.PathValue("digest")
		var size int64
		err := s.DB.QueryRowContext(r.Context(), `SELECT size_bytes FROM capability.package_versions WHERE object_ref=$1 AND sha256=$1 AND status='uploaded' LIMIT 1`, d).Scan(&size)
		if err == sql.ErrNoRows {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "object read unavailable", http.StatusServiceUnavailable)
			return
		}
		obj, err := s.Objects.store.OpenImmutable(r.Context(), d)
		if err != nil {
			http.Error(w, "object read unavailable", http.StatusServiceUnavailable)
			return
		}
		defer obj.Close()
		if obj.Size != size {
			http.Error(w, "object integrity unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("ETag", `"`+strings.TrimPrefix(d, "sha256:")+`"`)
		w.Header().Set("Content-Length", strconv.FormatInt(obj.Size, 10))
		if _, err = io.Copy(w, obj.Body); err != nil {
			return
		}
	})
	return mux, nil
}
