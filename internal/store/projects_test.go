package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var uniqUserCounter int

func mustUser(t *testing.T, s *Store) string {
	t.Helper()
	uniqUserCounter++
	u, err := s.Users().UpsertOAuth(context.Background(), OAuthProfile{
		Provider: "github",
		Subject:  t.Name() + "-" + time.Now().Format("150405.000000000") + "-" + itoa(uniqUserCounter),
	})
	require.NoError(t, err)
	return u.ID
}

func itoa(i int) string {
	const digits = "0123456789"
	if i == 0 {
		return "0"
	}
	var b [20]byte
	bp := len(b)
	for i > 0 {
		bp--
		b[bp] = digits[i%10]
		i /= 10
	}
	return string(b[bp:])
}

func mustWrapper(t *testing.T, s *Store, userID, name string) string {
	t.Helper()
	w, _, err := s.Wrappers().Create(context.Background(), WrapperCreate{
		UserID: userID, Name: name, OS: "linux", Arch: "amd64",
	})
	require.NoError(t, err)
	return w.ID
}

func sampleProject(slug, cwd string) ProjectUpsert {
	return ProjectUpsert{
		Slug:            slug,
		Cwd:             cwd,
		Name:            cwd[len("/Users/me/"):],
		SessionCount:    2,
		FirstActivityAt: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
		LastActivityAt:  time.Date(2026, 5, 9, 18, 0, 0, 0, time.UTC),
	}
}

func TestProjectsUpsertCreates(t *testing.T) {
	s := NewTestStore(t, "proj_create")
	u := mustUser(t, s)
	w := mustWrapper(t, s, u, "w1")

	ids, err := s.Projects().UpsertMany(context.Background(), u, w, []ProjectUpsert{
		sampleProject("-Users-me-foo", "/Users/me/foo"),
	})
	require.NoError(t, err)
	require.Len(t, ids, 1)
	require.NotEmpty(t, ids["-Users-me-foo"])

	got, err := s.Projects().ListByWrapper(context.Background(), w)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "-Users-me-foo", got[0].Slug)
	require.Equal(t, "foo", got[0].Name)
	require.Equal(t, 2, got[0].SessionCount)
}

func TestProjectsUpsertKeepsIDOnSubsequentCalls(t *testing.T) {
	s := NewTestStore(t, "proj_upsert_idstable")
	u := mustUser(t, s)
	w := mustWrapper(t, s, u, "w1")

	ids1, err := s.Projects().UpsertMany(context.Background(), u, w, []ProjectUpsert{
		sampleProject("-x", "/Users/me/x"),
	})
	require.NoError(t, err)
	first := ids1["-x"]

	p := sampleProject("-x", "/Users/me/x")
	p.SessionCount = 99
	ids2, err := s.Projects().UpsertMany(context.Background(), u, w, []ProjectUpsert{p})
	require.NoError(t, err)
	require.Equal(t, first, ids2["-x"], "id must be stable across upserts")

	got, _ := s.Projects().ListByWrapper(context.Background(), w)
	require.Equal(t, 99, got[0].SessionCount)
}

func TestProjectsDeleteForWrapperExcept(t *testing.T) {
	s := NewTestStore(t, "proj_delete_except")
	u := mustUser(t, s)
	w := mustWrapper(t, s, u, "w1")

	_, err := s.Projects().UpsertMany(context.Background(), u, w, []ProjectUpsert{
		sampleProject("-a", "/Users/me/a"),
		sampleProject("-b", "/Users/me/b"),
		sampleProject("-c", "/Users/me/c"),
	})
	require.NoError(t, err)

	require.NoError(t, s.Projects().DeleteForWrapperExcept(context.Background(), w, []string{"-a", "-c"}))
	got, _ := s.Projects().ListByWrapper(context.Background(), w)
	slugs := []string{}
	for _, p := range got {
		slugs = append(slugs, p.Slug)
	}
	require.ElementsMatch(t, []string{"-a", "-c"}, slugs)
}

func TestProjectsGetByIDReturnsProject(t *testing.T) {
	s := NewTestStore(t, "proj_get_by_id")
	u := mustUser(t, s)
	w := mustWrapper(t, s, u, "w1")

	ids, err := s.Projects().UpsertMany(context.Background(), u, w, []ProjectUpsert{
		sampleProject("-x", "/Users/me/x"),
	})
	require.NoError(t, err)
	pid := ids["-x"]

	got, err := s.Projects().GetByID(context.Background(), pid)
	require.NoError(t, err)
	require.Equal(t, "/Users/me/x", got.Cwd)
	require.Equal(t, "-x", got.Slug)
	require.Equal(t, w, got.WrapperID)

	_, err = s.Projects().GetByID(context.Background(), "000000000000000000000000")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestProjectsListByUserScopesToOwner(t *testing.T) {
	s := NewTestStore(t, "proj_scope")
	u1, u2 := mustUser(t, s), mustUser(t, s)
	require.NotEqual(t, u1, u2)
	w1 := mustWrapper(t, s, u1, "w1")
	w2 := mustWrapper(t, s, u2, "w2")

	_, _ = s.Projects().UpsertMany(context.Background(), u1, w1, []ProjectUpsert{
		sampleProject("-x", "/Users/me/x"),
	})
	_, _ = s.Projects().UpsertMany(context.Background(), u2, w2, []ProjectUpsert{
		sampleProject("-y", "/Users/me/y"),
	})

	gotU1, _ := s.Projects().ListByUser(context.Background(), u1)
	require.Len(t, gotU1, 1)
	require.Equal(t, "-x", gotU1[0].Slug)
}
