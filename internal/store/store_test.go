package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewConnectsAndPings(t *testing.T) {
	uri := MustStartMongo(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	s, err := New(ctx, uri, "claude_switch_test")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	require.NoError(t, s.Ping(ctx))
}

func TestNewCreatesAllRequiredIndexes(t *testing.T) {
	uri := MustStartMongo(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	s, err := New(ctx, uri, "claude_switch_idx")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	cases := []struct {
		coll    string
		indexes []string // index name fragments to expect
	}{
		{"users", []string{"oauth_provider_1_oauth_subject_1", "email_1"}},
		{"wrappers", []string{"user_id_1_paired_at_-1", "refresh_token_id_1"}},
		{"wrapper_access_tokens", []string{"token_hash_1", "expires_at_1"}},
		{"pairing_codes", []string{"code_1", "expires_at_1"}},
		{"sessions", []string{"user_id_1_created_at_-1", "wrapper_id_1_status_1"}},
		{"session_messages", []string{"session_id_1_ts_1", "user_id_1_ts_-1", "ts_1"}},
		{"auth_sessions", []string{"user_id_1", "expires_at_1"}},
	}
	for _, c := range cases {
		got, err := s.IndexNames(ctx, c.coll)
		require.NoError(t, err, "collection %s", c.coll)
		for _, want := range c.indexes {
			require.Contains(t, got, want, "collection %s missing index %s", c.coll, want)
		}
	}
}
