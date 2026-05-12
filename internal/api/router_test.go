package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jleal52/claude-switch/internal/hub"
	"github.com/jleal52/claude-switch/internal/searchhub"
)

// TestRouterMountsWSWrapper guards against regressions where the
// /ws/wrapper mount is silently dropped. The wswrapper handler rejects
// requests without a bearer with 401; the SPA file server (the catch-all
// "/" route) would instead serve index.html with 200, which is what made
// every wrapper appear offline in production before this test existed.
func TestRouterMountsWSWrapper(t *testing.T) {
	s := newTestStore(t, "router_wswrapper")
	r := NewRouter(RouterConfig{
		Store:     s,
		Hub:       hub.New(),
		SearchHub: searchhub.New(nil, 0),
		BaseURL:   "https://example.test",
	})

	req := httptest.NewRequest("GET", "/ws/wrapper", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	require.Equal(t, http.StatusUnauthorized, rr.Code, "/ws/wrapper without bearer must be 401, not the SPA fallback")
}

// TestRouterMountsApiSearch likewise asserts /api/search is reachable
// when SearchHub is provided (it shouldn't fall through to the SPA).
func TestRouterMountsApiSearch(t *testing.T) {
	s := newTestStore(t, "router_search")
	r := NewRouter(RouterConfig{
		Store:     s,
		Hub:       hub.New(),
		SearchHub: searchhub.New(nil, 0),
		BaseURL:   "https://example.test",
	})

	req := httptest.NewRequest("POST", "/api/search", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	// 401 (no session) or 400 (no body) are both acceptable; 200 with HTML
	// would mean the catch-all swallowed the request.
	require.NotEqual(t, http.StatusOK, rr.Code)
	require.True(t, rr.Code == http.StatusUnauthorized || rr.Code == http.StatusBadRequest || rr.Code == http.StatusForbidden,
		"unexpected status %d", rr.Code)
}
