package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"kg-acme/internal/cli"
	"kg-acme/internal/state"
	"kg-acme/internal/surface"
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}
type response struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id"`
	Result  any    `json:"result,omitempty"`
	Error   any    `json:"error,omitempty"`
}

func main() {
	path, err := state.DefaultSnapshotPath()
	if err != nil {
		fatal(err)
	}
	snapshot, err := state.LoadSnapshot(path)
	if err != nil {
		fatal(fmt.Errorf("snapshot unavailable; run kgctl refresh: %w", err))
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1<<20), 64<<20)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var req request
		if json.Unmarshal([]byte(line), &req) != nil {
			continue
		}
		if resp := dispatch(context.Background(), snapshot, path, req); resp != nil {
			_ = encoder.Encode(resp)
		}
	}
}

func dispatch(ctx context.Context, snapshot surface.Snapshot, path string, req request) *response {
	switch req.Method {
	case "initialize":
		return ok(req.ID, map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{"listChanged": false}}, "serverInfo": map[string]any{"name": "kg-mcp", "version": cli.Version}})
	case "notifications/initialized":
		return nil
	case "ping":
		return ok(req.ID, map[string]any{})
	case "tools/list":
		var tools []map[string]any
		for _, capability := range snapshot.Capabilities {
			if !capability.Available {
				continue
			}
			tools = append(tools, map[string]any{"name": toolName(capability), "title": capability.Title, "description": capability.Description, "inputSchema": expandedSchema(capability.InputSchema)})
		}
		return ok(req.ID, map[string]any{"tools": tools})
	case "tools/call":
		var params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if json.Unmarshal(req.Params, &params) != nil {
			return bad(req.ID, -32602, "invalid tool arguments")
		}
		for _, capability := range snapshot.Capabilities {
			if toolName(capability) == params.Name {
				return ok(req.ID, call(ctx, path, capability, params.Arguments))
			}
		}
		return bad(req.ID, -32602, "unknown tool: "+params.Name)
	default:
		return bad(req.ID, -32601, "method not found: "+req.Method)
	}
}

func toolName(capability surface.Capability) string {
	return "kg_" + strings.NewReplacer(".", "_", "-", "_").Replace(surface.PublicID(capability))
}
func expandedSchema(raw json.RawMessage) map[string]any {
	var schema map[string]any
	_ = json.Unmarshal(raw, &schema)
	if schema == nil {
		schema = map[string]any{"type": "object"}
	}
	properties, _ := schema["properties"].(map[string]any)
	if properties == nil {
		properties = map[string]any{}
		schema["properties"] = properties
	}
	properties["dry_run"] = map[string]any{"type": "boolean", "description": "Validate and plan without starting providers or models."}
	properties["allow_network"] = map[string]any{"type": "boolean"}
	properties["allow_data_egress"] = map[string]any{"type": "boolean"}
	properties["allow_model_download"] = map[string]any{"type": "boolean"}
	properties["allow_db_write"] = map[string]any{"type": "boolean"}
	return schema
}
func call(ctx context.Context, path string, capability surface.Capability, args map[string]any) map[string]any {
	var stdout, stderr bytes.Buffer
	arguments := []string{surface.PublicID(capability), "--json"}
	for _, pair := range []struct{ key, flag string }{{"dry_run", "--dry-run"}, {"allow_network", "--allow-network"}, {"allow_data_egress", "--allow-data-egress"}, {"allow_model_download", "--allow-model-download"}, {"allow_db_write", "--allow-db-write"}} {
		if enabled, _ := args[pair.key].(bool); enabled {
			arguments = append(arguments, pair.flag)
		}
		delete(args, pair.key)
	}
	data, _ := json.Marshal(args)
	arguments = append(arguments, "--params", string(data))
	runner := cli.Runner{Stdin: bytes.NewReader(nil), Stdout: &stdout, Stderr: &stderr, SnapshotPath: path}
	code := runner.Run(ctx, arguments)
	var structured any
	if json.Unmarshal(stdout.Bytes(), &structured) != nil {
		structured = map[string]any{"ok": false, "error": strings.TrimSpace(stderr.String())}
	}
	text, _ := json.MarshalIndent(structured, "", "  ")
	return map[string]any{"content": []map[string]any{{"type": "text", "text": string(text)}}, "structuredContent": structured, "isError": code != 0}
}
func ok(id, result any) *response { return &response{JSONRPC: "2.0", ID: id, Result: result} }
func bad(id any, code int, message string) *response {
	return &response{JSONRPC: "2.0", ID: id, Error: map[string]any{"code": code, "message": message}}
}
func fatal(err error) { fmt.Fprintln(os.Stderr, "kg-mcp:", err); os.Exit(4) }
