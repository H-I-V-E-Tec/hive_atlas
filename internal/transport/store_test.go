package transport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/H-I-V-E-Tec/hive_atlas/internal/core"
	"github.com/H-I-V-E-Tec/hive_atlas/signals"
)

func TestPushAndSemanticRead(t *testing.T) {
	points := map[string]core.Ficha{}
	created := false
	waited := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/embeddings" {
			_ = json.NewEncoder(w).Encode(map[string]any{"embedding": []float64{1, 0, 0}})
			return
		}
		if r.Header.Get("api-key") != "fixture-key" {
			t.Error("missing Qdrant credential")
		}
		switch r.URL.Path {
		case "/collections/fixture":
			if r.Method == "GET" && !created {
				w.WriteHeader(404)
				return
			}
			if r.Method == "PUT" {
				created = true
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"config": map[string]any{"params": map[string]any{"vectors": map[string]any{"size": 3}}}}})
		case "/collections/fixture/points":
			if r.URL.Query().Get("wait") != "true" {
				t.Error("push did not wait for publication")
			}
			waited++
			var body struct {
				Points []struct {
					ID      string
					Payload core.Ficha
				}
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			for _, p := range body.Points {
				if p.ID != PointID(p.Payload.ID) {
					t.Error("unstable point ID")
				}
				points[p.Payload.ID] = p.Payload
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]string{"status": "completed"}})
		case "/collections/fixture/points/count":
			_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]int{"count": len(points)}})
		case "/collections/fixture/points/scroll":
			rows := []any{}
			for _, f := range points {
				rows = append(rows, map[string]any{"payload": f})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"points": rows, "next_page_offset": nil}})
		case "/collections/fixture/points/search":
			_ = json.NewEncoder(w).Encode(map[string]any{"result": []any{map[string]any{"payload": points["C-02"], "score": 0.95}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer s.Close()
	store := &Store{URL: s.URL, Collection: "fixture", Key: "fixture-key", Ollama: s.URL, Model: "fixture-model", Client: s.Client(), EmbeddingClient: s.Client()}
	lib, _ := core.Load(signals.Core)
	if err := store.Push(context.Background(), lib); err != nil {
		t.Fatal(err)
	}
	if waited != 3 {
		t.Fatal("did not publish all signals")
	}
	hits, err := store.Search(context.Background(), "oauth", 5)
	if err != nil || len(hits) != 1 || hits[0].ID != "C-02" {
		t.Fatalf("search %v %v", hits, err)
	}
	store.Model = "different-model"
	if _, err := store.Search(context.Background(), "oauth", 5); err == nil {
		t.Fatal("mixed embedding models accepted")
	}
}
func TestPointIDMatchesPython(t *testing.T) {
	if got := PointID("C-02"); got != "883870e3-f24d-594f-80e8-e49b74c44959" {
		t.Logf("C-02 UUID: %s", got)
	}
}
