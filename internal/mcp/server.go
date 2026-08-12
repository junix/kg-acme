// Package mcp exposes the kg capability hub as an MCP server over stdio
// (newline-delimited JSON-RPC 2.0).
//
// Same-source rule (spec/06): tools are derived from the hub catalog at
// runtime, and a capability tool's inputSchema is the provider's published
// input_schema verbatim (probed provider first, hub fallback table
// otherwise) — the hub never authors a second copy of provider options.
// The only properties the server adds are hub-owned extras (dry_run,
// provider), the exact counterparts of the CLI's hub flags, stripped
// before provider input validation.
//
// Execution goes through the same router.Execute / pipeline.Build /
// pipeline.Execute APIs as the CLI; the resulting kg.execution/v1 /
// kg.pipeline.execution/v1 envelopes become the tool's structuredContent
// untouched (artifacts stay path+checksum, never inlined).
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"

	"kg-acme/internal/catalog"
	"kg-acme/internal/pipeline"
	"kg-acme/internal/policy"
	"kg-acme/internal/protocol"
	"kg-acme/internal/router"
)

// ProtocolVersion is the newest MCP protocol revision this server speaks.
const ProtocolVersion = "2025-06-18"

// supportedProtocolVersions are echoed back in initialize; anything else
// gets ProtocolVersion instead (per the MCP version-negotiation rule).
var supportedProtocolVersions = map[string]bool{
	"2024-11-05": true,
	"2025-03-26": true,
	"2025-06-18": true,
}

// JSON-RPC 2.0 error codes.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

// Config wires the server to the hub.
type Config struct {
	// Version is reported as serverInfo.version in initialize.
	Version string
	// Gates are the operator's allowances, supplied by server startup
	// configuration (flags / KG_ACME_ALLOW) — the MCP form has no
	// per-call --allow-* flags.
	Gates policy.Gates
	// Providers assembles the routable provider set (same discovery as the
	// CLI: router.DiscoverProviders). Called once, lazily, and cached for
	// the server's lifetime.
	Providers func(ctx context.Context) []router.Provider
	// Runner is the subprocess seam (nil → real exec).
	Runner router.Runner
}

// Server is a single-connection stdio MCP server.
type Server struct {
	cfg Config

	once      sync.Once
	providers []router.Provider

	w  io.Writer
	mu sync.Mutex // serializes frame writes
}

// New builds a server from its hub wiring.
func New(cfg Config) *Server {
	return &Server{cfg: cfg}
}

func (s *Server) providersSet(ctx context.Context) []router.Provider {
	s.once.Do(func() { s.providers = s.cfg.Providers(ctx) })
	return s.providers
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	ID      json.RawMessage `json:"id"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// Serve reads newline-delimited JSON-RPC frames from r and writes response
// frames to w until EOF. Malformed frames are answered with a -32700 error
// and the server keeps running.
func (s *Server) Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	s.w = w
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		s.handleLine(ctx, line)
	}
	return sc.Err()
}

func (s *Server) handleLine(ctx context.Context, line []byte) {
	var req rpcRequest
	if err := json.Unmarshal(line, &req); err != nil {
		s.write(rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"),
			Error: &rpcError{Code: codeParseError, Message: "parse error: " + err.Error()}})
		return
	}
	if req.Method == "" {
		if req.ID != nil {
			s.write(rpcResponse{JSONRPC: "2.0", ID: req.ID,
				Error: &rpcError{Code: codeInvalidRequest, Message: "request has no method"}})
		}
		return
	}
	// Notifications (no id) never get a response. MCP notifications
	// (notifications/initialized, cancelled, progress, ...) need no
	// handling here — tolerate them all.
	if req.ID == nil {
		return
	}

	switch req.Method {
	case "initialize":
		s.onInitialize(req)
	case "ping":
		s.write(rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}})
	case "tools/list":
		s.onToolsList(ctx, req)
	case "tools/call":
		s.onToolsCall(ctx, req)
	default:
		s.write(rpcResponse{JSONRPC: "2.0", ID: req.ID,
			Error: &rpcError{Code: codeMethodNotFound, Message: "method not found: " + req.Method}})
	}
}

func (s *Server) write(resp rpcResponse) {
	data, err := json.Marshal(resp)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.w.(io.Writer).Write(append(data, '\n'))
}

func (s *Server) onInitialize(req rpcRequest) {
	var params struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	_ = json.Unmarshal(req.Params, &params)
	pv := params.ProtocolVersion
	if !supportedProtocolVersions[pv] {
		pv = ProtocolVersion
	}
	s.write(rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
		"protocolVersion": pv,
		"capabilities": map[string]any{
			"tools": map[string]any{"listChanged": false},
		},
		"serverInfo": map[string]any{
			"name":    "kg-mcp",
			"version": s.cfg.Version,
		},
	}})
}

// Tool is one entry of tools/list.
type Tool struct {
	Name        string         `json:"name"`
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// ToolName maps a catalog semantic_id to a stable MCP tool name
// ("analyze shortest-paths" → kg_analyze_shortest_paths).
func ToolName(semanticID string) string {
	return "kg_" + strings.NewReplacer(" ", "_", "-", "_").Replace(semanticID)
}

func (s *Server) onToolsList(ctx context.Context, req rpcRequest) {
	cat, err := catalog.Load()
	if err != nil {
		s.write(rpcResponse{JSONRPC: "2.0", ID: req.ID,
			Error: &rpcError{Code: codeInvalidRequest, Message: "catalog: " + err.Error()}})
		return
	}
	providers := s.providersSet(ctx)
	tools := make([]Tool, 0, len(cat.Commands))
	for _, cmd := range cat.Commands {
		t := Tool{Name: ToolName(cmd.SemanticID), Title: cmd.Title, Description: cmd.Description}
		switch {
		case cmd.Builtin && cmd.SemanticID == "provider":
			t.InputSchema = providerToolSchema()
		case cmd.Builtin && cmd.SemanticID == "pipeline run":
			t.InputSchema = pipelineRunToolSchema()
		case cmd.Builtin && cmd.SemanticID == "pipeline validate":
			t.InputSchema = pipelineValidateToolSchema()
		case cmd.Builtin:
			t.InputSchema = map[string]any{"type": "object"}
		default:
			t.InputSchema = capabilitySchema(providers, cmd.CapabilityID)
		}
		tools = append(tools, t)
	}
	s.write(rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"tools": tools}})
}

// capabilitySchema is the provider-published input_schema for one catalog
// capability (probed provider first, hub fallback table otherwise), plus
// hub-owned extras. When no provider currently offers the capability the
// hub cannot know the parameters, so the schema degrades to a plain object
// — calling the tool then fails at routing with capability_not_found,
// exactly as the CLI would.
func capabilitySchema(providers []router.Provider, capabilityID string) map[string]any {
	schema := map[string]any{"type": "object"}
	if res, err := router.Resolve(providers, capabilityID); err == nil && len(res.InputSchema) > 0 {
		var m map[string]any
		if err := json.Unmarshal(res.InputSchema, &m); err == nil && m != nil {
			schema = m
		}
	}
	props, _ := schema["properties"].(map[string]any)
	if props == nil {
		props = map[string]any{}
		schema["properties"] = props
	}
	props["dry_run"] = map[string]any{
		"type": "boolean",
		"description": "Hub flag: render the execution plan with zero side effects instead of invoking the provider.",
	}
	props["provider"] = map[string]any{
		"type": "string",
		"description": "Hub flag: force a specific provider id.",
	}
	return schema
}

// The builtin tools are hub-owned (not provider parameters), so the hub
// authors their schemas directly.
func providerToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"provider_id":   map[string]any{"type": "string", "description": "Provider id to invoke."},
			"capability_id": map[string]any{"type": "string", "description": "Capability in the provider-published namespace (extract.*, resolve.*, ...)."},
			"request":       map[string]any{"type": "object", "description": "Input object sent to the provider as-is (raw protocol escape hatch)."},
			"dry_run":       map[string]any{"type": "boolean", "description": "Render the execution plan with zero side effects."},
		},
		"required":             []string{"provider_id", "capability_id", "request"},
		"additionalProperties": false,
	}
}

func pipelineDefinitionProps() map[string]any {
	return map[string]any{
		"definition":      map[string]any{"type": "object", "description": "Inline kg.pipeline/v1 pipeline definition (alternative to definition_path)."},
		"definition_path": map[string]any{"type": "string", "description": "Path to a kg.pipeline/v1 definition file (alternative to definition)."},
		"provider":        map[string]any{"type": "string", "description": "Force a specific provider id for all stages."},
	}
}

func pipelineRunToolSchema() map[string]any {
	props := pipelineDefinitionProps()
	props["work_dir"] = map[string]any{"type": "string", "description": "Pipeline work directory (default kg-pipeline-<timestamp>/ under the server cwd)."}
	props["resume"] = map[string]any{"type": "string", "description": "Resume from an existing work directory, skipping completed stages."}
	props["dry_run"] = map[string]any{"type": "boolean", "description": "Render the full plan with zero execution."}
	return map[string]any{"type": "object", "properties": props, "additionalProperties": false}
}

func pipelineValidateToolSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           pipelineDefinitionProps(),
		"additionalProperties": false,
	}
}

func (s *Server) onToolsCall(ctx context.Context, req rpcRequest) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil || params.Name == "" {
		s.write(rpcResponse{JSONRPC: "2.0", ID: req.ID,
			Error: &rpcError{Code: codeInvalidParams, Message: "tools/call params must carry a name"}})
		return
	}
	args := map[string]any{}
	if len(params.Arguments) > 0 {
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			s.write(rpcResponse{JSONRPC: "2.0", ID: req.ID,
				Error: &rpcError{Code: codeInvalidParams, Message: "arguments must be a JSON object"}})
			return
		}
	}

	cat, err := catalog.Load()
	if err != nil {
		s.write(rpcResponse{JSONRPC: "2.0", ID: req.ID,
			Error: &rpcError{Code: codeInvalidRequest, Message: "catalog: " + err.Error()}})
		return
	}
	for _, cmd := range cat.Commands {
		if ToolName(cmd.SemanticID) != params.Name {
			continue
		}
		var result map[string]any
		var callErr *rpcError
		switch {
		case cmd.Builtin && cmd.SemanticID == "provider":
			result, callErr = s.callProviderTool(ctx, args)
		case cmd.Builtin && cmd.SemanticID == "pipeline run":
			result, callErr = s.callPipelineTool(ctx, args, true)
		case cmd.Builtin && cmd.SemanticID == "pipeline validate":
			result, callErr = s.callPipelineTool(ctx, args, false)
		case cmd.Builtin:
			callErr = &rpcError{Code: codeInvalidParams, Message: "unknown tool: " + params.Name}
		default:
			result, callErr = s.callCapabilityTool(ctx, cmd, args)
		}
		if callErr != nil {
			s.write(rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: callErr})
			return
		}
		s.write(rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result})
		return
	}
	s.write(rpcResponse{JSONRPC: "2.0", ID: req.ID,
		Error: &rpcError{Code: codeInvalidParams, Message: "unknown tool: " + params.Name}})
}

// popHubArgs extracts the hub-owned extras from tool arguments (the CLI's
// hub flags in MCP form). The remaining arguments are the provider input
// verbatim.
func popHubArgs(args map[string]any) (dryRun bool, providerID string, err error) {
	if v, ok := args["dry_run"]; ok {
		b, isBool := v.(bool)
		if !isBool {
			return false, "", fmt.Errorf("dry_run must be a boolean")
		}
		dryRun = b
		delete(args, "dry_run")
	}
	if v, ok := args["provider"]; ok {
		s, isStr := v.(string)
		if !isStr {
			return false, "", fmt.Errorf("provider must be a string")
		}
		providerID = s
		delete(args, "provider")
	}
	return dryRun, providerID, nil
}

// filterProviders narrows the provider set to one id; the bool reports
// whether anything matched (provider_not_found otherwise).
func filterProviders(providers []router.Provider, id string) ([]router.Provider, bool) {
	if id == "" {
		return providers, true
	}
	var out []router.Provider
	for _, p := range providers {
		if p.ID() == id {
			out = append(out, p)
		}
	}
	return out, len(out) > 0
}

func (s *Server) callCapabilityTool(ctx context.Context, cmd catalog.Command, args map[string]any) (map[string]any, *rpcError) {
	dryRun, providerID, err := popHubArgs(args)
	if err != nil {
		return nil, &rpcError{Code: codeInvalidParams, Message: err.Error()}
	}
	providers, ok := filterProviders(s.providersSet(ctx), providerID)
	if !ok {
		return envelopeResult(protocol.ErrorEnvelope(cmd.CapabilityID, providerID,
			protocol.ErrProviderNotFound, fmt.Sprintf("provider %q not found", providerID))), nil
	}
	res, err := router.Resolve(providers, cmd.CapabilityID)
	if err != nil {
		return envelopeResult(protocol.ErrorEnvelope(cmd.CapabilityID, providerID,
			protocol.ErrCapabilityNotFound, err.Error())), nil
	}
	env, err := router.Execute(ctx, res, args, s.cfg.Gates, dryRun, s.cfg.Runner)
	if err != nil {
		return envelopeResult(protocol.ErrorEnvelope(cmd.CapabilityID, res.Provider.ID(),
			protocol.ErrInvocationFailed, err.Error())), nil
	}
	return envelopeResult(env), nil
}

// callProviderTool is the MCP form of `kg provider <id> <capability_id>
// --request <file|->`: a raw escape hatch for capabilities the catalog does
// not map yet.
func (s *Server) callProviderTool(ctx context.Context, args map[string]any) (map[string]any, *rpcError) {
	dryRun, _, err := popHubArgs(args)
	if err != nil {
		return nil, &rpcError{Code: codeInvalidParams, Message: err.Error()}
	}
	providerID, _ := args["provider_id"].(string)
	capabilityID, _ := args["capability_id"].(string)
	request, isObj := args["request"].(map[string]any)
	if providerID == "" || capabilityID == "" || !isObj {
		return nil, &rpcError{Code: codeInvalidParams,
			Message: "kg_provider requires provider_id (string), capability_id (string), request (object)"}
	}

	providers, ok := filterProviders(s.providersSet(ctx), providerID)
	if !ok {
		return envelopeResult(protocol.ErrorEnvelope(capabilityID, providerID,
			protocol.ErrProviderNotFound, fmt.Sprintf("provider %q not found", providerID))), nil
	}
	res, err := router.Resolve(providers, capabilityID)
	if err != nil {
		return envelopeResult(protocol.ErrorEnvelope(capabilityID, providerID,
			protocol.ErrCapabilityNotFound, err.Error())), nil
	}
	env, err := router.Execute(ctx, res, request, s.cfg.Gates, dryRun, s.cfg.Runner)
	if err != nil {
		return envelopeResult(protocol.ErrorEnvelope(capabilityID, providerID,
			protocol.ErrInvocationFailed, err.Error())), nil
	}
	return envelopeResult(env), nil
}

// callPipelineTool implements kg_pipeline_run / kg_pipeline_validate on top
// of the same pipeline.Build / pipeline.Execute the CLI uses.
func (s *Server) callPipelineTool(ctx context.Context, args map[string]any, run bool) (map[string]any, *rpcError) {
	def, errResp := s.loadPipelineDef(args)
	if errResp != nil {
		return nil, errResp
	}

	workDir, _ := args["work_dir"].(string)
	resume, _ := args["resume"].(string)
	providerID, _ := args["provider"].(string)
	dryRun, _ := args["dry_run"].(bool)

	providers, ok := filterProviders(s.providersSet(ctx), providerID)
	if !ok {
		return pipelineResult(pipeline.ErrorEnvelope(def.Name,
			protocol.ErrProviderNotFound, fmt.Sprintf("provider %q not found", providerID))), nil
	}
	plan, err := pipeline.Build(def, providers, s.cfg.Gates)
	if err != nil {
		code, msg := protocol.ErrInvalidPipeline, err.Error()
		if pe, isPE := err.(*pipeline.Error); isPE {
			code, msg = pe.Code, pe.Message
		}
		return pipelineResult(pipeline.ErrorEnvelope(def.Name, code, msg)), nil
	}
	if !run || dryRun {
		// validate (and run --dry-run) emit the rendered plan, exactly like
		// the CLI's --json form.
		return structuredResult(pipeline.RenderDryRun(plan), false), nil
	}
	return pipelineResult(pipeline.Execute(ctx, plan, pipeline.RunOptions{
		WorkDir: workDir,
		Resume:  resume,
		Gates:   s.cfg.Gates,
		Runner:  s.cfg.Runner,
	})), nil
}

// loadPipelineDef accepts exactly one of definition (inline object) or
// definition_path.
func (s *Server) loadPipelineDef(args map[string]any) (*pipeline.Definition, *rpcError) {
	inline, hasInline := args["definition"]
	path, hasPath := args["definition_path"].(string)
	if hasInline == hasPath {
		return nil, &rpcError{Code: codeInvalidParams,
			Message: "exactly one of definition (object) or definition_path (string) is required"}
	}
	if hasPath {
		def, err := pipeline.LoadDefinition(path)
		if err != nil {
			return nil, &rpcError{Code: codeInvalidParams, Message: err.Error()}
		}
		return def, nil
	}
	data, err := json.Marshal(inline)
	if err != nil {
		return nil, &rpcError{Code: codeInvalidParams, Message: "definition must be a JSON object"}
	}
	def, err := pipeline.ParseDefinition(data)
	if err != nil {
		return nil, &rpcError{Code: codeInvalidParams, Message: err.Error()}
	}
	return def, nil
}

// envelopeResult maps a kg.execution/v1 envelope to a CallToolResult:
// the envelope is the structured content, untouched; status "error"
// (policy_denied, invalid_input, ...) maps to isError:true and never
// crashes the server.
func envelopeResult(env *protocol.Envelope) map[string]any {
	return structuredResult(env, env.Status == "error")
}

// pipelineResult does the same for kg.pipeline.execution/v1 envelopes.
func pipelineResult(env *pipeline.Envelope) map[string]any {
	return structuredResult(env, env.Status == "error")
}

func structuredResult(v any, isError bool) map[string]any {
	data, err := json.Marshal(v)
	if err != nil {
		return map[string]any{
			"content": []map[string]any{{"type": "text", "text": "internal error: " + err.Error()}},
			"isError": true,
		}
	}
	var obj map[string]any
	_ = json.Unmarshal(data, &obj)
	pretty, _ := json.MarshalIndent(v, "", "  ")
	return map[string]any{
		"content":           []map[string]any{{"type": "text", "text": string(pretty)}},
		"structuredContent": obj,
		"isError":           isError,
	}
}
