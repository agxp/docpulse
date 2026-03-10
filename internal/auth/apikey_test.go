package auth

import (
	"strings"
	"testing"
)

// --- HashAPIKey ---

func TestHashAPIKey_Deterministic(t *testing.T) {
	key := "di_testkey123"
	if HashAPIKey(key) != HashAPIKey(key) {
		t.Error("HashAPIKey is not deterministic")
	}
}

func TestHashAPIKey_IsHex64Chars(t *testing.T) {
	h := HashAPIKey("di_somekey")
	if len(h) != 64 {
		t.Errorf("expected 64-char hex string (SHA-256), got len=%d: %s", len(h), h)
	}
	for _, c := range h {
		if !strings.ContainsRune("0123456789abcdef", c) {
			t.Errorf("non-hex character %q in hash", c)
		}
	}
}

func TestHashAPIKey_DifferentInputsDifferentHashes(t *testing.T) {
	if HashAPIKey("key_a") == HashAPIKey("key_b") {
		t.Error("different keys produced the same hash")
	}
}

func TestHashAPIKey_EmptyString(t *testing.T) {
	// SHA-256 of empty string is well-defined — just verify it doesn't panic
	h := HashAPIKey("")
	if len(h) != 64 {
		t.Errorf("expected 64-char hash for empty string, got %d", len(h))
	}
}

func TestHashAPIKey_KnownValue(t *testing.T) {
	// SHA-256("abc") = ba7816bf8f01cfea414140de5dae2ec73b00361bbef0469f492c539169782b
	// Verifies we're actually doing SHA-256 and not something else
	h := HashAPIKey("abc")
	if !strings.HasPrefix(h, "ba7816bf") {
		t.Errorf("unexpected hash for 'abc': %s", h)
	}
}

// --- GenerateAPIKey ---

func TestGenerateAPIKey_HasPrefix(t *testing.T) {
	raw, _, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(raw, "di_") {
		t.Errorf("expected key to start with 'di_', got %q", raw)
	}
}

func TestGenerateAPIKey_HashMatchesRawKey(t *testing.T) {
	raw, hash, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if HashAPIKey(raw) != hash {
		t.Error("returned hash does not match HashAPIKey(rawKey)")
	}
}

func TestGenerateAPIKey_Unique(t *testing.T) {
	raw1, _, _ := GenerateAPIKey()
	raw2, _, _ := GenerateAPIKey()
	if raw1 == raw2 {
		t.Error("two generated keys are identical")
	}
}

func TestGenerateAPIKey_HashIsUnique(t *testing.T) {
	_, hash1, _ := GenerateAPIKey()
	_, hash2, _ := GenerateAPIKey()
	if hash1 == hash2 {
		t.Error("two generated hashes are identical")
	}
}

func TestGenerateAPIKey_ReasonableLength(t *testing.T) {
	raw, _, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// "di_" + base64(32 bytes) = 3 + 43 = 46 chars
	if len(raw) < 40 {
		t.Errorf("key too short: %d chars", len(raw))
	}
}
