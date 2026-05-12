package proto

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCatalogDiffRoundTrip(t *testing.T) {
	d := CatalogDiff{
		Full: true,
		Projects: []CatalogProject{{
			Slug: "-x", Cwd: "/x", Name: "x",
			SessionCount: 1, FirstActivityAt: "2026-05-09T10:00:00Z", LastActivityAt: "2026-05-09T10:30:00Z",
		}},
		Transcripts: []CatalogTranscript{{
			JSONLUUID: "u1", Slug: "-x", Path: "-x/u1.jsonl",
			StartedAt: "2026-05-09T10:00:00Z", EndedAt: "2026-05-09T10:30:00Z",
			MessageCount: 3, Title: "hi", Bytes: 1024,
		}},
		RemovedTranscripts: []string{"u-old"},
	}
	raw, err := Encode(TypeCatalogDiff, "", d)
	require.NoError(t, err)

	typ, _, payload, err := Decode(raw)
	require.NoError(t, err)
	require.Equal(t, TypeCatalogDiff, typ)

	var got CatalogDiff
	require.NoError(t, payload.Into(&got))
	require.Equal(t, d, got)
}

func TestSearchRequestAndResultsRoundTrip(t *testing.T) {
	req := SearchRequest{
		Query:           "hello",
		ProjectID:       "abc",
		MaxResults:      100,
		SnippetChars:    120,
		CaseInsensitive: true,
	}
	raw, _ := Encode(TypeSearchRequest, "req-1", req)
	typ, sess, payload, err := Decode(raw)
	require.NoError(t, err)
	require.Equal(t, TypeSearchRequest, typ)
	require.Equal(t, "req-1", sess)
	var gotReq SearchRequest
	require.NoError(t, payload.Into(&gotReq))
	require.Equal(t, req, gotReq)

	res := SearchResults{
		Matches: []SearchMatch{
			{TranscriptID: "t1", MsgIndex: 5, Role: "user", Snippet: "...hello world...", Timestamp: "2026-05-09T10:00:00Z"},
		},
		Truncated: false,
		ElapsedMs: 42,
	}
	raw2, _ := Encode(TypeSearchResults, "req-1", res)
	typ2, sess2, payload2, err := Decode(raw2)
	require.NoError(t, err)
	require.Equal(t, TypeSearchResults, typ2)
	require.Equal(t, "req-1", sess2)
	var gotRes SearchResults
	require.NoError(t, payload2.Into(&gotRes))
	require.Equal(t, res, gotRes)
}
