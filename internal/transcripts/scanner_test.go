package transcripts

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScannerEmptyRoot(t *testing.T) {
	root := t.TempDir()
	cat, err := NewScanner(root).Scan(context.Background())
	require.NoError(t, err)
	snap := cat.Snapshot()
	require.Empty(t, snap.Projects)
	require.Empty(t, snap.Transcripts)
}

func TestScannerMissingRootReturnsEmpty(t *testing.T) {
	// Wrappers run before the user has ever opened Claude — the projects
	// dir may not exist yet. Scan must not error out; it should report an
	// empty catalog and let the watcher pick the dir up later.
	cat, err := NewScanner(filepath.Join(t.TempDir(), "never-created")).Scan(context.Background())
	require.NoError(t, err)
	require.Empty(t, cat.Snapshot().Projects)
}

func TestScannerOneProjectOneTranscript(t *testing.T) {
	root := t.TempDir()
	slug := "-Users-alice-code-foo"
	projDir := filepath.Join(root, slug)
	require.NoError(t, os.MkdirAll(projDir, 0o755))

	uuid := "11111111-2222-3333-4444-555555555555"
	jsonlPath := filepath.Join(projDir, uuid+".jsonl")
	writeJSONL(t, jsonlPath, []string{
		`{"type":"last-prompt","leafUuid":"x","sessionId":"` + uuid + `"}`,
		`{"type":"permission-mode"}`,
		`{"type":"user","timestamp":"2026-05-09T10:00:00.000Z","cwd":"/Users/alice/code/foo","message":{"role":"user","content":"hello there"}}`,
		`{"type":"assistant","timestamp":"2026-05-09T10:00:05.000Z","cwd":"/Users/alice/code/foo","message":{"role":"assistant","content":"hi"}}`,
	})

	cat, err := NewScanner(root).Scan(context.Background())
	require.NoError(t, err)
	snap := cat.Snapshot()

	require.Len(t, snap.Projects, 1)
	p := snap.Projects[0]
	require.Equal(t, slug, p.Slug)
	require.Equal(t, "/Users/alice/code/foo", p.Cwd)
	require.Equal(t, "foo", p.Name)
	require.Equal(t, 1, p.SessionCount)
	require.Equal(t, "2026-05-09T10:00:00.000Z", p.FirstActivityAt.UTC().Format("2006-01-02T15:04:05.000Z"))
	require.Equal(t, "2026-05-09T10:00:05.000Z", p.LastActivityAt.UTC().Format("2006-01-02T15:04:05.000Z"))

	require.Len(t, snap.Transcripts, 1)
	tr := snap.Transcripts[0]
	require.Equal(t, uuid, tr.JSONLUUID)
	require.Equal(t, slug, tr.Slug)
	require.Equal(t, filepath.Join(slug, uuid+".jsonl"), tr.Path)
	require.Equal(t, "2026-05-09T10:00:00.000Z", tr.StartedAt.UTC().Format("2006-01-02T15:04:05.000Z"))
	require.Equal(t, "2026-05-09T10:00:05.000Z", tr.EndedAt.UTC().Format("2006-01-02T15:04:05.000Z"))
	require.Equal(t, 4, tr.MessageCount) // total line count
	require.Equal(t, "hello there", tr.Title)
	require.Greater(t, tr.Bytes, int64(0))
}

func TestScannerExtractsTitleFromFirstRealUserPrompt(t *testing.T) {
	root := t.TempDir()
	slug := "-Users-alice-foo"
	require.NoError(t, os.MkdirAll(filepath.Join(root, slug), 0o755))

	// First "user" line is a Claude-injected caveat (isMeta=true). Title
	// should come from the next user message instead.
	writeJSONL(t, filepath.Join(root, slug, "abc.jsonl"), []string{
		`{"type":"user","isMeta":true,"timestamp":"2026-05-09T10:00:00.000Z","cwd":"/Users/alice/foo","message":{"role":"user","content":"<local-command-caveat>noise</local-command-caveat>"}}`,
		`{"type":"user","timestamp":"2026-05-09T10:00:01.000Z","cwd":"/Users/alice/foo","message":{"role":"user","content":"real prompt"}}`,
	})

	cat, err := NewScanner(root).Scan(context.Background())
	require.NoError(t, err)
	require.Equal(t, "real prompt", cat.Snapshot().Transcripts[0].Title)
}

func TestScannerSkipsClaudeCommandEnvelopes(t *testing.T) {
	root := t.TempDir()
	slug := "-Users-alice-foo"
	require.NoError(t, os.MkdirAll(filepath.Join(root, slug), 0o755))

	// First user message is a slash-command envelope (no isMeta but useless
	// as a title). Second is a real prompt.
	writeJSONL(t, filepath.Join(root, slug, "cmd.jsonl"), []string{
		`{"type":"user","timestamp":"2026-05-09T10:00:00.000Z","cwd":"/Users/alice/foo","message":{"role":"user","content":"<command-name>/exit</command-name> <command-message>exit</command-message> <command-args></command-args>"}}`,
		`{"type":"user","timestamp":"2026-05-09T10:00:01.000Z","cwd":"/Users/alice/foo","message":{"role":"user","content":"real question after a slash-command"}}`,
	})
	cat, err := NewScanner(root).Scan(context.Background())
	require.NoError(t, err)
	require.Equal(t, "real question after a slash-command", cat.Snapshot().Transcripts[0].Title)
}

func TestScannerSkipsLocalCommandCaveatEnvelopes(t *testing.T) {
	root := t.TempDir()
	slug := "-x"
	require.NoError(t, os.MkdirAll(filepath.Join(root, slug), 0o755))
	writeJSONL(t, filepath.Join(root, slug, "c.jsonl"), []string{
		`{"type":"user","timestamp":"2026-05-09T10:00:00.000Z","cwd":"/x","message":{"role":"user","content":"<local-command-caveat>noise</local-command-caveat>"}}`,
		`{"type":"user","timestamp":"2026-05-09T10:00:01.000Z","cwd":"/x","message":{"role":"user","content":"after the caveat"}}`,
	})
	cat, err := NewScanner(root).Scan(context.Background())
	require.NoError(t, err)
	require.Equal(t, "after the caveat", cat.Snapshot().Transcripts[0].Title)
}

func TestScannerHandlesContentList(t *testing.T) {
	root := t.TempDir()
	slug := "-x"
	require.NoError(t, os.MkdirAll(filepath.Join(root, slug), 0o755))

	writeJSONL(t, filepath.Join(root, slug, "ab.jsonl"), []string{
		`{"type":"user","timestamp":"2026-05-09T10:00:00.000Z","cwd":"/x","message":{"role":"user","content":[{"type":"text","text":"part one"},{"type":"text","text":" part two"}]}}`,
	})
	cat, err := NewScanner(root).Scan(context.Background())
	require.NoError(t, err)
	require.Equal(t, "part one part two", cat.Snapshot().Transcripts[0].Title)
}

func TestScannerTitleTruncatedTo120Chars(t *testing.T) {
	root := t.TempDir()
	slug := "-x"
	require.NoError(t, os.MkdirAll(filepath.Join(root, slug), 0o755))

	long := ""
	for i := 0; i < 200; i++ {
		long += "a"
	}
	writeJSONL(t, filepath.Join(root, slug, "ab.jsonl"), []string{
		`{"type":"user","timestamp":"2026-05-09T10:00:00.000Z","cwd":"/x","message":{"role":"user","content":"` + long + `"}}`,
	})
	cat, err := NewScanner(root).Scan(context.Background())
	require.NoError(t, err)
	require.Len(t, cat.Snapshot().Transcripts[0].Title, 120)
}

func TestScannerSkipsNonJSONLFiles(t *testing.T) {
	root := t.TempDir()
	slug := "-x"
	dir := filepath.Join(root, slug)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "stray.txt"), []byte("ignore me"), 0o644))
	writeJSONL(t, filepath.Join(dir, "ab.jsonl"), []string{
		`{"type":"user","timestamp":"2026-05-09T10:00:00.000Z","cwd":"/x","message":{"role":"user","content":"hi"}}`,
	})
	cat, err := NewScanner(root).Scan(context.Background())
	require.NoError(t, err)
	require.Len(t, cat.Snapshot().Transcripts, 1)
}

func TestScannerHandlesUnparseableLines(t *testing.T) {
	root := t.TempDir()
	slug := "-x"
	require.NoError(t, os.MkdirAll(filepath.Join(root, slug), 0o755))
	writeJSONL(t, filepath.Join(root, slug, "ab.jsonl"), []string{
		`not json at all`,
		`{"type":"user","timestamp":"2026-05-09T10:00:00.000Z","cwd":"/x","message":{"role":"user","content":"hi"}}`,
	})
	cat, err := NewScanner(root).Scan(context.Background())
	require.NoError(t, err)
	require.Len(t, cat.Snapshot().Transcripts, 1)
	require.Equal(t, "hi", cat.Snapshot().Transcripts[0].Title)
}

func TestScannerMultipleProjects(t *testing.T) {
	root := t.TempDir()
	for i, s := range []string{"-a", "-b", "-c"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, s), 0o755))
		uuid := "0000000-0000-0000-0000-00000000000" + string(rune('1'+i))
		writeJSONL(t, filepath.Join(root, s, uuid+".jsonl"), []string{
			`{"type":"user","timestamp":"2026-05-09T10:00:00.000Z","cwd":"/` + s[1:] + `","message":{"role":"user","content":"q"}}`,
		})
	}
	cat, err := NewScanner(root).Scan(context.Background())
	require.NoError(t, err)
	require.Len(t, cat.Snapshot().Projects, 3)
	require.Len(t, cat.Snapshot().Transcripts, 3)
}

// writeJSONL writes one JSON object per line.
func writeJSONL(t *testing.T, path string, lines []string) {
	t.Helper()
	content := ""
	for _, l := range lines {
		content += l + "\n"
	}
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}
