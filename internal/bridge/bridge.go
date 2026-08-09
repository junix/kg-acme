// Package bridge holds the hub's built-in compatibility bridges: fallback
// argv tables for CLIs that predate the kg.provider/v1 protocol.
//
// Iron rule: the hub never hardcodes provider options as logic — fallback
// tables are data, used only when the provider does not self-describe.
// When a probed provider's cli_spec differs from the fallback table, the
// provider wins and the hub emits a diagnostic.
//
// The tables are keyed by the provider-published capability namespace
// (extract.entities_relations, parse.multimodal, retrieve.ask, ...) — the
// provider id is the single source of truth, so each table mirrors the
// provider's published cli_spec as closely as possible. Only capabilities
// with a genuine argv invocation form are bridged: graph-in capabilities
// (detect.communities, resolve.coref, store.triples, ...) take their input
// document through the protocol invoke request on stdin and are therefore
// protocol-only — no fallback argv can drive them.
package bridge

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"kg-acme/internal/protocol"
)

// FallbackProvider is a built-in bridge for one legacy CLI.
type FallbackProvider struct {
	ID           string // provider id == binary name
	Bin          string
	Capabilities []FallbackCapability
}

// FallbackCapability is the hub's fallback description of one capability.
type FallbackCapability struct {
	CapabilityID string
	SideEffects  []string
	Output       protocol.OutputSpec
	InputSchema  json.RawMessage
	CLISpec      protocol.CLISpec
}

// CLISpecDiffDiagnostic is emitted when a probed provider cli_spec differs
// from the hub fallback table (provider is authoritative).
const CLISpecDiffDiagnostic = "cli_spec differs from hub data table (provider authoritative; hub data table is fallback)"

// Table returns the built-in fallback bridges.
func Table() []FallbackProvider {
	return []FallbackProvider{kgExtract(), kgMM(), ygr()}
}

// Find returns the fallback provider by id, or nil.
func Find(id string) *FallbackProvider {
	for _, p := range Table() {
		if p.ID == id {
			cp := p
			return &cp
		}
	}
	return nil
}

// Capability returns one fallback capability by capability_id, or nil.
func (p *FallbackProvider) Capability(capabilityID string) *FallbackCapability {
	for i := range p.Capabilities {
		if p.Capabilities[i].CapabilityID == capabilityID {
			return &p.Capabilities[i]
		}
	}
	return nil
}

// kgExtract mirrors kg-extract's published extract.entities_relations
// cli_spec. Legacy argv form: `kg-extract -o kg-protocol [--file f] [flags]`
// prints the kg.protocol.v1 document JSON on stdout (result-json). The
// graph-in capabilities kg-extract also publishes (detect.communities,
// resolve.coref, ...) are protocol-only and intentionally not bridged.
func kgExtract() FallbackProvider {
	flag := func(name, flag, kind string, order int, def any) protocol.FlagSpec {
		return protocol.FlagSpec{Name: name, Flag: flag, Kind: kind, Optional: true, Default: def, Order: order}
	}
	return FallbackProvider{
		ID:  "kg-extract",
		Bin: "kg-extract",
		Capabilities: []FallbackCapability{
			{
				CapabilityID: "extract.entities_relations",
				SideEffects:  []string{"network", "data_egress"},
				Output:       protocol.OutputSpec{Mode: "result-json", Kind: "kg-document"},
				InputSchema: json.RawMessage(`{
				  "type": "object",
				  "properties": {
				    "text": {"type": "string", "description": "inline input text (alternative to file)"},
				    "file": {"type": "string", "description": "input document path"},
				    "input_format": {"type": "string"},
				    "engine": {"type": "string"},
				    "backend": {"type": "string"},
				    "agent": {"type": "string"},
				    "model": {"type": "string"},
				    "chunker": {"type": "string"},
				    "schema": {"type": "string"},
				    "schema_mode": {"type": "string"},
				    "preset": {"type": "string"},
				    "preset_file": {"type": "string"},
				    "lang": {"type": "string"},
				    "max_rounds": {"type": "number"},
				    "merge_strategy": {"type": "string"},
				    "coref": {"type": "boolean"},
				    "canonical_direction": {"type": "boolean"},
				    "max_concurrency": {"type": "number"},
				    "relation_gleaning": {"type": "number"},
				    "mock_response": {"type": "string"},
				    "mock_tool_calls": {"type": "string"}
				  },
				  "additionalProperties": false
				}`),
				CLISpec: protocol.CLISpec{
					Subcommand:  []string{},
					Always:      []string{"-o", "kg-protocol"},
					Positionals: []protocol.PositionalSpec{},
					Flags: []protocol.FlagSpec{
						flag("file", "--file", protocol.FlagString, 1, nil),
						flag("input_format", "--input-format", protocol.FlagString, 2, "text"),
						flag("engine", "--engine", protocol.FlagString, 3, "simple"),
						flag("backend", "--backend", protocol.FlagString, 4, "llms"),
						flag("agent", "--agent", protocol.FlagString, 5, "minimaxcc"),
						flag("model", "--model", protocol.FlagString, 6, nil),
						flag("chunker", "--chunker", protocol.FlagString, 7, "recursive"),
						flag("schema", "--schema", protocol.FlagString, 8, nil),
						flag("schema_mode", "--schema-mode", protocol.FlagString, 9, "open"),
						flag("preset", "--preset", protocol.FlagString, 10, nil),
						flag("preset_file", "--preset-file", protocol.FlagString, 11, nil),
						flag("lang", "--lang", protocol.FlagString, 12, nil),
						flag("max_rounds", "--max-rounds", protocol.FlagNumber, 13, float64(1)),
						flag("merge_strategy", "--merge-strategy", protocol.FlagString, 14, "keep-existing"),
						flag("coref", "--coref", protocol.FlagBoolean, 15, false),
						flag("canonical_direction", "--canonical-direction", protocol.FlagBoolean, 16, false),
						flag("max_concurrency", "--max-concurrency", protocol.FlagNumber, 17, float64(8)),
						flag("relation_gleaning", "--relation-gleaning", protocol.FlagNumber, 18, float64(0)),
						flag("mock_response", "--mock-response", protocol.FlagString, 19, nil),
						flag("mock_tool_calls", "--mock-tool-calls", protocol.FlagString, 20, nil),
					},
				},
			},
		},
	}
}

// kgMM mirrors kg-mm's published parse.multimodal cli_spec. Legacy argv
// form: `kg-mm analyze <sidecar> [flags]`.
func kgMM() FallbackProvider {
	return FallbackProvider{
		ID:  "kg-mm",
		Bin: "kg-mm",
		Capabilities: []FallbackCapability{
			{
				CapabilityID: "parse.multimodal",
				SideEffects:  []string{"network", "data_egress"},
				Output:       protocol.OutputSpec{Mode: "result-json", Kind: "chunks"},
				InputSchema: json.RawMessage(`{
				  "type": "object",
				  "properties": {
				    "sidecar": {"type": "string", "description": "sidecar JSON describing the documents to parse"},
				    "backend": {"type": "string"},
				    "vlm_model": {"type": "string"},
				    "llm_bin": {"type": "string"},
				    "ocr_bin": {"type": "string"},
				    "no_ocr": {"type": "boolean"},
				    "language": {"type": "string"},
				    "mock_response": {"type": "array", "items": {"type": "string"}},
				    "mock_ocr": {"type": "string"}
				  },
				  "required": ["sidecar"],
				  "additionalProperties": false
				}`),
				CLISpec: protocol.CLISpec{
					Subcommand:  []string{"analyze"},
					Always:      []string{},
					Positionals: []protocol.PositionalSpec{{Name: "sidecar", Required: true}},
					Flags: []protocol.FlagSpec{
						{Name: "backend", Flag: "--backend", Kind: protocol.FlagString, Optional: true, Default: "cli", Order: 10},
						{Name: "vlm_model", Flag: "--vlm-model", Kind: protocol.FlagString, Optional: true, Order: 20},
						{Name: "llm_bin", Flag: "--llm-bin", Kind: protocol.FlagString, Optional: true, Default: "llm", Order: 30},
						{Name: "ocr_bin", Flag: "--ocr-bin", Kind: protocol.FlagString, Optional: true, Default: "ocr", Order: 40},
						{Name: "no_ocr", Flag: "--no-ocr", Kind: protocol.FlagBoolean, Optional: true, Default: false, Order: 50},
						{Name: "language", Flag: "--language", Kind: protocol.FlagString, Optional: true, Default: "English", Order: 60},
						{Name: "mock_response", Flag: "--mock-response", Kind: protocol.FlagArray, Optional: true, Repeatable: true, Order: 70},
						{Name: "mock_ocr", Flag: "--mock-ocr", Kind: protocol.FlagString, Optional: true, Default: "", Order: 80},
					},
				},
			},
		},
	}
}

// ygr mirrors ygr's published retrieve.ask cli_spec. Legacy argv form:
// `ygr ask --dataset <d> --question <q> [--mode m] [--top-k n] [--config c]`.
func ygr() FallbackProvider {
	return FallbackProvider{
		ID:  "ygr",
		Bin: "ygr",
		Capabilities: []FallbackCapability{
			{
				CapabilityID: "retrieve.ask",
				SideEffects:  []string{"network", "data_egress"},
				Output:       protocol.OutputSpec{Mode: "result-json", Kind: "json"},
				InputSchema: json.RawMessage(`{
				  "type": "object",
				  "properties": {
				    "dataset": {"type": "string", "description": "GraphRAG dataset with a built graph"},
				    "question": {"type": "string", "description": "natural-language question"},
				    "mode": {"type": "string"},
				    "top_k": {"type": "number"},
				    "config": {"type": "string"}
				  },
				  "required": ["dataset", "question"],
				  "additionalProperties": false
				}`),
				CLISpec: protocol.CLISpec{
					Subcommand:  []string{"ask"},
					Always:      []string{},
					Positionals: []protocol.PositionalSpec{},
					Flags: []protocol.FlagSpec{
						{Name: "dataset", Flag: "--dataset", Kind: protocol.FlagString, Order: 0},
						{Name: "question", Flag: "--question", Kind: protocol.FlagString, Order: 1},
						{Name: "mode", Flag: "--mode", Kind: protocol.FlagString, Optional: true, Default: "agent", Order: 2},
						{Name: "top_k", Flag: "--top-k", Kind: protocol.FlagNumber, Optional: true, Default: float64(5), Order: 3},
						{Name: "config", Flag: "--config", Kind: protocol.FlagString, Optional: true, Order: 4},
					},
				},
			},
		},
	}
}

// RenderArgv renders an argv vector (excluding argv[0]) from a cli_spec and
// an input object.
//
// Emission order: Always ++ Subcommand ++ Positionals ++ Flags. Flags are
// sorted by Order, tie-broken on Flag. Boolean flags emit only when enabled
// (true); negated booleans emit when false. Array flags repeat per element
// when Repeatable, else join with the Join separator. Flag defaults apply
// when the input omits the property.
func RenderArgv(spec protocol.CLISpec, input map[string]any) ([]string, error) {
	var argv []string
	argv = append(argv, spec.Always...)
	argv = append(argv, spec.Subcommand...)

	// Positionals, in spec order.
	for _, p := range spec.Positionals {
		v, ok := input[p.Name]
		if !ok || v == nil {
			if p.Required {
				return nil, fmt.Errorf("missing required positional %q", p.Name)
			}
			continue
		}
		s, err := stringValue(p.Name, v)
		if err != nil {
			return nil, err
		}
		argv = append(argv, s)
	}

	// Flags sorted by (Order, Flag).
	flags := append([]protocol.FlagSpec(nil), spec.Flags...)
	sort.SliceStable(flags, func(i, j int) bool {
		if flags[i].Order != flags[j].Order {
			return flags[i].Order < flags[j].Order
		}
		return flags[i].Flag < flags[j].Flag
	})
	for _, f := range flags {
		v, ok := input[f.Name]
		if !ok || v == nil {
			if f.Default != nil {
				v = f.Default
			} else {
				continue
			}
		}
		rendered, err := renderFlag(f, v)
		if err != nil {
			return nil, err
		}
		argv = append(argv, rendered...)
	}
	return argv, nil
}

func renderFlag(f protocol.FlagSpec, v any) ([]string, error) {
	switch f.Kind {
	case protocol.FlagBoolean:
		b, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("flag %q: expected boolean, got %v", f.Name, v)
		}
		enabled := b
		if f.Negated {
			enabled = !b
		}
		if enabled {
			return []string{f.Flag}, nil
		}
		return nil, nil
	case protocol.FlagArray:
		items, err := arrayValue(f.Name, v)
		if err != nil {
			return nil, err
		}
		if len(items) == 0 {
			return nil, nil
		}
		if f.Repeatable {
			var out []string
			for _, it := range items {
				out = append(out, f.Flag, it)
			}
			return out, nil
		}
		sep := f.Join
		if sep == "" {
			sep = ","
		}
		return []string{f.Flag, strings.Join(items, sep)}, nil
	case protocol.FlagString, protocol.FlagNumber:
		s, err := stringValue(f.Name, v)
		if err != nil {
			return nil, err
		}
		return []string{f.Flag, s}, nil
	default:
		return nil, fmt.Errorf("flag %q: unknown kind %q", f.Name, f.Kind)
	}
}

func stringValue(name string, v any) (string, error) {
	switch t := v.(type) {
	case string:
		return t, nil
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t)), nil
		}
		return fmt.Sprintf("%v", t), nil
	case int:
		return fmt.Sprintf("%d", t), nil
	case int64:
		return fmt.Sprintf("%d", t), nil
	case bool:
		return "", fmt.Errorf("property %q: boolean is not a string/number value", name)
	default:
		return "", fmt.Errorf("property %q: unsupported value type %T", name, v)
	}
}

func arrayValue(name string, v any) ([]string, error) {
	arr, ok := v.([]any)
	if !ok {
		if s, ok2 := v.([]string); ok2 {
			return s, nil
		}
		return nil, fmt.Errorf("property %q: expected array, got %T", name, v)
	}
	out := make([]string, 0, len(arr))
	for _, it := range arr {
		s, err := stringValue(name, it)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}
