package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A burst larger than one poll's read budget must be consumed across polls
// without losing or duplicating a line, and without allocating the burst.
func TestPollReadBudgetKeepsEveryLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	var burst strings.Builder
	const lines = 400
	for i := 0; i < lines; i++ {
		fmt.Fprintf(&burst, "nota %04d %s\n", i, strings.Repeat("x", 64))
	}
	if err := os.WriteFile(path, []byte(burst.String()), 0600); err != nil {
		t.Fatal(err)
	}
	w := Watcher{Library: library(t), Sources: []string{path}, BoardPath: filepath.Join(dir, "board.md"),
		Adapter: Adapter{ProgramID: "a"}, MaxPollBytes: 512}
	total, polls := 0, 0
	for {
		n, err := w.Poll()
		if err != nil {
			t.Fatal(err)
		}
		total += n
		polls++
		if n == 0 {
			break
		}
		if polls > lines {
			t.Fatal("poll made no progress")
		}
	}
	if total != lines {
		t.Fatalf("read %d of %d lines in %d polls", total, lines, polls)
	}
	if polls < 3 {
		t.Fatalf("budget was not applied: %d polls", polls)
	}
}

// The session window is bounded: the oldest events leave and the refusal
// report accounts for them.
func TestSessionWindowIsBounded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	w := Watcher{Library: library(t), Sources: []string{path}, BoardPath: filepath.Join(dir, "board.md"),
		Adapter: Adapter{ProgramID: "a"}, MaxEvents: 3}
	var lines strings.Builder
	for i := 0; i < 5; i++ {
		fmt.Fprintf(&lines, "nota %d\n", i)
	}
	if err := os.WriteFile(path, []byte(lines.String()), 0600); err != nil {
		t.Fatal(err)
	}
	if n, err := w.Poll(); err != nil || n != 5 {
		t.Fatalf("%d %v", n, err)
	}
	if len(w.Events) != 3 {
		t.Fatalf("window not bounded: %d events", len(w.Events))
	}
	if got := w.Refusals().Window; got != 2 {
		t.Fatalf("window refusals = %d, want 2", got)
	}
	if w.Events[0].Value != "nota 2" || w.Events[2].Value != "nota 4" {
		t.Fatalf("kept the wrong slice: %q..%q", w.Events[0].Value, w.Events[2].Value)
	}
}
