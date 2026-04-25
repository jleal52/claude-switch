package csrf

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVerifyAcceptsMatching(t *testing.T) {
	req := httptest.NewRequest("POST", "/x", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: "abc"})
	req.Header.Set(HeaderName, "abc")

	require.NoError(t, Verify(req))
}

func TestVerifyRejectsMissingCookie(t *testing.T) {
	req := httptest.NewRequest("POST", "/x", nil)
	req.Header.Set(HeaderName, "abc")

	err := Verify(req)
	require.ErrorIs(t, err, ErrMissingCookie)
}

func TestVerifyRejectsMissingHeader(t *testing.T) {
	req := httptest.NewRequest("POST", "/x", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: "abc"})

	err := Verify(req)
	require.ErrorIs(t, err, ErrMissingHeader)
}

func TestVerifyRejectsMismatch(t *testing.T) {
	req := httptest.NewRequest("POST", "/x", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: "abc"})
	req.Header.Set(HeaderName, "xyz")

	err := Verify(req)
	require.ErrorIs(t, err, ErrMismatch)
}
