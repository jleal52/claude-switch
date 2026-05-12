// Package searchhub dispatches search.request frames to a set of wrappers
// over the hub and aggregates the search.results that come back, with a
// hard timeout for slow or offline peers. It is the server-side bridge
// between the HTTP /api/search endpoint and the per-wrapper RPC.
package searchhub

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/jleal52/claude-switch/internal/proto"
)

// ErrWrapperOffline mirrors the hub's sentinel so callers don't have to
// import the hub package.
var ErrWrapperOffline = errors.New("searchhub: wrapper offline")

// Sender abstracts the bit of the hub the searchhub needs. The real hub
// implements this directly; tests pass a fake.
type Sender interface {
	SendSearchRequest(wrapperID, requestID string, body any) error
}

// Hub is the dispatcher. Construct one per server process. Safe for
// concurrent Dispatch / Deliver from many goroutines.
type Hub struct {
	sender  Sender
	timeout time.Duration

	mu       sync.Mutex
	awaiters map[string]*awaiter // requestID → awaiter
}

func New(sender Sender, timeout time.Duration) *Hub {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &Hub{
		sender:   sender,
		timeout:  timeout,
		awaiters: map[string]*awaiter{},
	}
}

// Query is a single search dispatched to a set of wrappers.
type Query struct {
	RequestID  string
	WrapperIDs []string // wrappers to fan out to
	Body       proto.SearchRequest
}

// Response aggregates results from all wrappers that participated.
type Response struct {
	Matches   []proto.SearchMatch
	ByWrapper map[string]WrapperStatus
}

// WrapperStatus is the per-wrapper outcome for a search.
type WrapperStatus struct {
	Status    string // "ok" | "offline" | "timeout" | "error"
	Count     int
	ElapsedMs int64
	Error     string `json:",omitempty"`
}

type deliveredResult struct {
	wrapperID string
	results   proto.SearchResults
}

type awaiter struct {
	expected map[string]bool // wrapperID → still waiting
	results  chan deliveredResult
}

// Dispatch sends the query to every wrapper in WrapperIDs, waits up to the
// configured timeout, and returns the aggregated response. Online wrappers
// that don't answer in time are reported with status "timeout"; ones the
// sender reports as offline are "offline".
func (h *Hub) Dispatch(ctx context.Context, q Query) (*Response, error) {
	if q.RequestID == "" {
		return nil, errors.New("searchhub: empty RequestID")
	}

	a := &awaiter{
		expected: make(map[string]bool, len(q.WrapperIDs)),
		results:  make(chan deliveredResult, len(q.WrapperIDs)),
	}
	for _, wid := range q.WrapperIDs {
		a.expected[wid] = true
	}
	h.mu.Lock()
	h.awaiters[q.RequestID] = a
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.awaiters, q.RequestID)
		h.mu.Unlock()
	}()

	resp := &Response{ByWrapper: map[string]WrapperStatus{}}
	// Fan out. Synchronous Send is fine: the wrapper conn write is bounded
	// and an error here is immediate (offline / closed).
	for _, wid := range q.WrapperIDs {
		if err := h.sender.SendSearchRequest(wid, q.RequestID, q.Body); err != nil {
			delete(a.expected, wid)
			if errors.Is(err, ErrWrapperOffline) {
				resp.ByWrapper[wid] = WrapperStatus{Status: "offline"}
			} else {
				resp.ByWrapper[wid] = WrapperStatus{Status: "error", Error: err.Error()}
			}
		}
	}

	timer := time.NewTimer(h.timeout)
	defer timer.Stop()
	for len(a.expected) > 0 {
		select {
		case <-ctx.Done():
			// Mark everything still pending as timeout.
			for wid := range a.expected {
				resp.ByWrapper[wid] = WrapperStatus{Status: "timeout"}
			}
			return resp, nil
		case <-timer.C:
			for wid := range a.expected {
				resp.ByWrapper[wid] = WrapperStatus{Status: "timeout"}
			}
			a.expected = nil
		case d := <-a.results:
			delete(a.expected, d.wrapperID)
			resp.Matches = append(resp.Matches, d.results.Matches...)
			resp.ByWrapper[d.wrapperID] = WrapperStatus{
				Status:    "ok",
				Count:     len(d.results.Matches),
				ElapsedMs: d.results.ElapsedMs,
			}
		}
	}
	// Sort matches deterministically for the API response.
	sort.SliceStable(resp.Matches, func(i, j int) bool {
		if resp.Matches[i].TranscriptID == resp.Matches[j].TranscriptID {
			return resp.Matches[i].MsgIndex < resp.Matches[j].MsgIndex
		}
		return resp.Matches[i].TranscriptID < resp.Matches[j].TranscriptID
	})
	return resp, nil
}

// Deliver routes a search.results frame to a waiting Dispatch. Called by
// wswrapper when a wrapper sends search.results. Stray deliveries (no
// matching awaiter) are dropped silently.
func (h *Hub) Deliver(requestID, wrapperID string, results proto.SearchResults) {
	h.mu.Lock()
	a, ok := h.awaiters[requestID]
	h.mu.Unlock()
	if !ok {
		return
	}
	select {
	case a.results <- deliveredResult{wrapperID: wrapperID, results: results}:
	default:
		// Channel is sized to len(WrapperIDs); falling here means a stray
		// duplicate delivery — drop it.
	}
}
