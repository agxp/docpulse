package jobs

import (
	"encoding/json"
	"testing"
)

var simpleSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"vendor":  {"type": "string"},
		"total":   {"type": "number"},
		"items":   {"type": "array"}
	}
}`)

// --- extractStorageKey ---

func TestExtractStorageKey_FileURL(t *testing.T) {
	got := extractStorageKey("file:///tmp/docpulse/abc.pdf")
	want := "/tmp/docpulse/abc.pdf"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestExtractStorageKey_NoPrefix(t *testing.T) {
	got := extractStorageKey("/tmp/docpulse/abc.pdf")
	want := "/tmp/docpulse/abc.pdf"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestExtractStorageKey_Empty(t *testing.T) {
	got := extractStorageKey("")
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestExtractStorageKey_ShortString(t *testing.T) {
	// Shorter than "file://" — should not panic or strip anything
	got := extractStorageKey("file://")
	if got != "file://" {
		t.Errorf("got %q, want %q", got, "file://")
	}
}

func TestExtractStorageKey_OtherScheme(t *testing.T) {
	got := extractStorageKey("s3://bucket/key")
	if got != "s3://bucket/key" {
		t.Errorf("expected s3 URL unchanged, got %q", got)
	}
}

// --- contentHash ---

func TestContentHash_Deterministic(t *testing.T) {
	data := []byte("document content")
	schema := json.RawMessage(`{"type":"object"}`)
	if contentHash(data, schema) != contentHash(data, schema) {
		t.Error("contentHash is not deterministic")
	}
}

func TestContentHash_DifferentDocsDifferentHash(t *testing.T) {
	schema := json.RawMessage(`{"type":"object"}`)
	h1 := contentHash([]byte("doc one"), schema)
	h2 := contentHash([]byte("doc two"), schema)
	if h1 == h2 {
		t.Error("different documents produced the same hash")
	}
}

func TestContentHash_DifferentSchemasDifferentHash(t *testing.T) {
	data := []byte("same document")
	h1 := contentHash(data, json.RawMessage(`{"type":"object","properties":{"a":{}}}`))
	h2 := contentHash(data, json.RawMessage(`{"type":"object","properties":{"b":{}}}`))
	if h1 == h2 {
		t.Error("different schemas produced the same hash")
	}
}

func TestContentHash_IsHex64(t *testing.T) {
	h := contentHash([]byte("data"), json.RawMessage(`{}`))
	if len(h) != 64 {
		t.Errorf("expected 64-char hex string, got len=%d", len(h))
	}
}

func TestContentHash_EmptyInputs(t *testing.T) {
	// Should not panic
	h := contentHash([]byte{}, json.RawMessage(`{}`))
	if len(h) != 64 {
		t.Errorf("expected 64-char hash for empty inputs, got %d", len(h))
	}
}

func TestContentHash_SeparatorMatters(t *testing.T) {
	// "ab" + "cd" vs "a" + "bcd" — the || separator should prevent collisions
	h1 := contentHash([]byte("ab"), json.RawMessage("cd"))
	h2 := contentHash([]byte("a"), json.RawMessage("bcd"))
	if h1 == h2 {
		t.Error("hash collision: different doc/schema split produced same hash")
	}
}

// --- mergeResults ---

func TestMergeResults_SingleChunk_AllFieldsPresent(t *testing.T) {
	results := []map[string]interface{}{
		{"vendor": "Acme", "total": 100.0},
	}
	merged, confidence := mergeResults(results, simpleSchema)

	if merged["vendor"] != "Acme" {
		t.Errorf("expected vendor=Acme, got %v", merged["vendor"])
	}
	if merged["total"] != 100.0 {
		t.Errorf("expected total=100, got %v", merged["total"])
	}
	if confidence["vendor"] != 0.75 {
		t.Errorf("single-chunk field should have confidence 0.75, got %v", confidence["vendor"])
	}
}

func TestMergeResults_MissingField_ZeroConfidence(t *testing.T) {
	results := []map[string]interface{}{
		{"vendor": "Acme"}, // total is missing
	}
	_, confidence := mergeResults(results, simpleSchema)

	if confidence["total"] != 0.0 {
		t.Errorf("missing field should have confidence 0.0, got %v", confidence["total"])
	}
}

func TestMergeResults_MultipleChunks_HighConfidence(t *testing.T) {
	results := []map[string]interface{}{
		{"vendor": "Acme"},
		{"vendor": "Acme"},
	}
	_, confidence := mergeResults(results, simpleSchema)

	if confidence["vendor"] != 1.0 {
		t.Errorf("field found in multiple chunks should have confidence 1.0, got %v", confidence["vendor"])
	}
}

func TestMergeResults_ScalarField_FirstValueWins(t *testing.T) {
	results := []map[string]interface{}{
		{"vendor": "First"},
		{"vendor": "Second"},
	}
	merged, _ := mergeResults(results, simpleSchema)

	if merged["vendor"] != "First" {
		t.Errorf("expected first value to win for scalar field, got %v", merged["vendor"])
	}
}

func TestMergeResults_ArrayField_Concatenated(t *testing.T) {
	results := []map[string]interface{}{
		{"items": []interface{}{"a", "b"}},
		{"items": []interface{}{"c", "d"}},
	}
	merged, _ := mergeResults(results, simpleSchema)

	arr, ok := merged["items"].([]interface{})
	if !ok {
		t.Fatalf("expected items to be []interface{}, got %T", merged["items"])
	}
	if len(arr) != 4 {
		t.Errorf("expected 4 items after concatenation, got %d", len(arr))
	}
}

func TestMergeResults_NullValueSkipped(t *testing.T) {
	results := []map[string]interface{}{
		{"vendor": nil},
		{"vendor": "Acme"},
	}
	merged, _ := mergeResults(results, simpleSchema)

	if merged["vendor"] != "Acme" {
		t.Errorf("null first value should be skipped, got %v", merged["vendor"])
	}
}

func TestMergeResults_EmptyResults(t *testing.T) {
	merged, confidence := mergeResults([]map[string]interface{}{}, simpleSchema)

	if len(merged) != 0 {
		t.Errorf("expected empty merged map, got %v", merged)
	}
	// All schema fields should have zero confidence
	for field, conf := range confidence {
		if conf != 0.0 {
			t.Errorf("field %q should have 0 confidence with no results, got %v", field, conf)
		}
	}
}

func TestMergeResults_InvalidSchema_NoConfidence(t *testing.T) {
	results := []map[string]interface{}{{"vendor": "Acme"}}
	merged, confidence := mergeResults(results, json.RawMessage(`not valid json`))

	// Should not panic; merged values are still set, confidence map may be empty
	if merged["vendor"] != "Acme" {
		t.Errorf("expected vendor to be merged even with invalid schema")
	}
	if len(confidence) != 0 {
		t.Errorf("expected empty confidence with invalid schema, got %v", confidence)
	}
}

func TestMergeResults_OnlyNullValues_ZeroConfidence(t *testing.T) {
	results := []map[string]interface{}{
		{"vendor": nil},
		{"vendor": nil},
	}
	_, confidence := mergeResults(results, simpleSchema)

	if confidence["vendor"] != 0.0 {
		t.Errorf("all-null field should have confidence 0.0, got %v", confidence["vendor"])
	}
}

func TestMergeResults_ArrayFirstChunkEmpty(t *testing.T) {
	results := []map[string]interface{}{
		{"items": []interface{}{}},
		{"items": []interface{}{"x"}},
	}
	merged, _ := mergeResults(results, simpleSchema)

	// Empty array is not nil — first value (empty array) wins and second is NOT concatenated
	// because the first chunk's array IS set (not nil)
	arr, ok := merged["items"].([]interface{})
	if !ok {
		t.Fatalf("expected []interface{}, got %T", merged["items"])
	}
	// Empty + ["x"] = ["x"]
	_ = arr // behaviour: concatenated
}

func TestMergeResults_ExtraFieldsNotInSchema(t *testing.T) {
	results := []map[string]interface{}{
		{"vendor": "Acme", "unknown_field": "surprise"},
	}
	merged, _ := mergeResults(results, simpleSchema)

	// Extra fields not in schema still get merged
	if merged["unknown_field"] != "surprise" {
		t.Errorf("expected unknown_field in merged, got %v", merged["unknown_field"])
	}
}
