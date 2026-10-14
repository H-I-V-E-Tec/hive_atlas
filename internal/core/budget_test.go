package core

import (
	"errors"
	"testing"
)

const deepChainLibrary = `{"schema_version":1,"signals":[
 {"id":"S-A","version":1,"state":"revisada","kind":"signal","signal_class":"LOGIC",
  "mechanism":"primeiro elo","patterns":["widget"],"feeds":["C-1"]},
 {"id":"C-1","version":1,"state":"revisada","kind":"chain","signal_class":"LOGIC",
  "mechanism":"segundo elo","requires":["S-A"],"feeds":["C-2"]},
 {"id":"C-2","version":1,"state":"revisada","kind":"chain","signal_class":"LOGIC",
  "mechanism":"terceiro elo","requires":["C-1"]}
]}`

// A budgeted evaluation must keep a composition deeper than two links: the
// retrieval only knows the first signal, and the chain beyond the chain has to
// be pulled in transitively.
func TestBudgetKeepsDeepChain(t *testing.T) {
	lib, err := Load([]byte(deepChainLibrary))
	if err != nil {
		t.Fatal(err)
	}
	events := []Evidence{{Kind: "nota", Value: "widget", ProgramID: "a", Asset: "h", Flow: "f"}}
	onlyFirst := func(string, int) ([]Hit, error) { return []Hit{{ID: "S-A"}}, nil }
	board, err := lib.RecommendWith(events, 1, onlyFirst)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, r := range board.Leads {
		got[r.ID] = true
	}
	for _, id := range []string{"S-A", "C-1", "C-2"} {
		if !got[id] {
			t.Fatalf("%s missing from budgeted leads %v", id, got)
		}
	}
}

// The injected retrieval decides the candidate set, which is what lets the
// server rank with its embeddings instead of the lexical fallback.
func TestBudgetUsesTheInjectedRetrieval(t *testing.T) {
	lib, err := Load([]byte(deepChainLibrary))
	if err != nil {
		t.Fatal(err)
	}
	events := []Evidence{{Kind: "nota", Value: "widget", ProgramID: "a", Asset: "h", Flow: "f"}}
	empty := func(string, int) ([]Hit, error) { return nil, nil }
	board, err := lib.RecommendWith(events, 1, empty)
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Leads) != 0 {
		t.Fatalf("retrieval was bypassed: %v", board.Leads)
	}
	sentinel := errors.New("retrieval unavailable")
	if _, err := lib.RecommendWith(events, 1, func(string, int) ([]Hit, error) { return nil, sentinel }); !errors.Is(err, sentinel) {
		t.Fatalf("retrieval error swallowed: %v", err)
	}
}
