package transport

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/H-I-V-E-Tec/hive_atlas/internal/core"
)

type Store struct {
	URL, Collection, Key, Ollama, Model string
	Client                              *http.Client
	EmbeddingClient                     *http.Client
}

func StoreFromEnv() (*Store, error) {
	get := func(k, d string) string {
		if v := os.Getenv(k); v != "" {
			return v
		}
		return d
	}
	url, err := ValidateURL(get("QDRANT_URL_REST", get("QDRANT_URL", "https://127.0.0.1:6333")))
	if err != nil {
		return nil, err
	}
	ollama, err := ValidateURL(get("OLLAMA_URL", "http://127.0.0.1:11434"))
	if err != nil {
		return nil, err
	}
	collection := get("ATLAS_COLLECTION", "atlas_signals_v01")
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`).MatchString(collection) {
		return nil, fmt.Errorf("invalid collection name")
	}
	key := os.Getenv("QDRANT_API_KEY")
	if file := os.Getenv("QDRANT_API_KEY_FILE"); file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("cannot read Qdrant credential")
		}
		key = strings.TrimSpace(string(data))
	}
	if key == "" {
		return nil, fmt.Errorf("server requires QDRANT_API_KEY_FILE or QDRANT_API_KEY")
	}
	client, err := HTTPClient(os.Getenv("QDRANT_TLS_CA_FILE"))
	if err != nil {
		return nil, err
	}
	embeddingClient, _ := HTTPClient("")
	return &Store{URL: url, Collection: collection, Key: key, Ollama: ollama, Model: get("EMBEDDING_MODEL", "nomic-embed-text"), Client: client, EmbeddingClient: embeddingClient}, nil
}

type statusError struct{ status int }

func (e statusError) Error() string { return fmt.Sprintf("Qdrant returned HTTP %d", e.status) }
func (s *Store) request(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.URL+path, reader)
	if err != nil {
		return fmt.Errorf("invalid Qdrant request")
	}
	req.Header.Set("api-key", s.Key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.Client.Do(req)
	if err != nil {
		return fmt.Errorf("Qdrant connection failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return statusError{resp.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20+1))
	if err != nil || len(data) > 8<<20 {
		return fmt.Errorf("invalid Qdrant response size")
	}
	if out != nil {
		if json.Unmarshal(data, out) != nil {
			return fmt.Errorf("invalid Qdrant JSON")
		}
	}
	return nil
}
func (s *Store) embed(ctx context.Context, text string) ([]float64, error) {
	var out struct {
		Embedding []float64 `json:"embedding"`
	}
	if err := JSON(ctx, s.EmbeddingClient, "POST", s.Ollama+"/api/embeddings", "", map[string]string{"model": s.Model, "prompt": text}, &out); err != nil {
		return nil, fmt.Errorf("embedding service unavailable")
	}
	if len(out.Embedding) == 0 {
		return nil, fmt.Errorf("empty embedding")
	}
	for _, v := range out.Embedding {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, fmt.Errorf("invalid embedding")
		}
	}
	return out.Embedding, nil
}
func (s *Store) Library(ctx context.Context) (*core.Library, error) {
	lib := &core.Library{SchemaVersion: 1, Signals: []core.Ficha{}}
	var offset any
	seenOffsets := map[string]bool{}
	for pages := 0; pages < 1000; pages++ {
		body := map[string]any{"limit": 256, "with_payload": true, "with_vector": false}
		if offset != nil {
			body["offset"] = offset
		}
		var out struct {
			Result struct {
				Points []struct {
					Payload core.Ficha `json:"payload"`
				} `json:"points"`
				Offset any `json:"next_page_offset"`
			} `json:"result"`
		}
		if err := s.request(ctx, "POST", "/collections/"+s.Collection+"/points/scroll", body, &out); err != nil {
			return nil, err
		}
		for _, p := range out.Result.Points {
			lib.Signals = append(lib.Signals, p.Payload)
		}
		offset = out.Result.Offset
		if offset == nil {
			if err := lib.Compile(); err != nil {
				return nil, err
			}
			return lib, nil
		}
		encoded, _ := json.Marshal(offset)
		if seenOffsets[string(encoded)] {
			return nil, fmt.Errorf("repeated Qdrant scroll offset")
		}
		seenOffsets[string(encoded)] = true
	}
	return nil, fmt.Errorf("library exceeds scroll page limit")
}
func (s *Store) Search(ctx context.Context, q string, k int) ([]core.Hit, error) {
	lib, err := s.Library(ctx)
	if err != nil {
		return nil, err
	}
	vec, err := s.embed(ctx, q)
	if err != nil {
		return nil, err
	}
	byID := map[string]core.Ficha{}
	for _, f := range lib.Signals {
		if f.EmbeddingModel != s.Model || f.EmbeddingDimension != len(vec) {
			return nil, fmt.Errorf("library embedding model/dimension mismatch; republish with push")
		}
		byID[f.ID] = f
	}
	var out struct {
		Result []struct {
			Payload core.Ficha `json:"payload"`
			Score   float64    `json:"score"`
		} `json:"result"`
	}
	if err := s.request(ctx, "POST", "/collections/"+s.Collection+"/points/search", map[string]any{"vector": vec, "limit": k, "with_payload": true}, &out); err != nil {
		return nil, err
	}
	hits := []core.Hit{}
	for _, h := range out.Result {
		f, ok := byID[h.Payload.ID]
		if !ok {
			return nil, fmt.Errorf("search returned unknown signal")
		}
		hits = append(hits, core.Hit{ID: f.ID, Score: h.Score, Mechanism: f.Mechanism})
	}
	return hits, nil
}
func PointID(id string) string {
	ns, _ := hex.DecodeString("a71a5000000040008000000000000000")
	h := sha1.New()
	h.Write(ns)
	h.Write([]byte(id))
	b := h.Sum(nil)[:16]
	b[6] = (b[6] & 15) | 80
	b[8] = (b[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
func (s *Store) Push(ctx context.Context, lib *core.Library) error {
	if err := lib.Compile(); err != nil {
		return err
	}
	if len(lib.Signals) == 0 {
		return fmt.Errorf("refusing empty library")
	}
	probe, err := s.embed(ctx, "atlas dimension probe")
	if err != nil {
		return err
	}
	var metadata struct {
		Result struct {
			Config struct {
				Params struct {
					Vectors struct {
						Size int `json:"size"`
					} `json:"vectors"`
				} `json:"params"`
			} `json:"config"`
		} `json:"result"`
	}
	err = s.request(ctx, "GET", "/collections/"+s.Collection, nil, &metadata)
	if status, ok := err.(statusError); ok && status.status == 404 {
		if err := s.request(ctx, "PUT", "/collections/"+s.Collection, map[string]any{"vectors": map[string]any{"size": len(probe), "distance": "Cosine"}}, nil); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if metadata.Result.Config.Params.Vectors.Size != len(probe) {
		return fmt.Errorf("collection dimension differs from embedding model; use a new collection")
	}
	for _, f := range lib.Signals {
		vec, err := s.embed(ctx, core.FichaText(f))
		if err != nil {
			return err
		}
		if len(vec) != len(probe) {
			return fmt.Errorf("embedding dimension changed during push")
		}
		f.EmbeddingModel = s.Model
		f.EmbeddingDimension = len(vec)
		if err := s.request(ctx, "PUT", "/collections/"+s.Collection+"/points?wait=true", map[string]any{"points": []any{map[string]any{"id": PointID(f.ID), "vector": vec, "payload": f}}}, nil); err != nil {
			return err
		}
	}
	var count struct {
		Result struct {
			Count int `json:"count"`
		} `json:"result"`
	}
	if err := s.request(ctx, "POST", "/collections/"+s.Collection+"/points/count", map[string]bool{"exact": true}, &count); err != nil {
		return err
	}
	if count.Result.Count != len(lib.Signals) {
		return fmt.Errorf("collection point count differs from library; use a clean release collection")
	}
	return nil
}
