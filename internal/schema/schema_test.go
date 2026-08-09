package schema

import (
	"encoding/json"
	"strings"
	"testing"
)

func validManifest(t *testing.T) []byte {
	t.Helper()
	return []byte(`{
	  "protocol": "kg.provider/v1",
	  "protocol_versions": [1],
	  "provider": {"id": "fake", "version": "0.1.0", "description": "fake provider"},
	  "capabilities": [{
	    "capability_id": "extract.entities_relations",
	    "title": "Extract",
	    "description": "Extracts.",
	    "side_effects": ["network"],
	    "input_schema": {"type": "object"},
	    "output": {"mode": "result-json", "kind": "kg-document"},
	    "cli_spec": {"subcommand": [], "always": [], "positionals": [], "flags": []}
	  }]
	}`)
}

func TestValidateManifestOK(t *testing.T) {
	if err := ValidateManifest(validManifest(t)); err != nil {
		t.Errorf("valid manifest must pass: %v", err)
	}
}

func TestValidateManifestRejects(t *testing.T) {
	cases := []struct {
		name string
		doc  string
	}{
		{"wrong protocol", strings.Replace(string(validManifest(t)), "kg.provider/v1", "kg.provider/v9", 1)},
		{"missing provider", `{"protocol":"kg.provider/v1","protocol_versions":[1],"capabilities":[]}`},
		{"bad side effect", strings.Replace(string(validManifest(t)), `"network"`, `"teleport"`, 1)},
		{"bad flag kind", strings.Replace(string(validManifest(t)), `"flags": []`, `"flags": [{"name":"x","flag":"--x","kind":"wat"}]`, 1)},
		{"not json", `not json`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateManifest([]byte(tc.doc)); err == nil {
				t.Error("invalid manifest must be rejected")
			}
		})
	}
}

func TestValidateAvailable(t *testing.T) {
	ok := `{"available": true, "ready": [{"name": "ollama", "kind": "service"}], "missing": [], "cache_dir": "/tmp/x"}`
	if err := ValidateAvailable([]byte(ok)); err != nil {
		t.Errorf("valid available report must pass: %v", err)
	}
	bad := `{"available": "yes", "ready": [], "missing": []}`
	if err := ValidateAvailable([]byte(bad)); err == nil {
		t.Error("non-boolean available must be rejected")
	}
}

func TestValidateInputAdditionalProperties(t *testing.T) {
	closed := json.RawMessage(`{
	  "type": "object",
	  "properties": {"file": {"type": "string"}},
	  "required": ["file"],
	  "additionalProperties": false
	}`)
	open := json.RawMessage(`{
	  "type": "object",
	  "properties": {"file": {"type": "string"}},
	  "required": ["file"]
	}`)

	// Closed schema rejects unknown properties.
	if err := ValidateInput(closed, map[string]any{"file": "a.md", "bogus": 1}); err == nil {
		t.Error("closed schema must reject unknown properties")
	}
	// Closed schema accepts known properties.
	if err := ValidateInput(closed, map[string]any{"file": "a.md"}); err != nil {
		t.Errorf("closed schema must accept known properties: %v", err)
	}
	// Open schema (additionalProperties absent) allows unknown properties.
	if err := ValidateInput(open, map[string]any{"file": "a.md", "extra": 42}); err != nil {
		t.Errorf("open schema must allow unknown properties: %v", err)
	}
	// Required applies in both.
	if err := ValidateInput(open, map[string]any{}); err == nil {
		t.Error("missing required property must be rejected")
	}
	// Type errors are caught.
	if err := ValidateInput(closed, map[string]any{"file": 123}); err == nil {
		t.Error("wrong property type must be rejected")
	}
	// Enum is enforced.
	withEnum := json.RawMessage(`{
	  "type": "object",
	  "properties": {"mode": {"type": "string", "enum": ["local", "global"]}}
	}`)
	if err := ValidateInput(withEnum, map[string]any{"mode": "cosmic"}); err == nil {
		t.Error("enum violation must be rejected")
	}
	if err := ValidateInput(withEnum, map[string]any{"mode": "local"}); err != nil {
		t.Errorf("enum member must pass: %v", err)
	}
}
