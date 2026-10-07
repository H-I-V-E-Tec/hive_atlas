package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/H-I-V-E-Tec/hive_atlas/signals"
)

func library(t *testing.T) *Library {
	t.Helper()
	lib, err := Load(signals.Core)
	if err != nil {
		t.Fatal(err)
	}
	return lib
}
func TestReviewedCorpus(t *testing.T) {
	data, err := os.ReadFile("../../eval/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var suite struct {
		Cases []struct {
			ID                  string
			Evidence            []Evidence
			Expected, Forbidden []string
		}
	}
	if err := json.Unmarshal(data, &suite); err != nil {
		t.Fatal(err)
	}
	for _, c := range suite.Cases {
		t.Run(c.ID, func(t *testing.T) {
			for _, k := range []int{0, 5} {
				got := []string{}
				for _, r := range library(t).Recommend(c.Evidence, k).Leads {
					got = append(got, r.ID)
					if r.State != "UNTESTED" {
						t.Fatal(r)
					}
				}
				sort.Strings(got)
				expected := append([]string{}, c.Expected...)
				sort.Strings(expected)
				if !reflect.DeepEqual(got, expected) {
					t.Fatalf("k=%d got %v want %v", k, got, expected)
				}
			}
		})
	}
}
func TestFlowIsolation(t *testing.T) {
	for _, field := range []string{"program", "asset", "flow"} {
		t.Run(field, func(t *testing.T) {
			a := Evidence{Kind: "param", Value: "redirect=destination", ProgramID: "a", Asset: "host", Flow: "login"}
			b := Evidence{Kind: "nota", Value: "client_id oauth", ProgramID: "a", Asset: "host", Flow: "login"}
			switch field {
			case "program":
				b.ProgramID = "b"
			case "asset":
				b.Asset = "other"
			case "flow":
				b.Flow = "other"
			}
			for _, r := range library(t).Recommend([]Evidence{a, b}, 0).Leads {
				if r.ID == "C-02" {
					t.Fatal("composed across scope")
				}
			}
		})
	}
}
func TestWatcherPartialRotationAndRefusals(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	board := filepath.Join(dir, "board.md")
	w := Watcher{Library: library(t), Sources: []string{path}, BoardPath: board, Adapter: Adapter{ProgramID: "a"}}
	if err := os.WriteFile(path, []byte(`{"kind":"param","program_id":"a","value":"redirect_uri`), 0600); err != nil {
		t.Fatal(err)
	}
	if n, err := w.Poll(); err != nil || n != 0 {
		t.Fatalf("partial %d %v", n, err)
	}
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	_, _ = f.WriteString(" client_id\"}\n{\"kind\":\"nota\",\"program_id\":\"b\",\"value\":\"other\"}\nAuthorization: Bearer secret\n{broken}\n")
	_ = f.Close()
	if n, err := w.Poll(); err != nil || n != 1 {
		t.Fatalf("complete %d %v", n, err)
	}
	if w.Adapter.RejectedProgram != 1 || w.Adapter.RejectedSecret != 1 || w.Adapter.RejectedInvalid != 1 {
		t.Fatal(w.Adapter)
	}
	if err := os.Rename(path, path+".old"); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(path, []byte("clean note\n"), 0600)
	if n, err := w.Poll(); err != nil || n != 1 {
		t.Fatalf("rotation %d %v", n, err)
	}
	if n, err := w.Poll(); err != nil || n != 0 {
		t.Fatalf("duplicate %d %v", n, err)
	}
}
func TestCredentialsRejected(t *testing.T) {
	for _, s := range []string{`Authorization: Bearer synthetic-secret`, `Cookie: session=synthetic`, `{"authorization":"Bearer synthetic"}`, `{"value":"Authorization: Bearer synthetic"}`, `ghp_abcdefghijklmnopqrstuvwx`, `api_key=synthetic`, `{"password":"synthetic"}`} {
		if !HasSecret(s) {
			t.Errorf("not refused: %q", s)
		}
	}
	for _, s := range []string{`{"value":"Authorization:\tBearer synthetic"}`, `{"Authoriz\u0061tion":"Bearer synthetic"}`} {
		if !HasSecret(s) {
			t.Fatal("escaped credential accepted")
		}
	}
	if HasSecret("GET /authorize?client_id=public&redirect_uri=callback") {
		t.Fatal("public OAuth metadata rejected")
	}
}
func TestInvalidLibraryRejected(t *testing.T) {
	lib := library(t)
	lib.Signals[0].Patterns = []string{`(?=unsupported)`}
	if lib.Compile() == nil {
		t.Fatal("unsupported regex silently accepted")
	}
}

func TestUnicodeSignalBoundaries(t *testing.T) {
	for _, value := range []string{"áredirect_uri", "redirect_urição", "préredirect_urição"} {
		for _, r := range library(t).Recommend([]Evidence{{Kind: "nota", Value: value}}, 0).Leads {
			if r.ID == "S-REDIR-01" {
				t.Fatalf("Unicode word was split: %s", value)
			}
		}
	}
}
