package app

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/H-I-V-E-Tec/hive_atlas/internal/core"
	"github.com/H-I-V-E-Tec/hive_atlas/internal/transport"
)

var protocolVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}
var tools = json.RawMessage(`[
{"name":"atlas_observe","description":"Evaluate sanitized evidence and return ranked advisory signals [UNTESTED]. Never touches a target.","inputSchema":{"type":"object","properties":{"evidence":{"type":"array","items":{"type":"object"}},"k":{"type":"integer","default":0,"minimum":0,"maximum":100}},"required":["evidence"]}},
{"name":"atlas_signals_search","description":"Search the reviewed signal library.","inputSchema":{"type":"object","properties":{"query":{"type":"string"},"k":{"type":"integer","default":5,"minimum":1,"maximum":100}},"required":["query"]}},
{"name":"atlas_feedback","description":"Record a reviewed outcome for the authenticated member.","inputSchema":{"type":"object","properties":{"signal_id":{"type":"string"},"outcome":{"type":"string","enum":["confirmado","descartado","inconclusivo"]},"program_id":{"type":"string"},"note":{"type":"string"}},"required":["signal_id","outcome"]}}
]`)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}
type MCP struct {
	Backend      transport.Backend
	initialized  bool
	ready        bool
	FeedbackPath string
}

var feedbackMu sync.Mutex

func failure(id json.RawMessage, code int, message string) *response {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	return &response{JSONRPC: "2.0", ID: id, Error: &rpcError{code, message}}
}
func (m *MCP) Handle(ctx context.Context, line []byte) *response {
	var req request
	if !json.Valid(line) {
		return failure(nil, -32700, "parse error")
	}
	if json.Unmarshal(line, &req) != nil || req.JSONRPC != "2.0" || req.Method == "" {
		return failure(nil, -32600, "invalid request")
	}
	if len(req.ID) > 0 && string(req.ID) != "null" {
		var id any
		_ = json.Unmarshal(req.ID, &id)
		switch id.(type) {
		case float64, string:
		default:
			return failure(nil, -32600, "invalid request id")
		}
	}
	if len(req.ID) == 0 {
		if req.Method == "notifications/initialized" && m.initialized {
			m.ready = true
		}
		return nil
	}
	var result any
	switch req.Method {
	case "initialize":
		if m.initialized {
			return failure(req.ID, -32600, "already initialized")
		}
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if json.Unmarshal(req.Params, &params) != nil {
			return failure(req.ID, -32602, "invalid initialize params")
		}
		version := protocolVersions[0]
		for _, v := range protocolVersions {
			if v == params.ProtocolVersion {
				version = v
			}
		}
		m.initialized = true
		result = map[string]any{"protocolVersion": version, "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "hive-atlas", "version": Version}}
	case "ping":
		result = map[string]any{}
	case "tools/list":
		if !m.ready {
			return failure(req.ID, -32000, "initialize the session first")
		}
		result = map[string]any{"tools": tools}
	case "tools/call":
		if !m.ready {
			return failure(req.ID, -32000, "initialize the session first")
		}
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if json.Unmarshal(req.Params, &params) != nil || params.Name == "" {
			return failure(req.ID, -32602, "invalid tool params")
		}
		if params.Name != "atlas_observe" && params.Name != "atlas_signals_search" && params.Name != "atlas_feedback" {
			return failure(req.ID, -32602, "unknown tool")
		}
		payload, err := m.call(ctx, params.Name, params.Arguments)
		text := ""
		if err != nil {
			text = err.Error()
		} else {
			encoded, _ := json.Marshal(payload)
			text = string(encoded)
		}
		result = map[string]any{"content": []any{map[string]string{"type": "text", "text": text}}, "isError": err != nil}
	default:
		return failure(req.ID, -32601, "method not found")
	}
	return &response{JSONRPC: "2.0", ID: req.ID, Result: result}
}
func (m *MCP) call(ctx context.Context, name string, args json.RawMessage) (any, error) {
	switch name {
	case "atlas_observe":
		var a struct {
			Evidence *[]core.Evidence `json:"evidence"`
			K        int              `json:"k"`
		}
		if json.Unmarshal(args, &a) != nil || a.Evidence == nil || a.K < 0 || a.K > 100 || len(*a.Evidence) > 10000 {
			return nil, fmt.Errorf("invalid observe arguments")
		}
		for _, e := range *a.Evidence {
			if err := e.Validate(); err != nil {
				return nil, err
			}
		}
		lib, err := m.Backend.Library(ctx)
		if err != nil {
			return nil, err
		}
		return lib.Recommend(*a.Evidence, a.K), nil
	case "atlas_signals_search":
		var a struct {
			Query string `json:"query"`
			K     *int   `json:"k"`
		}
		if json.Unmarshal(args, &a) != nil {
			return nil, fmt.Errorf("invalid search arguments")
		}
		k := 5
		if a.K != nil {
			k = *a.K
		}
		if strings.TrimSpace(a.Query) == "" || len(a.Query) > 8192 || k < 1 || k > 100 {
			return nil, fmt.Errorf("invalid search arguments")
		}
		if core.HasSecret(a.Query) {
			return nil, fmt.Errorf("sanitize search query before sending credentials")
		}
		hits, err := m.Backend.Search(ctx, a.Query, k)
		for i := range hits {
			hits[i].Score = math.Round(hits[i].Score*1000) / 1000
		}
		return hits, err
	case "atlas_feedback":
		var a struct {
			SignalID  string `json:"signal_id"`
			Outcome   string `json:"outcome"`
			ProgramID string `json:"program_id"`
			Note      string `json:"note"`
			TS        string `json:"ts"`
		}
		if json.Unmarshal(args, &a) != nil || a.SignalID == "" || (a.Outcome != "confirmado" && a.Outcome != "descartado" && a.Outcome != "inconclusivo") || len(a.Note) > 16384 {
			return nil, fmt.Errorf("invalid feedback arguments")
		}
		if core.HasSecret(string(args)) {
			return nil, fmt.Errorf("sanitize feedback before persisting credentials")
		}
		lib, err := m.Backend.Library(ctx)
		if err != nil {
			return nil, err
		}
		known := false
		for _, signal := range lib.Signals {
			if signal.ID == a.SignalID {
				known = true
				break
			}
		}
		if !known {
			return nil, fmt.Errorf("unknown signal_id")
		}
		a.TS = time.Now().UTC().Format(time.RFC3339)
		path := m.FeedbackPath
		if path == "" {
			path = os.Getenv("ATLAS_FEEDBACK_FILE")
		}
		if path == "" {
			path = filepath.Join(hiveHome(), "atlas", "feedback.jsonl")
		}
		feedbackMu.Lock()
		defer feedbackMu.Unlock()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		if err := json.NewEncoder(f).Encode(a); err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "recorded": a.TS}, nil
	}
	return nil, fmt.Errorf("unknown tool")
}
func (m *MCP) Run(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 65536), 2<<20)
	encoder := json.NewEncoder(out)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line := scanner.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		if r := m.Handle(ctx, line); r != nil {
			if err := encoder.Encode(r); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}
