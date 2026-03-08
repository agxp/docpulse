package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// GenerateAPIKey creates a new random API key and returns the raw key (shown once)
// and its SHA-256 hash (stored in the database).
func GenerateAPIKey() (rawKey, keyHash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("generating random bytes: %w", err)
	}
	rawKey = "di_" + base64.RawURLEncoding.EncodeToString(b)
	keyHash = HashAPIKey(rawKey)
	return rawKey, keyHash, nil
}

// HashAPIKey returns the SHA-256 hex digest of the given key.
func HashAPIKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}
