// Package csrf implements double-submit cookie protection.
// Cookie is non-HttpOnly so the SPA can read it and mirror its value into
// a request header. Server compares; mismatch = reject.
package csrf

import (
	"errors"
	"net/http"
)

const (
	CookieName = "cs_csrf"
	HeaderName = "X-CSRF-Token"
)

var (
	ErrMissingCookie = errors.New("csrf: missing cookie")
	ErrMissingHeader = errors.New("csrf: missing header")
	ErrMismatch      = errors.New("csrf: cookie/header mismatch")
)

// Verify checks the request carries a matching CSRF cookie and header.
// Use only on mutating endpoints (POST/DELETE/PATCH/PUT).
func Verify(r *http.Request) error {
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" {
		return ErrMissingCookie
	}
	h := r.Header.Get(HeaderName)
	if h == "" {
		return ErrMissingHeader
	}
	if c.Value != h {
		return ErrMismatch
	}
	return nil
}

// Set writes the CSRF cookie. token is the auth_session.csrf_token.
func Set(w http.ResponseWriter, token string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: false, // SPA must read it
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}
