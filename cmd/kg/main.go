// Command kg is the capability hub CLI for the KG toolchain.
//
// The hub integrates providers — it never implements KG algorithms itself
// and never hardcodes provider options: probed providers self-describe via
// `describe --json`, and the hub renders argv/validation/presentation from
// that description. Built-in bridge tables are fallback only.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"kg-acme/internal/bridge"
	"kg-acme/internal/catalog"
	"kg-acme/internal/discover"
	"kg-acme/internal/policy"
	"kg-acme/internal/protocol"
	"kg-acme/internal/router"
)

// hubFlags are the flags the hub itself owns; everything else belongs to
// the provider capability being invoked.
type hubFlags struct {
	json           bool
	dryRun         bool
	gates          policy.Gates
	provider       string
	providerBin    discover.Overrides
	request        string
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		usage()
		return 2
	}
	cat, err := catalog.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "kg: %v\n", err)
		return 2
	}

	switch args[0] {
	case "list":
		hf, rest, err := parseHubFlags(args[1:])
		if err != nil || len(rest) > 0 {
			fmt.Fprintf(os.Stderr, "kg list: %v\n", firstErr(err, "unexpected arguments"))
			return 2
		}
		return cmdList(hf)
	case "describe":
		hf, rest, err := parseHubFlags(args[1:])
		if err != nil || len(rest) != 1 {
			fmt.Fprintf(os.Stderr, "usage: kg describe <provider> [--json]\n")
			return 2
		}
		return cmdDescribe(hf, rest[0])
	case "-h", "--help", "help":
		usage()
		return 0
	}

	cmd, consumed := cat.FindPath(args)
	if cmd == nil {
		fmt.Fprintf(os.Stderr, "kg: unknown command %q\n\n", args[0])
		usage()
		return 2
	}

	hf, rest, err := parseHubFlags(args[consumed:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "kg %s: %v\n", cmd.Path(), err)
		return 2
	}

	switch {
	case cmd.Builtin && cmd.SemanticID == "provider":
		return cmdProvider(hf, rest)
	case cmd.Builtin:
		// pipeline (Phase 2): stub.
		fmt.Fprintf(os.Stderr, "kg %s: not implemented yet (Phase 2 pipeline runner)\n", cmd.SemanticID)
		return 2
	default:
		return cmdCapability(hf, *cmd, rest)
	}
}

func usage() {
	cat, err := catalog.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "kg: %v\n", err)
		return
	}
	fmt.Fprintln(os.Stderr, `kg — capability hub for the KG toolchain

Usage:
  kg list [--json]                       list discovered providers and capabilities
  kg describe <provider> [--json]        show a provider's self-description
  kg <command> [args] [hub flags]        run a capability (extract/dedup/communities/store/ask/parse)
  kg provider <id> <capability_id> --request <file|->  raw protocol escape hatch

Hub flags:
  --json                  emit exactly one kg.execution/v1 envelope on stdout
  --dry-run               render the execution plan with zero side effects
  --allow-network         allow the network side effect
  --allow-data-egress     allow data egress side effect
  --allow-model-download  allow model downloads
  --allow-db-write        allow database writes
  --provider <id>         force a specific provider
  --provider-bin ID=PATH  explicit provider executable (repeatable)

Commands:`)
	for _, c := range cat.Commands {
		marker := ""
		if c.Stub {
			marker = " (stub)"
		}
		fmt.Fprintf(os.Stderr, "  %-12s %s%s\n", c.Path(), c.Description, marker)
	}
}

// parseHubFlags extracts hub-owned flags, leaving provider args untouched.
func parseHubFlags(args []string) (hubFlags, []string, error) {
	hf := hubFlags{providerBin: discover.Overrides{}}
	var rest []string
	for i := 0; i < len(args); i++ {
		tok := args[i]
		next := func() (string, error) {
			if i+1 >= len(args) {
				return "", fmt.Errorf("flag %s expects a value", tok)
			}
			i++
			return args[i], nil
		}
		switch {
		case tok == "--json":
			hf.json = true
		case tok == "--dry-run":
			hf.dryRun = true
		case tok == "--allow-network":
			hf.gates.AllowNetwork = true
		case tok == "--allow-data-egress":
			hf.gates.AllowDataEgress = true
		case tok == "--allow-model-download":
			hf.gates.AllowModelDownload = true
		case tok == "--allow-db-write":
			hf.gates.AllowDBWrite = true
		case tok == "--provider":
			v, err := next()
			if err != nil {
				return hf, nil, err
			}
			hf.provider = v
		case tok == "--request":
			v, err := next()
			if err != nil {
				return hf, nil, err
			}
			hf.request = v
		case strings.HasPrefix(tok, "--provider-bin="):
			kv := strings.TrimPrefix(tok, "--provider-bin=")
			id, path, ok := strings.Cut(kv, "=")
			if !ok || id == "" || path == "" {
				return hf, nil, fmt.Errorf("--provider-bin expects ID=PATH, got %q", kv)
			}
			hf.providerBin[id] = path
		case tok == "--provider-bin":
			v, err := next()
			if err != nil {
				return hf, nil, err
			}
			id, path, ok := strings.Cut(v, "=")
			if !ok || id == "" || path == "" {
				return hf, nil, fmt.Errorf("--provider-bin expects ID=PATH, got %q", v)
			}
			hf.providerBin[id] = path
		default:
			rest = append(rest, tok)
		}
	}
	return hf, rest, nil
}

// buildProviders assembles the routable provider set: legacy bridges found
// on disk plus protocol-native kg-provider-* binaries.
func buildProviders(ctx context.Context, hf hubFlags) []router.Provider {
	env := discover.DefaultEnv()
	var out []router.Provider
	seen := map[string]bool{}

	for _, fb := range bridge.Table() {
		path := discover.FindExecutable(fb.Bin, hf.providerBin, env)
		if path == "" {
			continue
		}
		fbCopy := fb
		st := discover.Probe(ctx, fb.ID, path)
		out = append(out, router.Provider{Status: st, Fallback: &fbCopy})
		seen[st.ID] = true
	}
	for name, path := range discover.ScanProviders(env) {
		if seen[name] {
			continue
		}
		st := discover.Probe(ctx, name, path)
		out = append(out, router.Provider{Status: st})
		seen[st.ID] = true
	}
	// Explicit overrides for providers unknown to the hub.
	for id, path := range hf.providerBin {
		if seen[id] || !discover.IsExecutable(path) {
			continue
		}
		st := discover.Probe(ctx, id, path)
		out = append(out, router.Provider{Status: st})
		seen[st.ID] = true
	}
	return out
}

func cmdList(hf hubFlags) int {
	ctx := context.Background()
	providers := buildProviders(ctx, hf)
	if hf.json {
		type entry struct {
			discover.ProviderStatus
			Fallback bool `json:"fallback"`
		}
		var list []entry
		for _, p := range providers {
			list = append(list, entry{p.Status, p.Fallback != nil})
		}
		writeJSON(list)
		return 0
	}
	if len(providers) == 0 {
		fmt.Println("no providers found (looked in --provider-bin, ~/sync/<os>-<arch>-bin, ~/sync/bin, PATH)")
		return 0
	}
	for _, p := range providers {
		st := p.Status
		probed := "unprobed"
		if st.Probed {
			probed = fmt.Sprintf("probed v%d", st.Version)
		}
		avail := "unknown"
		if st.Available != nil {
			avail = fmt.Sprintf("%t", st.Available.Available)
		}
		kind := "protocol"
		if p.Fallback != nil {
			kind = "bridge"
		}
		fmt.Printf("%-14s %-8s %-10s available=%-7s %s\n", st.ID, kind, probed, avail, st.Path)
		caps := capabilitySummaries(p)
		for _, c := range caps {
			fmt.Printf("  %-14s %s\n", "", c)
		}
	}
	return 0
}

func capabilitySummaries(p router.Provider) []string {
	var out []string
	if p.Status.Manifest != nil {
		for _, c := range p.Status.Manifest.Capabilities {
			out = append(out, fmt.Sprintf("%s [%s] %s", c.CapabilityID, strings.Join(c.SideEffects, ","), c.Title))
		}
		return out
	}
	if p.Fallback != nil {
		for _, c := range p.Fallback.Capabilities {
			out = append(out, fmt.Sprintf("%s [%s] (fallback table)", c.CapabilityID, strings.Join(c.SideEffects, ",")))
		}
	}
	return out
}

func cmdDescribe(hf hubFlags, id string) int {
	ctx := context.Background()
	for _, p := range buildProviders(ctx, hf) {
		if p.ID() != id {
			continue
		}
		if p.Status.Manifest != nil {
			writeJSON(p.Status.Manifest)
			return 0
		}
		if p.Status.ProbeErrorCode != "" {
			msg := "provider failed to self-describe"
			if len(p.Status.Diagnostics) > 0 {
				msg = p.Status.Diagnostics[len(p.Status.Diagnostics)-1].Message
			}
			return emitError(hf, "", id, p.Status.ProbeErrorCode, msg)
		}
		if p.Fallback != nil {
			writeJSON(fallbackManifest(p))
			return 0
		}
	}
	fmt.Fprintf(os.Stderr, "kg describe: %s: provider %q not found\n", protocol.ErrProviderNotFound, id)
	return 1
}

// fallbackManifest renders the hub fallback table as a manifest-shaped
// document, clearly marked as fallback data.
func fallbackManifest(p router.Provider) map[string]any {
	var caps []map[string]any
	for _, c := range p.Fallback.Capabilities {
		var inputSchema any
		_ = json.Unmarshal(c.InputSchema, &inputSchema)
		caps = append(caps, map[string]any{
			"capability_id": c.CapabilityID,
			"side_effects":  c.SideEffects,
			"input_schema":  inputSchema,
			"output":        c.Output,
			"cli_spec":      c.CLISpec,
		})
	}
	return map[string]any{
		"protocol":  protocol.ProviderProtocol,
		"probed":    false,
		"note":      "hub fallback data table; provider did not self-describe",
		"provider":  map[string]any{"id": p.ID(), "path": p.Status.Path},
		"available": p.Status.Available,
		"capabilities": caps,
	}
}

func cmdCapability(hf hubFlags, cmd catalog.Command, args []string) int {
	ctx := context.Background()
	providers := buildProviders(ctx, hf)
	if hf.provider != "" {
		var filtered []router.Provider
		for _, p := range providers {
			if p.ID() == hf.provider {
				filtered = append(filtered, p)
			}
		}
		if len(filtered) == 0 {
			return emitError(hf, cmd.CapabilityID, hf.provider, protocol.ErrProviderNotFound,
				fmt.Sprintf("provider %q not found", hf.provider))
		}
		providers = filtered
	}

	res, err := router.Resolve(providers, cmd.CapabilityID)
	if err != nil {
		return emitError(hf, cmd.CapabilityID, hf.provider, protocol.ErrCapabilityNotFound, err.Error())
	}
	input, err := router.ParseInput(res.CLISpec, args)
	if err != nil {
		return emitError(hf, cmd.CapabilityID, res.Provider.ID(), protocol.ErrInvalidInput, err.Error())
	}
	env, err := router.Execute(ctx, res, input, hf.gates, hf.dryRun, nil)
	if err != nil {
		return emitError(hf, cmd.CapabilityID, res.Provider.ID(), protocol.ErrInvocationFailed, err.Error())
	}
	return emitEnvelope(hf, env)
}

// cmdProvider is the raw escape hatch:
// `kg provider <id> <capability_id> --request <file|->`.
func cmdProvider(hf hubFlags, args []string) int {
	if len(args) != 2 || hf.request == "" {
		fmt.Fprintf(os.Stderr, "usage: kg provider <id> <capability_id> --request <file|-> [--json] [--dry-run] [--allow-*]\n")
		return 2
	}
	id, capabilityID := args[0], args[1]

	var req []byte
	if hf.request == "-" {
		b, err := readAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "kg provider: reading stdin: %v\n", err)
			return 1
		}
		req = b
	} else {
		b, err := os.ReadFile(hf.request)
		if err != nil {
			fmt.Fprintf(os.Stderr, "kg provider: reading %s: %v\n", hf.request, err)
			return 1
		}
		req = b
	}
	var input map[string]any
	if err := json.Unmarshal(req, &input); err != nil {
		return emitError(hf, capabilityID, id, protocol.ErrInvalidInput,
			fmt.Sprintf("request is not a JSON object: %v", err))
	}

	ctx := context.Background()
	var target *router.Provider
	for _, p := range buildProviders(ctx, hf) {
		if p.ID() == id {
			cp := p
			target = &cp
			break
		}
	}
	if target == nil {
		return emitError(hf, capabilityID, id, protocol.ErrProviderNotFound,
			fmt.Sprintf("provider %q not found", id))
	}
	res, err := router.Resolve([]router.Provider{*target}, capabilityID)
	if err != nil {
		return emitError(hf, capabilityID, id, protocol.ErrCapabilityNotFound, err.Error())
	}
	env, err := router.Execute(ctx, res, input, hf.gates, hf.dryRun, nil)
	if err != nil {
		return emitError(hf, capabilityID, id, protocol.ErrInvocationFailed, err.Error())
	}
	return emitEnvelope(hf, env)
}

func emitError(hf hubFlags, capabilityID, provider, code, msg string) int {
	env := protocol.ErrorEnvelope(capabilityID, provider, code, msg)
	if hf.json {
		writeJSON(env)
	} else {
		fmt.Fprintf(os.Stderr, "kg: %s: %s\n", code, msg)
	}
	return 1
}

// emitEnvelope prints the result. With --json stdout carries exactly one
// envelope and nothing else.
func emitEnvelope(hf hubFlags, env *protocol.Envelope) int {
	if hf.json {
		writeJSON(env)
	} else {
		for _, d := range env.Diagnostics {
			fmt.Fprintf(os.Stderr, "kg: %s: %s\n", d.Severity, d.Message)
		}
		if env.Status == "error" && env.Error != nil {
			fmt.Fprintf(os.Stderr, "kg: %s: %s\n", env.Error.Code, env.Error.Message)
		} else {
			pretty, _ := json.MarshalIndent(env.Result, "", "  ")
			fmt.Println(string(pretty))
			for _, a := range env.Artifacts {
				fmt.Printf("artifact: %s (%s %s)\n", a.Path, a.Kind, a.Checksum)
			}
		}
	}
	if env.Status == "error" {
		return 1
	}
	return 0
}

func writeJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	_ = enc.Encode(v)
}

func readAll(f *os.File) ([]byte, error) {
	return io.ReadAll(f)
}

func firstErr(err error, fallback string) string {
	if err != nil {
		return err.Error()
	}
	return fallback
}
