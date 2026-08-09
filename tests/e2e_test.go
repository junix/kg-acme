// Package tests exercises the kg binary end to end against fake provider
// shell scripts: one protocol-native provider (describe/available/invoke),
// one legacy CLI reached through the hub fallback bridge, and two broken
// providers for the malformed/unsupported version error split.
package tests

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"kg-acme/internal/bridge"
	"kg-acme/internal/protocol"
)

var (
	kgBin  string
	binDir string
	homeDir string
)

func TestMain(m *testing.M) {
	root, err := os.MkdirTemp("", "kg-acme-e2e")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(root)

	kgBin = filepath.Join(root, "kg")
	build := exec.Command("go", "build", "-o", kgBin, "kg-acme/cmd/kg")
	build.Dir = ".."
	if out, err := build.CombinedOutput(); err != nil {
		panic("building kg: " + err.Error() + "\n" + string(out))
	}

	binDir = filepath.Join(root, "bin")
	homeDir = filepath.Join(root, "home")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		panic(err)
	}
	if err := os.MkdirAll(homeDir, 0o755); err != nil {
		panic(err)
	}

	writeScript(filepath.Join(binDir, "kg-provider-fake"), fakeProviderScript)
	writeScript(filepath.Join(binDir, "kg-extract"), legacyExtractScript)
	writeScript(filepath.Join(binDir, "kg-extract-ng"), probedExtractScript)
	writeScript(filepath.Join(binDir, "kg-provider-bad"), badManifestScript)
	writeScript(filepath.Join(binDir, "kg-provider-newver"), newVersionScript)

	os.Exit(m.Run())
}

func writeScript(path, body string) {
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		panic(err)
	}
}

const fakeProviderScript = `#!/bin/sh
case "$1" in
  describe)
    cat <<'JSON'
{
  "protocol": "kg.provider/v1",
  "protocol_versions": [1],
  "provider": {"id": "kg-provider-fake", "version": "0.1.0", "description": "fake protocol provider"},
  "capabilities": [{
    "capability_id": "extract.entities_relations",
    "title": "Fake extract",
    "description": "Echoes the request.",
    "side_effects": ["network"],
    "input_schema": {"type": "object", "properties": {"file": {"type": "string"}}, "required": ["file"], "additionalProperties": false},
    "output": {"mode": "result-json", "kind": "kg-document"},
    "cli_spec": {"subcommand": ["extract"], "always": [], "positionals": [{"name": "file", "required": true}], "flags": []}
  }]
}
JSON
    ;;
  available)
    echo '{"available":true,"ready":[{"name":"fake-dep","kind":"test"}],"missing":[],"cache_dir":"/tmp/fake-cache"}'
    ;;
  invoke)
    cap="$2"
    req=$(cat)
    echo "invoke log line" >&2
    printf '{"protocol":"kg.execution/v1","capability_id":"%s","provider":"kg-provider-fake","status":"ok","result":{"echo":%s}}\n' "$cap" "$req"
    ;;
esac
exit 0
`

const legacyExtractScript = `#!/bin/sh
echo "kg-extract log: $*" >&2
echo '{"entities":["Alice"]}'
exit 0
`

// probedExtractScript is a protocol-native kg-extract: it self-describes with
// a cli_spec that deliberately differs from the hub fallback table (positional
// file instead of --file), so the drift diagnostic must fire while the
// provider's spec stays authoritative.
const probedExtractScript = `#!/bin/sh
case "$1" in
  describe)
    cat <<'JSON'
{
  "protocol": "kg.provider/v1",
  "protocol_versions": [1],
  "provider": {"id": "kg-extract", "version": "9.9", "description": "protocol-native kg-extract double"},
  "capabilities": [{
    "capability_id": "extract.entities_relations",
    "title": "Extract entities and relations",
    "description": "Echoes the request.",
    "side_effects": ["network"],
    "input_schema": {"type": "object", "properties": {"file": {"type": "string"}}, "required": ["file"], "additionalProperties": false},
    "output": {"mode": "result-json", "kind": "kg-document"},
    "cli_spec": {"subcommand": [], "always": [], "positionals": [{"name": "file", "required": true}], "flags": []}
  }]
}
JSON
    ;;
  available)
    echo '{"available":true,"ready":[],"missing":[],"cache_dir":"/tmp/fake-cache"}'
    ;;
  invoke)
    cap="$2"
    req=$(cat)
    printf '{"protocol":"kg.execution/v1","capability_id":"%s","provider":"kg-extract","status":"ok","result":{"echo":%s}}\n' "$cap" "$req"
    ;;
esac
exit 0
`

const badManifestScript = `#!/bin/sh
case "$1" in
  describe) echo '{"protocol":"kg.provider/v1","protocol_versions":[1]}' ;;
  available) echo '{"available":true,"ready":[],"missing":[]}' ;;
esac
exit 0
`

const newVersionScript = `#!/bin/sh
case "$1" in
  describe)
    cat <<'JSON'
{
  "protocol": "kg.provider/v1",
  "protocol_versions": [2],
  "provider": {"id": "kg-provider-newver", "version": "9.9", "description": "future provider"},
  "capabilities": [{
    "capability_id": "extract.entities_relations",
    "title": "Future extract",
    "description": "Speaks v2 only.",
    "side_effects": ["network"],
    "input_schema": {"type": "object"},
    "output": {"mode": "result-json", "kind": "json"},
    "cli_spec": {"subcommand": [], "always": [], "positionals": [], "flags": []}
  }]
}
JSON
    ;;
  available) echo '{"available":true,"ready":[],"missing":[]}' ;;
esac
exit 0
`

// runKG executes the built binary with HOME pointed at the fake layout and
// PATH preferring the fake bin dir but keeping the system paths (provider
// scripts need coreutils like cat).
func runKG(t *testing.T, stdin string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(kgBin, args...)
	cmd.Env = []string{
		"PATH=" + binDir + ":/usr/bin:/bin:/usr/sbin:/sbin",
		"HOME=" + homeDir,
	}
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out, errb strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("running kg %v: %v", args, err)
	}
	return out.String(), errb.String(), code
}

func parseEnvelope(t *testing.T, stdout string) protocol.Envelope {
	t.Helper()
	var env protocol.Envelope
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &env); err != nil {
		t.Fatalf("stdout must be exactly one JSON envelope, got %q: %v", stdout, err)
	}
	return env
}

func TestListDiscoversProviders(t *testing.T) {
	stdout, _, code := runKG(t, "", "list", "--json")
	if code != 0 {
		t.Fatalf("kg list exited %d", code)
	}
	var entries []struct {
		ID       string `json:"id"`
		Probed   bool   `json:"probed"`
		Fallback bool   `json:"fallback"`
	}
	if err := json.Unmarshal([]byte(stdout), &entries); err != nil {
		t.Fatalf("list --json output: %v\n%s", err, stdout)
	}
	byID := map[string]struct {
		Probed   bool
		Fallback bool
	}{}
	for _, e := range entries {
		byID[e.ID] = struct {
			Probed   bool
			Fallback bool
		}{e.Probed, e.Fallback}
	}
	fake, ok := byID["kg-provider-fake"]
	if !ok || !fake.Probed || fake.Fallback {
		t.Errorf("kg-provider-fake should be probed protocol provider: %+v", byID)
	}
	legacy, ok := byID["kg-extract"]
	if !ok || legacy.Probed || !legacy.Fallback {
		t.Errorf("kg-extract should be a fallback bridge: %+v", byID)
	}
}

func TestExtractPolicyDeniedByDefault(t *testing.T) {
	stdout, _, code := runKG(t, "", "extract", "doc.md", "--json")
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	env := parseEnvelope(t, stdout)
	if env.Status != "error" || env.Error == nil || env.Error.Code != protocol.ErrPolicyDenied {
		t.Errorf("expected policy_denied, got %+v", env.Error)
	}
}

func TestExtractViaProtocolProvider(t *testing.T) {
	stdout, _, code := runKG(t, "", "extract", "doc.md", "--json", "--allow-network")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, stdout)
	}
	env := parseEnvelope(t, stdout)
	if env.Status != "ok" || env.Provider != "kg-provider-fake" {
		t.Fatalf("expected ok from kg-provider-fake, got %+v", env)
	}
	result, ok := env.Result.(map[string]any)
	if !ok {
		t.Fatalf("result should be an object, got %#v", env.Result)
	}
	echo, ok := result["echo"].(map[string]any)
	if !ok {
		t.Fatalf("result.echo should be the request, got %#v", result)
	}
	input, ok := echo["input"].(map[string]any)
	if !ok || input["file"] != "doc.md" {
		t.Errorf("provider should receive input.file=doc.md, got %#v", echo)
	}
}

func TestExtractDryRun(t *testing.T) {
	stdout, _, code := runKG(t, "", "extract", "doc.md", "--dry-run", "--json")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	env := parseEnvelope(t, stdout)
	plan, ok := env.Result.(map[string]any)
	if !ok || plan["dry_run"] != true {
		t.Fatalf("expected dry-run plan, got %#v", env.Result)
	}
	if plan["would_execute"] != false {
		t.Errorf("gates closed → would_execute=false, got %v", plan["would_execute"])
	}
}

func TestExtractViaFallbackBridge(t *testing.T) {
	stdout, _, code := runKG(t, "",
		"extract", "--file", "doc.md", "--provider", "kg-extract",
		"--allow-network", "--allow-data-egress", "--json")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, stdout)
	}
	env := parseEnvelope(t, stdout)
	if env.Status != "ok" || env.Provider != "kg-extract" {
		t.Fatalf("expected ok from kg-extract, got %+v", env)
	}
	result, ok := env.Result.(map[string]any)
	if !ok {
		t.Fatalf("result should be parsed provider JSON, got %#v", env.Result)
	}
	entities, ok := result["entities"].([]any)
	if !ok || len(entities) != 1 || entities[0] != "Alice" {
		t.Errorf("expected entities [Alice], got %#v", result["entities"])
	}
}

func TestExtractFallbackStillGated(t *testing.T) {
	// kg-extract declares data_egress too; allowing only network is not enough.
	stdout, _, code := runKG(t, "",
		"extract", "--file", "doc.md", "--provider", "kg-extract", "--allow-network", "--json")
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	env := parseEnvelope(t, stdout)
	if env.Error == nil || env.Error.Code != protocol.ErrPolicyDenied {
		t.Errorf("expected policy_denied, got %+v", env.Error)
	}
	if !strings.Contains(env.Error.Message, "data_egress") {
		t.Errorf("error should name the denied effect, got %q", env.Error.Message)
	}
}

// A protocol-native provider injected as kg-extract must take the probed
// protocol path (no silent fallback), and its cli_spec — which differs from
// the hub fallback table — must trigger the drift diagnostic.
func TestExtractViaProbedKgExtract(t *testing.T) {
	inject := "--provider-bin=kg-extract=" + filepath.Join(binDir, "kg-extract-ng")

	// Dry-run: the plan shows the probed protocol-native path.
	stdout, _, code := runKG(t, "", "extract", "doc.md", inject, "--dry-run", "--json")
	if code != 0 {
		t.Fatalf("dry-run exit %d: %s", code, stdout)
	}
	env := parseEnvelope(t, stdout)
	plan, ok := env.Result.(map[string]any)
	if !ok || plan["probed"] != true {
		t.Fatalf("injected kg-extract must resolve probed:true, got %#v", env.Result)
	}

	// Real run: protocol invoke, drift diagnostic on the envelope.
	stdout, _, code = runKG(t, "", "extract", "doc.md", inject, "--json", "--allow-network")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, stdout)
	}
	env = parseEnvelope(t, stdout)
	if env.Status != "ok" || env.Provider != "kg-extract" {
		t.Fatalf("expected ok from probed kg-extract, got %+v", env)
	}
	found := false
	for _, d := range env.Diagnostics {
		if d.Message == bridge.CLISpecDiffDiagnostic {
			found = true
		}
	}
	if !found {
		t.Errorf("expected cli_spec drift diagnostic, got %+v", env.Diagnostics)
	}
	result, ok := env.Result.(map[string]any)
	if !ok {
		t.Fatalf("result should be an object, got %#v", env.Result)
	}
	echo, ok := result["echo"].(map[string]any)
	if !ok || echo["capability_id"] != "extract.entities_relations" {
		t.Errorf("provider should receive capability extract.entities_relations, got %#v", echo)
	}
}

// Every stable catalog command routes to the provider-published capability
// namespace (here unresolvable → capability_not_found naming the new id).
func TestCatalogCommandsRouteToNewNamespace(t *testing.T) {
	cases := []struct {
		args    []string
		wantCap string
	}{
		{[]string{"dedup"}, "resolve.coref"},
		{[]string{"communities"}, "detect.communities"},
		{[]string{"communities", "hierarchy"}, "detect.communities_hierarchy"},
		{[]string{"communities", "summaries"}, "summarize.communities"},
		{[]string{"store"}, "store.triples"},
		{[]string{"ask"}, "retrieve.ask"},
		{[]string{"parse"}, "parse.multimodal"},
	}
	for _, tc := range cases {
		args := append(append([]string{}, tc.args...), "--json")
		stdout, _, code := runKG(t, "", args...)
		if code != 1 {
			t.Errorf("%v: expected exit 1, got %d: %s", tc.args, code, stdout)
			continue
		}
		env := parseEnvelope(t, stdout)
		if env.Error == nil || env.Error.Code != protocol.ErrCapabilityNotFound {
			t.Errorf("%v: expected capability_not_found, got %+v", tc.args, env.Error)
		}
		if env.CapabilityID != tc.wantCap {
			t.Errorf("%v: expected capability_id %q, got %q", tc.args, tc.wantCap, env.CapabilityID)
		}
	}
}

func TestProviderEscapeHatch(t *testing.T) {
	stdout, _, code := runKG(t, `{"file":"x.md"}`,
		"provider", "kg-provider-fake", "extract.entities_relations", "--request", "-",
		"--json", "--allow-network")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, stdout)
	}
	env := parseEnvelope(t, stdout)
	if env.Status != "ok" || env.CapabilityID != "extract.entities_relations" {
		t.Errorf("escape hatch envelope: %+v", env)
	}
}

func TestProviderEscapeHatchFromFile(t *testing.T) {
	reqFile := filepath.Join(homeDir, "req.json")
	if err := os.WriteFile(reqFile, []byte(`{"file":"y.md"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, _, code := runKG(t, "",
		"provider", "kg-provider-fake", "extract.entities_relations", "--request", reqFile,
		"--json", "--allow-network")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, stdout)
	}
	env := parseEnvelope(t, stdout)
	if env.Status != "ok" {
		t.Errorf("escape hatch from file: %+v", env)
	}
}

func TestDescribeMalformedVsUnsupported(t *testing.T) {
	stdout, _, code := runKG(t, "", "describe", "kg-provider-bad", "--json")
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	env := parseEnvelope(t, stdout)
	if env.Error == nil || env.Error.Code != protocol.ErrMalformedManifest {
		t.Errorf("expected malformed_manifest, got %+v", env.Error)
	}

	stdout, _, code = runKG(t, "", "describe", "kg-provider-newver", "--json")
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	env = parseEnvelope(t, stdout)
	if env.Error == nil || env.Error.Code != protocol.ErrUnsupportedSchemaVersion {
		t.Errorf("expected unsupported_schema_version, got %+v", env.Error)
	}
}

func TestDescribeProbedProvider(t *testing.T) {
	stdout, _, code := runKG(t, "", "describe", "kg-provider-fake", "--json")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	var m protocol.Manifest
	if err := json.Unmarshal([]byte(stdout), &m); err != nil {
		t.Fatalf("describe output: %v", err)
	}
	if m.Provider.ID != "kg-provider-fake" || len(m.Capabilities) != 1 {
		t.Errorf("manifest: %+v", m.Provider)
	}
}

func TestPipelineStub(t *testing.T) {
	_, _, code := runKG(t, "", "pipeline")
	if code != 2 {
		t.Errorf("pipeline stub should exit 2, got %d", code)
	}
}
