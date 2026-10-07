package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/H-I-V-E-Tec/hive_atlas/internal/core"
	"github.com/H-I-V-E-Tec/hive_atlas/internal/transport"
)

// RemoteMCP only forwards MCP messages. The ordinary client runs no engine,
// downloads no signal library and never receives database credentials.
type RemoteMCP struct {
	Remote           transport.Remote
	version, session string
}

func (m *RemoteMCP) Send(ctx context.Context, data []byte) (*response, error) {
	var req request
	if !json.Valid(data) {
		return failure(nil, -32700, "parse error"), nil
	}
	if json.Unmarshal(data, &req) != nil || req.Method == "" || req.JSONRPC != "2.0" {
		return failure(nil, -32600, "invalid request"), nil
	}
	// Check credentials before crossing the network boundary as well as on the
	// service. A rejected notification cannot have a JSON-RPC response.
	if req.Method == "tools/call" && core.HasSecret(string(req.Params)) {
		if len(req.ID) == 0 {
			return nil, fmt.Errorf("sanitize tool arguments before sending credentials")
		}
		return &response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"content": []any{map[string]string{"type": "text", "text": "sanitize tool arguments before sending credentials"}}, "isError": true}}, nil
	}
	token, err := m.Remote.Token()
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, m.Remote.URL+"/mcp", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("invalid MCP endpoint")
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	if m.version != "" {
		httpReq.Header.Set("MCP-Protocol-Version", m.version)
	}
	if m.session != "" {
		httpReq.Header.Set("MCP-Session-Id", m.session)
	}
	res, err := m.Remote.Client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("remote MCP connection failed")
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case http.StatusAccepted:
		if len(req.ID) == 0 {
			return nil, nil
		}
		return nil, fmt.Errorf("remote MCP omitted a response")
	case http.StatusUnauthorized:
		return nil, fmt.Errorf("session rejected; run hive login")
	case http.StatusForbidden:
		return nil, fmt.Errorf("product.atlas permission required")
	case http.StatusOK, http.StatusBadRequest:
	default:
		return nil, fmt.Errorf("remote MCP returned HTTP %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, (8<<20)+1))
	if err != nil || len(body) > 8<<20 {
		return nil, fmt.Errorf("invalid or oversized MCP response")
	}
	media, _, _ := mime.ParseMediaType(res.Header.Get("Content-Type"))
	if media == "text/event-stream" {
		body, err = sseResponse(body, req.ID)
		if err != nil {
			return nil, err
		}
	} else if media != "application/json" {
		return nil, fmt.Errorf("invalid MCP response content type")
	}
	var reply response
	if json.Unmarshal(body, &reply) != nil || reply.JSONRPC != "2.0" || !bytes.Equal(reply.ID, req.ID) {
		return nil, fmt.Errorf("invalid MCP response")
	}
	if req.Method == "initialize" && reply.Error == nil {
		var init struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if raw, err := json.Marshal(reply.Result); err == nil {
			_ = json.Unmarshal(raw, &init)
		}
		if !httpProtocol(init.ProtocolVersion) {
			return nil, fmt.Errorf("remote MCP negotiated an unsupported protocol")
		}
		m.version, m.session = init.ProtocolVersion, res.Header.Get("MCP-Session-Id")
	}
	return &reply, nil
}

func sseResponse(body []byte, id json.RawMessage) ([]byte, error) {
	var event []string
	for _, line := range append(strings.Split(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n"), "") {
		if line == "" && len(event) > 0 {
			data := []byte(strings.Join(event, "\n"))
			var res response
			if json.Unmarshal(data, &res) == nil && bytes.Equal(res.ID, id) {
				return data, nil
			}
			event = nil
		} else if strings.HasPrefix(line, "data:") {
			event = append(event, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	return nil, fmt.Errorf("MCP stream omitted a response")
}

func (m *RemoteMCP) Run(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 65536), 2<<20)
	enc := json.NewEncoder(out)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		r, err := m.Send(ctx, scanner.Bytes())
		if err != nil {
			var req request
			_ = json.Unmarshal(scanner.Bytes(), &req)
			if len(req.ID) == 0 {
				return err
			}
			r = failure(req.ID, -32000, err.Error())
		}
		if r != nil {
			if err := enc.Encode(r); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}

func (m *RemoteMCP) Observe(ctx context.Context, events []core.Evidence) (core.Board, error) {
	data, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "watch", "method": "tools/call", "params": map[string]any{"name": "atlas_observe", "arguments": map[string]any{"evidence": events}}})
	res, err := m.Send(ctx, data)
	if err != nil {
		return core.Board{}, err
	}
	if res.Error != nil {
		return core.Board{}, fmt.Errorf("remote observe failed: %s", res.Error.Message)
	}
	raw, _ := json.Marshal(res.Result)
	var result struct {
		IsError bool `json:"isError"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if json.Unmarshal(raw, &result) != nil || result.IsError || len(result.Content) != 1 {
		return core.Board{}, fmt.Errorf("remote observe failed")
	}
	var board core.Board
	if json.Unmarshal([]byte(result.Content[0].Text), &board) != nil {
		return board, fmt.Errorf("invalid remote board")
	}
	return board, nil
}

func (m *RemoteMCP) Initialize(ctx context.Context) error {
	res, err := m.Send(ctx, []byte(`{"jsonrpc":"2.0","id":"init","method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"hive-atlas","version":"dev"}}}`))
	if err != nil {
		return err
	}
	if res.Error != nil {
		return fmt.Errorf("MCP initialization rejected")
	}
	_, err = m.Send(ctx, []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	return err
}
