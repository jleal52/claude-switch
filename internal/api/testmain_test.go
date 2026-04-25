package api

import (
	"testing"

	"github.com/jleal52/claude-switch/internal/store"
)

// newTestStore wraps the store-package helper so handler tests can grab a
// fresh database without re-implementing the testcontainer plumbing.
func newTestStore(t *testing.T, label string) *store.Store {
	return store.NewTestStore(t, label)
}

// fakeProfile is a small helper for tests that need an OAuthProfile.
func fakeProfile(subject string) store.OAuthProfile {
	return store.OAuthProfile{Provider: "github", Subject: subject}
}
