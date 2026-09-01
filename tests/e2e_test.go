package tests

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

var binaries string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "kg-acme-e2e-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	binaries = dir
	root := filepath.Clean("..")
	for _, name := range []string{"kg", "kgctl", "kg-mcp"} {
		command := exec.Command("go", "build", "-o", filepath.Join(dir, name), "./cmd/"+name)
		command.Dir = root
		if output, err := command.CombinedOutput(); err != nil {
			panic(string(output))
		}
	}
	os.Exit(m.Run())
}

func TestSnapshotMetadataAndDryRunNeverStartProvider(t *testing.T) {
	home, provider, log := fixture(t)
	run(t, home, "kgctl", "refresh", "--provider-bin", "fake="+provider)
	before := readLog(t, log)
	for _, invocation := range [][]string{
		{"kg", "--help"},
		{"kg", "list"},
		{"kg", "list", "--prefix", "test", "--level", "0", "--tree"},
		{"kg", "test.echo", "--describe"},
		{"kg", "test", "--describe"},
		{"kg", "test.echo", "--params", `{"value":"hello"}`, "--dry-run", "--json"},
		{"kgctl", "capabilities", "list"},
		{"kgctl", "route", "explain", "test.echo", "--json"},
		{"kgctl", "completion", "zsh"},
	} {
		run(t, home, invocation[0], invocation[1:]...)
	}
	definition := filepath.Join(home, "pipeline.json")
	if err := os.WriteFile(definition, []byte(`{"pipeline":"kg.pipeline/v1","name":"public-ids","stages":[{"id":"echo","capability":"test.echo","input":{"value":"hello"}}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, home, "kg", "pipeline.validate", definition, "--json")
	run(t, home, "kg", "pipeline.run", definition, "--dry-run", "--json")
	if after := readLog(t, log); after != before {
		t.Fatalf("metadata/dry-run started provider:\nbefore=%q\nafter=%q", before, after)
	}
}

func TestActualInvocationRevalidatesOnlySelectedProvider(t *testing.T) {
	home, provider, log := fixture(t)
	run(t, home, "kgctl", "refresh", "--provider-bin", "fake="+provider)
	before := strings.Count(readLog(t, log), "\n")
	output := run(t, home, "kg", "test.echo", "--params", `{"value":"hello"}`, "--json")
	var envelope map[string]any
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope["status"] != "ok" || envelope["capability_id"] != "test.echo" {
		t.Fatalf("unexpected envelope: %s", output)
	}
	afterLog := readLog(t, log)
	after := strings.Count(afterLog, "\n")
	if after-before != 3 {
		t.Fatalf("actual call should describe, available, then invoke exactly once; delta=%d log=%q", after-before, afterLog)
	}
	if !strings.Contains(afterLog, "invoke test.echo --request -") {
		t.Fatalf("invoke missing: %q", afterLog)
	}
}

func TestDiscoveryAndDescriptionContract(t *testing.T) {
	home, provider, _ := fixture(t)
	run(t, home, "kgctl", "refresh", "--provider-bin", "fake="+provider)
	help := run(t, home, "kg", "--help")
	if !strings.Contains(help, "CAPABILITY ID") || !strings.Contains(help, "test.echo") || strings.Contains(help, "kg.test.echo") {
		t.Fatalf("unexpected help:\n%s", help)
	}
	list := run(t, home, "kgctl", "capabilities", "list")
	first := strings.Split(strings.TrimSpace(list), "\n")[0]
	if strings.Contains(first, "STATUS") || strings.Contains(first, "AVAILABLE") {
		t.Fatalf("default list must have exactly ID and description columns:\n%s", list)
	}
	listHelp := run(t, home, "kg", "list", "--help")
	if !strings.Contains(listHelp, "--prefix <DOTTED-PREFIX>") || !strings.Contains(listHelp, "0 means all levels") {
		t.Fatalf("list help is incomplete:\n%s", listHelp)
	}
	listJSON := run(t, home, "kg", "list", "--json")
	var inventory struct {
		Items []struct {
			CapabilityID string `json:"capability_id"`
			Available    *bool  `json:"available"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(listJSON), &inventory); err != nil || len(inventory.Items) == 0 || inventory.Items[0].Available == nil {
		t.Fatalf("list JSON must publish capability_id and available: %v %s", err, listJSON)
	}
	description := run(t, home, "kg", "test.echo", "--describe")
	var atomic map[string]any
	if err := json.Unmarshal([]byte(description), &atomic); err != nil {
		t.Fatalf("atomic describe must be an object: %v", err)
	}
	group := run(t, home, "kg", "test", "--describe")
	var grouped []map[string]any
	if err := json.Unmarshal([]byte(group), &grouped); err != nil || len(grouped) != 1 {
		t.Fatalf("group describe must be a list: %v %s", err, group)
	}
	if !strings.Contains(description, "integration_path") {
		t.Fatalf("source integration path missing: %s", description)
	}
	if !strings.Contains(description, "implementation_paths") || !strings.Contains(description, "output_schema") || !strings.Contains(description, "error_contract") {
		t.Fatalf("describe contract is incomplete: %s", description)
	}
	tree := run(t, home, "kg", "list", "--tree")
	if !strings.Contains(tree, "test — Discover operations related to echo a value.") {
		t.Fatalf("tree group description is not informative: %s", tree)
	}
}

func TestMCPToolsListUsesSnapshotOnly(t *testing.T) {
	home, provider, log := fixture(t)
	run(t, home, "kgctl", "refresh", "--provider-bin", "fake="+provider)
	before := readLog(t, log)
	command := exec.Command(filepath.Join(binaries, "kg-mcp"))
	command.Env = environment(home)
	command.Stdin = strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/list\"}\n")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("kg-mcp: %v: %s", err, output)
	}
	if !strings.Contains(string(output), "kg_test_echo") {
		t.Fatalf("tool missing: %s", output)
	}
	if after := readLog(t, log); after != before {
		t.Fatalf("MCP tools/list started provider: before=%q after=%q", before, after)
	}
}

// mcp runs kg-mcp against the given request lines and returns the response
// lines, in order. Notifications and unparseable lines must produce none.
func mcp(t *testing.T, home, requests string) []string {
	t.Helper()
	command := exec.Command(filepath.Join(binaries, "kg-mcp"))
	command.Env = environment(home)
	command.Stdin = strings.NewReader(requests)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("kg-mcp: %v: %s", err, output)
	}
	var responses []string
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line != "" {
			responses = append(responses, line)
		}
	}
	return responses
}

// mcpTools runs tools/list and returns the published tools by name.
func mcpTools(t *testing.T, home string) map[string]map[string]any {
	t.Helper()
	responses := mcp(t, home, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`+"\n")
	if len(responses) != 1 {
		t.Fatalf("tools/list must answer exactly once, got %d: %v", len(responses), responses)
	}
	var envelope struct {
		Result struct {
			Tools []map[string]any `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(responses[0]), &envelope); err != nil {
		t.Fatalf("tools/list response is not JSON: %v: %s", err, responses[0])
	}
	tools := map[string]map[string]any{}
	for _, tool := range envelope.Result.Tools {
		tools[tool["name"].(string)] = tool
	}
	return tools
}

// The dispatch contract: initialize/ping answer, notifications and invalid
// lines stay silent, and every error path returns the documented JSON-RPC
// code and message.
func TestMCPDispatchContract(t *testing.T) {
	home, provider, _ := fixture(t)
	run(t, home, "kgctl", "refresh", "--provider-bin", "fake="+provider)
	responses := mcp(t, home, strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize"}`,
		`{"jsonrpc":"2.0","id":2,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":3,"method":"notifications/initialized"}`,
		`not json`,
		``,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":42}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"no_such_tool"}}`,
		`{"jsonrpc":"2.0","id":6,"method":"resources/list"}`,
	}, "\n")+"\n")
	if len(responses) != 5 {
		t.Fatalf("notifications, empty and invalid lines must produce no response, got %d:\n%s",
			len(responses), strings.Join(responses, "\n"))
	}
	var decoded []struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      float64         `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	for _, line := range responses {
		var r struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      float64         `json:"id"`
			Result  json.RawMessage `json:"result"`
			Error   *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("response is not JSON-RPC: %v: %s", err, line)
		}
		decoded = append(decoded, r)
	}
	wantIDs := []float64{1, 2, 4, 5, 6} // id 3 is the silent notification
	for i, r := range decoded {
		if r.JSONRPC != "2.0" {
			t.Errorf("response %d: jsonrpc = %q, want 2.0", i+1, r.JSONRPC)
		}
		if r.ID != wantIDs[i] {
			t.Errorf("response %d: id = %v, want %v (responses must stay in request order)", i+1, r.ID, wantIDs[i])
		}
	}

	// initialize
	var init struct {
		ProtocolVersion string `json:"protocolVersion"`
		Capabilities    struct {
			Tools struct {
				ListChanged bool `json:"listChanged"`
			} `json:"tools"`
		} `json:"capabilities"`
		ServerInfo struct {
			Name string `json:"name"`
		} `json:"serverInfo"`
	}
	if decoded[0].Error != nil {
		t.Fatalf("initialize errored: %+v", decoded[0].Error)
	}
	if err := json.Unmarshal(decoded[0].Result, &init); err != nil {
		t.Fatalf("initialize result: %v", err)
	}
	if init.ProtocolVersion != "2025-06-18" || init.ServerInfo.Name != "kg-mcp" {
		t.Errorf("initialize: protocol=%q server=%q", init.ProtocolVersion, init.ServerInfo.Name)
	}
	if init.Capabilities.Tools.ListChanged {
		t.Error("initialize must advertise tools.listChanged=false")
	}

	// ping answers an empty result object
	var ping map[string]any
	if decoded[1].Error != nil {
		t.Fatalf("ping errored: %+v", decoded[1].Error)
	}
	if err := json.Unmarshal(decoded[1].Result, &ping); err != nil || len(ping) != 0 {
		t.Errorf("ping result must be an empty object: %v %s", err, decoded[1].Result)
	}

	for _, tc := range []struct {
		resp     int
		wantCode int
		wantMsg  string
	}{
		{2, -32602, "invalid tool arguments"},
		{3, -32602, "unknown tool: no_such_tool"},
		{4, -32601, "method not found: resources/list"},
	} {
		if decoded[tc.resp].Error == nil || decoded[tc.resp].Error.Code != tc.wantCode ||
			decoded[tc.resp].Error.Message != tc.wantMsg {
			got := decoded[tc.resp].Error
			t.Errorf("response %d: want code %d %q, got %+v", tc.resp+1, tc.wantCode, tc.wantMsg, got)
		}
	}
}

// tools/list publishes available capabilities with the hub's gate flags
// injected into every input schema; an unavailable provider's tools are
// withheld while hub capabilities stay.
func TestMCPToolsListSchemaAndAvailability(t *testing.T) {
	home, provider, _ := fixture(t)
	run(t, home, "kgctl", "refresh", "--provider-bin", "fake="+provider)
	tool, ok := mcpTools(t, home)["kg_test_echo"]
	if !ok {
		t.Fatal("kg_test_echo must be published as a tool")
	}
	if tool["title"] != "Echo a value" {
		t.Errorf("tool title: %v", tool["title"])
	}
	schema, ok := tool["inputSchema"].(map[string]any)
	if !ok {
		t.Fatalf("tool inputSchema must be an object: %#v", tool["inputSchema"])
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("inputSchema.properties must be an object: %#v", schema["properties"])
	}
	if _, ok := properties["value"]; !ok {
		t.Error("provider-declared property must survive schema expansion")
	}
	for _, gate := range []string{"dry_run", "allow_network", "allow_data_egress", "allow_model_download", "allow_db_write"} {
		if _, ok := properties[gate]; !ok {
			t.Errorf("expanded schema must inject %q: %v", gate, properties)
		}
	}

	// A provider reporting available:false is withheld from tools/list.
	home2, provider2, _ := fixtureWithAvailable(t, `printf '%s\n' '{"available":false,"ready":[],"missing":[]}'`)
	run(t, home2, "kgctl", "refresh", "--provider-bin", "fake="+provider2)
	tools := mcpTools(t, home2)
	if _, ok := tools["kg_test_echo"]; ok {
		t.Error("unavailable provider's tool must not be listed")
	}
	if _, ok := tools["kg_pipeline_run"]; !ok {
		t.Errorf("hub capability must stay listed, got %v", tools)
	}
}

// tools/call routes through the kg CLI: dry_run maps to --dry-run, the gate
// keys never leak into the provider params, and the provider never starts.
func TestMCPCallDryRunNeverStartsProvider(t *testing.T) {
	home, provider, log := fixture(t)
	run(t, home, "kgctl", "refresh", "--provider-bin", "fake="+provider)
	before := readLog(t, log)
	responses := mcp(t, home,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"kg_test_echo","arguments":{"value":"hello","dry_run":true}}}`+"\n")
	if len(responses) != 1 {
		t.Fatalf("tools/call must answer exactly once, got %d: %v", len(responses), responses)
	}
	var envelope struct {
		Result struct {
			IsError           bool           `json:"isError"`
			StructuredContent map[string]any `json:"structuredContent"`
		} `json:"result"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(responses[0]), &envelope); err != nil {
		t.Fatalf("tools/call response is not JSON: %v: %s", err, responses[0])
	}
	if envelope.Error != nil {
		t.Fatalf("tools/call dry-run errored: %+v", envelope.Error)
	}
	if envelope.Result.IsError {
		t.Error("dry-run call must not be an error result")
	}
	if envelope.Result.StructuredContent["status"] != "ok" {
		t.Errorf("structuredContent.status = %v, want ok: %v",
			envelope.Result.StructuredContent["status"], envelope.Result.StructuredContent)
	}
	if envelope.Result.StructuredContent["capability_id"] != "test.echo" {
		t.Errorf("structuredContent.capability_id = %v, want test.echo",
			envelope.Result.StructuredContent["capability_id"])
	}
	plan, ok := envelope.Result.StructuredContent["result"].(map[string]any)
	if !ok {
		t.Fatalf("structuredContent.result must carry the dry-run plan: %#v",
			envelope.Result.StructuredContent["result"])
	}
	// test.echo declares no side effects, so no gate denies it: the plan says
	// it would execute — yet the provider log proves it never did.
	if plan["would_execute"] != true {
		t.Errorf("no side effects denied → would_execute=true, got %v (plan=%v)", plan["would_execute"], plan)
	}
	if denied, ok := plan["denied"].([]any); ok && len(denied) != 0 {
		t.Errorf("no effects should be denied, got %v", denied)
	}
	if after := readLog(t, log); after != before {
		t.Fatalf("MCP dry-run call started provider:\nbefore=%q\nafter=%q", before, after)
	}
}

// runFail runs a hub binary expecting a non-zero exit and returns its
// combined output and exit code.
func runFail(t *testing.T, home, name string, args ...string) (string, int) {
	t.Helper()
	command := exec.Command(filepath.Join(binaries, name), args...)
	command.Env = environment(home)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("%s %v: expected non-zero exit, got 0:\n%s", name, args, output)
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("%s %v: %v\n%s", name, args, err, output)
	}
	return string(output), exitErr.ExitCode()
}

// The kg execution CLI's failure contract: every documented rejection exits
// 1 with a "kg: <message>" line on stderr — never a panic or partial stdout.
// Each row pins one guard in parseGlobal / argumentsObject / decodeParams.
func TestCLIFailureContract(t *testing.T) {
	home, provider, _ := fixture(t)
	run(t, home, "kgctl", "refresh", "--provider-bin", "fake="+provider)
	paramsFile := filepath.Join(home, "params.json")

	cases := []struct {
		name    string
		args    []string
		wantMsg string
	}{
		{"unknown capability", []string{"kg", "no.such-capability"},
			"kg: capability not found: no.such-capability; run kg list"},
		{"provider-bin belongs to kgctl", []string{"kg", "list", "--provider-bin", "fake=" + provider},
			"kg: provider and inventory options belong to kgctl"},
		{"--all belongs to kgctl", []string{"kg", "list", "--all"},
			"kg: provider and inventory options belong to kgctl"},
		{"describe with execution options", []string{"kg", "test.echo", "--describe", "--params", `{"value":"x"}`},
			"kg: --describe cannot be combined with execution arguments or options"},
		{"params plus positional argument", []string{"kg", "test.echo", "--params", `{"value":"x"}`, "extra"},
			"kg: --params cannot be combined with positional capability arguments"},
		{"dry-run cannot read params file", []string{"kg", "test.echo", "--dry-run", "--params", "@" + paramsFile},
			"kg: dry-run does not read files; pass --params as inline JSON"},
		{"invalid params JSON", []string{"kg", "test.echo", "--params", "nope"},
			"kg: invalid --params JSON"},
		{"params null is not an object", []string{"kg", "test.echo", "--params", "null"},
			"kg: --params must contain a JSON object"},
		{"params trailing second document", []string{"kg", "test.echo", "--params", `{"value":"x"} {}`},
			"kg: --params must contain exactly one JSON object"},
		{"params missing value", []string{"kg", "test.echo", "--params"},
			"kg: --params requires a value"},
		{"provider-bin malformed", []string{"kg", "list", "--provider-bin", "nope"},
			"kg: --provider-bin expects ID=PATH"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			output, code := runFail(t, home, tc.args[0], tc.args[1:]...)
			if code != 1 {
				t.Errorf("exit code = %d, want 1", code)
			}
			if !strings.Contains(output, tc.wantMsg) {
				t.Errorf("output should contain %q:\n%s", tc.wantMsg, output)
			}
		})
	}

	t.Run("unknown capability --json emits exactly one error envelope", func(t *testing.T) {
		output, code := runFail(t, home, "kg", "no.such-capability", "--json")
		if code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
		var envelope struct {
			SchemaVersion string `json:"schema_version"`
			OK            bool   `json:"ok"`
			Error         struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &envelope); err != nil {
			t.Fatalf("--json stdout must be exactly one envelope: %v\n%s", err, output)
		}
		if envelope.SchemaVersion != "kg.error/v1" || envelope.OK {
			t.Errorf("error envelope: %s", output)
		}
		if envelope.Error.Code != "error" {
			t.Errorf("error.code = %q, want \"error\"", envelope.Error.Code)
		}
		if !strings.Contains(envelope.Error.Message, "capability not found: no.such-capability") {
			t.Errorf("error.message should name the capability: %q", envelope.Error.Message)
		}
	})

	t.Run("version prints a semantic version", func(t *testing.T) {
		output := run(t, home, "kg", "version")
		if !regexp.MustCompile(`^\d+\.\d+\.\d+\n$`).MatchString(output) {
			t.Errorf("kg version should print a bare semver, got %q", output)
		}
	})
}

func fixture(t *testing.T) (home, provider, log string) {
	t.Helper()
	return fixtureWithAvailable(t, `printf '%s\n' '{"available":true,"ready":[],"missing":[]}'`)
}

// fixtureWithAvailable is fixture with a custom shell snippet for the
// available action, so tests can publish an unavailable provider.
func fixtureWithAvailable(t *testing.T, available string) (home, provider, log string) {
	t.Helper()
	home = t.TempDir()
	log = filepath.Join(home, "provider.log")
	provider = filepath.Join(home, "fake-provider")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "` + log + `"
case "$1" in
  describe)
    printf '%s\n' '{"protocol":"kg.provider/v1","protocol_versions":[1],"provider":{"id":"fake","version":"1.0.0","description":"Fake provider"},"source":{"local_code_path":"/tmp/fake-provider-source"},"capabilities":[{"capability_id":"test.echo","title":"Echo a value","description":"Return the supplied value without changing it.","side_effects":[],"input_schema":{"type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false},"output":{"mode":"result-json","kind":"json"},"cli_spec":{"subcommand":[],"always":[],"positionals":[],"flags":[]}}]}' ;;
  available) ` + available + ` ;;
  invoke) printf '%s\n' '{"protocol":"kg.execution/v1","capability_id":"test.echo","provider":"fake","status":"ok","result":{"value":"hello"}}' ;;
esac
`
	if err := os.WriteFile(provider, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return
}

func run(t *testing.T, home, name string, args ...string) string {
	t.Helper()
	command := exec.Command(filepath.Join(binaries, name), args...)
	command.Env = environment(home)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, output)
	}
	return string(output)
}
func environment(home string) []string {
	path := os.Getenv("PATH")
	if runtime.GOOS == "windows" {
		return []string{"USERPROFILE=" + home, "PATH=" + path}
	}
	return []string{"HOME=" + home, "PATH=" + path}
}
func readLog(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
