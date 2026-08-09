// Package schema validates provider describe/available output against the
// embedded kg.provider/v1 JSON Schemas, and invocation inputs against each
// capability's declared input_schema (additionalProperties semantics:
// closed schemas reject unknown properties, open/absent ones allow them).
package schema

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed provider-v1.schema.json
var providerSchema []byte

//go:embed available-v1.schema.json
var availableSchema []byte

func compile(name string, raw json.RawMessage) (*jsonschema.Schema, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("schema %s: %w", name, err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(name, doc); err != nil {
		return nil, fmt.Errorf("schema %s: %w", name, err)
	}
	return c.Compile(name)
}

func decode(raw []byte) (any, error) {
	var doc any
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	return doc, nil
}

// ValidateManifest validates describe --json output against provider-v1.
func ValidateManifest(raw []byte) error {
	sch, err := compile("provider-v1.schema.json", providerSchema)
	if err != nil {
		return err
	}
	doc, err := decode(raw)
	if err != nil {
		return fmt.Errorf("manifest is not valid JSON: %w", err)
	}
	if err := sch.Validate(doc); err != nil {
		return fmt.Errorf("manifest violates provider-v1 schema: %w", err)
	}
	return nil
}

// ValidateAvailable validates available --json output against available-v1.
func ValidateAvailable(raw []byte) error {
	sch, err := compile("available-v1.schema.json", availableSchema)
	if err != nil {
		return err
	}
	doc, err := decode(raw)
	if err != nil {
		return fmt.Errorf("available report is not valid JSON: %w", err)
	}
	if err := sch.Validate(doc); err != nil {
		return fmt.Errorf("available report violates available-v1 schema: %w", err)
	}
	return nil
}

// ValidateInput validates an invocation input object against a capability's
// declared input_schema.
func ValidateInput(inputSchema json.RawMessage, input map[string]any) error {
	if len(inputSchema) == 0 {
		return nil
	}
	sch, err := compile("input-schema.json", inputSchema)
	if err != nil {
		return fmt.Errorf("invalid input_schema: %w", err)
	}
	if err := sch.Validate(any(input)); err != nil {
		return fmt.Errorf("input violates input_schema: %w", err)
	}
	return nil
}
