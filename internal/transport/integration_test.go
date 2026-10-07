package transport

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/H-I-V-E-Tec/hive_atlas/internal/core"
	"github.com/H-I-V-E-Tec/hive_atlas/signals"
	"github.com/golang-jwt/jwt/v5"
)

func TestAuthenticatedRemoteLibraryAndSearch(t *testing.T) {
	t.Setenv("HIVE_TOKEN", "")
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	center := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/jwks.json" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]string{"kty": "RSA", "kid": "fixture", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())}}})
	}))
	defer center.Close()
	auth, _ := NewAuthorizer(center.URL)
	lib, _ := core.Load(signals.Core)
	s := httptest.NewServer(API(Local{lib}, auth, "v2.0.0"))
	defer s.Close()
	mint := func(aud, issuer string, expiry int64, allowed bool) string {
		permissions := []string{}
		if allowed {
			permissions = append(permissions, "product.atlas")
		}
		tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{"sub": "00000000-0000-4000-8000-000000000001", "aud": aud, "iss": issuer, "exp": expiry, "permissions": permissions})
		tok.Header["kid"] = "fixture"
		raw, err := tok.SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	now := time.Now().Unix()
	good := mint("hive", center.URL, now+600, true)
	path := filepath.Join(t.TempDir(), "token")
	_ = os.WriteFile(path, []byte(good), 0600)
	r := Remote{URL: s.URL, TokenFile: path, Client: s.Client()}
	remoteLib, err := r.Library(context.Background())
	if err != nil || len(remoteLib.Signals) != 3 {
		t.Fatalf("library: %v", err)
	}
	hits, err := r.Search(context.Background(), "oauth redirect_uri client_id", 3)
	if err != nil || len(hits) == 0 {
		t.Fatalf("search: %v", err)
	}
	for _, tc := range []struct {
		name, token string
		status      int
	}{
		{"missing", "", 401}, {"mind audience", mint("mind", center.URL, now+600, true), 401}, {"issuer", mint("hive", "https://other.invalid", now+600, true), 401}, {"expired", mint("hive", center.URL, now-1, true), 401}, {"permission", mint("hive", center.URL, now+600, false), 403}, {"tampered", good + "x", 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", s.URL+"/api/v1/library", nil)
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			resp, err := s.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.status {
				t.Fatalf("status %d", resp.StatusCode)
			}
		})
	}
	for _, body := range []string{`{"query":"oauth","k":-1}`, `{"query":"oauth","extra":1}`, `{"query":"oauth"} {}`, `{}`, `{"query":"oauth","k":101}`} {
		req, _ := http.NewRequest("POST", s.URL+"/api/v1/search", bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+good)
		resp, err := s.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != 400 {
			t.Errorf("bad body status %d", resp.StatusCode)
		}
	}
}
func TestCredentialRedirectBlocked(t *testing.T) {
	leaked := false
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked = true }))
	defer other.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	c, _ := HTTPClient("")
	err := JSON(context.Background(), c, "GET", origin.URL, "synthetic-token", nil, nil)
	if err == nil || leaked {
		t.Fatal("credential redirect followed")
	}
}
func TestURLBoundary(t *testing.T) {
	for _, raw := range []string{"http://example.org", "https://user:secret@example.org", "https://example.org?token=secret", "file:///tmp/x"} {
		if _, err := ValidateURL(raw); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
}
