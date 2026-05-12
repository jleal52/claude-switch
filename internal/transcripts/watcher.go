package transcripts

import (
	"context"
	"time"
)

// DefaultWatchInterval is the polling cadence when none is set. 30 s gives a
// good UX/CPU trade-off: portal feels current without the wrapper hammering
// the filesystem.
const DefaultWatchInterval = 30 * time.Second

// Update is what the wrapper feeds into its `catalog.diff` emitter.
//
// On the first tick after Run starts, Full is true and Diff is nil — the
// caller emits a `catalog.diff full=true` payload from Snapshot. On
// subsequent ticks Full is false and Diff carries only what changed since
// the previous tick.
type Update struct {
	Snapshot *Snapshot
	Diff     *Diff
	Full     bool
}

// Watcher periodically re-scans Root and emits an Update whenever the
// catalog changes. Implemented as polling for V1 (cross-platform, no
// dependencies, deterministic). fsnotify can be layered on later without
// changing this API: a notify event would just shorten the time until the
// next poll.
type Watcher struct {
	Root     string
	Interval time.Duration
}

// Run drives the poll loop until ctx is cancelled. onUpdate is invoked
// synchronously from the loop, so a slow handler delays the next poll.
func (w *Watcher) Run(ctx context.Context, onUpdate func(Update)) error {
	interval := w.Interval
	if interval <= 0 {
		interval = DefaultWatchInterval
	}
	scanner := NewScanner(w.Root)

	// First scan: emit full snapshot.
	cat, err := scanner.Scan(ctx)
	if err != nil {
		return err
	}
	prev := cat.Snapshot()
	onUpdate(Update{Snapshot: prev, Full: true})

	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			cat, err := scanner.Scan(ctx)
			if err != nil {
				continue
			}
			curr := cat.Snapshot()
			d := cat.Diff(prev)
			if len(d.UpsertProjects) == 0 && len(d.UpsertTranscripts) == 0 && len(d.RemovedTranscripts) == 0 {
				continue
			}
			onUpdate(Update{Snapshot: curr, Diff: d})
			prev = curr
		}
	}
}
