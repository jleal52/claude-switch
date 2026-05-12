package transcripts

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func mkTranscript(uuid, slug string, started time.Time) *Transcript {
	return &Transcript{
		JSONLUUID:    uuid,
		Slug:         slug,
		Path:         slug + "/" + uuid + ".jsonl",
		StartedAt:    started,
		EndedAt:      started.Add(time.Minute),
		MessageCount: 3,
		Title:        "t",
		Bytes:        100,
	}
}

func TestCatalogPutTranscriptCreatesProject(t *testing.T) {
	c := newCatalog()
	now := time.Now().UTC().Truncate(time.Second)
	c.PutTranscript(mkTranscript("u1", "-x", now), "/x")

	snap := c.Snapshot()
	require.Len(t, snap.Projects, 1)
	require.Equal(t, "-x", snap.Projects[0].Slug)
	require.Equal(t, "/x", snap.Projects[0].Cwd)
	require.Equal(t, "x", snap.Projects[0].Name)
	require.Equal(t, 1, snap.Projects[0].SessionCount)
}

func TestCatalogPutTranscriptAggregatesActivity(t *testing.T) {
	c := newCatalog()
	t0 := time.Date(2026, 5, 9, 10, 0, 0, 0, time.UTC)
	c.PutTranscript(mkTranscript("u1", "-x", t0), "/x")
	c.PutTranscript(mkTranscript("u2", "-x", t0.Add(time.Hour)), "/x")

	snap := c.Snapshot()
	p := snap.Projects[0]
	require.Equal(t, 2, p.SessionCount)
	require.Equal(t, t0, p.FirstActivityAt)
	require.Equal(t, t0.Add(time.Hour).Add(time.Minute), p.LastActivityAt)
}

func TestCatalogRemoveTranscriptDropsProjectWhenEmpty(t *testing.T) {
	c := newCatalog()
	c.PutTranscript(mkTranscript("u1", "-x", time.Now().UTC()), "/x")
	c.RemoveTranscript("u1")
	require.Empty(t, c.Snapshot().Projects)
	require.Empty(t, c.Snapshot().Transcripts)
}

func TestCatalogRemoveTranscriptKeepsProjectIfMoreLeft(t *testing.T) {
	c := newCatalog()
	t0 := time.Now().UTC().Truncate(time.Second)
	c.PutTranscript(mkTranscript("u1", "-x", t0), "/x")
	c.PutTranscript(mkTranscript("u2", "-x", t0.Add(time.Hour)), "/x")
	c.RemoveTranscript("u1")

	snap := c.Snapshot()
	require.Len(t, snap.Projects, 1)
	require.Equal(t, 1, snap.Projects[0].SessionCount)
	require.Equal(t, t0.Add(time.Hour), snap.Projects[0].FirstActivityAt)
}

func TestCatalogDiffFirstRunMarksEverythingNew(t *testing.T) {
	c := newCatalog()
	c.PutTranscript(mkTranscript("u1", "-x", time.Now().UTC()), "/x")
	d := c.Diff(nil)
	require.Len(t, d.UpsertProjects, 1)
	require.Len(t, d.UpsertTranscripts, 1)
	require.Empty(t, d.RemovedTranscripts)
}

func TestCatalogDiffNoChangeYieldsEmpty(t *testing.T) {
	c := newCatalog()
	c.PutTranscript(mkTranscript("u1", "-x", time.Now().UTC().Truncate(time.Second)), "/x")
	prev := c.Snapshot()
	d := c.Diff(prev)
	require.Empty(t, d.UpsertProjects)
	require.Empty(t, d.UpsertTranscripts)
	require.Empty(t, d.RemovedTranscripts)
}

func TestCatalogDiffDetectsUpsertsAndRemovals(t *testing.T) {
	c := newCatalog()
	t0 := time.Now().UTC().Truncate(time.Second)
	c.PutTranscript(mkTranscript("u1", "-x", t0), "/x")
	c.PutTranscript(mkTranscript("u2", "-x", t0), "/x")
	prev := c.Snapshot()

	// Append to u1, add u3, remove u2.
	updated := mkTranscript("u1", "-x", t0)
	updated.MessageCount = 99
	c.PutTranscript(updated, "/x")
	c.PutTranscript(mkTranscript("u3", "-x", t0), "/x")
	c.RemoveTranscript("u2")

	d := c.Diff(prev)
	uuids := make([]string, 0, len(d.UpsertTranscripts))
	for _, t := range d.UpsertTranscripts {
		uuids = append(uuids, t.JSONLUUID)
	}
	require.ElementsMatch(t, []string{"u1", "u3"}, uuids)
	require.Equal(t, []string{"u2"}, d.RemovedTranscripts)
}
