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
	// The object data plane owns one namespace: /objects. The same-origin entry
	// forwards a request's path verbatim, so the ingest route has to live in the
	// same namespace as the read route. A separate /parts route would be
	// shadowed by the read pattern: its path matches every /objects/{segment}
	// request, and its method is GET, so the upload would be refused with 405.
	//
	// The router refuses a pair of patterns that overlap without one being
	// strictly more specific, so the ingest route has to be method-scoped too:
	// a method-less route under /objects/ conflicts with the read route's own
	// path. PUT is the ingest method, and OPTIONS answers the browser's
	// preflight, which the same handler serves.
	//
	// Direct-to-storage providers hand the browser a scoped presigned URL, so
	// Capability never accepts part bytes and does not expose the ingest route.
	if d, ok := s.Objects.store.(interface{ DirectUploadOnly() bool }); !ok || !d.DirectUploadOnly() {
		mux.Handle("PUT /objects/parts", s.UploadHandler())
		mux.Handle("OPTIONS /objects/parts", s.UploadHandler())
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
