package store

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
)

func TestSessionsCreateAndGet(t *testing.T) {
	s := NewTestStore(t, "sessions_basic")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, OAuthProfile{Provider: "github", Subject: "u1"})
	w, _, _ := s.Wrappers().Create(ctx, WrapperCreate{UserID: u.ID, Name: "x", OS: "linux", Arch: "amd64"})

	sid := ulid.Make().String()
	sess, err := s.Sessions().Create(ctx, SessionCreate{
		ID: sid, UserID: u.ID, WrapperID: w.ID, Cwd: "/tmp", Account: "default",
	})
	require.NoError(t, err)
	require.Equal(t, sid, sess.ID)
	require.Equal(t, "starting", sess.Status)

	got, err := s.Sessions().GetByID(ctx, sid)
	require.NoError(t, err)
	require.Equal(t, "/tmp", got.Cwd)
}

func TestSessionsTransitions(t *testing.T) {
	s := NewTestStore(t, "sessions_xitions")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, OAuthProfile{Provider: "github", Subject: "u2"})
	w, _, _ := s.Wrappers().Create(ctx, WrapperCreate{UserID: u.ID, Name: "x", OS: "linux", Arch: "amd64"})
	sid := ulid.Make().String()
	_, err := s.Sessions().Create(ctx, SessionCreate{ID: sid, UserID: u.ID, WrapperID: w.ID, Cwd: "/", Account: "default"})
	require.NoError(t, err)

	require.NoError(t, s.Sessions().MarkRunning(ctx, sid, "abcd1234"))
	got, _ := s.Sessions().GetByID(ctx, sid)
	require.Equal(t, "running", got.Status)
	require.Equal(t, "abcd1234", got.JSONLUUID)

	require.NoError(t, s.Sessions().MarkExited(ctx, sid, 0, "normal", ""))
	got, _ = s.Sessions().GetByID(ctx, sid)
	require.Equal(t, "exited", got.Status)
	require.NotNil(t, got.ExitCode)
	require.Equal(t, 0, *got.ExitCode)
}

func TestSessionsListByUser(t *testing.T) {
	s := NewTestStore(t, "sessions_list")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, OAuthProfile{Provider: "github", Subject: "u3"})
	w, _, _ := s.Wrappers().Create(ctx, WrapperCreate{UserID: u.ID, Name: "x", OS: "linux", Arch: "amd64"})

	for i := 0; i < 3; i++ {
		_, err := s.Sessions().Create(ctx, SessionCreate{
			ID: ulid.Make().String(), UserID: u.ID, WrapperID: w.ID,
			Cwd: "/", Account: "default",
		})
		require.NoError(t, err)
	}

	got, err := s.Sessions().ListByUser(ctx, u.ID, "")
	require.NoError(t, err)
	require.Len(t, got, 3)
}
