package transport

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func jwks(t *testing.T, key *rsa.PublicKey, kid string) []byte {
	t.Helper()
	data, err := json.Marshal(map[string]any{"keys": []any{map[string]string{
		"kty": "RSA", "kid": kid,
		"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// A Center outage must not reject a session whose signing key is already
// known, and a refresh that drops the key must still revoke it.
func TestKnownKeySurvivesJWKSOutageButHonoursRevocation(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	document := jwks(t, &key.PublicKey, "fixture")
	available := true
	center := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/jwks.json" {
			http.NotFound(w, r)
			return
		}
		if !available {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write(document)
	}))
	defer center.Close()

	auth, err := NewAuthorizer(center.URL)
	if err != nil {
		t.Fatal(err)
	}
	token := func() string {
		tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
			"sub": "00000000-0000-4000-8000-000000000001", "aud": "hive",
			"iss": center.URL, "exp": time.Now().Add(10 * time.Minute).Unix(),
			"permissions": []string{"product.atlas"},
		})
		tok.Header["kid"] = "fixture"
		raw, err := tok.SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	ctx := context.Background()
	if _, err := auth.Validate(ctx, token()); err != nil {
		t.Fatalf("first validation: %v", err)
	}

	// Age the cache past its freshness window and take the Center down.
	stale := func() {
		auth.mu.Lock()
		auth.fetched = time.Now().Add(-time.Hour)
		auth.lastRefresh = time.Now().Add(-time.Hour)
		auth.mu.Unlock()
	}
	stale()
	available = false
	if _, err := auth.Validate(ctx, token()); err != nil {
		t.Fatalf("outage rejected a known key: %v", err)
	}

	// The Center comes back having rotated the key away: the session is revoked.
	rotated, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	document = jwks(t, &rotated.PublicKey, "rotated")
	available = true
	stale()
	if _, err := auth.Validate(ctx, token()); err == nil {
		t.Fatal("a revoked key was still accepted")
	}
}
