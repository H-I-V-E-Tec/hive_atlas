package app

import (
	"encoding/json"
	"testing"
)

// Rewriting an unsupported protocol version on an initialize NOTIFICATION must
// not give it an id: a notification has no JSON-RPC response.
func TestNotificationStaysIdLessWhenRewritten(t *testing.T) {
	var req request
	if err := json.Unmarshal([]byte(`{"jsonrpc":"2.0","method":"initialize","params":{"protocolVersion":"1999-01-01"}}`), &req); err != nil {
		t.Fatal(err)
	}
	var values map[string]any
	if err := json.Unmarshal(req.Params, &values); err != nil {
		t.Fatal(err)
	}
	values["protocolVersion"] = protocolVersions[0]
	var err error
	if req.Params, err = json.Marshal(values); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var round request
	if err := json.Unmarshal(body, &round); err != nil {
		t.Fatal(err)
	}
	if len(round.ID) != 0 {
		t.Fatalf("rewrite injected an id: %s", body)
	}
	m := &MCP{Backend: nil}
	if res := m.Handle(t.Context(), body); res != nil {
		t.Fatalf("notification answered: %+v", res)
	}
}
