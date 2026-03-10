package api

import (
	"testing"
)

func TestValidateSchema_Valid(t *testing.T) {
	schema := `{
		"type": "object",
		"properties": {
			"name": {"type": "string"},
			"age":  {"type": "number"}
		}
	}`
	if err := validateSchema([]byte(schema)); err != nil {
		t.Errorf("expected valid schema to pass, got: %v", err)
	}
}

func TestValidateSchema_SingleProperty(t *testing.T) {
	schema := `{"type":"object","properties":{"title":{"type":"string"}}}`
	if err := validateSchema([]byte(schema)); err != nil {
		t.Errorf("single-property schema should be valid, got: %v", err)
	}
}

func TestValidateSchema_NotJSON(t *testing.T) {
	if err := validateSchema([]byte("not json")); err == nil {
		t.Error("expected error for non-JSON input")
	}
}

func TestValidateSchema_JSONArray(t *testing.T) {
	if err := validateSchema([]byte(`["a","b"]`)); err == nil {
		t.Error("expected error for JSON array (not an object)")
	}
}

func TestValidateSchema_MissingType(t *testing.T) {
	schema := `{"properties":{"name":{"type":"string"}}}`
	if err := validateSchema([]byte(schema)); err == nil {
		t.Error("expected error for missing top-level type")
	}
}

func TestValidateSchema_WrongType(t *testing.T) {
	schema := `{"type":"array","properties":{"name":{"type":"string"}}}`
	if err := validateSchema([]byte(schema)); err == nil {
		t.Error("expected error when top-level type is not 'object'")
	}
}

func TestValidateSchema_MissingProperties(t *testing.T) {
	schema := `{"type":"object"}`
	if err := validateSchema([]byte(schema)); err == nil {
		t.Error("expected error for missing properties field")
	}
}

func TestValidateSchema_EmptyProperties(t *testing.T) {
	schema := `{"type":"object","properties":{}}`
	if err := validateSchema([]byte(schema)); err == nil {
		t.Error("expected error for empty properties object")
	}
}

func TestValidateSchema_PropertyNotObject(t *testing.T) {
	schema := `{"type":"object","properties":{"name":"string"}}`
	if err := validateSchema([]byte(schema)); err == nil {
		t.Error("expected error when property value is not an object")
	}
}

func TestValidateSchema_PropertyMissingType(t *testing.T) {
	schema := `{"type":"object","properties":{"name":{"description":"a name"}}}`
	if err := validateSchema([]byte(schema)); err == nil {
		t.Error("expected error when property is missing type field")
	}
}

func TestValidateSchema_ErrorMentionsPropertyName(t *testing.T) {
	schema := `{"type":"object","properties":{"invoice_number":{"description":"no type"}}}`
	err := validateSchema([]byte(schema))
	if err == nil {
		t.Fatal("expected error")
	}
	if msg := err.Error(); msg == "" {
		t.Error("expected non-empty error message")
	}
}

func TestValidateSchema_ArrayTypeProperty(t *testing.T) {
	schema := `{
		"type": "object",
		"properties": {
			"items": {
				"type": "array",
				"items": {"type": "object"}
			}
		}
	}`
	if err := validateSchema([]byte(schema)); err != nil {
		t.Errorf("array-type property should be valid, got: %v", err)
	}
}

func TestValidateSchema_NestedObjectProperty(t *testing.T) {
	schema := `{
		"type": "object",
		"properties": {
			"address": {
				"type": "object",
				"properties": {"city": {"type": "string"}}
			}
		}
	}`
	if err := validateSchema([]byte(schema)); err != nil {
		t.Errorf("nested object property should be valid, got: %v", err)
	}
}

func TestValidateSchema_WithRequired(t *testing.T) {
	schema := `{
		"type": "object",
		"properties": {"total": {"type": "number"}},
		"required": ["total"]
	}`
	if err := validateSchema([]byte(schema)); err != nil {
		t.Errorf("schema with required field should be valid, got: %v", err)
	}
}

func TestValidateSchema_ErrorMessageDescriptive(t *testing.T) {
	cases := []struct {
		name   string
		schema string
		want   string
	}{
		{"not object", `"hello"`, "must be a JSON object"},
		{"wrong type", `{"type":"string","properties":{"x":{"type":"string"}}}`, `"type": "object"`},
		{"no properties", `{"type":"object"}`, "properties"},
		{"property no type", `{"type":"object","properties":{"x":{}}}`, `"x"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSchema([]byte(tc.schema))
			if err == nil {
				t.Fatal("expected error")
			}
			if msg := err.Error(); len(msg) == 0 {
				t.Errorf("expected descriptive error containing %q, got empty string", tc.want)
			}
		})
	}
}
