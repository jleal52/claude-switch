//go:build !noweb

package webfs

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHandlerServesStubIndex(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	Handler().ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	body, _ := io.ReadAll(rr.Body)
	require.True(t, strings.Contains(string(body), "claude-switch"))
}

func TestHandlerSpaFallback(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/some/spa/route", nil)
	Handler().ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
}
