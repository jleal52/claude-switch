package searchhub

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jleal52/claude-switch/internal/proto"
)

// fakeSender pretends to be the hub: each Send call records what it
// would have written to the wrapper.
type fakeSender struct {
	mu       sync.Mutex
	sent     map[string]proto.SearchRequest // requestID → req
	offline  map[string]bool                // wrapperID → true
	sendErrs map[string]error               // wrapperID → err
}

func newFakeSender() *fakeSender {
	return &fakeSender{sent: map[string]proto.SearchRequest{}, offline: map[string]bool{}, sendErrs: map[string]error{}}
}

func (f *fakeSender) SendSearchRequest(wrapperID, requestID string, body any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.offline[wrapperID] {
		return ErrWrapperOffline
	}
	if err := f.sendErrs[wrapperID]; err != nil {
		return err
	}
	f.sent[requestID+"/"+wrapperID] = body.(proto.SearchRequest)
	return nil
}

func TestDispatchAggregatesOnlineWrappers(t *testing.T) {
	sender := newFakeSender()
	hub := New(sender, 200*time.Millisecond)

	// Two wrappers; both reply quickly.
	results := make(chan deliveredResult, 2)
	go func() {
		time.Sleep(10 * time.Millisecond)
		hub.Deliver("req-1", "w1", proto.SearchResults{
			Matches:   []proto.SearchMatch{{TranscriptID: "t1", Snippet: "x"}},
			ElapsedMs: 5,
		})
		results <- deliveredResult{}
		hub.Deliver("req-1", "w2", proto.SearchResults{
			Matches:   []proto.SearchMatch{{TranscriptID: "t2", Snippet: "y"}},
			ElapsedMs: 7,
		})
		results <- deliveredResult{}
	}()

	resp, err := hub.Dispatch(context.Background(), Query{
		RequestID:    "req-1",
		WrapperIDs:   []string{"w1", "w2"},
		Body:         proto.SearchRequest{Query: "x", MaxResults: 10},
	})
	require.NoError(t, err)
	require.Len(t, resp.Matches, 2)
	require.Equal(t, "ok", resp.ByWrapper["w1"].Status)
	require.Equal(t, "ok", resp.ByWrapper["w2"].Status)

	<-results
	<-results
}

func TestDispatchMarksOfflineWrappers(t *testing.T) {
	sender := newFakeSender()
	sender.offline["w-down"] = true
	hub := New(sender, 100*time.Millisecond)

	// Online wrapper "w-up" replies, "w-down" is reported by sender as offline.
	go func() {
		time.Sleep(10 * time.Millisecond)
		hub.Deliver("req-2", "w-up", proto.SearchResults{Matches: []proto.SearchMatch{{TranscriptID: "tu"}}})
	}()

	resp, err := hub.Dispatch(context.Background(), Query{
		RequestID:  "req-2",
		WrapperIDs: []string{"w-up", "w-down"},
		Body:       proto.SearchRequest{Query: "x"},
	})
	require.NoError(t, err)
	require.Equal(t, "ok", resp.ByWrapper["w-up"].Status)
	require.Equal(t, "offline", resp.ByWrapper["w-down"].Status)
	require.Len(t, resp.Matches, 1)
}

func TestDispatchTimesOutSlowWrapper(t *testing.T) {
	sender := newFakeSender()
	hub := New(sender, 80*time.Millisecond)

	go func() {
		time.Sleep(20 * time.Millisecond)
		hub.Deliver("req-3", "fast", proto.SearchResults{Matches: []proto.SearchMatch{{TranscriptID: "f"}}})
		// "slow" never replies.
	}()

	resp, err := hub.Dispatch(context.Background(), Query{
		RequestID:  "req-3",
		WrapperIDs: []string{"fast", "slow"},
		Body:       proto.SearchRequest{Query: "x"},
	})
	require.NoError(t, err)
	require.Equal(t, "ok", resp.ByWrapper["fast"].Status)
	require.Equal(t, "timeout", resp.ByWrapper["slow"].Status)
}

func TestDispatchIgnoresStrayDeliveries(t *testing.T) {
	sender := newFakeSender()
	hub := New(sender, 50*time.Millisecond)

	// No outstanding Dispatch — Deliver should be a no-op (and definitely
	// not panic).
	hub.Deliver("never-asked", "w1", proto.SearchResults{})
}
