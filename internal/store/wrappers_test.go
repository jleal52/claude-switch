package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWrappersInsertAndList(t *testing.T) {
	s := NewTestStore(t, "wrappers_basic")
	ctx := context.Background()

	u, err := s.Users().UpsertOAuth(ctx, OAuthProfile{Provider: "github", Subject: "u1"})
	require.NoError(t, err)

	w, plain, err := s.Wrappers().Create(ctx, WrapperCreate{
		UserID: u.ID, Name: "ireland", OS: "linux", Arch: "amd64", Version: "0.1.0",
	})
	require.NoError(t, err)
	require.NotEmpty(t, w.ID)
	require.NotEmpty(t, plain) // refresh token returned to wrapper

	list, err := s.Wrappers().ListByUser(ctx, u.ID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, "ireland", list[0].Name)
}

func TestWrappersVerifyRefreshToken(t *testing.T) {
	s := NewTestStore(t, "wrappers_refresh")
	ctx := context.Background()

	u, _ := s.Users().UpsertOAuth(ctx, OAuthProfile{Provider: "github", Subject: "u2"})
	w, plain, err := s.Wrappers().Create(ctx, WrapperCreate{UserID: u.ID, Name: "x", OS: "linux", Arch: "amd64"})
	require.NoError(t, err)

	got, err := s.Wrappers().VerifyRefreshToken(ctx, plain)
	require.NoError(t, err)
	require.Equal(t, w.ID, got.ID)

	_, err = s.Wrappers().VerifyRefreshToken(ctx, plain+"tamper")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestWrappersRevokedRejectsVerify(t *testing.T) {
	s := NewTestStore(t, "wrappers_revoked")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, OAuthProfile{Provider: "github", Subject: "u3"})
	w, plain, _ := s.Wrappers().Create(ctx, WrapperCreate{UserID: u.ID, Name: "x", OS: "linux", Arch: "amd64"})

	require.NoError(t, s.Wrappers().Revoke(ctx, w.ID))
	_, err := s.Wrappers().VerifyRefreshToken(ctx, plain)
	require.ErrorIs(t, err, ErrRevoked)
}

func TestWrapperAccessTokenLifecycle(t *testing.T) {
	s := NewTestStore(t, "wrappers_access")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, OAuthProfile{Provider: "github", Subject: "u4"})
	w, _, _ := s.Wrappers().Create(ctx, WrapperCreate{UserID: u.ID, Name: "x", OS: "linux", Arch: "amd64"})

	plain, expiresAt, err := s.WrapperTokens().Issue(ctx, w.ID, u.ID, time.Hour)
	require.NoError(t, err)
	require.NotEmpty(t, plain)
	require.True(t, expiresAt.After(time.Now()))

	got, err := s.WrapperTokens().Verify(ctx, plain)
	require.NoError(t, err)
	require.Equal(t, w.ID, got.WrapperID)

	_, err = s.WrapperTokens().Verify(ctx, plain+"x")
	require.ErrorIs(t, err, ErrNotFound)
}
