//go:build !noweb

// Package webfs serves the SPA bundle when present. The bundle is expected
// to be at ../../web (relative to this file's repo root) and is embedded at
// build time. If the web/ directory is empty (subsystem 3 not built yet),
// a small stub index.html is served instead.
package webfs

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed stub/index.html
var stubFS embed.FS

//go:embed all:stub
var bundle embed.FS

// Handler returns an http.Handler that serves the embedded bundle. Routes
// without a matching file fall back to index.html so client-side routing works.
func Handler() http.Handler {
	root, err := fs.Sub(bundle, "stub")
	if err != nil {
		panic(err)
	}
	fileSrv := http.FileServer(http.FS(root))
	indexBytes, _ := fs.ReadFile(stubFS, "stub/index.html")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, ".") && r.URL.Path != "/" {
			fileSrv.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(indexBytes)
	})
}

// Enabled reports whether the binary embeds a web bundle.
func Enabled() bool { return true }
