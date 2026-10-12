package transport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/H-I-V-E-Tec/hive_atlas/internal/core"
	"github.com/H-I-V-E-Tec/hive_atlas/signals"
)

// Every tool call and health probe used to scroll the whole collection and
// recompile the library. The published collection is immutable, so one scroll
// must serve the TTL.
func TestLibraryIsScrolledOncePerTTL(t *testing.T) {
	lib, err := core.Load(signals.Core)
	if err != nil {
		t.Fatal(err)
	}
	var scrolls, searches int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/embeddings":
			_ = json.NewEncoder(w).Encode(map[string]any{"embedding": []float64{1, 0, 0}})
		case "/collections/fixture/points/scroll":
			atomic.AddInt64(&scrolls, 1)
			points := []any{}
			for _, f := range lib.Signals {
				f.EmbeddingModel = "fixture-model"
				f.EmbeddingDimension = 3
				points = append(points, map[string]any{"payload": f})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"points": points}})
		case "/collections/fixture/points/search":
			atomic.AddInt64(&searches, 1)
			_ = json.NewEncoder(w).Encode(map[string]any{"result": []any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	store := &Store{URL: server.URL, Collection: "fixture", Key: "fixture-key", Ollama: server.URL,
		Model: "fixture-model", Client: server.Client(), EmbeddingClient: server.Client()}
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := store.Library(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.Search(ctx, "authz on an object id", 5); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt64(&scrolls); got != 1 {
		t.Fatalf("scrolled %d times, want 1", got)
	}
	if got := atomic.LoadInt64(&searches); got != 1 {
		t.Fatalf("searched %d times, want 1", got)
	}

	// An expired window refetches, so a republish is still picked up.
	store.CacheTTL = time.Nanosecond
	if _, err := store.Library(ctx); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt64(&scrolls); got != 2 {
		t.Fatalf("expired cache scrolled %d times, want 2", got)
	}
}
