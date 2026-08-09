package router

import (
	"context"
	"encoding/json"
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
					InputSchema:  json.RawMessage(`{"type":"object","properties":{"file":{"type":"string"}},"required":["file"],"additionalProperties":false}`),
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
	input, err := ParseInput(spec, []string{
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

	if _, err := ParseInput(spec, []string{"--nope"}); err == nil {
		t.Error("unknown flag must error")
	}
	if _, err := ParseInput(spec, []string{"a", "b", "c"}); err == nil {
		t.Error("extra positional must error")
	}
	if _, err := ParseInput(spec, []string{"--topk", "notnum"}); err == nil {
		t.Error("non-number for number flag must error")
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
