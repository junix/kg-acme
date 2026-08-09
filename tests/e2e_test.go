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
	"kg-acme/internal/pipeline"
	"kg-acme/internal/protocol"
)

var (
	kgBin    string
	kgMCPBin string
	binDir   string
	homeDir  string
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

	kgMCPBin = filepath.Join(root, "kg-mcp")
	buildMCP := exec.Command("go", "build", "-o", kgMCPBin, "kg-acme/cmd/kg-mcp")
	buildMCP.Dir = ".."
	if out, err := buildMCP.CombinedOutput(); err != nil {
		panic("building kg-mcp: " + err.Error() + "\n" + string(out))
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
	writeScript(filepath.Join(binDir, "kg-provider-pipe"), pipeProviderScript)
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

// pipeProviderScript is a protocol-native provider covering the full
// pipeline chain: parse (chunks artifact), extract (kg-document artifact),
// coref (graph-in/out artifact), store (graph-in, result-json). Artifacts
// land under $HOME/fake-out; parse fails when its sidecar file is missing
// (drives the optional-skip test); extract appends a marker per invocation
// (drives the resume-skip assertion); store records the document path it
// received (proves edge wiring end to end).
const pipeProviderScript = `#!/bin/sh
case "$1" in
  describe)
    cat <<'JSON'
{
  "protocol": "kg.provider/v1",
  "protocol_versions": [1],
  "provider": {"id": "kg-provider-pipe", "version": "0.1.0", "description": "fake pipeline-capable provider"},
  "capabilities": [
    {
      "capability_id": "parse.multimodal",
      "title": "Fake parse",
      "description": "Writes a chunks JSONL artifact.",
      "side_effects": ["network"],
      "input_schema": {"type": "object", "properties": {"sidecar": {"type": "string"}}, "required": ["sidecar"], "additionalProperties": false},
      "output": {"mode": "artifact", "kind": "chunks"},
      "cli_spec": {"subcommand": [], "always": [], "positionals": [], "flags": []}
    },
    {
      "capability_id": "extract.entities_relations",
      "title": "Fake extract",
      "description": "Writes a kg-document artifact.",
      "side_effects": ["network", "data_egress"],
      "input_schema": {"type": "object", "properties": {"file": {"type": "string"}, "text": {"type": "string"}}, "additionalProperties": false},
      "output": {"mode": "artifact", "kind": "kg-document"},
      "cli_spec": {"subcommand": [], "always": [], "positionals": [], "flags": []}
    },
    {
      "capability_id": "resolve.coref",
      "title": "Fake coref",
      "description": "Copies the input kg-document.",
      "side_effects": [],
      "input_schema": {"type": "object", "properties": {"document": {"type": "string"}, "document_file": {"type": "string"}}, "additionalProperties": false},
      "output": {"mode": "artifact", "kind": "kg-document"},
      "cli_spec": {"subcommand": [], "always": [], "positionals": [], "flags": []}
    },
    {
      "capability_id": "store.triples",
      "title": "Fake store",
      "description": "Records the received document path.",
      "side_effects": ["writes_db"],
      "input_schema": {"type": "object", "properties": {"document": {"type": "string"}, "document_file": {"type": "string"}}, "additionalProperties": false},
      "output": {"mode": "result-json", "kind": "json"},
      "cli_spec": {"subcommand": [], "always": [], "positionals": [], "flags": []}
    }
  ]
}
JSON
    ;;
  available)
    echo '{"available":true,"ready":[],"missing":[]}'
    ;;
  invoke)
    cap="$2"
    req=$(cat)
    out="$HOME/fake-out"
    mkdir -p "$out"
    art=""
    kind=""
    case "$cap" in
      parse.multimodal)
        sidecar=$(printf '%s' "$req" | sed -n 's/.*"sidecar":"\([^"]*\)".*/\1/p')
        if [ ! -f "$sidecar" ]; then
          printf '{"protocol":"kg.execution/v1","capability_id":"%s","provider":"kg-provider-pipe","status":"error","error":{"code":"invocation_failed","message":"sidecar not found: %s"}}\n' "$cap" "$sidecar"
          exit 0
        fi
        art="$out/parse-chunks.jsonl"
        echo '{"text":"hello chunk"}' > "$art"
        kind="chunks"
        ;;
      extract.entities_relations)
        echo run >> "$out/extract-runs"
        art="$out/extract-kg.json"
        printf '{"schema_version":"kg.protocol.v1","entities":[{"name":"Alice"}],"relations":[]}\n' > "$art"
        kind="kg-document"
        ;;
      resolve.coref)
        doc=$(printf '%s' "$req" | sed -n 's/.*"document_file":"\([^"]*\)".*/\1/p')
        art="$out/dedup-kg.json"
        cp "$doc" "$art"
        kind="kg-document"
        ;;
      store.triples)
        doc=$(printf '%s' "$req" | sed -n 's/.*"document_file":"\([^"]*\)".*/\1/p')
        printf '%s\n' "$doc" > "$out/store-received"
        printf '{"protocol":"kg.execution/v1","capability_id":"%s","provider":"kg-provider-pipe","status":"ok","result":{"stored":true}}\n' "$cap"
        exit 0
        ;;
      *)
        exit 1
        ;;
    esac
    sum=$(shasum -a 256 "$art" | cut -d' ' -f1)
    printf '{"protocol":"kg.execution/v1","capability_id":"%s","provider":"kg-provider-pipe","status":"ok","artifacts":[{"path":"%s","kind":"%s","checksum":"sha256:%s"}]}\n' "$cap" "$art" "$kind" "$sum"
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
		{[]string{"communities", "semantic"}, "detect.communities_semantic"},
		{[]string{"store"}, "store.triples"},
		{[]string{"ask"}, "retrieve.ask"},
		{[]string{"parse"}, "parse.multimodal"},
		{[]string{"layout", "compute"}, "layout.compute"},
		{[]string{"analyze", "centrality"}, "analyze.centrality"},
		{[]string{"embed", "nodes"}, "embed.nodes"},
	}
	for _, tc := range cases {
		// Constrain routing to a provider that offers none of these
		// capabilities, so resolution fails and the envelope names the
		// catalog-mapped capability_id.
		args := append(append([]string{}, tc.args...), "--provider", "kg-provider-fake", "--json")
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

// writePipelineDef renders a kg.pipeline/v1 definition into a temp file.
func writePipelineDef(t *testing.T, def string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pipeline.json")
	if err := os.WriteFile(path, []byte(def), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func parsePipelineEnvelope(t *testing.T, stdout string) pipeline.Envelope {
	t.Helper()
	var env pipeline.Envelope
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &env); err != nil {
		t.Fatalf("stdout must be exactly one pipeline envelope, got %q: %v", stdout, err)
	}
	return env
}

// chainPipelineDef wires parse → extract → dedup → store through the fake
// kg-provider-pipe. sidecar must be an existing file (parse fails
// otherwise, which the optional-skip test exploits).
func chainPipelineDef(sidecar string) string {
	return `{
  "pipeline": "kg.pipeline/v1",
  "name": "doc-to-graph",
  "stages": [
    {"id": "parse", "capability": "parse.multimodal", "optional": true,
     "input": {"sidecar": "` + sidecar + `"}},
    {"id": "extract", "capability": "extract.entities_relations",
     "input": {"file": "doc.md"}},
    {"id": "dedup", "capability": "resolve.coref",
     "input_from": {"stage": "extract", "artifact_kind": "kg-document", "as": "document_file"}},
    {"id": "store", "capability": "store.triples",
     "input_from": {"stage": "dedup", "artifact_kind": "kg-document", "as": "document_file"}}
  ]
}`
}

func TestPipelineFullChain(t *testing.T) {
	sidecar := filepath.Join(t.TempDir(), "sidecar.json")
	if err := os.WriteFile(sidecar, []byte(`{"docs":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	workDir := filepath.Join(t.TempDir(), "work")
	def := writePipelineDef(t, chainPipelineDef(sidecar))

	stdout, _, code := runKG(t, "", "pipeline", "run", def,
		"--provider", "kg-provider-pipe",
		"--work-dir", workDir, "--json",
		"--allow-network", "--allow-data-egress", "--allow-db-write")
	if code != 0 {
		t.Fatalf("pipeline run exited %d: %s", code, stdout)
	}
	env := parsePipelineEnvelope(t, stdout)
	if env.Protocol != "kg.pipeline.execution/v1" || env.Status != "ok" || env.Pipeline != "doc-to-graph" {
		t.Fatalf("pipeline envelope: %+v", env)
	}
	wantOrder := []string{"parse", "extract", "dedup", "store"}
	if len(env.Stages) != len(wantOrder) {
		t.Fatalf("expected %d stages, got %+v", len(wantOrder), env.Stages)
	}
	for i, s := range env.Stages {
		if s.ID != wantOrder[i] || s.Status != "ok" {
			t.Errorf("stage %d: %+v", i, s)
		}
		if s.Provider != "kg-provider-pipe" {
			t.Errorf("stage %s provider = %q", s.ID, s.Provider)
		}
	}
	// Artifacts are copied into the work dir with checksums.
	for _, id := range []string{"parse", "extract", "dedup"} {
		s := env.Stages[0]
		for _, st := range env.Stages {
			if st.ID == id {
				s = st
			}
		}
		if len(s.Artifacts) != 1 || filepath.Dir(s.Artifacts[0].Path) != workDir || s.Artifacts[0].Checksum == "" {
			t.Errorf("stage %s artifacts: %+v", id, s.Artifacts)
		}
		if _, err := os.Stat(s.Artifacts[0].Path); err != nil {
			t.Errorf("stage %s artifact missing: %v", id, err)
		}
		// Per-stage envelope recorded for resume.
		if _, err := os.Stat(filepath.Join(workDir, "stage-"+id+".envelope.json")); err != nil {
			t.Errorf("stage envelope missing for %s: %v", id, err)
		}
	}
	// Edge wiring reached the store: it received dedup's work-dir artifact.
	received, err := os.ReadFile(filepath.Join(homeDir, "fake-out", "store-received"))
	if err != nil {
		t.Fatalf("store did not record its input: %v", err)
	}
	dedupArt := env.Stages[2].Artifacts[0].Path
	if strings.TrimSpace(string(received)) != dedupArt {
		t.Errorf("store received %q, expected dedup artifact %q", received, dedupArt)
	}
}

func TestPipelineValidateRejectsIncompatibleEdge(t *testing.T) {
	// chunks (parse output) cannot feed the graph-in property document_file.
	def := writePipelineDef(t, `{
	  "pipeline": "kg.pipeline/v1", "name": "bad-edge",
	  "stages": [
	    {"id": "parse", "capability": "parse.multimodal", "input": {"sidecar": "s.json"}},
	    {"id": "dedup", "capability": "resolve.coref",
	     "input_from": {"stage": "parse", "artifact_kind": "chunks", "as": "document_file"}}
	  ]
	}`)
	stdout, _, code := runKG(t, "", "pipeline", "validate", def, "--provider", "kg-provider-pipe", "--json")
	if code != 1 {
		t.Fatalf("expected exit 1, got %d: %s", code, stdout)
	}
	env := parsePipelineEnvelope(t, stdout)
	if env.Error == nil || env.Error.Code != protocol.ErrIncompatibleStageEdge {
		t.Errorf("expected incompatible_stage_edge, got %+v", env.Error)
	}
}

func TestPipelineValidateOk(t *testing.T) {
	def := writePipelineDef(t, chainPipelineDef("s.json"))
	_, stderr, code := runKG(t, "", "pipeline", "validate", def, "--provider", "kg-provider-pipe")
	if code != 0 {
		t.Fatalf("validate exited %d: %s", code, stderr)
	}
	if !strings.Contains(stderr, "parse → extract → dedup → store") {
		t.Errorf("validate should print the topological order, got %q", stderr)
	}
}

func TestPipelineGatePrecheck(t *testing.T) {
	sidecar := filepath.Join(t.TempDir(), "sidecar.json")
	if err := os.WriteFile(sidecar, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	workDir := filepath.Join(t.TempDir(), "work")
	def := writePipelineDef(t, chainPipelineDef(sidecar))
	os.Remove(filepath.Join(homeDir, "fake-out", "extract-runs"))

	stdout, _, code := runKG(t, "", "pipeline", "run", def, "--provider", "kg-provider-pipe", "--work-dir", workDir, "--json")
	if code != 1 {
		t.Fatalf("expected exit 1, got %d: %s", code, stdout)
	}
	env := parsePipelineEnvelope(t, stdout)
	if env.Error == nil || env.Error.Code != protocol.ErrPolicyDenied {
		t.Fatalf("expected policy_denied, got %+v", env.Error)
	}
	for _, want := range []string{"--allow-network", "--allow-data-egress", "--allow-db-write"} {
		if !strings.Contains(env.Error.Message, want) {
			t.Errorf("error should name %s: %q", want, env.Error.Message)
		}
	}
	if len(env.Stages) != 0 {
		t.Errorf("no stage may run under gate denial: %+v", env.Stages)
	}
	// fail fast: providers never started.
	if _, err := os.Stat(filepath.Join(homeDir, "fake-out", "extract-runs")); !os.IsNotExist(err) {
		t.Errorf("extract must not have run under gate denial")
	}
}

func TestPipelineDryRun(t *testing.T) {
	def := writePipelineDef(t, chainPipelineDef("s.json"))
	workDir := filepath.Join(t.TempDir(), "work")
	stdout, _, code := runKG(t, "", "pipeline", "run", def, "--dry-run", "--provider", "kg-provider-pipe", "--work-dir", workDir, "--json")
	if code != 0 {
		t.Fatalf("dry-run exited %d: %s", code, stdout)
	}
	env := parsePipelineEnvelope(t, stdout)
	if !env.DryRun || env.Status != "ok" {
		t.Fatalf("dry-run envelope: %+v", env)
	}
	for _, s := range env.Stages {
		if s.Status != "planned" {
			t.Errorf("stage %s status %s, want planned", s.ID, s.Status)
		}
	}
	// Resolved input shows the injection placeholder for wired edges.
	var dedup *pipeline.StageResult
	for i := range env.Stages {
		if env.Stages[i].ID == "dedup" {
			dedup = &env.Stages[i]
		}
	}
	if dedup == nil || dedup.Input["document_file"] != "kg-pipeline://extract/kg-document" {
		t.Errorf("dedup planned input: %+v", dedup)
	}
	// Zero execution: the work dir was never created.
	if _, err := os.Stat(workDir); !os.IsNotExist(err) {
		t.Errorf("dry-run must not create the work dir")
	}
}

func TestPipelineResume(t *testing.T) {
	sidecar := filepath.Join(t.TempDir(), "sidecar.json")
	if err := os.WriteFile(sidecar, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	workDir := filepath.Join(t.TempDir(), "work")
	def := writePipelineDef(t, chainPipelineDef(sidecar))
	gates := []string{"--provider", "kg-provider-pipe", "--allow-network", "--allow-data-egress", "--allow-db-write", "--json"}
	os.Remove(filepath.Join(homeDir, "fake-out", "extract-runs"))

	args := append([]string{"pipeline", "run", def, "--work-dir", workDir}, gates...)
	stdout, _, code := runKG(t, "", args...)
	if code != 0 {
		t.Fatalf("first run exited %d: %s", code, stdout)
	}
	countRuns := func() int {
		data, err := os.ReadFile(filepath.Join(homeDir, "fake-out", "extract-runs"))
		if err != nil {
			return 0
		}
		return strings.Count(strings.TrimSpace(string(data)), "run")
	}
	if n := countRuns(); n != 1 {
		t.Fatalf("extract should have run once, ran %d", n)
	}

	stdout, _, code = runKG(t, "", "pipeline", "run", def, "--resume", workDir, "--provider", "kg-provider-pipe", "--json",
		"--allow-network", "--allow-data-egress", "--allow-db-write")
	if code != 0 {
		t.Fatalf("resume run exited %d: %s", code, stdout)
	}
	env := parsePipelineEnvelope(t, stdout)
	if env.Status != "ok" {
		t.Fatalf("resume run failed: %+v", env.Error)
	}
	if n := countRuns(); n != 1 {
		t.Errorf("resume must skip completed stages, extract ran %d times", n)
	}
	reused := 0
	for _, d := range env.Diagnostics {
		if strings.Contains(d.Message, "reused from") {
			reused++
		}
	}
	if reused != 4 {
		t.Errorf("expected 4 reuse diagnostics, got %d: %+v", reused, env.Diagnostics)
	}
}

func TestPipelineOptionalStageSkipped(t *testing.T) {
	workDir := filepath.Join(t.TempDir(), "work")
	def := writePipelineDef(t, chainPipelineDef(filepath.Join(t.TempDir(), "missing-sidecar.json")))
	stdout, _, code := runKG(t, "", "pipeline", "run", def, "--provider", "kg-provider-pipe", "--work-dir", workDir, "--json",
		"--allow-network", "--allow-data-egress", "--allow-db-write")
	if code != 0 {
		t.Fatalf("optional failure must not fail the pipeline, exit %d: %s", code, stdout)
	}
	env := parsePipelineEnvelope(t, stdout)
	if env.Status != "ok" {
		t.Fatalf("pipeline should be ok, got %+v", env.Error)
	}
	if env.Stages[0].ID != "parse" || env.Stages[0].Status != "skipped" {
		t.Errorf("parse should be skipped: %+v", env.Stages[0])
	}
	found := false
	for _, d := range env.Diagnostics {
		if d.Severity == "warning" && strings.Contains(d.Message, "optional stage \"parse\" failed, skipping") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected optional-skip diagnostic, got %+v", env.Diagnostics)
	}
}

// Stages listed out of dependency order must still execute in topological
// order (DAG semantics, not definition order).
func TestPipelineTopologicalOrder(t *testing.T) {
	def := writePipelineDef(t, `{
	  "pipeline": "kg.pipeline/v1", "name": "shuffled",
	  "stages": [
	    {"id": "store", "capability": "store.triples",
	     "input_from": {"stage": "dedup", "as": "document_file"}},
	    {"id": "dedup", "capability": "resolve.coref",
	     "input_from": {"stage": "extract", "as": "document_file"}},
	    {"id": "extract", "capability": "extract.entities_relations",
	     "input": {"file": "doc.md"}}
	  ]
	}`)
	workDir := filepath.Join(t.TempDir(), "work")
	stdout, _, code := runKG(t, "", "pipeline", "run", def, "--provider", "kg-provider-pipe", "--work-dir", workDir, "--json",
		"--allow-network", "--allow-data-egress", "--allow-db-write")
	if code != 0 {
		t.Fatalf("pipeline run exited %d: %s", code, stdout)
	}
	env := parsePipelineEnvelope(t, stdout)
	want := []string{"extract", "dedup", "store"}
	if len(env.Stages) != len(want) {
		t.Fatalf("stages: %+v", env.Stages)
	}
	for i, s := range env.Stages {
		if s.ID != want[i] || s.Status != "ok" {
			t.Errorf("stage %d = %s (%s), want %s ok", i, s.ID, s.Status, want[i])
		}
	}
}
