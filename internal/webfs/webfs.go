//go:build !noweb

package webfs

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var bundle embed.FS

// Handler returns an http.Handler that serves the embedded bundle. Routes
// without a matching file fall back to index.html so client-side routing works.
func Handler() http.Handler {
	root, err := fs.Sub(bundle, "dist")
	if err != nil {
		panic(err)
	}
	fileSrv := http.FileServer(http.FS(root))
	indexBytes, _ := fs.ReadFile(root, "index.html")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, ".") && r.URL.Path != "/" {
			fileSrv.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(indexBytes)
	})
}

// Enabled reports whether the binary embeds a web bundle (true here).
func Enabled() bool { return true }
