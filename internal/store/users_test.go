package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUsersUpsertCreatesThenReturnsSame(t *testing.T) {
	s := NewTestStore(t, "users_upsert")
	ctx := context.Background()

	u1, err := s.Users().UpsertOAuth(ctx, OAuthProfile{
		Provider: "github", Subject: "1234", Email: "a@example.com",
		Name: "Ada", AvatarURL: "https://...",
	})
	require.NoError(t, err)
	require.NotEmpty(t, u1.ID)
	require.False(t, u1.CreatedAt.IsZero())

	u2, err := s.Users().UpsertOAuth(ctx, OAuthProfile{
		Provider: "github", Subject: "1234", Email: "a@example.com",
		Name: "Ada Lovelace", AvatarURL: "https://...new",
	})
	require.NoError(t, err)
	require.Equal(t, u1.ID, u2.ID)
	require.Equal(t, "Ada Lovelace", u2.Name)
	require.Equal(t, "https://...new", u2.AvatarURL)
}

func TestUsersGetByIDNotFound(t *testing.T) {
	s := NewTestStore(t, "users_getmissing")
	ctx := context.Background()

	_, err := s.Users().GetByID(ctx, "000000000000000000000000")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestUsersMarkLoginUpdatesTimestamp(t *testing.T) {
	s := NewTestStore(t, "users_marklogin")
	ctx := context.Background()

	u, err := s.Users().UpsertOAuth(ctx, OAuthProfile{Provider: "google", Subject: "g1", Email: "x@y"})
	require.NoError(t, err)
	prev := u.LastLoginAt

	time.Sleep(5 * time.Millisecond)
	require.NoError(t, s.Users().MarkLogin(ctx, u.ID))

	got, err := s.Users().GetByID(ctx, u.ID)
	require.NoError(t, err)
	require.True(t, got.LastLoginAt.After(prev))
}

func TestUsersSetKeepTranscripts(t *testing.T) {
	s := NewTestStore(t, "users_keep")
	ctx := context.Background()

	u, err := s.Users().UpsertOAuth(ctx, OAuthProfile{Provider: "github", Subject: "k1"})
	require.NoError(t, err)
	require.False(t, u.KeepTranscripts)

	require.NoError(t, s.Users().SetKeepTranscripts(ctx, u.ID, true))
	got, err := s.Users().GetByID(ctx, u.ID)
	require.NoError(t, err)
	require.True(t, got.KeepTranscripts)
}

func TestUsersSetTranscriptRetention(t *testing.T) {
	s := NewTestStore(t, "users_retention")
	ctx := context.Background()

	u, _ := s.Users().UpsertOAuth(ctx, OAuthProfile{Provider: "github", Subject: "ret1"})
	require.Equal(t, 0, u.TranscriptRetentionDays)

	require.NoError(t, s.Users().SetTranscriptRetention(ctx, u.ID, 30))

	got, err := s.Users().GetByID(ctx, u.ID)
	require.NoError(t, err)
	require.Equal(t, 30, got.TranscriptRetentionDays)
}
