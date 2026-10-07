package transport

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/H-I-V-E-Tec/hive_atlas/internal/core"
)

func API(backend Backend, auth *Authorizer, version string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		lib, err := backend.Library(r.Context())
		if err != nil || len(lib.Signals) == 0 {
			write(w, 503, map[string]string{"status": "unavailable"})
			return
		}
		if _, err := backend.Search(r.Context(), "atlas health probe", 1); err != nil {
			write(w, 503, map[string]string{"status": "unavailable"})
			return
		}
		write(w, 200, map[string]any{"status": "ok", "version": version, "signals": len(lib.Signals)})
	})
	protect := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			value := r.Header.Get("Authorization")
			if !strings.HasPrefix(value, "Bearer ") {
				write(w, 401, map[string]string{"error": "invalid_token"})
				return
			}
			claims, err := auth.Validate(r.Context(), strings.TrimPrefix(value, "Bearer "))
			if err != nil {
				write(w, 401, map[string]string{"error": "invalid_token"})
				return
			}
			if !claims.Allowed() {
				write(w, 403, map[string]string{"error": "insufficient_permissions"})
				return
			}
			next(w, r)
		}
	}
	mux.HandleFunc("GET /api/v1/library", protect(func(w http.ResponseWriter, r *http.Request) {
		lib, err := backend.Library(r.Context())
		if err != nil {
			write(w, 503, map[string]string{"error": "library_unavailable"})
			return
		}
		write(w, 200, lib)
	}))
	mux.HandleFunc("POST /api/v1/search", protect(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		r.Body = http.MaxBytesReader(w, r.Body, 16384)
		var body struct {
			Query string `json:"query"`
			K     *int   `json:"k"`
		}
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		err := dec.Decode(&body)
		if err == nil {
			if dec.Decode(new(any)) != io.EOF {
				err = io.ErrUnexpectedEOF
			}
		}
		k := 5
		if body.K != nil {
			k = *body.K
		}
		if err != nil || strings.TrimSpace(body.Query) == "" || len(body.Query) > 8192 || !utf8.ValidString(body.Query) || core.HasSecret(body.Query) || k < 1 || k > 100 {
			write(w, 400, map[string]string{"error": "invalid_request"})
			return
		}
		hits, err := backend.Search(r.Context(), body.Query, k)
		if err != nil {
			write(w, 503, map[string]string{"error": "search_unavailable"})
			return
		}
		write(w, 200, hits)
	}))
	return mux
}
func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
