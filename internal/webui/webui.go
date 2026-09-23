// Package webui serves the dashboard's built assets from inside the binary,
// so one container carries both the API and the UI on the same origin.
//
// The Vite build lands in web/dist; `make web` (and the Docker build) copy it
// into ./dist before `go build`. The committed dist/.gitkeep keeps the embed
// pattern valid on a checkout that never built the UI, and Handler answers
// with a plain explanation instead of a blank page in that case.
package webui

import (
	"embed"
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var embedded embed.FS

// Dist is the embedded build output, rooted at the directory index.html
// lives in.
func Dist() fs.FS {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		// fs.Sub only fails on an invalid path, and "dist" is a literal.
		panic(err)
	}
	return sub
}

// Handler serves fsys as a single-page app: real files are served as they
// are, and any other path without a file extension gets index.html so the
// client router can resolve deep links such as /runs/<id>.
func Handler(fsys fs.FS) http.Handler {
	files := http.FileServerFS(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name != "" && name != "index.html" {
			if st, err := fs.Stat(fsys, name); err == nil && !st.IsDir() {
				if strings.HasPrefix(name, "assets/") {
					// Vite fingerprints everything under assets/.
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
			// A missing file with an extension is a broken asset reference,
			// not a route; answering it with HTML only hides the real error.
			if path.Ext(name) != "" {
				http.NotFound(w, r)
				return
			}
		}
		serveIndex(w, r, fsys)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, fsys fs.FS) {
	body, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			http.Error(w, "The dashboard is not built into this binary. Run `make web` and rebuild, or use the published image.",
				http.StatusServiceUnavailable)
			return
		}
		http.Error(w, "could not read index.html", http.StatusInternalServerError)
		return
	}
	// index.html names the fingerprinted bundles, so it must never be cached
	// or a deploy leaves browsers asking for assets that no longer exist.
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}
