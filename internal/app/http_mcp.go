package app

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/H-I-V-E-Tec/hive_atlas/internal/transport"
)

// MCPHTTP is a stateless Streamable HTTP endpoint. Every message is authorized;
// no session identifiers or evidence are retained between requests.
func MCPHTTP(backend transport.Backend, auth *transport.Authorizer, stateDir string, origins []string) http.Handler {
	allowed := map[string]bool{}
	for _, raw := range append(origins, auth.Center) {
		if u, err := url.Parse(raw); err == nil && u.Host != "" {
			allowed[u.Scheme+"://"+u.Host] = true
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if origin := r.Header.Get("Origin"); origin != "" && !allowed[origin] {
			http.Error(w, "origin forbidden", http.StatusForbidden)
			return
		}
		value := r.Header.Get("Authorization")
		if !strings.HasPrefix(value, "Bearer ") {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		claims, err := auth.Validate(r.Context(), strings.TrimPrefix(value, "Bearer "))
		if err != nil {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		if !claims.Allowed() {
			http.Error(w, "product.atlas permission required", http.StatusForbidden)
			return
		}
		if v := r.Header.Get("MCP-Protocol-Version"); v != "" && !httpProtocol(v) {
			http.Error(w, "unsupported protocol version", http.StatusBadRequest)
			return
		}
		if r.Method != http.MethodPost {
			// GET streams and explicit DELETE are optional for a stateless service.
			w.Header().Set("Allow", "POST")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if !accepts(r.Header.Get("Accept"), "application/json") || !accepts(r.Header.Get("Accept"), "text/event-stream") {
			http.Error(w, "accept application/json and text/event-stream", http.StatusNotAcceptable)
			return
		}
		media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if media != "application/json" {
			http.Error(w, "application/json required", http.StatusUnsupportedMediaType)
			return
		}
		defer r.Body.Close()
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2<<20))
		if err != nil {
			http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
			return
		}
		if !utf8.Valid(body) {
			http.Error(w, "UTF-8 required", http.StatusBadRequest)
			return
		}
		m := &MCP{Backend: backend, initialized: true, ready: true, FeedbackPath: filepath.Join(stateDir, "feedback", claims.Subject+".jsonl")}
		var req request
		_ = json.Unmarshal(body, &req)
		if req.Method == "initialize" {
			m.initialized, m.ready = false, false
			var params struct {
				ProtocolVersion string `json:"protocolVersion"`
			}
			if json.Unmarshal(req.Params, &params) == nil && !httpProtocol(params.ProtocolVersion) {
				// Never advertise the old HTTP+SSE transport on this endpoint.
				var values map[string]any
				_ = json.Unmarshal(req.Params, &values)
				if values != nil {
					values["protocolVersion"] = protocolVersions[0]
					req.Params, _ = json.Marshal(values)
					body, _ = json.Marshal(req)
				}
			}
		}
		res := m.Handle(r.Context(), body)
		if res == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if res.Error != nil && (res.Error.Code == -32700 || res.Error.Code == -32600) {
			w.WriteHeader(http.StatusBadRequest)
		}
		_ = json.NewEncoder(w).Encode(res)
	})
}

func httpProtocol(version string) bool {
	for _, v := range protocolVersions[:3] {
		if v == version {
			return true
		}
	}
	return false
}

func accepts(header, wanted string) bool {
	for _, item := range strings.Split(header, ",") {
		media, params, err := mime.ParseMediaType(strings.TrimSpace(item))
		if err == nil && params["q"] != "0" && (media == wanted || media == "*/*") {
			return true
		}
	}
	return false
}
