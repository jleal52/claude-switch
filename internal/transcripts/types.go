// Package transcripts scans Claude's local history under ~/.claude/projects/
// and produces a Catalog of projects and JSONL transcripts that the wrapper
// reports to the server. Pure logic: no networking, no global state.
package transcripts

import "time"

// Project corresponds to one sub-directory of ~/.claude/projects/. The slug is
// the directory name as Claude writes it; cwd is the absolute working directory
// recovered from events inside the transcripts (slug → cwd is not reversible
// because path components may themselves contain '-').
type Project struct {
	Slug            string    `json:"slug"`
	Cwd             string    `json:"cwd"`
	Name            string    `json:"name"`
	SessionCount    int       `json:"session_count"`
	FirstActivityAt time.Time `json:"first_activity_at"`
	LastActivityAt  time.Time `json:"last_activity_at"`
}

// Transcript is one JSONL file inside a project directory.
type Transcript struct {
	JSONLUUID    string    `json:"jsonl_uuid"`
	Slug         string    `json:"slug"`
	Path         string    `json:"path"`
	StartedAt    time.Time `json:"started_at"`
	EndedAt      time.Time `json:"ended_at"`
	MessageCount int       `json:"message_count"`
	Title        string    `json:"title"`
	Bytes        int64     `json:"bytes"`
}

// Snapshot is an immutable view of a Catalog at a point in time. Returned by
// Catalog.Snapshot and accepted by Catalog.Diff to compute deltas.
type Snapshot struct {
	Projects    []*Project    `json:"projects"`
	Transcripts []*Transcript `json:"transcripts"`
}

// Diff is what the wrapper sends as a `catalog.diff full=false` payload. Full
// snapshots are sent as a Snapshot, not a Diff.
type Diff struct {
	UpsertProjects     []*Project    `json:"upsert_projects"`
	UpsertTranscripts  []*Transcript `json:"upsert_transcripts"`
	RemovedTranscripts []string      `json:"removed_transcripts"`
}
