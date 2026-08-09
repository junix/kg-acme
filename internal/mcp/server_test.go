// Frame-level tests for the stdio MCP server: initialize handshake,
// tools/list same-source shape, tools/call end to end through a fake
// runner, policy_denied as structured content, and JSON-RPC fault
// tolerance — all over in-memory pipes.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"kg-acme/internal/bridge"
	"kg-acme/internal/catalog"
	"kg-acme/internal/discover"
	"kg-acme/internal/pipeline"
	"kg-acme/internal/policy"
	"kg-acme/internal/protocol"
	"kg-acme/internal/router"
)

// fakeManifest builds a probed provider offering extract.entities_relations
// with a distinctive input_schema property (fake_field) so tests can assert
// the tool schema is the provider's, verbatim.
func fakeProvider() router.Provider {
	return router.Provider{
		Status: discover.ProviderStatus{
			ID:     "fake",
			Path:   "/fake/fake",
			Weight: 1.0,
			Probed: true,
			Manifest: &protocol.Manifest{
				Protocol:         protocol.ProviderProtocol,
				ProtocolVersions: []int{1},
				Provider:         protocol.ProviderInfo{ID: "fake", Version: "0.1"},
				Capabilities: []protocol.Capability{{
					CapabilityID: "extract.entities_relations",
					Title:        "Fake extract",
					SideEffects:  []string{"network"},
					InputSchema: json.RawMessage(`{
					  "type": "object",
					  "properties": {"fake_field": {"type": "string"}},
					  "required": ["fake_field"],
					  "additionalProperties": false
					}`),
					Output:  protocol.OutputSpec{Mode: "result-json", Kind: "kg-document"},
					CLISpec: protocol.CLISpec{},
				}},
			},
		},
	}
}

// recordingRunner captures invocations and replies with a canned envelope.
type recordingRunner struct {
	calls   [][]string
	stdin   [][]byte
	replies []*protocol.Envelope
}

func (r *recordingRunner) Run(_ context.Context, name string, args []string, stdin []byte) ([]byte, []byte, error) {
	r.calls = append(r.calls, append([]string{name}, args...))
	r.stdin = append(r.stdin, stdin)
	env := protocol.NewEnvelope("extract.entities_relations", "fake")
	env.Status = "ok"
	var req any
	_ = json.Unmarshal(stdin, &req)
	env.Result = map[string]any{"echo": req}
	r.replies = append(r.replies, env)
	data, _ := json.Marshal(env)
	return data, nil, nil
}

func newTestServer(gates policy.Gates, providers []router.Provider, run router.Runner) *Server {
	return New(Config{
		Version: "test",
		Gates:   gates,
		Providers: func(context.Context) []router.Provider {
			return providers
		},
		Runner: run,
	})
}

// exchange feeds lines to a server and returns the response frames.
func exchange(t *testing.T, srv *Server, lines ...string) []map[string]any {
	t.Helper()
	var in, out strings.Builder
	for _, l := range lines {
		in.WriteString(l + "\n")
	}
	if err := srv.Serve(context.Background(), strings.NewReader(in.String()), &out); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	var frames []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if l == "" {
			continue
		}
		var f map[string]any
		if err := json.Unmarshal([]byte(l), &f); err != nil {
			t.Fatalf("response frame is not JSON: %q: %v", l, err)
		}
		frames = append(frames, f)
	}
	return frames
}

func initializeLine(id int, protocolVersion string) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"initialize","params":{"protocolVersion":%q,"capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`, id, protocolVersion)
}

func TestInitializeHandshake(t *testing.T) {
	srv := newTestServer(policy.Gates{}, nil, nil)
	frames := exchange(t, srv, initializeLine(1, "2025-06-18"))
	if len(frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(frames))
	}
	f := frames[0]
	if f["jsonrpc"] != "2.0" || f["id"].(float64) != 1 {
		t.Errorf("frame envelope: %v", f)
	}
	result := f["result"].(map[string]any)
	if result["protocolVersion"] != "2025-06-18" {
		t.Errorf("protocolVersion should echo the client's: %v", result)
	}
	caps := result["capabilities"].(map[string]any)
	if _, ok := caps["tools"]; !ok {
		t.Errorf("capabilities.tools missing: %v", caps)
	}
	info := result["serverInfo"].(map[string]any)
	if info["name"] != "kg-mcp" || info["version"] != "test" {
		t.Errorf("serverInfo: %v", info)
	}
}

func TestInitializeUnsupportedVersionGetsLatest(t *testing.T) {
	srv := newTestServer(policy.Gates{}, nil, nil)
	frames := exchange(t, srv, initializeLine(1, "1999-01-01"))
	result := frames[0]["result"].(map[string]any)
	if result["protocolVersion"] != ProtocolVersion {
		t.Errorf("unknown client version must get our latest %q, got %v", ProtocolVersion, result["protocolVersion"])
	}
}

func TestNotificationsTolerated(t *testing.T) {
	srv := newTestServer(policy.Gates{}, nil, nil)
	frames := exchange(t, srv,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":1,"reason":"x"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"ping"}`,
	)
	if len(frames) != 1 {
		t.Fatalf("notifications must not produce frames; got %d", len(frames))
	}
	if frames[0]["id"].(float64) != 2 {
		t.Errorf("only the ping may be answered: %v", frames)
	}
}

func TestInvalidJSONSurvives(t *testing.T) {
	srv := newTestServer(policy.Gates{}, nil, nil)
	frames := exchange(t, srv,
		`this is not json`,
		`{"jsonrpc":"2.0","id":7,"method":"ping"}`,
	)
	if len(frames) != 2 {
		t.Fatalf("expected parse-error + ping frames, got %d", len(frames))
	}
	errObj := frames[0]["error"].(map[string]any)
	if errObj["code"].(float64) != -32700 {
		t.Errorf("expected -32700 parse error, got %v", frames[0])
	}
	if string(mustJSON(t, frames[0]["id"])) != "null" {
		t.Errorf("parse error id must be null: %v", frames[0]["id"])
	}
	if frames[1]["id"].(float64) != 7 {
		t.Errorf("server must keep serving after a parse error: %v", frames)
	}
}

func TestUnknownMethod(t *testing.T) {
	srv := newTestServer(policy.Gates{}, nil, nil)
	frames := exchange(t, srv, `{"jsonrpc":"2.0","id":3,"method":"resources/list"}`)
	errObj := frames[0]["error"].(map[string]any)
	if errObj["code"].(float64) != -32601 {
		t.Errorf("expected -32601, got %v", frames[0])
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func toolsList(t *testing.T, srv *Server) map[string]any {
	t.Helper()
	frames := exchange(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	if len(frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(frames))
	}
	result, ok := frames[0]["result"].(map[string]any)
	if !ok {
		t.Fatalf("tools/list result: %v", frames[0])
	}
	return result
}

func toolsByName(result map[string]any) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, raw := range result["tools"].([]any) {
		tool := raw.(map[string]any)
		out[tool["name"].(string)] = tool
	}
	return out
}

// The tool surface must mirror the catalog one-to-one — same commands,
// stable kg_<semantic_id> names.
func TestToolsListMirrorsCatalog(t *testing.T) {
	srv := newTestServer(policy.Gates{}, []router.Provider{fakeProvider()}, nil)
	byName := toolsByName(toolsList(t, srv))

	cat, err := catalog.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(byName) != len(cat.Commands) {
		t.Errorf("tool count %d != catalog command count %d", len(byName), len(cat.Commands))
	}
	for _, cmd := range cat.Commands {
		name := ToolName(cmd.SemanticID)
		tool, ok := byName[name]
		if !ok {
			t.Errorf("catalog command %q has no tool %q", cmd.SemanticID, name)
			continue
		}
		if tool["description"] != cmd.Description {
			t.Errorf("tool %q description must come from the catalog: %v", name, tool["description"])
		}
		if _, ok := tool["inputSchema"].(map[string]any); !ok {
			t.Errorf("tool %q missing inputSchema", name)
		}
	}
	for _, want := range []string{
		"kg_extract", "kg_dedup", "kg_communities", "kg_communities_hierarchy",
		"kg_communities_summaries", "kg_store", "kg_ask", "kg_parse",
		"kg_provider", "kg_pipeline_run", "kg_pipeline_validate",
	} {
		if _, ok := byName[want]; !ok {
			t.Errorf("expected tool %q", want)
		}
	}
}

// Same source (铁律 2): a capability tool's inputSchema is the probed
// provider's input_schema verbatim, plus hub extras — never re-authored.
func TestToolsListCapabilitySchemaFromProvider(t *testing.T) {
	srv := newTestServer(policy.Gates{}, []router.Provider{fakeProvider()}, nil)
	byName := toolsByName(toolsList(t, srv))

	schema := byName["kg_extract"]["inputSchema"].(map[string]any)
	props := schema["properties"].(map[string]any)
	if _, ok := props["fake_field"]; !ok {
		t.Errorf("provider-published property fake_field must appear verbatim: %v", props)
	}
	required := schema["required"].([]any)
	if len(required) != 1 || required[0] != "fake_field" {
		t.Errorf("provider required list must survive: %v", schema["required"])
	}
	// Hub extras (CLI hub flags in MCP form).
	for _, extra := range []string{"dry_run", "provider"} {
		if _, ok := props[extra]; !ok {
			t.Errorf("hub extra %q missing from kg_extract schema", extra)
		}
	}
}

// With no probed provider the fallback bridge table supplies the schema —
// the same data the CLI falls back to.
func TestToolsListCapabilitySchemaFromFallback(t *testing.T) {
	fb := bridge.Find("kg-extract")
	if fb == nil {
		t.Fatal("kg-extract fallback bridge missing")
	}
	providers := []router.Provider{{
		Status:   discover.ProviderStatus{ID: "kg-extract", Path: "/fake/kg-extract", Weight: 1.0},
		Fallback: fb,
	}}
	srv := newTestServer(policy.Gates{}, providers, nil)
	byName := toolsByName(toolsList(t, srv))

	props := byName["kg_extract"]["inputSchema"].(map[string]any)["properties"].(map[string]any)
	if _, ok := props["mock_response"]; !ok {
		t.Errorf("fallback-table property mock_response must appear: %v", props)
	}
}

func TestToolsCallEndToEnd(t *testing.T) {
	run := &recordingRunner{}
	srv := newTestServer(policy.Gates{AllowNetwork: true}, []router.Provider{fakeProvider()}, run)
	call := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"kg_extract","arguments":{"fake_field":"doc.md","dry_run":false,"provider":"fake"}}}`
	frames := exchange(t, srv, call)
	if len(frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(frames))
	}
	result := frames[0]["result"].(map[string]any)
	if result["isError"] != false {
		t.Fatalf("call should succeed: %v", result)
	}
	sc := result["structuredContent"].(map[string]any)
	if sc["protocol"] != protocol.ExecutionProtocol || sc["status"] != "ok" {
		t.Errorf("structured content must be the kg.execution/v1 envelope: %v", sc)
	}
	// The provider received the input minus hub extras.
	if len(run.stdin) != 1 {
		t.Fatalf("provider not invoked: %v", run.calls)
	}
	var req map[string]any
	if err := json.Unmarshal(run.stdin[0], &req); err != nil {
		t.Fatal(err)
	}
	input := req["input"].(map[string]any)
	if input["fake_field"] != "doc.md" {
		t.Errorf("provider input: %v", input)
	}
	if _, leaked := input["dry_run"]; leaked {
		t.Errorf("hub extra dry_run must be stripped before validation: %v", input)
	}
	if _, leaked := input["provider"]; leaked {
		t.Errorf("hub extra provider must be stripped before validation: %v", input)
	}
	// The text content mirrors the structured content for plain clients.
	content := result["content"].([]any)
	if len(content) != 1 || content[0].(map[string]any)["type"] != "text" {
		t.Errorf("content: %v", content)
	}
}

func TestToolsCallPolicyDeniedStructured(t *testing.T) {
	run := &recordingRunner{}
	srv := newTestServer(policy.Gates{}, []router.Provider{fakeProvider()}, run) // all gates closed
	call := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"kg_extract","arguments":{"fake_field":"doc.md"}}}`
	frames := exchange(t, srv, call)
	result := frames[0]["result"].(map[string]any)
	if result["isError"] != true {
		t.Fatalf("policy denial must map to isError:true: %v", result)
	}
	sc := result["structuredContent"].(map[string]any)
	if sc["status"] != "error" {
		t.Fatalf("envelope status: %v", sc)
	}
	errInfo := sc["error"].(map[string]any)
	if errInfo["code"] != protocol.ErrPolicyDenied {
		t.Errorf("expected policy_denied, got %v", errInfo)
	}
	if len(run.calls) != 0 {
		t.Errorf("provider must never start under policy denial: %v", run.calls)
	}
}

func TestToolsCallUnknownTool(t *testing.T) {
	srv := newTestServer(policy.Gates{}, []router.Provider{fakeProvider()}, nil)
	frames := exchange(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"nope","arguments":{}}}`)
	errObj := frames[0]["error"].(map[string]any)
	if errObj["code"].(float64) != -32602 {
		t.Errorf("expected -32602, got %v", frames[0])
	}
}

func TestToolsCallCapabilityNotFound(t *testing.T) {
	srv := newTestServer(policy.Gates{}, []router.Provider{fakeProvider()}, nil)
	call := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"kg_dedup","arguments":{"document_file":"kg.json"}}}`
	frames := exchange(t, srv, call)
	result := frames[0]["result"].(map[string]any)
	sc := result["structuredContent"].(map[string]any)
	if result["isError"] != true || sc["error"].(map[string]any)["code"] != protocol.ErrCapabilityNotFound {
		t.Errorf("expected structured capability_not_found, got %v", sc)
	}
	if sc["capability_id"] != "resolve.coref" {
		t.Errorf("kg_dedup must map to resolve.coref: %v", sc)
	}
}

func TestPipelineValidateTool(t *testing.T) {
	// The fake provider only offers extract; a one-stage pipeline validates.
	run := &recordingRunner{}
	srv := newTestServer(policy.Gates{AllowNetwork: true}, []router.Provider{fakeProvider()}, run)
	call := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"kg_pipeline_validate","arguments":{"definition":{"pipeline":"kg.pipeline/v1","name":"one","stages":[{"id":"extract","capability":"extract.entities_relations","input":{"fake_field":"doc.md"}}]}}}}`
	frames := exchange(t, srv, call)
	result := frames[0]["result"].(map[string]any)
	sc := result["structuredContent"].(map[string]any)
	if sc["protocol"] != pipelineProtocol() || sc["status"] != "ok" || sc["dry_run"] != true {
		t.Errorf("validate must render the plan envelope: %v", sc)
	}
	if len(run.calls) != 0 {
		t.Errorf("validate must not execute: %v", run.calls)
	}
}

func TestPipelineRunToolGatePrecheck(t *testing.T) {
	run := &recordingRunner{}
	srv := newTestServer(policy.Gates{}, []router.Provider{fakeProvider()}, run) // gates closed
	call := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"kg_pipeline_run","arguments":{"definition":{"pipeline":"kg.pipeline/v1","name":"one","stages":[{"id":"extract","capability":"extract.entities_relations","input":{"fake_field":"doc.md"}}]},"work_dir":"/tmp/kg-mcp-test-work"}}}`
	frames := exchange(t, srv, call)
	result := frames[0]["result"].(map[string]any)
	sc := result["structuredContent"].(map[string]any)
	if result["isError"] != true || sc["error"].(map[string]any)["code"] != protocol.ErrPolicyDenied {
		t.Errorf("expected pipeline policy_denied, got %v", sc)
	}
	if len(run.calls) != 0 {
		t.Errorf("pipeline must fail fast before any provider starts: %v", run.calls)
	}
}

func pipelineProtocol() string {
	env := pipeline.ErrorEnvelope("", "", "")
	return env.Protocol
}

// A request frame carrying an id but no method is an invalid request (-32600),
// distinct from method-not-found (-32601).
func TestEmptyMethodInvalidRequest(t *testing.T) {
	srv := newTestServer(policy.Gates{}, nil, nil)
	frames := exchange(t, srv, `{"jsonrpc":"2.0","id":5}`)
	if len(frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(frames))
	}
	errObj := frames[0]["error"].(map[string]any)
	if errObj["code"].(float64) != -32600 {
		t.Errorf("expected -32600 invalid request, got %v", frames[0])
	}
	if frames[0]["id"].(float64) != 5 {
		t.Errorf("error must echo the request id: %v", frames[0]["id"])
	}
}

func TestToolsCallParamValidation(t *testing.T) {
	srv := newTestServer(policy.Gates{}, []router.Provider{fakeProvider()}, nil)
	cases := []struct {
		name string
		call string
	}{
		// params without a name.
		{"missing name", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"arguments":{}}}`},
		// params with an empty name.
		{"empty name", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"","arguments":{}}}`},
		// arguments is not a JSON object.
		{"arguments not object", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"kg_extract","arguments":[1,2]}}`},
		// arguments is a JSON string rather than an object.
		{"arguments string", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"kg_extract","arguments":"nope"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			frames := exchange(t, srv, tc.call)
			if len(frames) != 1 {
				t.Fatalf("expected 1 frame, got %d", len(frames))
			}
			errObj, ok := frames[0]["error"].(map[string]any)
			if !ok {
				t.Fatalf("expected error frame, got %v", frames[0])
			}
			if errObj["code"].(float64) != -32602 {
				t.Errorf("expected -32602 invalid params, got %v", errObj)
			}
		})
	}
}

// Hub-owned extras (dry_run, provider) are type-checked before routing: a
// wrong-typed value is invalid params, not a provider failure.
func TestPopHubArgsTypeChecks(t *testing.T) {
	srv := newTestServer(policy.Gates{AllowNetwork: true}, []router.Provider{fakeProvider()}, nil)
	cases := []struct {
		name string
		call string
		want string
	}{
		{
			"dry_run not boolean",
			`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"kg_extract","arguments":{"fake_field":"d.md","dry_run":"yes"}}}`,
			"dry_run must be a boolean",
		},
		{
			"provider not string",
			`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"kg_extract","arguments":{"fake_field":"d.md","provider":5}}}`,
			"provider must be a string",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			frames := exchange(t, srv, tc.call)
			errObj := frames[0]["error"].(map[string]any)
			if errObj["code"].(float64) != -32602 {
				t.Errorf("expected -32602, got %v", errObj)
			}
			if !strings.Contains(errObj["message"].(string), tc.want) {
				t.Errorf("message %q should contain %q", errObj["message"], tc.want)
			}
		})
	}
}

// The kg_provider escape hatch validates its required fields before routing.
func TestCallProviderToolValidation(t *testing.T) {
	srv := newTestServer(policy.Gates{AllowNetwork: true}, []router.Provider{fakeProvider()}, nil)
	cases := []struct {
		name string
		args string
	}{
		{"missing provider_id", `"capability_id":"extract.entities_relations","request":{}`},
		{"missing capability_id", `"provider_id":"fake","request":{}`},
		{"missing request", `"provider_id":"fake","capability_id":"extract.entities_relations"`},
		{"request not object", `"provider_id":"fake","capability_id":"extract.entities_relations","request":"x"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			call := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"kg_provider","arguments":{%s}}}`, tc.args)
			frames := exchange(t, srv, call)
			errObj := frames[0]["error"].(map[string]any)
			if errObj["code"].(float64) != -32602 {
				t.Errorf("expected -32602 invalid params, got %v", errObj)
			}
		})
	}
}

// An unknown provider filter degrades to a provider_not_found envelope (not a
// JSON-RPC error): the hub stays callable, the failure is structured content.
func TestFilterProvidersNotFoundStructured(t *testing.T) {
	srv := newTestServer(policy.Gates{AllowNetwork: true}, []router.Provider{fakeProvider()}, nil)
	call := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"kg_extract","arguments":{"fake_field":"d.md","provider":"ghost"}}}`
	frames := exchange(t, srv, call)
	result := frames[0]["result"].(map[string]any)
	if result["isError"] != true {
		t.Fatalf("expected isError:true for unknown provider: %v", result)
	}
	sc := result["structuredContent"].(map[string]any)
	if sc["status"] != "error" {
		t.Fatalf("envelope status: %v", sc)
	}
	errInfo := sc["error"].(map[string]any)
	if errInfo["code"] != protocol.ErrProviderNotFound {
		t.Errorf("expected provider_not_found, got %v", errInfo)
	}
	if sc["provider"] != "ghost" {
		t.Errorf("envelope should carry the unknown provider id: %v", sc["provider"])
	}
}

// loadPipelineDef accepts exactly one definition source; zero or both are
// invalid params.
func TestLoadPipelineDefSourceRules(t *testing.T) {
	srv := newTestServer(policy.Gates{AllowNetwork: true}, []router.Provider{fakeProvider()}, nil)
	inline := `"definition":{"pipeline":"kg.pipeline/v1","name":"n","stages":[{"id":"e","capability":"extract.entities_relations","input":{"fake_field":"d.md"}}]}`
	cases := []struct {
		name string
		args string // inner contents of the arguments object
	}{
		{"neither source", ""},
		{"both sources", inline + `,"definition_path":"/tmp/x.json"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			call := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"kg_pipeline_validate","arguments":{%s}}}`, tc.args)
			frames := exchange(t, srv, call)
			errObj := frames[0]["error"].(map[string]any)
			if errObj["code"].(float64) != -32602 {
				t.Errorf("expected -32602 invalid params, got %v", errObj)
			}
			if !strings.Contains(errObj["message"].(string), "exactly one of") {
				t.Errorf("message should explain the one-of rule: %v", errObj["message"])
			}
		})
	}
}
