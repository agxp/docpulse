package llm

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/agxp/docpulse/internal/domain"
)

// testRouter returns a Router with no real OpenAI client — safe for pure function tests.
func testRouter(threshold int) *Router {
	return &Router{
		fastModel:           "gpt-4o-mini",
		strongModel:         "gpt-4o",
		complexityThreshold: threshold,
	}
}

var (
	simpleSchema = json.RawMessage(`{
		"type": "object",
		"properties": {
			"vendor": {"type": "string"},
			"total":  {"type": "number"}
		},
		"required": ["vendor", "total"]
	}`)

	nestedSchema = json.RawMessage(`{
		"type": "object",
		"properties": {
			"vendor":     {"type": "string"},
			"line_items": {"type": "array", "items": {"type": "object"}}
		}
	}`)

	objectSchema = json.RawMessage(`{
		"type": "object",
		"properties": {
			"address": {"type": "object"}
		}
	}`)

	noPropertiesSchema = json.RawMessage(`{"type": "object"}`)
	invalidSchema      = json.RawMessage(`not json`)
)

// --- countSchemaFields ---

func TestCountSchemaFields_SimpleSchema(t *testing.T) {
	if got := countSchemaFields(simpleSchema); got != 2 {
		t.Errorf("expected 2, got %d", got)
	}
}

func TestCountSchemaFields_NoProperties(t *testing.T) {
	if got := countSchemaFields(noPropertiesSchema); got != 0 {
		t.Errorf("expected 0, got %d", got)
	}
}

func TestCountSchemaFields_InvalidJSON(t *testing.T) {
	if got := countSchemaFields(invalidSchema); got != 0 {
		t.Errorf("expected 0 for invalid JSON, got %d", got)
	}
}

func TestCountSchemaFields_Empty(t *testing.T) {
	if got := countSchemaFields(json.RawMessage(`{}`)); got != 0 {
		t.Errorf("expected 0 for empty schema, got %d", got)
	}
}

func TestCountSchemaFields_ManyFields(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{
		"a":{},"b":{},"c":{},"d":{},"e":{},"f":{},"g":{},"h":{},"i":{},"j":{}
	}}`)
	if got := countSchemaFields(schema); got != 10 {
		t.Errorf("expected 10, got %d", got)
	}
}

// --- hasNestedStructures ---

func TestHasNestedStructures_ArrayField_True(t *testing.T) {
	if !hasNestedStructures(nestedSchema) {
		t.Error("expected true for schema with array field")
	}
}

func TestHasNestedStructures_ObjectField_True(t *testing.T) {
	if !hasNestedStructures(objectSchema) {
		t.Error("expected true for schema with object field")
	}
}

func TestHasNestedStructures_SimpleSchema_False(t *testing.T) {
	if hasNestedStructures(simpleSchema) {
		t.Error("expected false for schema with only scalar fields")
	}
}

func TestHasNestedStructures_NoProperties_False(t *testing.T) {
	if hasNestedStructures(noPropertiesSchema) {
		t.Error("expected false for schema with no properties")
	}
}

func TestHasNestedStructures_InvalidJSON_False(t *testing.T) {
	if hasNestedStructures(invalidSchema) {
		t.Error("expected false for invalid JSON")
	}
}

func TestHasNestedStructures_EmptySchema_False(t *testing.T) {
	if hasNestedStructures(json.RawMessage(`{}`)) {
		t.Error("expected false for empty schema")
	}
}

// --- selectTier ---

func TestSelectTier_SimpleSchema_ShortText_Fast(t *testing.T) {
	r := testRouter(10)
	req := ExtractionRequest{
		ChunkText: "short text",
		Schema:    simpleSchema, // 2 fields, no nesting
	}
	if got := r.selectTier(req); got != domain.ModelTierFast {
		t.Errorf("expected fast, got %s", got)
	}
}

func TestSelectTier_ManyFields_Strong(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{
		"a":{},"b":{},"c":{},"d":{},"e":{}
	}}`)
	r := testRouter(4) // threshold=4, schema has 5 fields
	req := ExtractionRequest{ChunkText: "short", Schema: schema}
	if got := r.selectTier(req); got != domain.ModelTierStrong {
		t.Errorf("expected strong for high field count, got %s", got)
	}
}

func TestSelectTier_ExactlyAtThreshold_Fast(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{
		"a":{},"b":{},"c":{},"d":{}
	}}`)
	r := testRouter(4) // threshold=4, schema has 4 fields — not greater than, so fast
	req := ExtractionRequest{ChunkText: "short", Schema: schema}
	if got := r.selectTier(req); got != domain.ModelTierFast {
		t.Errorf("expected fast at exact threshold, got %s", got)
	}
}

func TestSelectTier_LongChunk_Strong(t *testing.T) {
	r := testRouter(10)
	req := ExtractionRequest{
		ChunkText: strings.Repeat("x", 3001),
		Schema:    simpleSchema,
	}
	if got := r.selectTier(req); got != domain.ModelTierStrong {
		t.Errorf("expected strong for long chunk, got %s", got)
	}
}

func TestSelectTier_ExactlyAtChunkLimit_Fast(t *testing.T) {
	r := testRouter(10)
	req := ExtractionRequest{
		ChunkText: strings.Repeat("x", 3000), // not greater than 3000
		Schema:    simpleSchema,
	}
	if got := r.selectTier(req); got != domain.ModelTierFast {
		t.Errorf("expected fast at exact chunk limit, got %s", got)
	}
}

func TestSelectTier_NestedSchema_Strong(t *testing.T) {
	r := testRouter(10)
	req := ExtractionRequest{ChunkText: "short", Schema: nestedSchema}
	if got := r.selectTier(req); got != domain.ModelTierStrong {
		t.Errorf("expected strong for nested schema, got %s", got)
	}
}

// --- modelForTier ---

func TestModelForTier_Fast(t *testing.T) {
	r := testRouter(10)
	if got := r.modelForTier(domain.ModelTierFast); got != "gpt-4o-mini" {
		t.Errorf("expected gpt-4o-mini, got %s", got)
	}
}

func TestModelForTier_Strong(t *testing.T) {
	r := testRouter(10)
	if got := r.modelForTier(domain.ModelTierStrong); got != "gpt-4o" {
		t.Errorf("expected gpt-4o, got %s", got)
	}
}

// --- validateResponse ---

func TestValidateResponse_AllRequiredPresent_NoError(t *testing.T) {
	r := testRouter(10)
	raw := json.RawMessage(`{"vendor":"Acme","total":100}`)
	if err := r.validateResponse(raw, simpleSchema); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateResponse_MissingRequired_Error(t *testing.T) {
	r := testRouter(10)
	raw := json.RawMessage(`{"vendor":"Acme"}`) // missing "total"
	if err := r.validateResponse(raw, simpleSchema); err == nil {
		t.Error("expected error for missing required field")
	}
}

func TestValidateResponse_ExtraFields_NoError(t *testing.T) {
	r := testRouter(10)
	raw := json.RawMessage(`{"vendor":"Acme","total":100,"bonus":"field"}`)
	if err := r.validateResponse(raw, simpleSchema); err != nil {
		t.Errorf("unexpected error for extra fields: %v", err)
	}
}

func TestValidateResponse_InvalidJSON_Error(t *testing.T) {
	r := testRouter(10)
	if err := r.validateResponse(json.RawMessage(`not json`), simpleSchema); err == nil {
		t.Error("expected error for invalid JSON response")
	}
}

func TestValidateResponse_InvalidSchema_NoError(t *testing.T) {
	// Can't parse schema → skip required check → no error
	r := testRouter(10)
	raw := json.RawMessage(`{"vendor":"Acme"}`)
	if err := r.validateResponse(raw, invalidSchema); err != nil {
		t.Errorf("unparseable schema should not cause error, got: %v", err)
	}
}

func TestValidateResponse_NoRequiredFields_NoError(t *testing.T) {
	r := testRouter(10)
	raw := json.RawMessage(`{}`)
	schema := json.RawMessage(`{"type":"object","properties":{"vendor":{"type":"string"}}}`)
	if err := r.validateResponse(raw, schema); err != nil {
		t.Errorf("schema with no required fields should not error, got: %v", err)
	}
}

func TestValidateResponse_NullRequiredField_Error(t *testing.T) {
	r := testRouter(10)
	// Field is present but null — key exists in JSON so it passes
	raw := json.RawMessage(`{"vendor":null,"total":null}`)
	if err := r.validateResponse(raw, simpleSchema); err != nil {
		t.Errorf("null field is still present in JSON, should not error: %v", err)
	}
}

// --- buildSystemPrompt ---

func TestBuildSystemPrompt_ContainsSchema(t *testing.T) {
	schema := json.RawMessage(`{"type":"object"}`)
	prompt := buildSystemPrompt(schema, 0, 1)
	if !strings.Contains(prompt, `{"type":"object"}`) {
		t.Error("system prompt should contain the schema")
	}
}

func TestBuildSystemPrompt_SingleChunk_NoChunkNote(t *testing.T) {
	prompt := buildSystemPrompt(simpleSchema, 0, 1)
	if strings.Contains(prompt, "chunk") {
		t.Error("single-chunk prompt should not mention chunk numbering")
	}
}

func TestBuildSystemPrompt_MultiChunk_ContainsChunkNote(t *testing.T) {
	prompt := buildSystemPrompt(simpleSchema, 2, 5)
	if !strings.Contains(prompt, "chunk 3 of 5") {
		t.Errorf("multi-chunk prompt should say 'chunk 3 of 5', got:\n%s", prompt)
	}
}

func TestBuildSystemPrompt_ContainsRules(t *testing.T) {
	prompt := buildSystemPrompt(simpleSchema, 0, 1)
	for _, rule := range []string{"RULES", "null", "hallucinate", "EXTRACTION SCHEMA"} {
		if !strings.Contains(prompt, rule) {
			t.Errorf("prompt missing expected content: %q", rule)
		}
	}
}

func TestBuildSystemPrompt_FirstChunkOfMany(t *testing.T) {
	prompt := buildSystemPrompt(simpleSchema, 0, 3)
	if !strings.Contains(prompt, "chunk 1 of 3") {
		t.Errorf("expected 'chunk 1 of 3' in prompt, got:\n%s", prompt)
	}
}

// --- buildUserPrompt ---

func TestBuildUserPrompt_ContainsText(t *testing.T) {
	text := "Invoice: INV-001"
	prompt := buildUserPrompt(text)
	if !strings.Contains(prompt, text) {
		t.Errorf("user prompt should contain the document text")
	}
}

func TestBuildUserPrompt_HasDocumentTextHeader(t *testing.T) {
	prompt := buildUserPrompt("anything")
	if !strings.HasPrefix(prompt, "DOCUMENT TEXT:") {
		t.Errorf("user prompt should start with 'DOCUMENT TEXT:', got: %q", prompt[:min(30, len(prompt))])
	}
}

func TestBuildUserPrompt_EmptyText(t *testing.T) {
	prompt := buildUserPrompt("")
	if prompt == "" {
		t.Error("prompt should not be empty even for empty text")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
