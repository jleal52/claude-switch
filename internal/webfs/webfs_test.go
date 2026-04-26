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

func TestHandlerServesIndex(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	Handler().ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	body, _ := io.ReadAll(rr.Body)
	require.True(t, strings.Contains(string(body), "<div id=\"root\">"))
}

func TestHandlerSpaFallback(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/some/spa/route", nil)
	Handler().ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
}
