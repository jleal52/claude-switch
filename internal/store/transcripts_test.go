package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func sampleTranscript(uuid, slug string, started time.Time) TranscriptUpsert {
	return TranscriptUpsert{
		JSONLUUID:    uuid,
		ProjectSlug:  slug,
		Path:         slug + "/" + uuid + ".jsonl",
		StartedAt:    started,
		EndedAt:      started.Add(10 * time.Minute),
		MessageCount: 7,
		Title:        "hello " + uuid,
		Bytes:        2048,
	}
}

func TestTranscriptsReplaceForWrapperFullSet(t *testing.T) {
	s := NewTestStore(t, "tr_replace")
	u := mustUser(t, s)
	w := mustWrapper(t, s, u, "w1")
	t0 := time.Date(2026, 5, 9, 10, 0, 0, 0, time.UTC)

	require.NoError(t, s.Transcripts().ReplaceForWrapper(context.Background(), u, w,
		[]ProjectUpsert{sampleProject("-x", "/Users/me/x")},
		[]TranscriptUpsert{
			sampleTranscript("u1", "-x", t0),
			sampleTranscript("u2", "-x", t0.Add(time.Hour)),
		},
	))

	got, err := s.Transcripts().ListByWrapper(context.Background(), w, 10)
	require.NoError(t, err)
	require.Len(t, got, 2)

	// Replace with a smaller set: u1 should vanish.
	require.NoError(t, s.Transcripts().ReplaceForWrapper(context.Background(), u, w,
		[]ProjectUpsert{sampleProject("-x", "/Users/me/x")},
		[]TranscriptUpsert{sampleTranscript("u2", "-x", t0.Add(time.Hour))},
	))
	got, _ = s.Transcripts().ListByWrapper(context.Background(), w, 10)
	require.Len(t, got, 1)
	require.Equal(t, "u2", got[0].JSONLUUID)
}

func TestTranscriptsUpsertManyResolvesProjectBySlug(t *testing.T) {
	s := NewTestStore(t, "tr_resolve")
	u := mustUser(t, s)
	w := mustWrapper(t, s, u, "w1")

	ids, err := s.Projects().UpsertMany(context.Background(), u, w, []ProjectUpsert{
		sampleProject("-x", "/Users/me/x"),
	})
	require.NoError(t, err)
	pid := ids["-x"]

	require.NoError(t, s.Transcripts().UpsertMany(context.Background(), u, w, ids, []TranscriptUpsert{
		sampleTranscript("u1", "-x", time.Now().UTC()),
	}))

	got, _ := s.Transcripts().ListByWrapper(context.Background(), w, 10)
	require.Len(t, got, 1)
	require.Equal(t, pid, got[0].ProjectID)
}

func TestTranscriptsDeleteByUUIDs(t *testing.T) {
	s := NewTestStore(t, "tr_delete")
	u := mustUser(t, s)
	w := mustWrapper(t, s, u, "w1")
	t0 := time.Now().UTC()

	require.NoError(t, s.Transcripts().ReplaceForWrapper(context.Background(), u, w,
		[]ProjectUpsert{sampleProject("-x", "/Users/me/x")},
		[]TranscriptUpsert{
			sampleTranscript("u1", "-x", t0),
			sampleTranscript("u2", "-x", t0),
			sampleTranscript("u3", "-x", t0),
		},
	))

	require.NoError(t, s.Transcripts().DeleteByUUIDs(context.Background(), w, []string{"u1", "u3"}))
	got, _ := s.Transcripts().ListByWrapper(context.Background(), w, 10)
	require.Len(t, got, 1)
	require.Equal(t, "u2", got[0].JSONLUUID)
}

func TestTranscriptsListRecentByUserSortsByStartedDesc(t *testing.T) {
	s := NewTestStore(t, "tr_recent")
	u := mustUser(t, s)
	w := mustWrapper(t, s, u, "w1")
	t0 := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)

	require.NoError(t, s.Transcripts().ReplaceForWrapper(context.Background(), u, w,
		[]ProjectUpsert{sampleProject("-x", "/Users/me/x")},
		[]TranscriptUpsert{
			sampleTranscript("old", "-x", t0),
			sampleTranscript("new", "-x", t0.Add(48*time.Hour)),
			sampleTranscript("mid", "-x", t0.Add(24*time.Hour)),
		},
	))

	got, err := s.Transcripts().ListRecentByUser(context.Background(), u, 10)
	require.NoError(t, err)
	require.Len(t, got, 3)
	require.Equal(t, []string{"new", "mid", "old"}, []string{got[0].JSONLUUID, got[1].JSONLUUID, got[2].JSONLUUID})
}

func TestTranscriptsListByProject(t *testing.T) {
	s := NewTestStore(t, "tr_by_proj")
	u := mustUser(t, s)
	w := mustWrapper(t, s, u, "w1")
	t0 := time.Now().UTC()

	require.NoError(t, s.Transcripts().ReplaceForWrapper(context.Background(), u, w,
		[]ProjectUpsert{
			sampleProject("-x", "/Users/me/x"),
			sampleProject("-y", "/Users/me/y"),
		},
		[]TranscriptUpsert{
			sampleTranscript("a", "-x", t0),
			sampleTranscript("b", "-x", t0),
			sampleTranscript("c", "-y", t0),
		},
	))

	projs, _ := s.Projects().ListByWrapper(context.Background(), w)
	var xID string
	for _, p := range projs {
		if p.Slug == "-x" {
			xID = p.ID
		}
	}
	require.NotEmpty(t, xID)
	got, err := s.Transcripts().ListByProject(context.Background(), xID, 10)
	require.NoError(t, err)
	require.Len(t, got, 2)
}

func TestTranscriptsGetByID(t *testing.T) {
	s := NewTestStore(t, "tr_get")
	u := mustUser(t, s)
	w := mustWrapper(t, s, u, "w1")

	require.NoError(t, s.Transcripts().ReplaceForWrapper(context.Background(), u, w,
		[]ProjectUpsert{sampleProject("-x", "/Users/me/x")},
		[]TranscriptUpsert{sampleTranscript("u1", "-x", time.Now().UTC())},
	))
	all, _ := s.Transcripts().ListByWrapper(context.Background(), w, 10)
	require.Len(t, all, 1)
	got, err := s.Transcripts().GetByID(context.Background(), all[0].ID)
	require.NoError(t, err)
	require.Equal(t, "u1", got.JSONLUUID)
}
