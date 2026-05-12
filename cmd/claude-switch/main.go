package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jleal52/claude-switch/internal/auth"
	"github.com/jleal52/claude-switch/internal/config"
	"github.com/jleal52/claude-switch/internal/proto"
	"github.com/jleal52/claude-switch/internal/pty"
	"github.com/jleal52/claude-switch/internal/session"
	"github.com/jleal52/claude-switch/internal/transcripts"
	"github.com/jleal52/claude-switch/internal/ws"
)

const wrapperVersion = "0.1.0-dev"

type jobAdapter struct {
	assign func(*exec.Cmd) error
}

func (j jobAdapter) Assign(cmd *exec.Cmd) error { return j.assign(cmd) }

func main() { os.Exit(run()) }

func run() int {
	fs := flag.NewFlagSet("claude-switch", flag.ExitOnError)
	var (
		configPath = fs.String("config", "", "config file (default: platform-specific)")
		serverURL  = fs.String("server-url", "", "override server WebSocket URL")
		logLevel   = fs.String("log-level", "", "override log level")
		claudeBin  = fs.String("command", "", "override path to claude binary (testing)")
	)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage:")
		fmt.Fprintln(os.Stderr, "  claude-switch [flags]         run the wrapper")
		fmt.Fprintln(os.Stderr, "  claude-switch pair <server>   pair with a server by device code")
		fs.PrintDefaults()
	}

	// Subcommand dispatch.
	if len(os.Args) >= 2 && os.Args[1] == "pair" {
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "pair requires a server base URL")
			return 2
		}
		return runPair(signalCtx(), os.Args[2])
	}
	_ = fs.Parse(os.Args[1:])

	// Load config.
	cfgPath := *configPath
	if cfgPath == "" {
		p, err := config.DefaultPath()
		if err == nil {
			cfgPath = p
		}
	}
	cfg, err := config.LoadFromPath(cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		return 1
	}
	if *serverURL != "" {
		cfg.ServerURL = *serverURL
	}
	if *logLevel != "" {
		cfg.LogLevel = *logLevel
	}

	// Logger.
	lvl := slog.LevelInfo
	switch cfg.LogLevel {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})))

	// Load credentials (pair-if-needed is left to explicit subcommand).
	credsPath, err := auth.DefaultCredentialsPath()
	if err != nil {
		slog.Error("credentials path", "err", err)
		return 1
	}
	creds, err := auth.Load(credsPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintln(os.Stderr, "not paired — run: claude-switch pair <server-base-url>")
			return 2
		}
		slog.Error("load credentials", "err", err)
		return 1
	}
	if cfg.ServerURL == "" {
		cfg.ServerURL = creds.ServerEndpoint
	}

	// Refresher keeps the access token current for the lifetime of the
	// process: refreshes when within 10 min of expiry and exposes a Token()
	// callback that the ws client consults on every reconnect.
	serverBase := httpBaseFromWs(creds.ServerEndpoint)
	refresher := auth.NewRefresher(auth.RefresherConfig{
		ServerBase: serverBase,
		Margin:     10 * time.Minute,
		Interval:   time.Minute,
		OnSave: func(c *auth.Credentials) error {
			return auth.Save(credsPath, c)
		},
	})
	refresher.Set(creds)
	// Force a refresh upfront if we're already within the margin, so the
	// initial WS dial uses a fresh token. ErrRevoked → unrecoverable.
	if time.Now().Add(10 * time.Minute).After(creds.ExpiresAt) {
		if err := refresher.RefreshNow(context.Background()); err != nil {
			if errors.Is(err, auth.ErrRevoked) {
				_ = os.Remove(credsPath)
				fmt.Fprintln(os.Stderr, "credentials revoked — run: claude-switch pair <server-base-url>")
				return 2
			}
			slog.Error("token refresh", "err", err)
			return 1
		}
	}
	creds = refresher.Snapshot()

	// Locate claude binary.
	bin := *claudeBin
	if bin == "" {
		p, err := exec.LookPath("claude")
		if err != nil {
			fmt.Fprintln(os.Stderr, "`claude` not on PATH; use --command to override")
			return 1
		}
		bin = p
	}

	// Windows Job Object: children die when the wrapper exits.
	job, assignToJob, err := createJob()
	if err != nil {
		slog.Error("create job object", "err", err)
		return 1
	}
	defer job.Close()

	// Build supervisor.
	home, _ := os.UserHomeDir()
	claudeHome := filepath.Join(home, ".claude")
	events := make(chan session.Event, 256)
	sup := session.NewSupervisor(session.Config{
		ClaudeBin:  bin,
		Start:      pty.Start,
		ClaudeHome: claudeHome,
		Job:        jobAdapter{assign: assignToJob},
	}, events)

	// Wrapper ID: hostname + 4 hex bytes of PID.
	host, _ := os.Hostname()
	wid := fmt.Sprintf("%s-%x", filepath.Base(host), os.Getpid()&0xffff)

	// Transcripts catalog: initial scan in foreground so the very first
	// connect carries a populated catalog.diff full=true.
	catalogRoot := filepath.Join(claudeHome, "projects")
	scanCtx, scanCancel := context.WithTimeout(context.Background(), 30*time.Second)
	initialCat, err := transcripts.NewScanner(catalogRoot).Scan(scanCtx)
	scanCancel()
	if err != nil {
		slog.Warn("transcripts initial scan", "err", err)
		initialCat = newEmptyCatalog()
	}

	var catalogMu sync.RWMutex
	currentSnap := initialCat.Snapshot()
	catalogUpdates := make(chan proto.CatalogDiff, 32)

	catalogSource := func() *proto.CatalogDiff {
		catalogMu.RLock()
		defer catalogMu.RUnlock()
		d := snapshotToCatalogDiff(currentSnap)
		return &d
	}

	searcher := &transcripts.Searcher{
		Root: catalogRoot,
		Catalog: nil, // updated alongside currentSnap below
	}

	cli := ws.NewClient(ws.Config{
		URL:            cfg.ServerURL,
		TokenSource:    refresher.Token,
		WrapperID:      wid,
		Version:        wrapperVersion,
		CatalogSource:  catalogSource,
		CatalogUpdates: catalogUpdates,
		SearchExecutor: func(ctx context.Context, req proto.SearchRequest) proto.SearchResults {
			catalogMu.RLock()
			searcher.Catalog = liveCatalogFromSnapshot(currentSnap)
			catalogMu.RUnlock()
			return searcher.Search(ctx, req)
		},
	}, sup, events)

	ctx := signalCtx()
	go sup.Run(ctx)
	go func() { _ = refresher.Run(ctx) }()
	go runCatalogWatcher(ctx, catalogRoot, &catalogMu, &currentSnap, catalogUpdates)

	if err := cli.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("ws run", "err", err)
		return 1
	}
	return 0
}

func newEmptyCatalog() *transcripts.Catalog { return transcripts.NewCatalog() }

// snapshotToCatalogDiff converts a transcripts.Snapshot to a wire-ready
// proto.CatalogDiff with Full=true.
func snapshotToCatalogDiff(s *transcripts.Snapshot) proto.CatalogDiff {
	if s == nil {
		return proto.CatalogDiff{Full: true}
	}
	d := proto.CatalogDiff{Full: true}
	for _, p := range s.Projects {
		d.Projects = append(d.Projects, proto.CatalogProject{
			Slug: p.Slug, Cwd: p.Cwd, Name: p.Name,
			SessionCount:    p.SessionCount,
			FirstActivityAt: p.FirstActivityAt.UTC().Format(time.RFC3339Nano),
			LastActivityAt:  p.LastActivityAt.UTC().Format(time.RFC3339Nano),
		})
	}
	for _, t := range s.Transcripts {
		d.Transcripts = append(d.Transcripts, proto.CatalogTranscript{
			JSONLUUID: t.JSONLUUID, Slug: t.Slug, Path: t.Path,
			StartedAt:    t.StartedAt.UTC().Format(time.RFC3339Nano),
			EndedAt:      t.EndedAt.UTC().Format(time.RFC3339Nano),
			MessageCount: t.MessageCount, Title: t.Title, Bytes: t.Bytes,
		})
	}
	return d
}

// diffToCatalogDiff converts an incremental transcripts.Diff to the wire
// payload (Full=false).
func diffToCatalogDiff(d *transcripts.Diff) proto.CatalogDiff {
	if d == nil {
		return proto.CatalogDiff{}
	}
	out := proto.CatalogDiff{Full: false, RemovedTranscripts: d.RemovedTranscripts}
	for _, p := range d.UpsertProjects {
		out.Projects = append(out.Projects, proto.CatalogProject{
			Slug: p.Slug, Cwd: p.Cwd, Name: p.Name,
			SessionCount:    p.SessionCount,
			FirstActivityAt: p.FirstActivityAt.UTC().Format(time.RFC3339Nano),
			LastActivityAt:  p.LastActivityAt.UTC().Format(time.RFC3339Nano),
		})
	}
	for _, t := range d.UpsertTranscripts {
		out.Transcripts = append(out.Transcripts, proto.CatalogTranscript{
			JSONLUUID: t.JSONLUUID, Slug: t.Slug, Path: t.Path,
			StartedAt:    t.StartedAt.UTC().Format(time.RFC3339Nano),
			EndedAt:      t.EndedAt.UTC().Format(time.RFC3339Nano),
			MessageCount: t.MessageCount, Title: t.Title, Bytes: t.Bytes,
		})
	}
	return out
}

// liveCatalogFromSnapshot rebuilds a Catalog from a Snapshot so the
// Searcher can use it without a long-lived reference to the watcher.
func liveCatalogFromSnapshot(s *transcripts.Snapshot) *transcripts.Catalog {
	c := transcripts.CatalogFromSnapshot(s)
	return c
}

// runCatalogWatcher drives the polling watcher: it updates currentSnap
// and pushes incremental diffs onto the wire channel.
func runCatalogWatcher(ctx context.Context, root string, mu *sync.RWMutex, snap **transcripts.Snapshot, out chan<- proto.CatalogDiff) {
	w := &transcripts.Watcher{Root: root, Interval: transcripts.DefaultWatchInterval}
	seenFirst := false
	_ = w.Run(ctx, func(u transcripts.Update) {
		mu.Lock()
		*snap = u.Snapshot
		mu.Unlock()
		if !seenFirst {
			// Watcher's first tick replicates main's initial Scan; the
			// next reconnect's CatalogSource will pick it up.
			seenFirst = true
			return
		}
		select {
		case out <- diffToCatalogDiff(u.Diff):
		case <-ctx.Done():
		}
	})
}

func signalCtx() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() { <-ch; cancel() }()
	return ctx
}

// httpBaseFromWs strips ws:// or wss:// scheme and path, returning just
// scheme://host[:port]. The /device/token/refresh endpoint is rooted at
// the server, not at the wrapper WS path, so we must drop the entire
// path component.
func httpBaseFromWs(endpoint string) string {
	base := endpoint
	if strings.HasPrefix(base, "wss://") {
		base = "https://" + base[len("wss://"):]
	} else if strings.HasPrefix(base, "ws://") {
		base = "http://" + base[len("ws://"):]
	}
	// Find the first '/' after "scheme://" and trim from there onward.
	schemeEnd := strings.Index(base, "://")
	if schemeEnd < 0 {
		return base
	}
	hostStart := schemeEnd + 3
	if i := strings.Index(base[hostStart:], "/"); i >= 0 {
		base = base[:hostStart+i]
	}
	return base
}
