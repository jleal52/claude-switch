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
	"syscall"
	"time"

	"github.com/jleal52/claude-switch/internal/auth"
	"github.com/jleal52/claude-switch/internal/config"
	"github.com/jleal52/claude-switch/internal/pty"
	"github.com/jleal52/claude-switch/internal/session"
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

	// Refresh access token if it's near expiry (5 min margin) or expired.
	if time.Now().Add(5 * time.Minute).After(creds.ExpiresAt) {
		serverBase := httpBaseFromWs(creds.ServerEndpoint)
		refreshed, err := auth.Refresh(context.Background(), serverBase, creds.RefreshToken, nil)
		if err != nil {
			if errors.Is(err, auth.ErrRevoked) {
				_ = os.Remove(credsPath)
				fmt.Fprintln(os.Stderr, "credentials revoked — run: claude-switch pair <server-base-url>")
				return 2
			}
			slog.Error("token refresh", "err", err)
			return 1
		}
		refreshed.ServerEndpoint = creds.ServerEndpoint
		if err := auth.Save(credsPath, refreshed); err != nil {
			slog.Error("save refreshed creds", "err", err)
			return 1
		}
		creds = refreshed
	}

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

	cli := ws.NewClient(ws.Config{
		URL:       cfg.ServerURL,
		Token:     creds.AccessToken,
		WrapperID: wid,
		Version:   wrapperVersion,
	}, sup, events)

	ctx := signalCtx()
	go sup.Run(ctx)
	if err := cli.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("ws run", "err", err)
		return 1
	}
	return 0
}

func signalCtx() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() { <-ch; cancel() }()
	return ctx
}

func httpBaseFromWs(endpoint string) string {
	base := endpoint
	if strings.HasPrefix(base, "wss://") {
		base = "https://" + base[len("wss://"):]
	} else if strings.HasPrefix(base, "ws://") {
		base = "http://" + base[len("ws://"):]
	}
	if i := strings.LastIndex(base, "/"); i > len("https://") {
		base = base[:i]
	}
	return base
}
