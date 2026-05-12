package transcripts

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jleal52/claude-switch/internal/proto"
)

// seedCatalog creates the on-disk fixture + the in-memory Catalog.
func seedCatalog(t *testing.T, root string) *Catalog {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "-x"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "-y"), 0o755))
	writeJSONL(t, filepath.Join(root, "-x", "u1.jsonl"), []string{
		`{"type":"user","timestamp":"2026-05-09T10:00:00.000Z","cwd":"/x","message":{"role":"user","content":"the quick brown fox"}}`,
		`{"type":"assistant","timestamp":"2026-05-09T10:00:01.000Z","cwd":"/x","message":{"role":"assistant","content":"jumps over the lazy dog"}}`,
	})
	writeJSONL(t, filepath.Join(root, "-y", "u2.jsonl"), []string{
		`{"type":"user","timestamp":"2026-05-09T10:01:00.000Z","cwd":"/y","message":{"role":"user","content":"a different conversation entirely"}}`,
	})
	cat, err := NewScanner(root).Scan(context.Background())
	require.NoError(t, err)
	return cat
}

func TestSearcherFindsLiteralAcrossTranscripts(t *testing.T) {
	root := t.TempDir()
	cat := seedCatalog(t, root)
	s := &Searcher{Root: root, Catalog: cat}
	res := s.Search(context.Background(), proto.SearchRequest{Query: "fox", MaxResults: 10, SnippetChars: 40})
	require.Len(t, res.Matches, 1)
	require.Equal(t, "u1", res.Matches[0].TranscriptID)
	require.Contains(t, res.Matches[0].Snippet, "fox")
}

func TestSearcherCaseInsensitive(t *testing.T) {
	root := t.TempDir()
	cat := seedCatalog(t, root)
	s := &Searcher{Root: root, Catalog: cat}
	res := s.Search(context.Background(), proto.SearchRequest{Query: "FOX", MaxResults: 10, CaseInsensitive: true})
	require.Len(t, res.Matches, 1)
}

func TestSearcherCaseSensitiveByDefault(t *testing.T) {
	root := t.TempDir()
	cat := seedCatalog(t, root)
	s := &Searcher{Root: root, Catalog: cat}
	res := s.Search(context.Background(), proto.SearchRequest{Query: "FOX", MaxResults: 10})
	require.Empty(t, res.Matches)
}

func TestSearcherScopeByProjectSlug(t *testing.T) {
	root := t.TempDir()
	cat := seedCatalog(t, root)
	s := &Searcher{Root: root, Catalog: cat}
	// "conversation" appears only in -y. With ProjectID = "-x" we want zero hits.
	res := s.Search(context.Background(), proto.SearchRequest{Query: "conversation", ProjectID: "-x", MaxResults: 10})
	require.Empty(t, res.Matches)
	res = s.Search(context.Background(), proto.SearchRequest{Query: "conversation", ProjectID: "-y", MaxResults: 10})
	require.Len(t, res.Matches, 1)
}

func TestSearcherScopeByTranscriptIDs(t *testing.T) {
	root := t.TempDir()
	cat := seedCatalog(t, root)
	s := &Searcher{Root: root, Catalog: cat}
	res := s.Search(context.Background(), proto.SearchRequest{Query: "fox", TranscriptIDs: []string{"u2"}, MaxResults: 10})
	require.Empty(t, res.Matches, "u2 has no 'fox'")
}

func TestSearcherTruncatesAtMaxResults(t *testing.T) {
	root := t.TempDir()
	cat := seedCatalog(t, root)
	s := &Searcher{Root: root, Catalog: cat}
	// Both messages in u1 contain "the".
	res := s.Search(context.Background(), proto.SearchRequest{Query: "the", MaxResults: 1})
	require.Len(t, res.Matches, 1)
	require.True(t, res.Truncated)
}
