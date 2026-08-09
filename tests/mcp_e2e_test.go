// End-to-end tests for kg-mcp: a real server process spoken to over pipes
// — initialize handshake, tools/list same-source shape against the fake
// providers, tools/call through the fake protocol provider, policy gates
// from startup flags and KG_ACME_ALLOW, and malformed-frame tolerance.
package tests

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// mcpSession is a running kg-mcp process with framed stdio.
type mcpSession struct {
	cmd    *exec.Cmd
	stdin  *bufio.Writer
	scan   *bufio.Scanner
	nextID int
}

// startMCP launches kg-mcp with the fake provider layout. extraEnv entries
// override the base environment (KG_ACME_ALLOW tests).
func startMCP(t *testing.T, extraEnv []string, args ...string) *mcpSession {
	t.Helper()
	cmd := exec.Command(kgMCPBin, args...)
	env := []string{
		"PATH=" + binDir + ":/usr/bin:/bin:/usr/sbin:/sbin",
		"HOME=" + homeDir,
	}
	cmd.Env = append(env, extraEnv...)

	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = nil // inherit
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	scan := bufio.NewScanner(out)
	scan.Buffer(make([]byte, 0, 1<<20), 16<<20)
	s := &mcpSession{cmd: cmd, stdin: bufio.NewWriter(in), scan: scan, nextID: 0}
	t.Cleanup(func() {
		_ = in.Close()
		_ = cmd.Wait()
	})
	return s
}

// call sends one request frame and reads its response frame.
func (s *mcpSession) call(t *testing.T, method string, params any) map[string]any {
	t.Helper()
	s.nextID++
	frame := map[string]any{
		"jsonrpc": "2.0",
		"id":      s.nextID,
		"method":  method,
	}
	if params != nil {
		frame["params"] = params
	}
	data, err := json.Marshal(frame)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.stdin.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := s.stdin.WriteByte('\n'); err != nil {
		t.Fatal(err)
	}
	if err := s.stdin.Flush(); err != nil {
		t.Fatal(err)
	}
	if !s.scan.Scan() {
		t.Fatalf("kg-mcp closed stdout while answering %s: %v", method, s.scan.Err())
	}
	var resp map[string]any
	if err := json.Unmarshal(s.scan.Bytes(), &resp); err != nil {
		t.Fatalf("response frame is not JSON: %q: %v", s.scan.Text(), err)
	}
	if int(resp["id"].(float64)) != s.nextID {
		t.Fatalf("response id %v does not match request id %d", resp["id"], s.nextID)
	}
	return resp
}

func (s *mcpSession) sendRaw(t *testing.T, line string) map[string]any {
	t.Helper()
	if _, err := s.stdin.WriteString(line + "\n"); err != nil {
		t.Fatal(err)
	}
	if err := s.stdin.Flush(); err != nil {
		t.Fatal(err)
	}
	if !s.scan.Scan() {
		t.Fatalf("kg-mcp closed stdout: %v", s.scan.Err())
	}
	var resp map[string]any
	if err := json.Unmarshal(s.scan.Bytes(), &resp); err != nil {
		t.Fatalf("response frame is not JSON: %q: %v", s.scan.Text(), err)
	}
	return resp
}

func (s *mcpSession) initialize(t *testing.T) map[string]any {
	t.Helper()
	resp := s.call(t, "initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "e2e", "version": "0"},
	})
	// notifications/initialized — must not produce a frame; the next call's
	// id check would catch a stray frame.
	if _, err := s.stdin.WriteString(`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n"); err != nil {
		t.Fatal(err)
	}
	if err := s.stdin.Flush(); err != nil {
		t.Fatal(err)
	}
	return resp
}

func toolCallResult(t *testing.T, resp map[string]any) map[string]any {
	t.Helper()
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("tools/call response has no result: %v", resp)
	}
	return result
}

func TestMCPHandshakeAndToolsList(t *testing.T) {
	s := startMCP(t, nil)
	resp := s.initialize(t)
	result := resp["result"].(map[string]any)
	if result["protocolVersion"] != "2025-06-18" {
		t.Errorf("protocolVersion: %v", result)
	}
	info := result["serverInfo"].(map[string]any)
	if info["name"] != "kg-mcp" {
		t.Errorf("serverInfo: %v", info)
	}

	resp = s.call(t, "tools/list", map[string]any{})
	tools := resp["result"].(map[string]any)["tools"].([]any)
	byName := map[string]map[string]any{}
	for _, raw := range tools {
		tool := raw.(map[string]any)
		byName[tool["name"].(string)] = tool
	}
	// Same source as the catalog: every stable command is a tool.
	for _, want := range []string{
		"kg_extract", "kg_dedup", "kg_communities", "kg_communities_hierarchy",
		"kg_communities_summaries", "kg_store", "kg_ask", "kg_parse",
		"kg_provider", "kg_pipeline_run", "kg_pipeline_validate",
	} {
		if _, ok := byName[want]; !ok {
			t.Errorf("missing tool %q (have %d tools)", want, len(byName))
		}
	}
	// The probed kg-provider-fake owns kg_extract's inputSchema: its
	// published property "file" must appear (plus hub extras).
	props := byName["kg_extract"]["inputSchema"].(map[string]any)["properties"].(map[string]any)
	if _, ok := props["file"]; !ok {
		t.Errorf("kg_extract schema must come from the probed provider: %v", props)
	}
	if _, ok := props["dry_run"]; !ok {
		t.Errorf("hub extra dry_run missing: %v", props)
	}
}

func TestMCPCallExtractViaFakeProvider(t *testing.T) {
	s := startMCP(t, nil, "--allow-network")
	s.initialize(t)

	resp := s.call(t, "tools/call", map[string]any{
		"name":      "kg_extract",
		"arguments": map[string]any{"file": "doc.md"},
	})
	result := toolCallResult(t, resp)
	if result["isError"] != false {
		t.Fatalf("extract call failed: %v", result)
	}
	sc := result["structuredContent"].(map[string]any)
	if sc["protocol"] != "kg.execution/v1" || sc["status"] != "ok" || sc["provider"] != "kg-provider-fake" {
		t.Fatalf("structured content must be the execution envelope: %v", sc)
	}
	echo := sc["result"].(map[string]any)["echo"].(map[string]any)
	input := echo["input"].(map[string]any)
	if input["file"] != "doc.md" {
		t.Errorf("provider received input %v", input)
	}
}

func TestMCPPolicyDeniedWithoutStartupFlags(t *testing.T) {
	s := startMCP(t, nil) // no --allow-*, no env
	s.initialize(t)
	resp := s.call(t, "tools/call", map[string]any{
		"name":      "kg_extract",
		"arguments": map[string]any{"file": "doc.md"},
	})
	result := toolCallResult(t, resp)
	if result["isError"] != true {
		t.Fatalf("expected isError:true, got %v", result)
	}
	sc := result["structuredContent"].(map[string]any)
	if sc["error"].(map[string]any)["code"] != "policy_denied" {
		t.Errorf("expected policy_denied, got %v", sc["error"])
	}
	// The server survives: a following ping is answered normally.
	ping := s.call(t, "ping", map[string]any{})
	if ping["result"] == nil {
		t.Errorf("server must stay alive after policy_denied: %v", ping)
	}
}

func TestMCPAllowFromEnv(t *testing.T) {
	s := startMCP(t, []string{"KG_ACME_ALLOW=network"})
	s.initialize(t)
	resp := s.call(t, "tools/call", map[string]any{
		"name":      "kg_extract",
		"arguments": map[string]any{"file": "doc.md"},
	})
	result := toolCallResult(t, resp)
	if result["isError"] != false {
		t.Fatalf("KG_ACME_ALLOW=network must open the network gate: %v", result)
	}
}

func TestMCPInvalidJSONTolerated(t *testing.T) {
	s := startMCP(t, nil)
	s.initialize(t)
	resp := s.sendRaw(t, `{"jsonrpc":"2.0","id": oops,`)
	errObj := resp["error"].(map[string]any)
	if errObj["code"].(float64) != -32700 {
		t.Errorf("expected -32700, got %v", resp)
	}
	// And the connection still works afterwards.
	ping := s.call(t, "ping", map[string]any{})
	if ping["result"] == nil {
		t.Errorf("server must survive malformed frames: %v", ping)
	}
}

func TestMCPPipelineRunEndToEnd(t *testing.T) {
	workDir := t.TempDir() + "/work"
	sidecar := t.TempDir() + "/sidecar.json"
	if err := writeFile(sidecar, `{"docs":[]}`); err != nil {
		t.Fatal(err)
	}
	def := fmt.Sprintf(`{
	  "pipeline": "kg.pipeline/v1",
	  "name": "mcp-chain",
	  "stages": [
	    {"id": "parse", "capability": "parse.multimodal",
	     "input": {"sidecar": %q}},
	    {"id": "extract", "capability": "extract.entities_relations",
	     "input": {"file": "doc.md"}},
	    {"id": "store", "capability": "store.triples",
	     "input_from": {"stage": "extract", "artifact_kind": "kg-document", "as": "document_file"}}
	  ]
	}`, sidecar)
	var defObj map[string]any
	if err := json.Unmarshal([]byte(def), &defObj); err != nil {
		t.Fatal(err)
	}

	s := startMCP(t, nil, "--provider-bin=placeholder=/bin/true",
		"--allow-network", "--allow-data-egress", "--allow-db-write")
	s.initialize(t)
	resp := s.call(t, "tools/call", map[string]any{
		"name": "kg_pipeline_run",
		"arguments": map[string]any{
			"definition": defObj,
			"work_dir":   workDir,
			"provider":   "kg-provider-pipe",
		},
	})
	result := toolCallResult(t, resp)
	if result["isError"] != false {
		t.Fatalf("pipeline run failed: %v", result)
	}
	sc := result["structuredContent"].(map[string]any)
	if sc["protocol"] != "kg.pipeline.execution/v1" || sc["status"] != "ok" || sc["pipeline"] != "mcp-chain" {
		t.Fatalf("pipeline envelope: %v", sc)
	}
	stages := sc["stages"].([]any)
	if len(stages) != 3 {
		t.Fatalf("expected 3 stages, got %v", stages)
	}
	for i, want := range []string{"parse", "extract", "store"} {
		st := stages[i].(map[string]any)
		if st["id"] != want || st["status"] != "ok" {
			t.Errorf("stage %d: %v", i, st)
		}
	}
	// Large artifacts are never inlined: extract carries path+checksum.
	extract := stages[1].(map[string]any)
	arts := extract["artifacts"].([]any)
	if len(arts) != 1 {
		t.Fatalf("extract artifacts: %v", extract)
	}
	art := arts[0].(map[string]any)
	if art["path"] == nil || !strings.HasPrefix(art["checksum"].(string), "sha256:") {
		t.Errorf("artifact must be path+checksum, got %v", art)
	}
	if _, err := os.Stat(art["path"].(string)); err != nil {
		t.Errorf("artifact file missing: %v", err)
	}
}

func writeFile(path, body string) error {
	return os.WriteFile(path, []byte(body), 0o644)
}
