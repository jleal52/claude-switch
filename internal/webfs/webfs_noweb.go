//go:build noweb

package webfs

import "net/http"

// Handler returns a handler that 404s every request. Used when the server
// is built with the `noweb` tag (headless API mode).
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
}

// Enabled reports whether the binary embeds a web bundle.
func Enabled() bool { return false }
