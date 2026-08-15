package tests

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
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

func fixture(t *testing.T) (home, provider, log string) {
	t.Helper()
	home = t.TempDir()
	log = filepath.Join(home, "provider.log")
	provider = filepath.Join(home, "fake-provider")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "` + log + `"
case "$1" in
  describe)
    printf '%s\n' '{"protocol":"kg.provider/v1","protocol_versions":[1],"provider":{"id":"fake","version":"1.0.0","description":"Fake provider"},"source":{"local_code_path":"/tmp/fake-provider-source"},"capabilities":[{"capability_id":"test.echo","title":"Echo a value","description":"Return the supplied value without changing it.","side_effects":[],"input_schema":{"type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false},"output":{"mode":"result-json","kind":"json"},"cli_spec":{"subcommand":[],"always":[],"positionals":[],"flags":[]}}]}' ;;
  available) printf '%s\n' '{"available":true,"ready":[],"missing":[]}' ;;
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
