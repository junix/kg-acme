package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"kg-acme/internal/pipeline"
	"kg-acme/internal/policy"
	"kg-acme/internal/protocol"
	"kg-acme/internal/router"
	"kg-acme/internal/state"
	"kg-acme/internal/surface"
)

const Version = "0.2.0"

type options struct {
	JSON, All, DryRun, Describe bool
	Gates                       policy.Gates
	Params, Output              string
	Prefix                      string
	Level                       int
	LevelSet, Tree              bool
	ProviderBins                map[string]string
}

type Runner struct {
	Stdin                    io.Reader
	Stdout, Stderr           io.Writer
	SnapshotPath, RoutesPath string
}

func (r *Runner) defaults() {
	if r.Stdin == nil {
		r.Stdin = os.Stdin
	}
	if r.Stdout == nil {
		r.Stdout = os.Stdout
	}
	if r.Stderr == nil {
		r.Stderr = os.Stderr
	}
}

func (r Runner) Run(ctx context.Context, arguments []string) int {
	r.defaults()
	opts, args, err := parseGlobal(arguments)
	if err != nil {
		return r.fail(err, opts.JSON)
	}
	if len(opts.ProviderBins) > 0 || opts.All {
		return r.fail(fmt.Errorf("provider and inventory options belong to kgctl"), opts.JSON)
	}
	path, err := r.snapshotPath()
	if err != nil {
		return r.fail(err, opts.JSON)
	}
	if len(args) == 0 || (len(args) == 1 && isHelp(args[0])) {
		snapshot, loadErr := state.LoadSnapshot(path)
		if errors.Is(loadErr, os.ErrNotExist) {
			r.missingSnapshotHelp(path)
			return 0
		}
		if loadErr != nil {
			return r.fail(fmt.Errorf("invalid capability snapshot: %w; run kgctl refresh", loadErr), opts.JSON)
		}
		r.rootHelp(snapshot)
		return 0
	}
	if len(args) == 1 && (args[0] == "version" || args[0] == "--version") {
		fmt.Fprintln(r.Stdout, Version)
		return 0
	}
	snapshot, err := state.LoadSnapshot(path)
	if err != nil {
		return r.fail(fmt.Errorf("capability snapshot unavailable: %w; run kgctl refresh", err), opts.JSON)
	}
	if args[0] == "list" {
		return r.finish(r.list(snapshot, opts, args[1:]), opts.JSON)
	}
	if opts.Describe && (len(args) != 1 || opts.Params != "" || opts.Output != "" || opts.DryRun || opts.Gates != (policy.Gates{})) {
		return r.fail(fmt.Errorf("--describe cannot be combined with execution arguments or options"), opts.JSON)
	}
	capability, found := surface.Find(snapshot, args[0])
	if !found {
		if opts.Describe {
			return r.finish(r.describeGroup(snapshot, args[0]), opts.JSON)
		}
		return r.fail(fmt.Errorf("capability not found: %s; run kg list", args[0]), opts.JSON)
	}
	if opts.Describe {
		return r.finish(r.describeCapability(capability), opts.JSON)
	}
	if len(args) == 2 && isHelp(args[1]) {
		r.capabilityHelp(capability)
		return 0
	}
	return r.finish(r.invoke(ctx, snapshot, capability, opts, args[1:]), opts.JSON)
}

func (r Runner) invoke(ctx context.Context, snapshot surface.Snapshot, capability surface.Capability, opts options, args []string) error {
	values, err := argumentsObject(capability, opts, args)
	if err != nil {
		return err
	}
	publicID := surface.PublicID(capability)
	if publicID == "pipeline.run" || publicID == "pipeline.validate" {
		return r.invokePipeline(ctx, snapshot, publicID, values, opts)
	}
	routesPath, err := r.routesPath()
	if err != nil {
		return err
	}
	routes, err := state.LoadRoutes(routesPath)
	if err != nil {
		return err
	}
	candidate, err := surface.Select(capability, routes.Routes[capability.SemanticID], opts.DryRun)
	if err != nil {
		return err
	}
	snapshotProvider, ok := providerByID(snapshot.Providers, candidate.ProviderID)
	if !ok {
		return fmt.Errorf("snapshot provider missing: %s", candidate.ProviderID)
	}
	resolved, err := router.Resolve([]router.Provider{snapshotProvider}, candidate.ProviderCapability)
	if err != nil {
		return err
	}
	if err := rejectOutputCollision(values); err != nil {
		return err
	}
	if err := router.ValidateInvocation(resolved, values, opts.Gates, opts.DryRun); err != nil {
		return err
	}
	if opts.DryRun {
		envelope, err := router.Execute(ctx, resolved, values, opts.Gates, true, nil)
		if err != nil {
			return err
		}
		envelope.CapabilityID = publicID
		return r.outputEnvelope(envelope, opts.JSON)
	}
	live := router.RevalidateProvider(ctx, snapshotProvider)
	liveResolved, err := router.Resolve([]router.Provider{live}, candidate.ProviderCapability)
	if err != nil {
		return fmt.Errorf("provider contract drift; run kgctl refresh: %w", err)
	}
	envelope, err := router.Execute(ctx, liveResolved, values, opts.Gates, false, nil)
	if err != nil {
		return err
	}
	envelope.CapabilityID = publicID
	return r.outputEnvelope(envelope, opts.JSON)
}

func (r Runner) invokePipeline(ctx context.Context, snapshot surface.Snapshot, publicID string, values map[string]any, opts options) error {
	path, _ := values["definition"].(string)
	if path == "" {
		return fmt.Errorf("definition is required")
	}
	definition, err := pipeline.LoadDefinition(path)
	if err != nil {
		return err
	}
	routesPath, err := r.routesPath()
	if err != nil {
		return err
	}
	routes, err := state.LoadRoutes(routesPath)
	if err != nil {
		return err
	}
	plan, err := pipeline.BuildWithResolver(definition, pipelineResolver(snapshot, snapshot.Providers, routes.Routes, opts.DryRun), opts.Gates)
	if err != nil {
		return err
	}
	if publicID == "pipeline.validate" {
		if opts.JSON {
			return writeJSON(r.Stdout, pipeline.RenderDryRun(plan))
		}
		fmt.Fprintf(r.Stdout, "pipeline %q is valid (%d stages)\n", definition.Name, len(plan.Order))
		return nil
	}
	if opts.DryRun {
		return r.outputPipeline(pipeline.RenderDryRun(plan), opts.JSON)
	}
	if len(plan.Denied) > 0 {
		return fmt.Errorf("side effects denied by policy: %s", strings.Join(plan.Denied, ", "))
	}
	// A pipeline legitimately selects several capabilities. Revalidate only the
	// providers present in the fully validated plan, then rebuild the plan.
	liveByID := map[string]router.Provider{}
	for _, stage := range plan.Order {
		id := stage.Resolved.Provider.ID()
		if _, done := liveByID[id]; done {
			continue
		}
		provider, ok := providerByID(snapshot.Providers, id)
		if !ok {
			return fmt.Errorf("snapshot provider missing: %s", id)
		}
		liveByID[id] = router.RevalidateProvider(ctx, provider)
	}
	providers := append([]router.Provider(nil), snapshot.Providers...)
	for index := range providers {
		if live, ok := liveByID[providers[index].ID()]; ok {
			providers[index] = live
		}
	}
	plan, err = pipeline.BuildWithResolver(definition, pipelineResolver(snapshot, providers, routes.Routes, false), opts.Gates)
	if err != nil {
		return fmt.Errorf("provider contract drift; run kgctl refresh: %w", err)
	}
	envelope := pipeline.Execute(ctx, plan, pipeline.RunOptions{WorkDir: stringValue(values["work_dir"]), Resume: stringValue(values["resume"]), Gates: opts.Gates})
	return r.outputPipeline(envelope, opts.JSON)
}

func pipelineResolver(snapshot surface.Snapshot, providers []router.Provider, routes map[string]string, dryRun bool) func(string) (*router.Resolved, error) {
	return func(publicID string) (*router.Resolved, error) {
		capability, ok := surface.Find(snapshot, publicID)
		if !ok || strings.HasPrefix(publicID, "pipeline.") {
			return nil, fmt.Errorf("public capability not found: %s", publicID)
		}
		candidate, err := surface.Select(capability, routes[capability.SemanticID], dryRun)
		if err != nil {
			return nil, err
		}
		provider, ok := providerByID(providers, candidate.ProviderID)
		if !ok {
			return nil, fmt.Errorf("provider unavailable for %s", publicID)
		}
		return router.Resolve([]router.Provider{provider}, candidate.ProviderCapability)
	}
}

func argumentsObject(capability surface.Capability, opts options, args []string) (map[string]any, error) {
	if opts.Params != "" {
		if len(args) > 0 {
			return nil, fmt.Errorf("--params cannot be combined with positional capability arguments")
		}
		if opts.DryRun && strings.HasPrefix(strings.TrimSpace(opts.Params), "@") {
			return nil, fmt.Errorf("dry-run does not read files; pass --params as inline JSON")
		}
		return decodeParams(opts.Params)
	}
	values, err := router.ParseInput(capability.CLISpec, capability.InputSchema, args)
	if err != nil {
		return nil, err
	}
	if opts.Output != "" {
		values["output"] = opts.Output
	}
	return values, nil
}

func decodeParams(spec string) (map[string]any, error) {
	var data []byte
	var err error
	if strings.HasPrefix(spec, "@") {
		if len(spec) == 1 {
			return nil, fmt.Errorf("--params @FILE requires a file path")
		}
		data, err = os.ReadFile(strings.TrimPrefix(spec, "@"))
	} else {
		data = []byte(spec)
	}
	if err != nil {
		return nil, err
	}
	var result map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil {
		return nil, fmt.Errorf("invalid --params JSON: %w", err)
	}
	if result == nil {
		return nil, fmt.Errorf("--params must contain a JSON object")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("--params must contain exactly one JSON object")
	}
	return result, nil
}

func parseGlobal(arguments []string) (options, []string, error) {
	opts := options{ProviderBins: map[string]string{}}
	var rest []string
	for index := 0; index < len(arguments); index++ {
		arg := arguments[index]
		take := func() (string, error) {
			if index+1 >= len(arguments) {
				return "", fmt.Errorf("%s requires a value", arg)
			}
			index++
			return arguments[index], nil
		}
		switch arg {
		case "--json":
			opts.JSON = true
		case "--all":
			opts.All = true
		case "--dry-run":
			opts.DryRun = true
		case "--describe":
			opts.Describe = true
		case "--allow-network":
			opts.Gates.AllowNetwork = true
		case "--allow-data-egress":
			opts.Gates.AllowDataEgress = true
		case "--allow-model-download":
			opts.Gates.AllowModelDownload = true
		case "--allow-db-write":
			opts.Gates.AllowDBWrite = true
		case "--params":
			value, err := take()
			if err != nil {
				return opts, nil, err
			}
			opts.Params = value
		case "-o", "--output":
			value, err := take()
			if err != nil {
				return opts, nil, err
			}
			opts.Output = value
		case "--provider-bin":
			value, err := take()
			if err != nil {
				return opts, nil, err
			}
			id, path, ok := strings.Cut(value, "=")
			if !ok || id == "" || path == "" {
				return opts, nil, fmt.Errorf("--provider-bin expects ID=PATH")
			}
			opts.ProviderBins[id] = path
		default:
			rest = append(rest, arg)
		}
	}
	return opts, rest, nil
}

func providerByID(providers []router.Provider, id string) (router.Provider, bool) {
	for _, provider := range providers {
		if provider.ID() == id {
			return provider, true
		}
	}
	return router.Provider{}, false
}
func stringValue(value any) string { text, _ := value.(string); return text }
func isHelp(value string) bool     { return value == "--help" || value == "-h" || value == "help" }

func rejectOutputCollision(values map[string]any) error {
	output, _ := values["output"].(string)
	if output == "" {
		return nil
	}
	absOutput, _ := filepath.Abs(output)
	for _, key := range []string{"input", "file", "document_file", "sidecar", "definition"} {
		input, _ := values[key].(string)
		if input == "" {
			continue
		}
		absInput, _ := filepath.Abs(input)
		if absInput == absOutput {
			return fmt.Errorf("output must not overwrite input: %s", output)
		}
	}
	return nil
}

func (r Runner) outputEnvelope(envelope *protocol.Envelope, jsonMode bool) error {
	if jsonMode {
		return writeJSON(r.Stdout, envelope)
	}
	if envelope.Status == "error" && envelope.Error != nil {
		return fmt.Errorf("%s: %s", envelope.Error.Code, envelope.Error.Message)
	}
	data, _ := json.MarshalIndent(envelope.Result, "", "  ")
	fmt.Fprintln(r.Stdout, string(data))
	for _, artifact := range envelope.Artifacts {
		fmt.Fprintf(r.Stdout, "artifact: %s (%s)\n", artifact.Path, artifact.Kind)
	}
	return nil
}

func (r Runner) outputPipeline(envelope *pipeline.Envelope, jsonMode bool) error {
	if jsonMode {
		return writeJSON(r.Stdout, envelope)
	}
	if envelope.Status == "error" && envelope.Error != nil {
		return fmt.Errorf("%s: %s", envelope.Error.Code, envelope.Error.Message)
	}
	for _, stage := range envelope.Stages {
		fmt.Fprintf(r.Stdout, "%-16s %-28s %s\n", stage.ID, stage.Capability, stage.Status)
	}
	return nil
}

func writeJSON(out io.Writer, value any) error {
	encoder := json.NewEncoder(out)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}
func (r Runner) finish(err error, jsonMode bool) int {
	if err != nil {
		return r.fail(err, jsonMode)
	}
	return 0
}
func (r Runner) fail(err error, jsonMode bool) int {
	if jsonMode {
		_ = writeJSON(r.Stdout, map[string]any{"schema_version": "kg.error/v1", "ok": false, "error": map[string]any{"code": "error", "message": err.Error()}})
	} else {
		fmt.Fprintln(r.Stderr, "kg:", err)
	}
	return 1
}
func (r Runner) snapshotPath() (string, error) {
	if r.SnapshotPath != "" {
		return r.SnapshotPath, nil
	}
	return state.DefaultSnapshotPath()
}
func (r Runner) routesPath() (string, error) {
	if r.RoutesPath != "" {
		return r.RoutesPath, nil
	}
	return state.DefaultRoutesPath()
}
