package store

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
)

func TestMessagesAppendAndList(t *testing.T) {
	s := NewTestStore(t, "messages_basic")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, OAuthProfile{Provider: "github", Subject: "u1"})
	sid := ulid.Make().String()

	for i := 0; i < 3; i++ {
		require.NoError(t, s.Messages().Append(ctx, sid, u.ID, time.Now(), "line "+string(rune('a'+i))))
	}

	out, err := s.Messages().List(ctx, sid, time.Time{}, 10)
	require.NoError(t, err)
	require.Len(t, out, 3)
	require.Equal(t, "line a", out[0].Entry)
}

func TestMessagesListSinceFilters(t *testing.T) {
	s := NewTestStore(t, "messages_since")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, OAuthProfile{Provider: "github", Subject: "u2"})
	sid := ulid.Make().String()

	t0 := time.Now().UTC()
	require.NoError(t, s.Messages().Append(ctx, sid, u.ID, t0, "old"))
	require.NoError(t, s.Messages().Append(ctx, sid, u.ID, t0.Add(time.Second), "new"))

	out, err := s.Messages().List(ctx, sid, t0.Add(500*time.Millisecond), 10)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, "new", out[0].Entry)
}
