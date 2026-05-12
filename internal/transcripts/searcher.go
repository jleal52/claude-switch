package transcripts

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jleal52/claude-switch/internal/proto"
)

// DefaultSearchTimeout caps a single search.request execution. The server
// applies its own 15 s ceiling on top; this is the wrapper-internal limit
// so a busy filesystem can't block forever.
const DefaultSearchTimeout = 10 * time.Second

// Searcher answers proto.SearchRequest frames by streaming JSONL files
// under Root and scanning for literal substring hits.
//
// The wire field SearchRequest.ProjectID is interpreted as a project SLUG
// (the dir name under ~/.claude/projects/). SearchRequest.TranscriptIDs
// are interpreted as JSONL UUIDs. The server's HTTP layer is responsible
// for translating its own ObjectIDs to these wrapper-side identifiers
// before fanning out.
type Searcher struct {
	Root    string
	Catalog *Catalog
	Timeout time.Duration
}

func (s *Searcher) Search(ctx context.Context, req proto.SearchRequest) proto.SearchResults {
	start := time.Now()
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = DefaultSearchTimeout
	}
	sctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	query := req.Query
	if req.CaseInsensitive {
		query = strings.ToLower(query)
	}
	snippetChars := req.SnippetChars
	if snippetChars <= 0 {
		snippetChars = 120
	}
	maxResults := req.MaxResults
	if maxResults <= 0 {
		maxResults = 100
	}

	uuidFilter := map[string]bool{}
	for _, id := range req.TranscriptIDs {
		uuidFilter[id] = true
	}

	candidates := []*Transcript{}
	if s.Catalog != nil {
		for _, t := range s.Catalog.Snapshot().Transcripts {
			if req.ProjectID != "" && t.Slug != req.ProjectID {
				continue
			}
			if len(uuidFilter) > 0 && !uuidFilter[t.JSONLUUID] {
				continue
			}
			candidates = append(candidates, t)
		}
	}

	res := proto.SearchResults{Matches: []proto.SearchMatch{}}
	for _, tr := range candidates {
		if sctx.Err() != nil {
			res.Truncated = true
			break
		}
		if len(res.Matches) >= maxResults {
			res.Truncated = true
			break
		}
		path := filepath.Join(s.Root, tr.Path)
		matches, hitCap := scanTranscriptForMatches(sctx, path, tr.JSONLUUID, query, req.CaseInsensitive, snippetChars, maxResults-len(res.Matches))
		res.Matches = append(res.Matches, matches...)
		if hitCap {
			res.Truncated = true
			break
		}
	}
	res.ElapsedMs = time.Since(start).Milliseconds()
	return res
}

// scanTranscriptForMatches streams one JSONL file and returns substring
// hits. hitCap is true when we returned exactly remaining results, meaning
// the caller should set Truncated.
func scanTranscriptForMatches(ctx context.Context, path, uuid, query string, caseInsensitive bool, snippetChars, remaining int) (matches []proto.SearchMatch, hitCap bool) {
	if remaining <= 0 {
		return nil, true
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()

	br := bufio.NewReaderSize(f, 64*1024)
	idx := 0
	for {
		if ctx.Err() != nil {
			return matches, true
		}
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			if m, ok := matchInEvent(line, uuid, idx, query, caseInsensitive, snippetChars); ok {
				matches = append(matches, m)
				if len(matches) >= remaining {
					return matches, true
				}
			}
			idx++
		}
		if err == io.EOF {
			return matches, false
		}
		if err != nil {
			return matches, false
		}
	}
}

type searchEvent struct {
	Type      string        `json:"type"`
	Timestamp string        `json:"timestamp"`
	Message   *jsonlMessage `json:"message"`
}

func matchInEvent(line []byte, uuid string, idx int, query string, caseInsensitive bool, snippetChars int) (proto.SearchMatch, bool) {
	var ev searchEvent
	if err := json.Unmarshal(line, &ev); err != nil || ev.Message == nil {
		return proto.SearchMatch{}, false
	}
	text := extractText(ev.Message.Content)
	if text == "" {
		return proto.SearchMatch{}, false
	}
	hay := text
	if caseInsensitive {
		hay = strings.ToLower(hay)
	}
	pos := strings.Index(hay, query)
	if pos < 0 {
		return proto.SearchMatch{}, false
	}
	return proto.SearchMatch{
		TranscriptID: uuid,
		MsgIndex:     idx,
		Role:         ev.Message.Role,
		Snippet:      makeSnippet(text, pos, len(query), snippetChars),
		Timestamp:    ev.Timestamp,
	}, true
}

// makeSnippet returns up to snippetChars characters of text centred on
// the match. Splits the budget half before, half after the hit.
func makeSnippet(text string, pos, queryLen, snippetChars int) string {
	if snippetChars >= len(text) {
		return text
	}
	half := snippetChars / 2
	start := pos - half
	if start < 0 {
		start = 0
	}
	end := start + snippetChars
	if end > len(text) {
		end = len(text)
		start = end - snippetChars
		if start < 0 {
			start = 0
		}
	}
	out := text[start:end]
	if start > 0 {
		out = "…" + out
	}
	if end < len(text) {
		out = out + "…"
	}
	_ = queryLen // reserved for future highlighting
	return out
}
