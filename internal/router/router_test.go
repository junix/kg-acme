package router

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"kg-acme/internal/bridge"
	"kg-acme/internal/discover"
	"kg-acme/internal/policy"
	"kg-acme/internal/protocol"
)

// fakeRunner records invocations and returns canned output.
type fakeRunner struct {
	lastName   string
	lastArgs   []string
	lastStdin  []byte
	stdout     []byte
	stderr     []byte
	err        error
}

func (f *fakeRunner) Run(_ context.Context, name string, args []string, stdin []byte) ([]byte, []byte, error) {
	f.lastName, f.lastArgs, f.lastStdin = name, args, stdin
	return f.stdout, f.stderr, f.err
}

func probedProvider(t *testing.T, cliSpec protocol.CLISpec) Provider {
	t.Helper()
	return probedProviderWithSchema(t, cliSpec,
		`{"type":"object","properties":{"file":{"type":"string"}},"required":["file"],"additionalProperties":false}`)
}

// probedProviderWithSchema builds a probed provider whose capability carries
// the given cli_spec and input_schema (round-tripped through JSON so RawMessage
// maps behave like the real decoder path).
func probedProviderWithSchema(t *testing.T, cliSpec protocol.CLISpec, inputSchema string) Provider {
	t.Helper()
	specJSON, err := json.Marshal(cliSpec)
	if err != nil {
		t.Fatal(err)
	}
	var spec protocol.CLISpec
	if err := json.Unmarshal(specJSON, &spec); err != nil {
		t.Fatal(err)
	}
	return Provider{
		Status: discover.ProviderStatus{
			ID: "kg-provider-fake", Path: "/bin/fake", Probed: true, Version: 1, Weight: 1.0,
			Manifest: &protocol.Manifest{
				Protocol:         protocol.ProviderProtocol,
				ProtocolVersions: []int{1},
				Provider:         protocol.ProviderInfo{ID: "kg-provider-fake", Version: "1.0", Description: "fake"},
				Capabilities: []protocol.Capability{{
					CapabilityID: "extract.entities_relations",
					Title:        "Extract",
					Description:  "Extracts.",
					SideEffects:  []string{"network"},
					InputSchema:  json.RawMessage(inputSchema),
					Output:       protocol.OutputSpec{Mode: "result-json", Kind: "kg-document"},
					CLISpec:      spec,
				}},
			},
		},
	}
}

func fallbackProvider() Provider {
	fb := bridge.Find("kg-extract")
	return Provider{
		Status:   discover.ProviderStatus{ID: "kg-extract", Path: "/bin/kg-extract", Weight: 1.0},
		Fallback: fb,
	}
}

func TestParseInput(t *testing.T) {
	spec := protocol.CLISpec{
		Subcommand:  []string{"ask"},
		Positionals: []protocol.PositionalSpec{{Name: "question", Required: true}},
		Flags: []protocol.FlagSpec{
			{Name: "dataset", Flag: "-d", Kind: protocol.FlagString, Order: 10},
			{Name: "mode", Flag: "--mode", Kind: protocol.FlagString, Optional: true, Order: 20},
			{Name: "verbose", Flag: "--verbose", Kind: protocol.FlagBoolean, Optional: true, Order: 30},
			{Name: "tag", Flag: "--tag", Kind: protocol.FlagArray, Repeatable: true, Optional: true, Order: 40},
			{Name: "topk", Flag: "--topk", Kind: protocol.FlagNumber, Optional: true, Order: 50},
		},
	}
	input, err := ParseInput(spec, nil, []string{
		"what is rag?", "-d", "data/demo", "--mode", "local",
		"--verbose", "--tag", "a", "--tag", "b", "--topk", "5",
	})
	if err != nil {
		t.Fatal(err)
	}
	if input["question"] != "what is rag?" {
		t.Errorf("positional: %v", input["question"])
	}
	if input["dataset"] != "data/demo" || input["mode"] != "local" {
		t.Errorf("string flags: %v", input)
	}
	if input["verbose"] != true {
		t.Errorf("boolean flag: %v", input["verbose"])
	}
	if tags, ok := input["tag"].([]any); !ok || len(tags) != 2 || tags[0] != "a" || tags[1] != "b" {
		t.Errorf("array flag: %v", input["tag"])
	}
	if input["topk"] != float64(5) {
		t.Errorf("number flag: %v (%T)", input["topk"], input["topk"])
	}

	if _, err := ParseInput(spec, nil, []string{"--nope"}); err == nil {
		t.Error("unknown flag must error")
	}
	if _, err := ParseInput(spec, nil, []string{"a", "b", "c"}); err == nil {
		t.Error("extra positional must error")
	}
	if _, err := ParseInput(spec, nil, []string{"--topk", "notnum"}); err == nil {
		t.Error("non-number for number flag must error")
	}
}

// A value-expecting flag at the very end of the args (no following token) is
// an error for every consuming kind (string / number / array). A lone "-" is
// a positional, not a flag.
func TestParseInputFlagAtEndExpectsValue(t *testing.T) {
	spec := protocol.CLISpec{
		Positionals: []protocol.PositionalSpec{{Name: "q", Required: true}},
		Flags: []protocol.FlagSpec{
			{Name: "dataset", Flag: "-d", Kind: protocol.FlagString, Order: 10},
			{Name: "topk", Flag: "--topk", Kind: protocol.FlagNumber, Order: 20},
			{Name: "tag", Flag: "--tag", Kind: protocol.FlagArray, Order: 30},
		},
	}
	for _, args := range [][]string{
		{"-d"},          // string flag, no value
		{"--topk"},      // number flag, no value
		{"--tag"},       // array flag, no value
	} {
		if _, err := ParseInput(spec, nil, args); err == nil {
			t.Errorf("args %v: expected value-expected error", args)
		} else if !strings.Contains(err.Error(), "expects a value") {
			t.Errorf("args %v: error %q should mention 'expects a value'", args, err)
		}
	}
	// A lone "-" is a positional value, not a flag token.
	input, err := ParseInput(spec, nil, []string{"-"})
	if err != nil {
		t.Fatalf("lone dash as positional: %v", err)
	}
	if input["q"] != "-" {
		t.Errorf("lone dash should fill the positional: got %v", input["q"])
	}
}

func TestParseInputSchemaDerivedFlags(t *testing.T) {
	// Graph-in shape: empty cli_spec, inputs declared only in input_schema.
	schema := json.RawMessage(`{"type":"object","properties":{
		"document": {"type": "object"},
		"document_file": {"type": "string"},
		"merge_strategy": {"type": ["string", "null"]},
		"threshold": {"type": "number"},
		"force": {"type": "boolean"},
		"mode": {}
	},"additionalProperties":false}`)
	input, err := ParseInput(protocol.CLISpec{}, schema, []string{
		"--document-file", "kg.json", "--merge-strategy", "keep-existing",
		"--threshold", "0.5", "--force", "--mode", "fast",
	})
	if err != nil {
		t.Fatal(err)
	}
	if input["document_file"] != "kg.json" || input["merge_strategy"] != "keep-existing" {
		t.Errorf("string flags: %v", input)
	}
	if input["threshold"] != float64(0.5) {
		t.Errorf("number flag: %v (%T)", input["threshold"], input["threshold"])
	}
	if input["force"] != true {
		t.Errorf("boolean flag: %v", input["force"])
	}
	if input["mode"] != "fast" {
		t.Errorf("untyped property defaults to string flag: %v", input["mode"])
	}
	if _, err := ParseInput(protocol.CLISpec{}, schema, []string{"--nope"}); err == nil {
		t.Error("flag outside cli_spec and input_schema must error")
	}
	if _, err := ParseInput(protocol.CLISpec{}, schema, []string{"--document", "{}"}); err == nil {
		t.Error("object-typed property must not gain a derived flag")
	}

	// cli_spec stays authoritative: a property it declares is not re-derived
	// as --<property>.
	spec := protocol.CLISpec{Flags: []protocol.FlagSpec{
		{Name: "document_file", Flag: "-d", Kind: protocol.FlagString, Order: 10},
	}}
	input, err = ParseInput(spec, schema, []string{"-d", "kg.json"})
	if err != nil {
		t.Fatal(err)
	}
	if input["document_file"] != "kg.json" {
		t.Errorf("cli_spec flag: %v", input)
	}
	if _, err := ParseInput(spec, schema, []string{"--document-file", "kg.json"}); err == nil {
		t.Error("covered property must not gain a derived --document-file flag")
	}
	// ...but uncovered schema properties still derive flags alongside.
	if _, err := ParseInput(spec, schema, []string{"-d", "kg.json", "--merge-strategy", "x"}); err != nil {
		t.Errorf("uncovered schema property should still parse: %v", err)
	}
}

func TestResolvePrefersProbed(t *testing.T) {
	providers := []Provider{fallbackProvider(), probedProvider(t, protocol.CLISpec{})}
	res, err := Resolve(providers, "extract.entities_relations")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Probed || res.Provider.ID() != "kg-provider-fake" {
		t.Errorf("probed provider must win: %+v", res.Provider.ID())
	}
}

func TestResolveCapabilityNotFound(t *testing.T) {
	_, err := Resolve([]Provider{fallbackProvider()}, "kg.teleport")
	if err == nil || !strings.Contains(err.Error(), protocol.ErrCapabilityNotFound) {
		t.Errorf("expected capability_not_found, got %v", err)
	}
}

func TestResolveFallbackNewIDs(t *testing.T) {
	// The fallback bridge resolves the provider-published namespace.
	res, err := Resolve([]Provider{fallbackProvider()}, "extract.entities_relations")
	if err != nil {
		t.Fatal(err)
	}
	if res.Probed {
		t.Error("unprobed kg-extract must resolve via the fallback bridge")
	}
	// The retired kg.* namespace no longer resolves anywhere.
	_, err = Resolve([]Provider{fallbackProvider()}, "kg.extract")
	if err == nil || !strings.Contains(err.Error(), protocol.ErrCapabilityNotFound) {
		t.Errorf("retired kg.extract: expected capability_not_found, got %v", err)
	}
}

func TestCLISpecOverrideDiagnostic(t *testing.T) {
	// Probed cli_spec differs from the fallback table → diagnostic.
	diffSpec := protocol.CLISpec{Subcommand: []string{"extract"}}
	p := probedProvider(t, diffSpec)
	fb := bridge.Find("kg-extract")
	p.Fallback = fb
	res, err := Resolve([]Provider{p}, "extract.entities_relations")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range res.Diagnostics {
		if d.Message == bridge.CLISpecDiffDiagnostic {
			found = true
		}
	}
	if !found {
		t.Errorf("expected cli_spec diff diagnostic, got %v", res.Diagnostics)
	}

	// Identical cli_spec → no diagnostic.
	fbSpec := fb.Capability("extract.entities_relations").CLISpec
	p2 := probedProvider(t, fbSpec)
	p2.Fallback = fb
	res2, err := Resolve([]Provider{p2}, "extract.entities_relations")
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range res2.Diagnostics {
		if d.Message == bridge.CLISpecDiffDiagnostic {
			t.Errorf("identical cli_spec must not emit diff diagnostic")
		}
	}
}

func TestExecutePolicyGate(t *testing.T) {
	res, err := Resolve([]Provider{fallbackProvider()}, "extract.entities_relations")
	if err != nil {
		t.Fatal(err)
	}
	input := map[string]any{"file": "doc.md"}

	// Default: all gates closed → policy_denied, provider never runs.
	runner := &fakeRunner{}
	env, err := Execute(context.Background(), res, input, policy.Gates{}, false, runner)
	if err != nil {
		t.Fatal(err)
	}
	if env.Status != "error" || env.Error.Code != protocol.ErrPolicyDenied {
		t.Errorf("expected policy_denied envelope, got %+v", env.Error)
	}
	if runner.lastName != "" {
		t.Error("provider must not run when policy denies")
	}

	// Explicit allow → runs (fallback kg-extract declares network+data_egress).
	runner = &fakeRunner{stdout: []byte(`{"entities":[]}`)}
	gates := policy.Gates{AllowNetwork: true, AllowDataEgress: true}
	env, err = Execute(context.Background(), res, input, gates, false, runner)
	if err != nil {
		t.Fatal(err)
	}
	if env.Status != "ok" {
		t.Fatalf("expected ok, got %+v", env.Error)
	}
	if runner.lastName != "/bin/kg-extract" {
		t.Errorf("wrong binary: %q", runner.lastName)
	}
	joined := strings.Join(runner.lastArgs, " ")
	if !strings.Contains(joined, "--file doc.md") {
		t.Errorf("wrong argv: %v", runner.lastArgs)
	}
	if m, ok := env.Result.(map[string]any); !ok || m["entities"] == nil {
		t.Errorf("result-json output should be parsed into result, got %#v", env.Result)
	}
}

func TestExecuteDryRunZeroSideEffects(t *testing.T) {
	res, err := Resolve([]Provider{fallbackProvider()}, "extract.entities_relations")
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	env, err := Execute(context.Background(), res, map[string]any{"file": "doc.md"}, policy.Gates{}, true, runner)
	if err != nil {
		t.Fatal(err)
	}
	if env.Status != "ok" {
		t.Fatalf("dry-run should be ok, got %+v", env.Error)
	}
	if runner.lastName != "" {
		t.Error("dry-run must never execute the provider")
	}
	plan, ok := env.Result.(map[string]any)
	if !ok {
		t.Fatalf("dry-run result should be a plan, got %#v", env.Result)
	}
	if plan["would_execute"] != false {
		t.Errorf("gates closed → would_execute=false, got %v", plan["would_execute"])
	}
	argv, ok := plan["argv"].([]string)
	if !ok || len(argv) < 2 {
		t.Fatalf("plan should carry argv, got %#v", plan["argv"])
	}
}

func TestExecuteInvalidInput(t *testing.T) {
	res, err := Resolve([]Provider{fallbackProvider()}, "extract.entities_relations")
	if err != nil {
		t.Fatal(err)
	}
	// additionalProperties: false in the fallback schema.
	env, err := Execute(context.Background(), res, map[string]any{"file": "a", "bogus": 1},
		policy.Gates{AllowNetwork: true, AllowModelDownload: true}, false, &fakeRunner{})
	if err != nil {
		t.Fatal(err)
	}
	if env.Status != "error" || env.Error.Code != protocol.ErrInvalidInput {
		t.Errorf("expected invalid_input, got %+v", env.Error)
	}
}

func TestExecuteInvokeProtocol(t *testing.T) {
	res, err := Resolve([]Provider{probedProvider(t, protocol.CLISpec{})}, "extract.entities_relations")
	if err != nil {
		t.Fatal(err)
	}
	envelope := protocol.Envelope{
		Protocol: protocol.ExecutionProtocol, CapabilityID: "extract.entities_relations",
		Provider: "kg-provider-fake", Status: "ok",
		Result: map[string]any{"entities": []any{}},
	}
	raw, _ := json.Marshal(envelope)
	runner := &fakeRunner{stdout: raw}

	env, err := Execute(context.Background(), res, map[string]any{"file": "doc.md"},
		policy.Gates{AllowNetwork: true}, false, runner)
	if err != nil {
		t.Fatal(err)
	}
	if env.Status != "ok" || env.Provider != "kg-provider-fake" {
		t.Errorf("envelope passthrough failed: %+v", env)
	}
	// The protocol path must use invoke with the request on stdin.
	wantArgs := []string{"invoke", "extract.entities_relations", "--request", "-"}
	if strings.Join(runner.lastArgs, " ") != strings.Join(wantArgs, " ") {
		t.Errorf("invoke argv: got %v want %v", runner.lastArgs, wantArgs)
	}
	var req map[string]any
	if err := json.Unmarshal(runner.lastStdin, &req); err != nil {
		t.Fatalf("stdin must be a JSON request: %v", err)
	}
	if req["capability_id"] != "extract.entities_relations" {
		t.Errorf("request capability_id: %v", req["capability_id"])
	}
	input, ok := req["input"].(map[string]any)
	if !ok || input["file"] != "doc.md" {
		t.Errorf("request input: %#v", req["input"])
	}
}

func TestExecuteInvokeInvalidEnvelope(t *testing.T) {
	res, err := Resolve([]Provider{probedProvider(t, protocol.CLISpec{})}, "extract.entities_relations")
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{stdout: []byte("not json at all")}
	env, err := Execute(context.Background(), res, map[string]any{"file": "doc.md"},
		policy.Gates{AllowNetwork: true}, false, runner)
	if err != nil {
		t.Fatal(err)
	}
	if env.Status != "error" || env.Error.Code != protocol.ErrInvocationFailed {
		t.Errorf("expected invocation_failed, got %+v", env.Error)
	}
}

// A RenderArgv failure (e.g. a required positional missing from the input)
// surfaces as an invalid_input envelope, and the provider never starts. The
// input_schema is left open so schema validation passes and the failure is
// isolated to argv rendering.
func TestExecuteRenderArgvFailureInvalidInput(t *testing.T) {
	spec := protocol.CLISpec{Positionals: []protocol.PositionalSpec{{Name: "file", Required: true}}}
	res, err := Resolve([]Provider{probedProviderWithSchema(t, spec,
		`{"type":"object","additionalProperties":true}`)}, "extract.entities_relations")
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	env, err := Execute(context.Background(), res, map[string]any{},
		policy.Gates{AllowNetwork: true}, false, runner)
	if err != nil {
		t.Fatal(err)
	}
	if env.Status != "error" || env.Error.Code != protocol.ErrInvalidInput {
		t.Fatalf("expected invalid_input, got %+v", env.Error)
	}
	if !strings.Contains(env.Error.Message, "missing required positional") {
		t.Errorf("error should mention the missing positional: %q", env.Error.Message)
	}
	if runner.lastName != "" {
		t.Error("provider must not start when argv rendering fails")
	}
}

// A fallback provider that exits non-zero yields an invocation_failed
// envelope carrying the provider's stderr, and the run error is wrapped
// (never a hard Go error from Execute).
func TestExecuteFallbackProviderError(t *testing.T) {
	res, err := Resolve([]Provider{fallbackProvider()}, "extract.entities_relations")
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{
		stderr: []byte("model not found"),
		err:    fmt.Errorf("exit status 2"),
	}
	env, err := Execute(context.Background(), res, map[string]any{"file": "doc.md"},
		policy.Gates{AllowNetwork: true, AllowDataEgress: true}, false, runner)
	if err != nil {
		t.Fatalf("Execute must not return a hard error: %v", err)
	}
	if env.Status != "error" || env.Error.Code != protocol.ErrInvocationFailed {
		t.Fatalf("expected invocation_failed, got %+v", env.Error)
	}
	for _, want := range []string{"provider exited with error", "exit status 2", "model not found"} {
		if !strings.Contains(env.Error.Message, want) {
			t.Errorf("error %q should contain %q", env.Error.Message, want)
		}
	}
}

// When a probed provider's invoke subprocess fails AND prints nothing on
// stdout, the hub reports invocation_failed with the underlying run error
// (rather than a JSON-parse error on empty output).
func TestExecuteInvokeRunErrorNoOutput(t *testing.T) {
	res, err := Resolve([]Provider{probedProvider(t, protocol.CLISpec{})}, "extract.entities_relations")
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{err: fmt.Errorf("signal: killed")}
	env, err := Execute(context.Background(), res, map[string]any{"file": "doc.md"},
		policy.Gates{AllowNetwork: true}, false, runner)
	if err != nil {
		t.Fatal(err)
	}
	if env.Status != "error" || env.Error.Code != protocol.ErrInvocationFailed {
		t.Fatalf("expected invocation_failed, got %+v", env.Error)
	}
	if !strings.Contains(env.Error.Message, "invoke failed") ||
		!strings.Contains(env.Error.Message, "signal: killed") {
		t.Errorf("error should wrap the run error: %q", env.Error.Message)
	}
}

// Dry-run with all gates open reports would_execute=true and lists the
// side effects that would be denied (none).
func TestExecuteDryRunWouldExecute(t *testing.T) {
	res, err := Resolve([]Provider{fallbackProvider()}, "extract.entities_relations")
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	env, err := Execute(context.Background(), res, map[string]any{"file": "doc.md"},
		policy.Gates{AllowNetwork: true, AllowDataEgress: true}, true, runner)
	if err != nil {
		t.Fatal(err)
	}
	plan := env.Result.(map[string]any)
	if plan["would_execute"] != true {
		t.Errorf("gates open → would_execute=true, got %v", plan["would_execute"])
	}
	if denied, ok := plan["denied"].([]string); !ok || len(denied) != 0 {
		t.Errorf("no effects should be denied, got %v", plan["denied"])
	}
	if runner.lastName != "" {
		t.Error("dry-run must not execute the provider even with gates open")
	}
}
