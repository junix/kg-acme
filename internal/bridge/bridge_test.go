package bridge

import (
	"reflect"
	"strings"
	"testing"

	"kg-acme/internal/catalog"
	"kg-acme/internal/protocol"
)

func TestRenderArgvEmissionOrder(t *testing.T) {
	spec := protocol.CLISpec{
		Always:     []string{"--global"},
		Subcommand: []string{"run", "fast"},
		Positionals: []protocol.PositionalSpec{
			{Name: "file", Required: true},
			{Name: "extra"},
		},
		Flags: []protocol.FlagSpec{
			{Name: "zz", Flag: "--zz", Kind: protocol.FlagString, Order: 30},
			{Name: "aa", Flag: "--aa", Kind: protocol.FlagString, Order: 10},
			{Name: "bb", Flag: "--bb", Kind: protocol.FlagString, Order: 10}, // tie with aa → flag name decides
		},
	}
	input := map[string]any{
		"file": "doc.md", "extra": "tail",
		"zz": "1", "aa": "2", "bb": "3",
	}
	got, err := RenderArgv(spec, input)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--global", "run", "fast", "doc.md", "tail", "--aa", "2", "--bb", "3", "--zz", "1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("emission order wrong:\n got %v\nwant %v", got, want)
	}
}

func TestRenderArgvBoolean(t *testing.T) {
	spec := protocol.CLISpec{Flags: []protocol.FlagSpec{
		{Name: "coref", Flag: "--coref", Kind: protocol.FlagBoolean, Order: 1},
		{Name: "no_color", Flag: "--no-color", Kind: protocol.FlagBoolean, Negated: true, Order: 2},
	}}

	// Only enabled booleans emit.
	got, err := RenderArgv(spec, map[string]any{"coref": true, "no_color": true})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"--coref"}; !reflect.DeepEqual(got, want) {
		t.Errorf("enabled boolean: got %v want %v", got, want)
	}

	// Negated emits when false.
	got, err = RenderArgv(spec, map[string]any{"coref": false, "no_color": false})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"--no-color"}; !reflect.DeepEqual(got, want) {
		t.Errorf("negated boolean: got %v want %v", got, want)
	}

	// Absent booleans emit nothing.
	got, err = RenderArgv(spec, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("absent booleans should emit nothing, got %v", got)
	}
}

func TestRenderArgvArray(t *testing.T) {
	spec := protocol.CLISpec{Flags: []protocol.FlagSpec{
		{Name: "include", Flag: "-I", Kind: protocol.FlagArray, Repeatable: true, Order: 1},
		{Name: "tags", Flag: "--tags", Kind: protocol.FlagArray, Join: ":", Order: 2},
	}}
	got, err := RenderArgv(spec, map[string]any{
		"include": []any{"a", "b"},
		"tags":    []any{"x", "y", "z"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-I", "a", "-I", "b", "--tags", "x:y:z"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("array flags: got %v want %v", got, want)
	}
}

func TestRenderArgvDefaultsAndNumbers(t *testing.T) {
	spec := protocol.CLISpec{Flags: []protocol.FlagSpec{
		{Name: "output", Flag: "-o", Kind: protocol.FlagString, Optional: true, Default: "json", Order: 1},
		{Name: "topk", Flag: "--topk", Kind: protocol.FlagNumber, Order: 2},
		{Name: "ratio", Flag: "--ratio", Kind: protocol.FlagNumber, Order: 3},
	}}
	got, err := RenderArgv(spec, map[string]any{"topk": float64(10), "ratio": float64(0.5)})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-o", "json", "--topk", "10", "--ratio", "0.5"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("defaults/numbers: got %v want %v", got, want)
	}
}

func TestRenderArgvRequiredPositional(t *testing.T) {
	spec := protocol.CLISpec{Positionals: []protocol.PositionalSpec{{Name: "file", Required: true}}}
	if _, err := RenderArgv(spec, map[string]any{}); err == nil {
		t.Error("missing required positional should error")
	}
}

func TestFallbackTableCoversKnownCLIs(t *testing.T) {
	table := Table()
	byID := map[string]FallbackProvider{}
	for _, p := range table {
		byID[p.ID] = p
	}
	if _, ok := byID["kg-extract"]; !ok {
		t.Error("missing kg-extract bridge")
	}
	if _, ok := byID["kg-mm"]; !ok {
		t.Error("missing kg-mm bridge")
	}
	if _, ok := byID["ygr"]; !ok {
		t.Error("missing ygr bridge")
	}
	if Find("kg-extract").Capability("extract.entities_relations") == nil {
		t.Error("kg-extract should offer extract.entities_relations")
	}
	if Find("kg-mm").Capability("parse.multimodal") == nil {
		t.Error("kg-mm should offer parse.multimodal")
	}
	if Find("ygr").Capability("retrieve.ask") == nil {
		t.Error("ygr should offer retrieve.ask")
	}

	// The retired kg.* namespace must be gone entirely (no published users).
	for _, p := range table {
		for _, c := range p.Capabilities {
			if strings.HasPrefix(c.CapabilityID, "kg.") {
				t.Errorf("%s still bridges retired capability %q", p.ID, c.CapabilityID)
			}
		}
	}

	// Graph-in capabilities are protocol-only: no fallback argv can drive
	// them (their input document travels via the invoke request on stdin).
	if Find("kg-extract").Capability("detect.communities") != nil {
		t.Error("detect.communities must not have a fallback bridge")
	}
	if Find("kg-extract").Capability("resolve.coref") != nil {
		t.Error("resolve.coref must not have a fallback bridge")
	}
}

// Every bridged capability id is one a stable catalog command routes to.
func TestFallbackTableMatchesCatalogCommands(t *testing.T) {
	cat, err := catalog.Load()
	if err != nil {
		t.Fatal(err)
	}
	routed := map[string]bool{}
	for _, cmd := range cat.CapabilityCommands() {
		routed[cmd.CapabilityID] = true
	}
	for _, p := range Table() {
		for _, c := range p.Capabilities {
			if !routed[c.CapabilityID] {
				t.Errorf("%s bridges %q, which no catalog command routes to", p.ID, c.CapabilityID)
			}
		}
	}
}

// The fallback bridges must render the documented legacy invocations.
func TestFallbackBridgeShapes(t *testing.T) {
	// kg-extract -o kg-protocol --file doc.md [--backend mock ...]
	ext := Find("kg-extract").Capability("extract.entities_relations")
	got, err := RenderArgv(ext.CLISpec, map[string]any{
		"file": "doc.md", "backend": "mock", "mock_response": "canned", "coref": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"-o", "kg-protocol",
		"--file", "doc.md", "--input-format", "text", "--engine", "simple",
		"--backend", "mock", "--agent", "minimaxcc", "--chunker", "recursive",
		"--schema-mode", "open", "--max-rounds", "1", "--merge-strategy", "keep-existing",
		"--coref", "--max-concurrency", "8", "--relation-gleaning", "0",
		"--mock-response", "canned",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("kg-extract argv:\n got %v\nwant %v", got, want)
	}

	// kg-mm analyze <sidecar> [defaults]
	mm := Find("kg-mm").Capability("parse.multimodal")
	got, err = RenderArgv(mm.CLISpec, map[string]any{"sidecar": "doc.json"})
	if err != nil {
		t.Fatal(err)
	}
	want = []string{
		"analyze", "doc.json",
		"--backend", "cli", "--llm-bin", "llm", "--ocr-bin", "ocr",
		"--language", "English", "--mock-ocr", "",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("kg-mm argv:\n got %v\nwant %v", got, want)
	}

	// ygr ask --dataset d --question q [defaults]
	ask := Find("ygr").Capability("retrieve.ask")
	got, err = RenderArgv(ask.CLISpec, map[string]any{
		"question": "what is RAG?", "dataset": "data/demo",
	})
	if err != nil {
		t.Fatal(err)
	}
	want = []string{"ask", "--dataset", "data/demo", "--question", "what is RAG?", "--mode", "agent", "--top-k", "5"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ygr argv:\n got %v\nwant %v", got, want)
	}
}
