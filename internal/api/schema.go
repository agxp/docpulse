package api

import (
	"encoding/json"
	"errors"
)

// validateSchema checks that raw is a JSON Schema object usable for extraction.
// Rules:
//   - Must be a JSON object
//   - Must have "type": "object"
//   - Must have a "properties" field containing at least one property
//   - Each property value must be a JSON object with a "type" field
func validateSchema(raw []byte) error {
	var s map[string]json.RawMessage
	if err := json.Unmarshal(raw, &s); err != nil {
		return errors.New("schema must be a JSON object")
	}

	var topType string
	if err := json.Unmarshal(s["type"], &topType); err != nil || topType != "object" {
		return errors.New(`schema must have "type": "object"`)
	}

	var props map[string]json.RawMessage
	if err := json.Unmarshal(s["properties"], &props); err != nil || len(props) == 0 {
		return errors.New(`schema must have a "properties" object with at least one field`)
	}

	for name, propRaw := range props {
		var prop map[string]json.RawMessage
		if err := json.Unmarshal(propRaw, &prop); err != nil {
			return errors.New(`property "` + name + `" must be a JSON object`)
		}
		var propType string
		if err := json.Unmarshal(prop["type"], &propType); err != nil || propType == "" {
			return errors.New(`property "` + name + `" is missing a "type" field`)
		}
	}

	return nil
}
