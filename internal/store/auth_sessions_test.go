package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAuthSessionsCreateAndGet(t *testing.T) {
	s := NewTestStore(t, "authsess_basic")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, OAuthProfile{Provider: "github", Subject: "u1"})

	sess, err := s.AuthSessions().Create(ctx, u.ID, 30*24*time.Hour)
	require.NoError(t, err)
	require.NotEmpty(t, sess.ID)
	require.NotEmpty(t, sess.CSRFToken)

	got, err := s.AuthSessions().GetByID(ctx, sess.ID)
	require.NoError(t, err)
	require.Equal(t, u.ID, got.UserID)
}

func TestAuthSessionsDeleteRevokes(t *testing.T) {
	s := NewTestStore(t, "authsess_delete")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, OAuthProfile{Provider: "github", Subject: "u2"})
	sess, _ := s.AuthSessions().Create(ctx, u.ID, time.Hour)

	require.NoError(t, s.AuthSessions().Delete(ctx, sess.ID))
	_, err := s.AuthSessions().GetByID(ctx, sess.ID)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestAuthSessionsTouchExtendsExpiry(t *testing.T) {
	s := NewTestStore(t, "authsess_touch")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, OAuthProfile{Provider: "github", Subject: "u3"})
	sess, _ := s.AuthSessions().Create(ctx, u.ID, time.Minute)

	time.Sleep(5 * time.Millisecond)
	require.NoError(t, s.AuthSessions().Touch(ctx, sess.ID, time.Hour))

	got, _ := s.AuthSessions().GetByID(ctx, sess.ID)
	require.True(t, got.ExpiresAt.Sub(time.Now()) > 30*time.Minute)
}
