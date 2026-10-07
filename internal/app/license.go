package app

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// Preserve the existing optional offline gate during the runtime migration.
// Remote library authorization uses the Center's asymmetric RS256 JWT instead.
func checkLicense() error {
	if os.Getenv("ATLAS_REQUIRE_LICENSE") != "1" {
		return nil
	}
	parts := strings.Split(strings.TrimSpace(os.Getenv("ATLAS_LICENSE")), ".")
	key, err := hex.DecodeString(os.Getenv("ATLAS_LICENSE_KEY"))
	if len(parts) != 2 || err != nil || len(key) == 0 {
		return fmt.Errorf("valid offline license required")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return fmt.Errorf("invalid license")
	}
	h := hmac.New(sha256.New, key)
	h.Write([]byte(parts[0]))
	if !hmac.Equal(h.Sum(nil), sig) {
		return fmt.Errorf("invalid license signature")
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return fmt.Errorf("invalid license")
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(body, &claims) != nil || claims.Exp != 0 && time.Now().Unix() >= claims.Exp {
		return fmt.Errorf("invalid or expired license")
	}
	return nil
}
