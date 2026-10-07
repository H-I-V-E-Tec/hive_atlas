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

func Run(args []string, in io.Reader, out, stderr io.Writer) int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	cmd := "mcp"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd = args[0]
		args = args[1:]
	}
	if cmd == "help" || cmd == "--help" {
		fmt.Fprintln(out, "hive atlas [mcp|version|doctor|setup|watch|serve|push|eval]\nRemote MCP: <center>/atlas/mcp, shared JWT from hive login.\nUse hive login, hive atlas setup --client codex, or --offline for development.")
		return 0
	}
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	center := fs.String("center-url", "", "HIVE Center HTTPS URL")
	atlasURL := fs.String("atlas-url", "", "Atlas API HTTPS URL")
	libPath := fs.String("library", "", "reviewed library file (default: embedded)")
	offline := fs.Bool("offline", false, "use the embedded/local library")
	jsonOut := fs.Bool("json", false, "JSON output")
	addr := fs.String("addr", env("ATLAS_HTTP_ADDR", "127.0.0.1:8444"), "HTTP listen address (serve)")
	local := fs.Bool("local", false, "serve the embedded/local library (development)")
	output := fs.String("output", "", "output file")
	clientName := fs.String("client", "codex", "agent: codex, claude-code, claude-desktop")
	configFile := fs.String("config", "", "agent config file override")
	program := fs.String("program-id", "", "watcher program scope")
	asset := fs.String("asset", "", "watcher asset")
	board := fs.String("board", "board.md", "watcher board path")
	interval := fs.Duration("interval", 2*time.Second, "watch polling interval")
	casesFile := fs.String("cases", "eval/cases.json", "evaluation cases")
	var sources sourceFlags
	fs.Var(&sources, "source", "JSONL evidence source (repeatable)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stderr, "unexpected arguments")
		return 2
	}
	for k, v := range map[string]string{"HIVE_CENTER_URL": *center, "HIVE_ATLAS_URL": *atlasURL, "ATLAS_LIBRARY": *libPath} {
		if v != "" {
			_ = os.Setenv(k, v)
		}
	}
	if *offline {
		_ = os.Setenv("ATLAS_OFFLINE", "1")
	}
	var err error
	switch cmd {
	case "version":
		_ = jsonOut
		err = json.NewEncoder(out).Encode(map[string]string{"product": "atlas", "version": Version, "revision": Revision, "runtime": "go"})
	case "login":
		err = login(ctx, in, stderr)
	case "logout":
		err = fmt.Errorf("authentication is shared across HIVE products; run hive logout")
	case "setup":
		err = setup(*clientName, *configFile)
	case "doctor":
		var remote *RemoteMCP
		remote, err = remoteMCP()
		if err == nil {
			err = remote.Initialize(ctx)
		}
		if err == nil {
			_, err = remote.Observe(ctx, []core.Evidence{})
		}
		if err == nil {
			err = json.NewEncoder(out).Encode(map[string]any{"ok": true, "version": Version, "transport": "streamable-http", "endpoint": remote.Remote.URL + "/mcp"})
		}
	case "mcp":
		err = checkLicense()
		if err == nil {
			if os.Getenv("ATLAS_OFFLINE") == "1" {
				var b transport.Backend
				b, err = backend()
				if err == nil {
					err = (&MCP{Backend: b}).Run(ctx, in, out)
				}
			} else {
				var remote *RemoteMCP
				remote, err = remoteMCP()
				if err == nil {
					err = remote.Run(ctx, in, out)
				}
			}
		}
	case "serve":
		var b transport.Backend
		if *local {
			var lib *core.Library
			lib, err = localLibrary(*libPath)
			b = transport.Local{Lib: lib}
		} else {
			b, err = transport.StoreFromEnv()
		}
		if err == nil {
			_, err = b.Library(ctx)
		}
		var auth *transport.Authorizer
		if err == nil {
			var center string
			center, err = centerURL()
			if err == nil {
				auth, err = transport.NewAuthorizer(center)
			}
		}
		if err == nil {
			mux := http.NewServeMux()
			origins := strings.Fields(os.Getenv("ATLAS_ALLOWED_ORIGINS"))
			if publicURL := os.Getenv("HIVE_ATLAS_URL"); publicURL != "" {
				origins = append(origins, publicURL)
			}
			mux.Handle("/mcp", MCPHTTP(b, auth, env("ATLAS_STATE_DIR", filepath.Join(hiveHome(), "atlas", "server")), origins))
			mux.Handle("/", transport.API(b, auth, Version))
			s := &http.Server{Addr: *addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 120 * time.Second, MaxHeaderBytes: 32768}
			go func() {
				<-ctx.Done()
				shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = s.Shutdown(shutdown)
			}()
			fmt.Fprintf(stderr, "Atlas remote MCP listening on %s/mcp\n", *addr)
			err = s.ListenAndServe()
			if errors.Is(err, http.ErrServerClosed) {
				err = nil
			}
		}
	case "push":
		var lib *core.Library
		lib, err = localLibrary(*libPath)
		if err == nil {
			var store *transport.Store
			store, err = transport.StoreFromEnv()
			if err == nil {
				err = store.Push(ctx, lib)
				if err == nil {
					err = json.NewEncoder(out).Encode(map[string]any{"ok": true, "signals": len(lib.Signals), "collection": store.Collection})
				}
			}
		}
	case "mint-reader":
		if *output == "" {
			err = fmt.Errorf("--output required")
			break
		}
		var store *transport.Store
		store, err = transport.StoreFromEnv()
		if err != nil {
			break
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"exp": time.Now().Add(365 * 24 * time.Hour).Unix(), "access": []any{map[string]string{"collection": store.Collection, "access": "r"}}})
		var signed string
		signed, err = token.SignedString([]byte(store.Key))
		if err == nil {
			err = privateWrite(*output, []byte(signed+"\n"))
		}
	case "watch":
		if len(sources) == 0 && os.Getenv("ATLAS_SOURCE") != "" {
			sources = filepath.SplitList(os.Getenv("ATLAS_SOURCE"))
		}
		if len(sources) == 0 || *program == "" || *interval <= 0 {
			err = fmt.Errorf("watch requires --source, --program-id and positive --interval")
			break
		}
		w := core.Watcher{Sources: sources, BoardPath: *board, Adapter: core.Adapter{ProgramID: *program, Asset: *asset}}
		if os.Getenv("ATLAS_OFFLINE") == "1" {
			w.Library, err = localLibrary(*libPath)
		} else {
			var remote *RemoteMCP
			remote, err = remoteMCP()
			if err == nil {
				err = remote.Initialize(ctx)
			}
			if err == nil {
				w.Recommend = func(events []core.Evidence) (core.Board, error) { return remote.Observe(ctx, events) }
			}
		}
		if err != nil {
			break
		}
		ticker := time.NewTicker(*interval)
		defer ticker.Stop()
		for err == nil {
			_, err = w.Poll()
			if err != nil {
				break
			}
			select {
			case <-ctx.Done():
				return 0
			case <-ticker.C:
			}
		}
	case "eval":
		err = evaluate(*casesFile, *libPath, out)
	default:
		err = fmt.Errorf("unknown Atlas command: %s", cmd)
	}
	if err != nil {
		fmt.Fprintln(stderr, "atlas:", err)
		return 1
	}
	return 0
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
