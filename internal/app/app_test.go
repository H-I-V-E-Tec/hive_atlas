package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/H-I-V-E-Tec/hive_atlas/internal/core"
	"github.com/H-I-V-E-Tec/hive_atlas/internal/transport"
	"github.com/H-I-V-E-Tec/hive_atlas/signals"
)

func mcp(t *testing.T) *MCP {
	t.Helper()
	lib, err := core.Load(signals.Core)
	if err != nil {
		t.Fatal(err)
	}
	return &MCP{Backend: transport.Local{Lib: lib}}
}
func initialize(t *testing.T, m *MCP, version string) {
	t.Helper()
	r := m.Handle(context.Background(), []byte(`{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"`+version+`"}}`))
	if r.Error != nil {
		t.Fatal(r)
	}
	if m.Handle(context.Background(), []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)) != nil {
		t.Fatal("notification response")
	}
}
func TestMCPProtocolNegotiation(t *testing.T) {
	for _, v := range append(append([]string{}, protocolVersions...), "unknown") {
		m := mcp(t)
		r := m.Handle(context.Background(), []byte(`{"jsonrpc":"2.0","id":"x","method":"initialize","params":{"protocolVersion":"`+v+`"}}`))
		want := v
		if v == "unknown" {
			want = protocolVersions[0]
		}
		if r.Result.(map[string]any)["protocolVersion"] != want {
			t.Fatal(r)
		}
		r = m.Handle(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
		if r.Error != nil {
			t.Fatal(r)
		}
	}
}
func TestMCPSequenceAndErrors(t *testing.T) {
	m := mcp(t)
	if m.Handle(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)).Error == nil {
		t.Fatal("called before init")
	}
	initialize(t, m, "2025-11-25")
	var out bytes.Buffer
	lines := `{bad}
{"jsonrpc":"2.0","method":"notifications/cancelled"}
{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"atlas_observe","arguments":{"evidence":[{"kind":"js_finding","value":"client_id redirect_uri","flow":"oauth"}]}}}
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"atlas_observe","arguments":{"evidence":[{"kind":"nota","value":"Authorization: Bearer synthetic"}]}}}
`
	if err := m.Run(context.Background(), strings.NewReader(lines), &out); err != nil {
		t.Fatal(err)
	}
	rows := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(rows) != 3 {
		t.Fatal(out.String())
	}
	if !strings.Contains(rows[1], "C-02") || !strings.Contains(rows[1], "UNTESTED") || !strings.Contains(rows[2], `"isError":true`) {
		t.Fatal(out.String())
	}
}
func TestFeedbackLocalAndPersistent(t *testing.T) {
	t.Setenv("HIVE_HOME", t.TempDir())
	t.Setenv("ATLAS_FEEDBACK_FILE", "")
	m := mcp(t)
	_, err := m.call(context.Background(), "atlas_feedback", json.RawMessage(`{"signal_id":"C-02","outcome":"confirmado","note":"reviewed"}`))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(hiveHome(), "atlas", "feedback.jsonl")
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Contains(data, []byte("reviewed")) {
		t.Fatal(err)
	}
}
func TestSetupPreservesOtherAgentConfig(t *testing.T) {
	t.Setenv("HIVE_HOME", t.TempDir())
	t.Setenv("HIVE_CENTER_URL", "https://center.example.test")
	t.Setenv("HIVE_ATLAS_URL", "https://atlas.example.test")
	t.Setenv("HIVE_LAUNCHER", filepath.Join(t.TempDir(), "hive", "bin", "hive"))
	path := filepath.Join(t.TempDir(), "config.toml")
	_ = os.WriteFile(path, []byte("model = \"example\"\n[mcp_servers.other]\ncommand = \"other\"\n[mcp_servers.hive_atlas]\ncommand = \"old\"\n[mcp_servers.hive_atlas.env]\nHIVE_ATLAS_URL = \"https://stale.invalid\"\n[profiles.default]\nmodel = \"example\"\n"), 0600)
	if err := setup("codex", path); err != nil {
		t.Fatal(err)
	}
	if err := setup("codex", path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if strings.Count(string(data), "[mcp_servers.hive_atlas.env]") != 1 || !strings.Contains(string(data), "https://atlas.example.test") {
		t.Fatal(string(data))
	}
	if strings.Count(string(data), "[mcp_servers.hive_atlas]") != 1 || !bytes.Contains(data, []byte("[profiles.default]")) || !bytes.Contains(data, []byte("[mcp_servers.other]")) {
		t.Fatal(string(data))
	}
}
func TestVersionIndependentOfLicenseAndInfrastructure(t *testing.T) {
	t.Setenv("ATLAS_REQUIRE_LICENSE", "1")
	var out, err bytes.Buffer
	if code := Run([]string{"version", "--json"}, strings.NewReader(""), &out, &err); code != 0 || !strings.Contains(out.String(), `"runtime":"go"`) {
		t.Fatalf("%d %s", code, err.String())
	}
}
