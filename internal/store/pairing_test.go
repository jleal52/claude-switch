package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPairingCreateAndGet(t *testing.T) {
	s := NewTestStore(t, "pair_create")
	ctx := context.Background()

	pc, err := s.Pairing().Create(ctx, WrapperDescriptor{
		Name: "ireland", OS: "linux", Arch: "amd64", Version: "0.1.0",
	}, 10*time.Minute)
	require.NoError(t, err)
	require.Len(t, pc.Code, 9) // ABCD-1234
	require.Equal(t, "pending", pc.Status)

	got, err := s.Pairing().GetByCode(ctx, pc.Code)
	require.NoError(t, err)
	require.Equal(t, pc.Code, got.Code)
}

func TestPairingApproveSetsUserAndStatus(t *testing.T) {
	s := NewTestStore(t, "pair_approve")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, OAuthProfile{Provider: "github", Subject: "u1"})
	pc, _ := s.Pairing().Create(ctx, WrapperDescriptor{Name: "n", OS: "linux", Arch: "amd64"}, time.Minute)

	require.NoError(t, s.Pairing().Approve(ctx, pc.Code, u.ID))

	got, _ := s.Pairing().GetByCode(ctx, pc.Code)
	require.Equal(t, "approved", got.Status)
	require.Equal(t, u.ID, got.UserID)
}

func TestPairingDeleteAfterRedeem(t *testing.T) {
	s := NewTestStore(t, "pair_delete")
	ctx := context.Background()
	pc, _ := s.Pairing().Create(ctx, WrapperDescriptor{Name: "n", OS: "linux", Arch: "amd64"}, time.Minute)

	require.NoError(t, s.Pairing().Delete(ctx, pc.Code))

	_, err := s.Pairing().GetByCode(ctx, pc.Code)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestPairingDoubleApproveIsError(t *testing.T) {
	s := NewTestStore(t, "pair_double")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, OAuthProfile{Provider: "github", Subject: "u2"})
	pc, _ := s.Pairing().Create(ctx, WrapperDescriptor{Name: "n", OS: "linux", Arch: "amd64"}, time.Minute)

	require.NoError(t, s.Pairing().Approve(ctx, pc.Code, u.ID))
	err := s.Pairing().Approve(ctx, pc.Code, u.ID)
	require.ErrorIs(t, err, ErrAlreadyApproved)
}
