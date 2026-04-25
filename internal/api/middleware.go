package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/jleal52/claude-switch/internal/csrf"
	"github.com/jleal52/claude-switch/internal/store"
)

type ctxKey int

const userCtxKey ctxKey = 0

type AuthMiddleware struct {
	store *store.Store
}

func NewAuthMiddleware(s *store.Store) *AuthMiddleware { return &AuthMiddleware{store: s} }

// Require returns an http.Handler that enforces an authenticated user. On
// state-changing methods (POST/PUT/PATCH/DELETE), it also enforces CSRF.
func (m *AuthMiddleware) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookieName)
		if err != nil || c.Value == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		sess, err := m.store.AuthSessions().GetByID(r.Context(), c.Value)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		// Refresh expiry on every request (rolling 30 days).
		_ = m.store.AuthSessions().Touch(r.Context(), sess.ID, authSessionTTL)

		if isMutating(r.Method) {
			if err := csrf.Verify(r); err != nil {
				http.Error(w, "csrf: "+err.Error(), http.StatusForbidden)
				return
			}
			if r.Header.Get(csrf.HeaderName) != sess.CSRFToken {
				http.Error(w, "csrf: token mismatch", http.StatusForbidden)
				return
			}
		}

		user, err := m.store.Users().GetByID(r.Context(), sess.UserID)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), userCtxKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// UserFromContext returns the authenticated user attached by Require.
func UserFromContext(ctx context.Context) (*store.User, bool) {
	u, ok := ctx.Value(userCtxKey).(*store.User)
	return u, ok
}

// MustUser panics if no user is in context. Use only inside handlers wrapped
// by Require.
func MustUser(ctx context.Context) *store.User {
	u, ok := UserFromContext(ctx)
	if !ok {
		panic(errors.New("api: no user in context"))
	}
	return u
}

func isMutating(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}
