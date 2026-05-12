package transcripts

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// TitleMaxChars caps the persisted title length. Server stores the same.
const TitleMaxChars = 120

// Scanner walks ~/.claude/projects/ producing a Catalog. The root is captured
// at construction so the same Scanner can rescan one transcript later (the
// watcher path).
type Scanner struct {
	root string
}

func NewScanner(root string) *Scanner { return &Scanner{root: root} }

// Scan returns a freshly populated Catalog. Missing root returns an empty
// catalog (the user has never opened Claude on this machine yet — not an
// error). Unparseable JSONL lines are skipped.
func (s *Scanner) Scan(ctx context.Context) (*Catalog, error) {
	cat := newCatalog()
	entries, err := os.ReadDir(s.root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cat, nil
		}
		return nil, err
	}
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !e.IsDir() {
			continue
		}
		if err := s.scanProject(cat, e.Name()); err != nil {
			return nil, err
		}
	}
	return cat, nil
}

func (s *Scanner) scanProject(cat *Catalog, slug string) error {
	projDir := filepath.Join(s.root, slug)
	entries, err := os.ReadDir(projDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		uuid := strings.TrimSuffix(name, ".jsonl")
		jsonlPath := filepath.Join(projDir, name)
		tr, cwd, err := scanTranscript(slug, uuid, jsonlPath)
		if err != nil {
			// Individual transcripts that fail (permission denied, truncated)
			// are skipped rather than aborting the whole scan.
			continue
		}
		cat.PutTranscript(tr, cwd)
	}
	return nil
}

// scanTranscript reads one JSONL file and returns a Transcript plus the cwd
// observed in events (the wrapper-side source of truth for the project's cwd
// — slug → cwd is not reversible when path components contain '-').
func scanTranscript(slug, uuid, path string) (*Transcript, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, "", err
	}

	tr := &Transcript{
		JSONLUUID: uuid,
		Slug:      slug,
		Path:      filepath.Join(slug, uuid+".jsonl"),
		Bytes:     info.Size(),
	}

	br := bufio.NewReaderSize(f, 64*1024)
	var (
		cwd   string
		title string
		count int
	)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			count++
			parseLine(line, tr, &cwd, &title)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, "", err
		}
	}
	tr.MessageCount = count
	tr.Title = title
	return tr, cwd, nil
}

// jsonlEvent is the trimmed shape this package cares about. The on-disk
// format has many more fields but we don't need them for the catalog.
type jsonlEvent struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	Cwd       string          `json:"cwd"`
	IsMeta    bool            `json:"isMeta"`
	Message   *jsonlMessage   `json:"message"`
}

type jsonlMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

func parseLine(line []byte, tr *Transcript, cwdOut, titleOut *string) {
	var ev jsonlEvent
	if err := json.Unmarshal(line, &ev); err != nil {
		return
	}
	if ev.Timestamp != "" {
		if ts, err := time.Parse(time.RFC3339Nano, ev.Timestamp); err == nil {
			if tr.StartedAt.IsZero() || ts.Before(tr.StartedAt) {
				tr.StartedAt = ts
			}
			if tr.EndedAt.IsZero() || ts.After(tr.EndedAt) {
				tr.EndedAt = ts
			}
		}
	}
	if *cwdOut == "" && ev.Cwd != "" {
		*cwdOut = ev.Cwd
	}
	if *titleOut == "" && ev.Type == "user" && !ev.IsMeta && ev.Message != nil {
		if t := extractText(ev.Message.Content); t != "" {
			trimmed := strings.TrimSpace(t)
			if !isClaudeCommandText(trimmed) {
				*titleOut = truncate(trimmed, TitleMaxChars)
			}
		}
	}
}

// isClaudeCommandText reports whether a user message body is one of
// Claude's command/caveat envelopes (e.g. `<command-name>/exit…</…>` or
// `<local-command-caveat>…</…>`). These are mechanical and useless as
// transcript titles, so we skip them and try the next user message.
func isClaudeCommandText(s string) bool {
	return strings.HasPrefix(s, "<command-") ||
		strings.HasPrefix(s, "<local-command-")
}

// extractText returns the user-facing text of a message body that may be
// either a raw string or a list of `{type, text}` parts (current Claude
// format). Other content-block types ("tool_use", etc.) are ignored.
func extractText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// Try string first.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	// Fall back to list of parts.
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err == nil {
		var sb strings.Builder
		for _, p := range parts {
			if p.Type == "text" {
				sb.WriteString(p.Text)
			}
		}
		return sb.String()
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
