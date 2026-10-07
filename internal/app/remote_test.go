package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/H-I-V-E-Tec/hive_atlas/internal/core"
	"github.com/H-I-V-E-Tec/hive_atlas/internal/transport"
	"github.com/H-I-V-E-Tec/hive_atlas/signals"
	"github.com/golang-jwt/jwt/v5"
)

func TestSharedSessionAndRemoteMCP(t *testing.T) {
	t.Setenv("HIVE_HOME", t.TempDir())
	t.Setenv("HIVE_TOKEN", "")
	t.Setenv("HIVE_TOKEN_FILE", "")
	t.Setenv("ATLAS_OFFLINE", "")
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	center := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/jwks.json" {
			t.Error("product attempted login")
			w.WriteHeader(404)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]string{"kty": "RSA", "kid": "fixture", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())}}})
	}))
	defer center.Close()
	t.Setenv("HIVE_CENTER_URL", center.URL)
	subject := "00000000-0000-4000-8000-000000000001"
	mint := func(aud, issuer string, expiry int64, permissions []string) string {
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{"sub": subject, "aud": aud, "iss": issuer, "exp": expiry, "permissions": permissions})
		token.Header["kid"] = "fixture"
		signed, _ := token.SignedString(key)
		return signed
	}
	token := mint("hive", center.URL, time.Now().Add(time.Hour).Unix(), []string{"product.mind", "product.atlas"})
	os.WriteFile(tokenPath(), []byte(token), 0600)
	lib, _ := core.Load(signals.Core)
	auth, _ := transport.NewAuthorizer(center.URL)
	state := t.TempDir()
	handler := MCPHTTP(transport.Local{Lib: lib}, auth, state, nil)
	var calls atomic.Int32
	var checkSession atomic.Bool
	checkSession.Store(true)
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/mcp" {
			t.Error("client downloaded library instead of calling remote MCP")
		}
		if checkSession.Load() && r.Header.Get("Authorization") != "Bearer "+token {
			t.Error("client did not use shared session")
		}
		handler.ServeHTTP(w, r)
	}))
	defer service.Close()
	t.Setenv("HIVE_ATLAS_URL", service.URL)
	remote, err := remoteMCP()
	if err != nil {
		t.Fatal(err)
	}
	if err = remote.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	board, err := remote.Observe(context.Background(), []core.Evidence{{Kind: "js_finding", Value: "client_id redirect_uri", Flow: "oauth", ProgramID: "synthetic"}})
	if err != nil || len(board.Leads) != 2 {
		t.Fatalf("remote board: %v %v", board, err)
	}
	res, err := remote.Send(context.Background(), []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"atlas_feedback","arguments":{"signal_id":"C-02","outcome":"confirmado","program_id":"synthetic","note":"reviewed"}}}`))
	if err != nil || res.Error != nil {
		t.Fatalf("feedback: %v %v", res, err)
	}
	data, err := os.ReadFile(filepath.Join(state, "feedback", subject+".jsonl"))
	if err != nil || !bytes.Contains(data, []byte("reviewed")) {
		t.Fatal("feedback not persisted on service", err)
	}
	if _, err := os.Stat(filepath.Join(hiveHome(), "atlas", "feedback.jsonl")); !os.IsNotExist(err) {
		t.Fatal("online feedback persisted locally")
	}
	before := calls.Load()
	res, err = remote.Send(context.Background(), []byte(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"atlas_observe","arguments":{"evidence":[{"kind":"nota","value":"Authorization: Bearer synthetic"}]}}}`))
	if err != nil || calls.Load() != before {
		t.Fatal("credentials crossed network boundary")
	}
	if _, err := os.Stat(filepath.Join(hiveHome(), "atlas", "token")); !os.IsNotExist(err) {
		t.Fatal("created separate Atlas session")
	}
	checkSession.Store(false)
	request := func(method, raw, bearer, origin, protocol string) *http.Response {
		r, _ := http.NewRequest(method, service.URL+"/mcp", strings.NewReader(raw))
		r.Header.Set("Accept", "application/json, text/event-stream")
		r.Header.Set("Content-Type", "application/json")
		if bearer != "" {
			r.Header.Set("Authorization", "Bearer "+bearer)
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if protocol != "" {
			r.Header.Set("MCP-Protocol-Version", protocol)
		}
		res, err := service.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	for _, tc := range []struct {
		name, token, origin, protocol string
		status                        int
	}{
		{"missing", "", "", "", 401},
		{"mind only", mint("hive", center.URL, time.Now().Add(time.Hour).Unix(), []string{"product.mind"}), "", "", 403},
		{"expired", mint("hive", center.URL, time.Now().Add(-time.Hour).Unix(), []string{"product.atlas"}), "", "", 401},
		{"issuer", mint("hive", "https://other.invalid", time.Now().Add(time.Hour).Unix(), []string{"product.atlas"}), "", "", 401},
		{"audience", mint("mind", center.URL, time.Now().Add(time.Hour).Unix(), []string{"product.atlas"}), "", "", 401},
		{"tampered", token + "x", "", "", 401}, {"origin", token, "https://evil.invalid", "", 403}, {"protocol", token, "", "invalid", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := request("POST", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, tc.token, tc.origin, tc.protocol)
			defer r.Body.Close()
			if r.StatusCode != tc.status {
				t.Fatal(r.StatusCode)
			}
		})
	}
	for _, method := range []string{"GET", "DELETE"} {
		r := request(method, "", token, "", "")
		r.Body.Close()
		if r.StatusCode != 405 {
			t.Fatal(r.StatusCode)
		}
	}
	r := request("POST", `{"jsonrpc":"2.0","method":"notifications/initialized"}`, token, center.URL, "2025-11-25")
	data, _ = io.ReadAll(r.Body)
	r.Body.Close()
	if r.StatusCode != 202 || len(data) != 0 {
		t.Fatal("notification response", r.StatusCode, string(data))
	}
	r = request("POST", `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"atlas_observe","arguments":{"evidence":[{"kind":"nota","value":"Authorization: Bearer synthetic"}]}}}`, token, "", "")
	data, _ = io.ReadAll(r.Body)
	r.Body.Close()
	if !bytes.Contains(data, []byte(`"isError":true`)) {
		t.Fatal("direct client bypassed sanitization", string(data))
	}
}

func TestMissingSessionNeverFallsBackToLocalEngine(t *testing.T) {
	t.Setenv("HIVE_HOME", t.TempDir())
	t.Setenv("HIVE_TOKEN", "")
	t.Setenv("HIVE_TOKEN_FILE", "")
	t.Setenv("ATLAS_OFFLINE", "")
	var out, stderr bytes.Buffer
	if code := Run([]string{"mcp"}, strings.NewReader(""), &out, &stderr); code != 1 || !strings.Contains(stderr.String(), "hive login") {
		t.Fatalf("%d %s", code, stderr.String())
	}
}

func TestSSEReply(t *testing.T) {
	data, err := sseResponse([]byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{}}\n\n"), json.RawMessage("2"))
	if err != nil || !json.Valid(data) {
		t.Fatal(err)
	}
}
