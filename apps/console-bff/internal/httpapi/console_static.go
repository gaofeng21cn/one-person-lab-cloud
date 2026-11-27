package httpapi

import (
	"bytes"
	"compress/gzip"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// handleConsoleStatic serves the compiled Console SPA bundle for the cloud
// identity so Console BFF no longer depends on the retained Control Plane for
// static assets or browser root requests.
func (s *Server) handleConsoleStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.NotFound(w, r)
		return
	}
	if isRejectedAPIOrInternalPath(r.URL.Path) {
		http.NotFound(w, r)
		return
	}
	dist := consoleDistDir()
	if strings.HasPrefix(r.URL.Path, "/assets/") {
		rel := strings.TrimPrefix(r.URL.Path, "/assets/")
		if !filepath.IsLocal(rel) || !serveConsoleStaticFile(w, r, filepath.Join(dist, "assets", rel), "public,max-age=31536000,immutable") {
			http.NotFound(w, r)
		}
		return
	}
	if filepath.Ext(r.URL.Path) != "" {
		rel := strings.TrimPrefix(r.URL.Path, "/")
		if !filepath.IsLocal(rel) || !serveConsoleStaticFile(w, r, filepath.Join(dist, rel), "public,max-age=86400") {
			http.NotFound(w, r)
		}
		return
	}
	if serveConsoleStaticFile(w, r, filepath.Join(dist, "index.html"), "no-cache") {
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fallback := []byte(`<!doctype html><html><head><title>OPL Console</title></head><body><div id="root"></div></body></html>`)
	http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(fallback))
}

func isRejectedAPIOrInternalPath(path string) bool {
	return path == "/api" || strings.HasPrefix(path, "/api/") || path == "/internal" || strings.HasPrefix(path, "/internal/")
}

func containsDotDotSegment(rawPath string) bool {
	for _, segment := range strings.Split(rawPath, "/") {
		if segment == ".." || strings.EqualFold(segment, "%2e%2e") {
			return true
		}
	}
	return false
}

func consoleDistDir() string {
	for _, dir := range []string{strings.TrimSpace(os.Getenv("OPL_CONSOLE_DIST_DIR")), "/app/dist", "dist", "../../dist", "../../../../dist"} {
		if dir == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, "index.html")); err == nil {
			return dir
		}
	}
	return "dist"
}

func serveConsoleStaticFile(w http.ResponseWriter, r *http.Request, path string, cacheControl string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	w.Header().Set("Cache-Control", cacheControl)
	if contentType := mime.TypeByExtension(filepath.Ext(path)); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	if !compressibleConsoleStaticAsset(path) {
		http.ServeContent(w, r, info.Name(), info.ModTime(), file)
		return true
	}
	w.Header().Add("Vary", "Accept-Encoding")
	if r.Header.Get("Range") != "" || !acceptsConsoleStaticGzip(r.Header.Get("Accept-Encoding")) {
		http.ServeContent(w, r, info.Name(), info.ModTime(), file)
		return true
	}
	var compressed bytes.Buffer
	zipper := gzip.NewWriter(&compressed)
	if _, err := io.Copy(zipper, file); err != nil {
		_ = zipper.Close()
		http.Error(w, "static asset read failed", http.StatusInternalServerError)
		return true
	}
	if err := zipper.Close(); err != nil {
		http.Error(w, "static asset compression failed", http.StatusInternalServerError)
		return true
	}
	w.Header().Set("Content-Encoding", "gzip")
	http.ServeContent(w, r, info.Name(), info.ModTime(), bytes.NewReader(compressed.Bytes()))
	return true
}

func compressibleConsoleStaticAsset(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".css", ".js", ".json", ".map", ".mjs", ".svg", ".txt", ".webmanifest", ".xml":
		return true
	default:
		return false
	}
}

func acceptsConsoleStaticGzip(value string) bool {
	for _, item := range strings.Split(value, ",") {
		parts := strings.Split(item, ";")
		if !strings.EqualFold(strings.TrimSpace(parts[0]), "gzip") {
			continue
		}
		for _, part := range parts[1:] {
			name, raw, ok := strings.Cut(part, "=")
			if !ok || !strings.EqualFold(strings.TrimSpace(name), "q") {
				continue
			}
			quality, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
			return err == nil && quality > 0
		}
		return true
	}
	return false
}
