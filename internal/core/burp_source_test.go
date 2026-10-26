package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testBurpSession = "11111111-1111-4111-8111-111111111111"
const testBurpSource = "22222222-2222-4222-8222-222222222222.jsonl"

func testManifest(now time.Time) burpManifest {
	return burpManifest{SchemaVersion: 1, ProgramID: "demo", SessionID: testBurpSession,
		Generation: 1, ScopeRevision: strings.Repeat("a", 64), State: "active",
		UpdatedAt: now.UTC().Format(time.RFC3339Nano), ExpiresAt: now.Add(time.Hour).UTC().Format(time.RFC3339Nano),
		FileName: testBurpSource}
}

func writeTestJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func testBurpEvent(id, kind string) map[string]any {
	return map[string]any{"schema_version": 1, "event_id": id, "ts": "2026-10-26T12:00:00Z",
		"program_id": "demo", "asset": "api.demo.invalid", "kind": kind, "endpoint": "/v1/users",
		"method": "GET", "role": nil, "flow": nil, "object": nil, "value": "GET /v1/users",
		"ref": "hive_burp:demo/" + testBurpSession + "/33333333-3333-4333-8333-333333333333"}
}

func eventLine(t *testing.T, event any) string {
	t.Helper()
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	return string(data) + "\n"
}

func burpWatcher(t *testing.T) (*Watcher, string, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(dir, "source.json")
	board := filepath.Join(dir, "board.md")
	writeTestJSON(t, manifest, testManifest(time.Now()))
	w := &Watcher{ManifestPath: manifest, BoardPath: board, Adapter: Adapter{ProgramID: "demo"},
		Recommend: func(events []Evidence) (Board, error) { return Board{Board: events[len(events)-1].Kind}, nil }}
	return w, filepath.Join(dir, testBurpSource), board
}

// ATLAS-BURP-AC01 / SPEC-009-AC01/02: complete v1 lines are unique even after
// leaving the bounded recommendation window; a partial JSON line waits.
func TestBurpWatcherDeduplicatesAndWaitsForNewline(t *testing.T) {
	w, source, board := burpWatcher(t)
	w.MaxEvents = 1
	a := testBurpEvent("44444444-4444-4444-8444-444444444444", "endpoint")
	b := testBurpEvent("55555555-5555-4555-8555-555555555555", "param")
	c := testBurpEvent("66666666-6666-4666-8666-666666666666", "header")
	partial := eventLine(t, c)
	if err := os.WriteFile(source, []byte(eventLine(t, a)+eventLine(t, b)+eventLine(t, a)+partial[:len(partial)-2]), 0600); err != nil {
		t.Fatal(err)
	}
	if n, err := w.Poll(); err != nil || n != 2 {
		t.Fatalf("first poll: %d %v", n, err)
	}
	if len(w.Events) != 1 || w.Events[0].Kind != "param" {
		t.Fatalf("window: %+v", w.Events)
	}
	if err := os.WriteFile(source, []byte(eventLine(t, a)+eventLine(t, b)+eventLine(t, a)+partial), 0600); err != nil {
		t.Fatal(err)
	}
	if n, err := w.Poll(); err != nil || n != 1 {
		t.Fatalf("completed line: %d %v", n, err)
	}
	if n, err := w.Poll(); err != nil || n != 0 {
		t.Fatalf("replay: %d %v", n, err)
	}
	data, err := os.ReadFile(board)
	if err != nil || !strings.Contains(string(data), "header") {
		t.Fatalf("board: %q %v", data, err)
	}
	if got := w.Refusals().Window; got != 2 {
		t.Fatalf("window drops: %d", got)
	}
}

// ATLAS-BURP-AC01: malformed, cross-program and secret-bearing lines never
// become notes or reach the recommendation corpus.
func TestBurpWatcherRejectsInvalidEnvelope(t *testing.T) {
	w, source, _ := burpWatcher(t)
	valid := testBurpEvent("44444444-4444-4444-8444-444444444444", "endpoint")
	other := testBurpEvent("55555555-5555-4555-8555-555555555555", "param")
	other["program_id"] = "other"
	secret := testBurpEvent("66666666-6666-4666-8666-666666666666", "header")
	secret["value"] = "Authorization: Bearer synthetic"
	nullValue := testBurpEvent("77777777-7777-4777-8777-777777777777", "header")
	nullValue["value"] = nil
	badRef := testBurpEvent("88888888-8888-4888-8888-888888888888", "header")
	badRef["ref"] = "hive_burp:demo/99999999-9999-4999-8999-999999999999/33333333-3333-4333-8333-333333333333"
	lines := eventLine(t, valid) + eventLine(t, other) + eventLine(t, secret) + eventLine(t, nullValue) + eventLine(t, badRef) + "{broken}\n"
	if err := os.WriteFile(source, []byte(lines), 0600); err != nil {
		t.Fatal(err)
	}
	if n, err := w.Poll(); err != nil || n != 1 {
		t.Fatalf("poll: %d %v", n, err)
	}
	if len(w.Events) != 1 || w.Events[0].Kind != "endpoint" || w.Refusals().Secret != 1 || w.Refusals().Invalid != 4 {
		t.Fatalf("events/refusals: %+v %+v", w.Events, w.Refusals())
	}
	if e := (&Adapter{ProgramID: "demo"}).Event(eventLine(t, valid), "legacy"); e != nil {
		t.Fatal("legacy watcher accepted Burp evidence without manifest")
	}
}

// ATLAS-BURP-AC02 / SPEC-009-AC03: invalidation removes outputs and the
// revoked generation cannot be reactivated from the same projection.
func TestBurpWatcherInvalidationAndNewGeneration(t *testing.T) {
	w, source, board := burpWatcher(t)
	e := testBurpEvent("44444444-4444-4444-8444-444444444444", "endpoint")
	if err := os.WriteFile(source, []byte(eventLine(t, e)), 0600); err != nil {
		t.Fatal(err)
	}
	if n, err := w.Poll(); err != nil || n != 1 {
		t.Fatalf("active: %d %v", n, err)
	}
	manifest := w.ManifestPath
	m := testManifest(time.Now())
	m.State = "closed"
	writeTestJSON(t, manifest, m)
	if _, err := w.Poll(); err == nil || len(w.Events) != 0 {
		t.Fatalf("closed source retained corpus: %v", err)
	}
	if _, err := os.Stat(board); !os.IsNotExist(err) {
		t.Fatalf("closed source retained board: %v", err)
	}
	m.State = "active"
	writeTestJSON(t, manifest, m)
	if _, err := w.Poll(); err == nil {
		t.Fatal("revoked generation reactivated")
	}
	m.Generation = 2
	m.FileName = "88888888-8888-4888-8888-888888888888.jsonl"
	writeTestJSON(t, manifest, m)
	if err := os.WriteFile(filepath.Join(filepath.Dir(source), m.FileName), []byte(eventLine(t, e)), 0600); err != nil {
		t.Fatal(err)
	}
	if n, err := w.Poll(); err != nil || n != 1 {
		t.Fatalf("new generation: %d %v", n, err)
	}
}

// ATLAS-BURP-AC02/03: expiry, mismatched program and unsafe source fail
// closed; concurrent invalidation cannot publish a board.
func TestBurpWatcherRejectsStaleUnsafeAndConcurrentInvalidation(t *testing.T) {
	t.Run("old board on start", func(t *testing.T) {
		w, source, board := burpWatcher(t)
		if err := os.WriteFile(source, nil, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(board, []byte("another program"), 0600); err != nil {
			t.Fatal(err)
		}
		if n, err := w.Poll(); err != nil || n != 0 {
			t.Fatalf("initial poll: %d %v", n, err)
		}
		if _, err := os.Stat(board); !os.IsNotExist(err) {
			t.Fatalf("old board survived new session: %v", err)
		}
	})
	t.Run("stale", func(t *testing.T) {
		w, _, board := burpWatcher(t)
		if err := os.WriteFile(board, []byte("old board"), 0600); err != nil {
			t.Fatal(err)
		}
		m := testManifest(time.Now().Add(-31 * time.Second))
		writeTestJSON(t, w.ManifestPath, m)
		if _, err := w.Poll(); err == nil {
			t.Fatal("stale manifest accepted")
		}
		if _, err := os.Stat(board); !os.IsNotExist(err) {
			t.Fatalf("stale board retained: %v", err)
		}
	})
	t.Run("other program", func(t *testing.T) {
		w, _, _ := burpWatcher(t)
		m := testManifest(time.Now())
		m.ProgramID = "other"
		writeTestJSON(t, w.ManifestPath, m)
		if _, err := w.Poll(); err == nil {
			t.Fatal("cross-program manifest accepted")
		}
	})
	t.Run("symlink", func(t *testing.T) {
		w, source, _ := burpWatcher(t)
		real := filepath.Join(t.TempDir(), "real.jsonl")
		if err := os.WriteFile(real, []byte(eventLine(t, testBurpEvent("44444444-4444-4444-8444-444444444444", "endpoint"))), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(real, source); err != nil {
			t.Fatal(err)
		}
		if _, err := w.Poll(); err == nil {
			t.Fatal("symlink source accepted")
		}
	})
	t.Run("projection quota", func(t *testing.T) {
		w, source, _ := burpWatcher(t)
		if err := os.WriteFile(source, nil, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Truncate(source, maxBurpSourceBytes+1); err != nil {
			t.Fatal(err)
		}
		if _, err := w.Poll(); err == nil {
			t.Fatal("oversized projection accepted")
		}
	})
	t.Run("truncation", func(t *testing.T) {
		w, source, board := burpWatcher(t)
		if err := os.WriteFile(source, []byte(eventLine(t, testBurpEvent("44444444-4444-4444-8444-444444444444", "endpoint"))), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := w.Poll(); err != nil {
			t.Fatal(err)
		}
		if err := os.Truncate(source, 0); err != nil {
			t.Fatal(err)
		}
		if _, err := w.Poll(); err == nil || len(w.Events) != 0 {
			t.Fatalf("truncated projection retained corpus: %v", err)
		}
		if _, err := os.Stat(board); !os.IsNotExist(err) {
			t.Fatalf("truncated projection retained board: %v", err)
		}
	})
	t.Run("during recommendation", func(t *testing.T) {
		w, source, board := burpWatcher(t)
		if err := os.WriteFile(source, []byte(eventLine(t, testBurpEvent("44444444-4444-4444-8444-444444444444", "endpoint"))), 0600); err != nil {
			t.Fatal(err)
		}
		w.Recommend = func([]Evidence) (Board, error) {
			m := testManifest(time.Now())
			m.State = "invalidated"
			writeTestJSON(t, w.ManifestPath, m)
			return Board{Board: "must not appear"}, nil
		}
		if _, err := w.Poll(); err == nil || len(w.Events) != 0 {
			t.Fatalf("concurrent invalidation retained corpus: %v", err)
		}
		if _, err := os.Stat(board); !os.IsNotExist(err) {
			t.Fatalf("board published after invalidation: %v", err)
		}
	})
}
