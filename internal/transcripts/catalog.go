package transcripts

import (
	"sort"
	"sync"
	"time"
)

// Catalog holds the current view of projects and transcripts in memory. Safe
// for concurrent reads via the methods below; writes are coordinated by the
// scanner/watcher pair that owns the instance.
type Catalog struct {
	mu          sync.RWMutex
	projects    map[string]*Project    // key: slug
	transcripts map[string]*Transcript // key: jsonl_uuid
}

func newCatalog() *Catalog {
	return &Catalog{
		projects:    map[string]*Project{},
		transcripts: map[string]*Transcript{},
	}
}

// NewCatalog returns an empty Catalog. Useful as a placeholder before the
// first scan completes.
func NewCatalog() *Catalog { return newCatalog() }

// CatalogFromSnapshot rebuilds a Catalog from a Snapshot. Used by callers
// that hold the latest snapshot (e.g. the wrapper's search executor) and
// need to hand a Searcher a queryable index.
func CatalogFromSnapshot(s *Snapshot) *Catalog {
	c := newCatalog()
	if s == nil {
		return c
	}
	for _, p := range s.Projects {
		cp := *p
		c.projects[p.Slug] = &cp
	}
	for _, t := range s.Transcripts {
		ct := *t
		c.transcripts[t.JSONLUUID] = &ct
	}
	return c
}

// PutTranscript inserts or replaces a transcript and updates its project's
// aggregate fields (session_count, first/last activity).
func (c *Catalog) PutTranscript(tr *Transcript, projCwd string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.transcripts[tr.JSONLUUID] = tr
	c.recomputeProject(tr.Slug, projCwd)
}

// RemoveTranscript deletes a transcript and updates its project's aggregates.
// If the project ends up with no transcripts left, it is removed too.
func (c *Catalog) RemoveTranscript(jsonlUUID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	tr, ok := c.transcripts[jsonlUUID]
	if !ok {
		return
	}
	delete(c.transcripts, jsonlUUID)
	c.recomputeProject(tr.Slug, "")
}

// recomputeProject walks every transcript of slug and refreshes the project
// row. preferCwd, when non-empty, is taken as the project's cwd when there
// is no existing one and the transcripts do not carry one either.
func (c *Catalog) recomputeProject(slug, preferCwd string) {
	var (
		count int
		first time.Time
		last  time.Time
	)
	for _, tr := range c.transcripts {
		if tr.Slug != slug {
			continue
		}
		count++
		if first.IsZero() || tr.StartedAt.Before(first) {
			first = tr.StartedAt
		}
		if last.IsZero() || tr.EndedAt.After(last) {
			last = tr.EndedAt
		}
	}
	if count == 0 {
		delete(c.projects, slug)
		return
	}
	p, ok := c.projects[slug]
	if !ok {
		p = &Project{Slug: slug}
		c.projects[slug] = p
	}
	if preferCwd != "" {
		p.Cwd = preferCwd
		p.Name = baseName(preferCwd)
	}
	p.SessionCount = count
	p.FirstActivityAt = first
	p.LastActivityAt = last
}

// Snapshot returns a sorted, immutable view of the current catalog. Slices
// are freshly allocated; callers may freely retain the result.
func (c *Catalog) Snapshot() *Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	projects := make([]*Project, 0, len(c.projects))
	for _, p := range c.projects {
		cp := *p
		projects = append(projects, &cp)
	}
	sort.Slice(projects, func(i, j int) bool { return projects[i].Slug < projects[j].Slug })
	transcripts := make([]*Transcript, 0, len(c.transcripts))
	for _, t := range c.transcripts {
		ct := *t
		transcripts = append(transcripts, &ct)
	}
	sort.Slice(transcripts, func(i, j int) bool { return transcripts[i].JSONLUUID < transcripts[j].JSONLUUID })
	return &Snapshot{Projects: projects, Transcripts: transcripts}
}

// Diff computes the changes between a previous snapshot and the catalog's
// current state. Used by the wrapper to emit `catalog.diff full=false`.
func (c *Catalog) Diff(prev *Snapshot) *Diff {
	curr := c.Snapshot()
	out := &Diff{}

	prevProjects := map[string]*Project{}
	if prev != nil {
		for _, p := range prev.Projects {
			prevProjects[p.Slug] = p
		}
	}
	for _, p := range curr.Projects {
		old, ok := prevProjects[p.Slug]
		if !ok || !projectsEqual(old, p) {
			out.UpsertProjects = append(out.UpsertProjects, p)
		}
	}

	prevTranscripts := map[string]*Transcript{}
	if prev != nil {
		for _, t := range prev.Transcripts {
			prevTranscripts[t.JSONLUUID] = t
		}
	}
	for _, t := range curr.Transcripts {
		old, ok := prevTranscripts[t.JSONLUUID]
		if !ok || !transcriptsEqual(old, t) {
			out.UpsertTranscripts = append(out.UpsertTranscripts, t)
		}
		delete(prevTranscripts, t.JSONLUUID)
	}
	for uuid := range prevTranscripts {
		out.RemovedTranscripts = append(out.RemovedTranscripts, uuid)
	}
	sort.Strings(out.RemovedTranscripts)
	return out
}

func projectsEqual(a, b *Project) bool {
	return a.Slug == b.Slug &&
		a.Cwd == b.Cwd &&
		a.Name == b.Name &&
		a.SessionCount == b.SessionCount &&
		a.FirstActivityAt.Equal(b.FirstActivityAt) &&
		a.LastActivityAt.Equal(b.LastActivityAt)
}

func transcriptsEqual(a, b *Transcript) bool {
	return a.JSONLUUID == b.JSONLUUID &&
		a.Slug == b.Slug &&
		a.Path == b.Path &&
		a.StartedAt.Equal(b.StartedAt) &&
		a.EndedAt.Equal(b.EndedAt) &&
		a.MessageCount == b.MessageCount &&
		a.Title == b.Title &&
		a.Bytes == b.Bytes
}

func baseName(p string) string {
	if p == "" {
		return ""
	}
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[i+1:]
		}
	}
	return p
}
