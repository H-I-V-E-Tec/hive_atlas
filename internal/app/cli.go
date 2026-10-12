package app

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/H-I-V-E-Tec/hive_atlas/internal/core"
	"github.com/H-I-V-E-Tec/hive_atlas/internal/transport"
	"github.com/golang-jwt/jwt/v5"
)

const usage = "hive atlas [mcp|version|doctor|setup|watch|serve|push|eval]\n" +
	"Remote MCP: <center>/atlas/mcp, shared JWT from hive login.\n" +
	"Use hive login, hive atlas setup --client codex, or --offline for development."

// options carries the parsed flags. Every command emits JSON, so --json is
// accepted for compatibility and never read.
type options struct {
	library, addr, output, client, config string
	program, asset, board, cases          string
	local, offline, jsonOut               bool
	interval                              time.Duration
	sources                               sourceFlags
}

func Run(args []string, in io.Reader, out, stderr io.Writer) int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	cmd := "mcp"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	if cmd == "help" || cmd == "--help" {
		fmt.Fprintln(out, usage)
		return 0
	}
	var o options
	var center, atlas string
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&center, "center-url", "", "HIVE Center HTTPS URL")
	fs.StringVar(&atlas, "atlas-url", "", "Atlas API HTTPS URL")
	fs.StringVar(&o.library, "library", "", "reviewed library file (default: embedded)")
	fs.BoolVar(&o.offline, "offline", false, "use the embedded/local library")
	fs.BoolVar(&o.jsonOut, "json", false, "JSON output")
	fs.StringVar(&o.addr, "addr", env("ATLAS_HTTP_ADDR", "127.0.0.1:8444"), "HTTP listen address (serve)")
	fs.BoolVar(&o.local, "local", false, "serve the embedded/local library (development)")
	fs.StringVar(&o.output, "output", "", "output file")
	fs.StringVar(&o.client, "client", "codex", "agent: codex, claude-code, claude-desktop")
	fs.StringVar(&o.config, "config", "", "agent config file override")
	fs.StringVar(&o.program, "program-id", "", "watcher program scope")
	fs.StringVar(&o.asset, "asset", "", "watcher asset")
	fs.StringVar(&o.board, "board", "board.md", "watcher board path")
	fs.DurationVar(&o.interval, "interval", 2*time.Second, "watch polling interval")
	fs.StringVar(&o.cases, "cases", "eval/cases.json", "evaluation cases")
	fs.Var(&o.sources, "source", "JSONL evidence source (repeatable)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stderr, "unexpected arguments")
		return 2
	}
	for key, value := range map[string]string{"HIVE_CENTER_URL": center, "HIVE_ATLAS_URL": atlas, "ATLAS_LIBRARY": o.library} {
		if value != "" {
			_ = os.Setenv(key, value)
		}
	}
	if o.offline {
		_ = os.Setenv("ATLAS_OFFLINE", "1")
	}
	if err := dispatch(ctx, cmd, &o, in, out, stderr); err != nil {
		fmt.Fprintln(stderr, "atlas:", err)
		return 1
	}
	return 0
}

// dispatch routes one command. Each command returns its own error, so a failure
// reads as a return instead of a chain of "if err == nil" blocks.
func dispatch(ctx context.Context, cmd string, o *options, in io.Reader, out, stderr io.Writer) error {
	switch cmd {
	case "version":
		return json.NewEncoder(out).Encode(map[string]string{"product": "atlas", "version": Version, "revision": Revision, "runtime": "go"})
	case "login":
		return login(ctx, in, stderr)
	case "logout":
		return fmt.Errorf("authentication is shared across HIVE products; run hive logout")
	case "setup":
		return setup(o.client, o.config)
	case "doctor":
		return doctor(ctx, out)
	case "mcp":
		return serveStdio(ctx, in, out)
	case "serve":
		return serveHTTP(ctx, o, stderr)
	case "push":
		return pushLibrary(ctx, o, out)
	case "mint-reader":
		return mintReader(o)
	case "watch":
		return watch(ctx, o, stderr)
	case "eval":
		return evaluate(o.cases, o.library, out)
	}
	return fmt.Errorf("unknown Atlas command: %s", cmd)
}

func doctor(ctx context.Context, out io.Writer) error {
	remote, err := remoteMCP()
	if err != nil {
		return err
	}
	if err := remote.Initialize(ctx); err != nil {
		return err
	}
	if _, err := remote.Observe(ctx, []core.Evidence{}); err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(map[string]any{"ok": true, "version": Version,
		"transport": "streamable-http", "endpoint": remote.Remote.URL + "/mcp"})
}

// serveStdio speaks MCP over stdio: offline it runs the engine locally, online
// it only bridges to the remote endpoint.
func serveStdio(ctx context.Context, in io.Reader, out io.Writer) error {
	if os.Getenv("ATLAS_OFFLINE") == "1" {
		b, err := backend()
		if err != nil {
			return err
		}
		return (&MCP{Backend: b}).Run(ctx, in, out)
	}
	remote, err := remoteMCP()
	if err != nil {
		return err
	}
	return remote.Run(ctx, in, out)
}

func serveHTTP(ctx context.Context, o *options, stderr io.Writer) error {
	var b transport.Backend
	if o.local {
		lib, err := localLibrary(o.library)
		if err != nil {
			return err
		}
		b = transport.Local{Lib: lib}
	} else {
		store, err := transport.StoreFromEnv()
		if err != nil {
			return err
		}
		b = store
	}
	if _, err := b.Library(ctx); err != nil {
		return err
	}
	center, err := centerURL()
	if err != nil {
		return err
	}
	auth, err := transport.NewAuthorizer(center)
	if err != nil {
		return err
	}
	origins := strings.Fields(os.Getenv("ATLAS_ALLOWED_ORIGINS"))
	if public := os.Getenv("HIVE_ATLAS_URL"); public != "" {
		origins = append(origins, public)
	}
	mux := http.NewServeMux()
	mux.Handle("/mcp", MCPHTTP(b, auth, env("ATLAS_STATE_DIR", filepath.Join(hiveHome(), "atlas", "server")), origins))
	mux.Handle("/", transport.API(b, auth, Version))
	server := &http.Server{Addr: o.addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 120 * time.Second,
		MaxHeaderBytes: 32768}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	fmt.Fprintf(stderr, "Atlas remote MCP listening on %s/mcp\n", o.addr)
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func pushLibrary(ctx context.Context, o *options, out io.Writer) error {
	lib, err := localLibrary(o.library)
	if err != nil {
		return err
	}
	store, err := transport.StoreFromEnv()
	if err != nil {
		return err
	}
	if err := store.Push(ctx, lib); err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(map[string]any{"ok": true, "signals": len(lib.Signals), "collection": store.Collection})
}

func mintReader(o *options) error {
	if o.output == "" {
		return fmt.Errorf("--output required")
	}
	store, err := transport.StoreFromEnv()
	if err != nil {
		return err
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"exp":    time.Now().Add(365 * 24 * time.Hour).Unix(),
		"access": []any{map[string]string{"collection": store.Collection, "access": "r"}}})
	signed, err := token.SignedString([]byte(store.Key))
	if err != nil {
		return err
	}
	return privateWrite(o.output, []byte(signed+"\n"))
}

func watch(ctx context.Context, o *options, stderr io.Writer) error {
	sources := o.sources
	if len(sources) == 0 && os.Getenv("ATLAS_SOURCE") != "" {
		sources = filepath.SplitList(os.Getenv("ATLAS_SOURCE"))
	}
	if len(sources) == 0 || o.program == "" || o.interval <= 0 {
		return fmt.Errorf("watch requires --source, --program-id and positive --interval")
	}
	w := core.Watcher{Sources: sources, BoardPath: o.board,
		Adapter: core.Adapter{ProgramID: o.program, Asset: o.asset}}
	if os.Getenv("ATLAS_OFFLINE") == "1" {
		lib, err := localLibrary(o.library)
		if err != nil {
			return err
		}
		w.Library = lib
	} else {
		remote, err := remoteMCP()
		if err != nil {
			return err
		}
		if err := remote.Initialize(ctx); err != nil {
			return err
		}
		w.Recommend = func(events []core.Evidence) (core.Board, error) { return remote.Observe(ctx, events) }
	}
	ticker := time.NewTicker(o.interval)
	defer ticker.Stop()
	// Report the running refusal totals, so silently dropped input is visible.
	// Counts only: refused content is never logged.
	var reported core.Refusals
	for {
		if _, err := w.Poll(); err != nil {
			return err
		}
		if r := w.Refusals(); r != reported {
			fmt.Fprintf(stderr, "atlas: dropped input - other program: %d, credential: %d, invalid: %d, outside window: %d\n",
				r.Program, r.Secret, r.Invalid, r.Window)
			reported = r
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

type sourceFlags []string

func (s *sourceFlags) String() string     { return strings.Join(*s, ",") }
func (s *sourceFlags) Set(v string) error { *s = append(*s, v); return nil }

func evaluate(path, libraryPath string, out io.Writer) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var suite struct {
		Cases []struct {
			ID        string          `json:"id"`
			Evidence  []core.Evidence `json:"evidence"`
			Expected  []string        `json:"expected"`
			Forbidden []string        `json:"forbidden"`
		} `json:"cases"`
	}
	if json.Unmarshal(data, &suite) != nil {
		return fmt.Errorf("invalid evaluation cases")
	}
	lib, err := localLibrary(libraryPath)
	if err != nil {
		return err
	}
	tp, fp, fn, regressions := 0, 0, 0, 0
	for _, c := range suite.Cases {
		leads := map[string]bool{}
		for _, r := range lib.Recommend(c.Evidence, 0).Leads {
			leads[r.ID] = true
		}
		expected := map[string]bool{}
		for _, id := range c.Expected {
			expected[id] = true
			if leads[id] {
				tp++
			} else {
				fn++
			}
		}
		for id := range leads {
			if !expected[id] {
				fp++
			}
		}
		for _, id := range c.Forbidden {
			if leads[id] {
				regressions++
			}
		}
	}
	if err := json.NewEncoder(out).Encode(map[string]int{"cases": len(suite.Cases), "tp": tp, "fp": fp, "fn": fn, "regressions": regressions}); err != nil {
		return err
	}
	if fp > 0 || fn > 0 || regressions > 0 {
		return fmt.Errorf("evaluation failed")
	}
	return nil
}
