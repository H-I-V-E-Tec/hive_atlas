package transport

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/H-I-V-E-Tec/hive_atlas/internal/core"
)

type Backend interface {
	Library(context.Context) (*core.Library, error)
	Search(context.Context, string, int) ([]core.Hit, error)
}
type Local struct{ Lib *core.Library }

func (l Local) Library(context.Context) (*core.Library, error) { return l.Lib, nil }
func (l Local) Search(_ context.Context, q string, k int) ([]core.Hit, error) {
	return l.Lib.Search(q, k), nil
}

type Remote struct {
	URL, TokenFile string
	Client         *http.Client
}

func (r Remote) Token() (string, error) {
	raw := os.Getenv("HIVE_TOKEN")
	if raw == "" {
		data, err := os.ReadFile(r.TokenFile)
		if err != nil {
			return "", fmt.Errorf("run hive login first")
		}
		raw = string(data)
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("run hive login first")
	}
	return raw, nil
}
func (r Remote) Library(ctx context.Context) (*core.Library, error) {
	token, err := r.Token()
	if err != nil {
		return nil, err
	}
	var lib core.Library
	if err := JSON(ctx, r.Client, "GET", r.URL+"/api/v1/library", token, nil, &lib); err != nil {
		return nil, err
	}
	if err := lib.Compile(); err != nil {
		return nil, err
	}
	return &lib, nil
}
func (r Remote) Search(ctx context.Context, q string, k int) ([]core.Hit, error) {
	token, err := r.Token()
	if err != nil {
		return nil, err
	}
	hits := []core.Hit{}
	err = JSON(ctx, r.Client, "POST", r.URL+"/api/v1/search", token, map[string]any{"query": q, "k": k}, &hits)
	if err != nil {
		return nil, err
	}
	return hits, nil
}
