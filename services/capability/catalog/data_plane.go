package catalog

import (
	"crypto/subtle"
	"database/sql"
	"fmt"
	"net/http"
	"os"
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
	mux.Handle("/parts", s.UploadHandler())
	mux.HandleFunc("GET /objects/{digest}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+readToken)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		d := "sha256:" + r.PathValue("digest")
		filename, err := s.Objects.objectPath(d)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		var size int64
		err = s.DB.QueryRowContext(r.Context(), `SELECT size_bytes FROM capability.package_versions WHERE object_ref=$1 AND sha256=$1 AND status='uploaded' LIMIT 1`, d).Scan(&size)
		if err == sql.ErrNoRows {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "object read unavailable", http.StatusServiceUnavailable)
			return
		}
		f, err := os.Open(filename)
		if err != nil {
			http.Error(w, "object read unavailable", http.StatusServiceUnavailable)
			return
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() != size {
			http.Error(w, "object integrity unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("ETag", `"`+strings.TrimPrefix(d, "sha256:")+`"`)
		http.ServeContent(w, r, "package.zip", info.ModTime(), f)
	})
	return mux, nil
}
