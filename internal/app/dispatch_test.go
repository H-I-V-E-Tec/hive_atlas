package app

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Splitting Run into one function per command makes each refusal reachable
// without standing up a Center, a store or an agent config.
func TestCommandRefusals(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		name, cmd, want string
		o               options
	}{
		{name: "unknown command", cmd: "nonsense", want: "unknown Atlas command"},
		{name: "logout belongs to the launcher", cmd: "logout", want: "shared across HIVE products"},
		{name: "login belongs to the launcher", cmd: "login", want: "run hive login"},
		{name: "mint-reader needs a destination", cmd: "mint-reader", want: "--output required"},
		{name: "watch needs a source", cmd: "watch", o: options{program: "acme", interval: time.Second}, want: "watch requires"},
		{name: "watch needs a program", cmd: "watch", o: options{sources: sourceFlags{"a.jsonl"}, interval: time.Second}, want: "watch requires"},
		{name: "watch needs a positive interval", cmd: "watch", o: options{sources: sourceFlags{"a.jsonl"}, program: "acme"}, want: "watch requires"},
		{name: "setup needs the installed launcher", cmd: "setup", o: options{client: "codex"}, want: "HIVE_LAUNCHER required"},
	} {
		t.Run(c.name, func(t *testing.T) {
			o := c.o
			err := dispatch(ctx, c.cmd, &o, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v, want something containing %q", err, c.want)
			}
		})
	}
}

// With the launcher present, setup still refuses an agent it cannot configure.
func TestSetupRefusesUnknownAgent(t *testing.T) {
	t.Setenv("HIVE_LAUNCHER", filepath.Join(t.TempDir(), "hive"))
	o := options{client: "emacs"}
	err := dispatch(context.Background(), "setup", &o, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "unknown agent") {
		t.Fatalf("got %v, want unknown agent", err)
	}
}

// eval reports the corpus result and fails closed on a corpus it cannot read.
func TestEvalReportsAndFailsClosed(t *testing.T) {
	var out bytes.Buffer
	if err := evaluate(filepath.Join("..", "..", "eval", "cases.json"), "", &out); err != nil {
		t.Fatalf("reviewed corpus: %v", err)
	}
	for _, want := range []string{`"cases":6`, `"fp":0`, `"fn":0`, `"regressions":0`} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("%s missing from %s", want, out.String())
		}
	}
	broken := filepath.Join(t.TempDir(), "cases.json")
	if err := evaluate(broken, "", &bytes.Buffer{}); err == nil {
		t.Fatal("a missing corpus was accepted")
	}
}
