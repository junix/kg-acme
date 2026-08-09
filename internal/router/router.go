// Package router resolves a capability_id to a provider, parses argv into
// an input object per the provider's cli_spec, enforces policy gates, and
// executes the invocation — via the kg.execution/v1 invoke protocol for
// probed providers, or via the hub fallback argv bridge for legacy CLIs.
package router

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"kg-acme/internal/bridge"
	"kg-acme/internal/discover"
	"kg-acme/internal/policy"
	"kg-acme/internal/protocol"
	"kg-acme/internal/schema"
)

// Provider is one routable provider: either probed (self-describing) or a
// legacy CLI reached through the hub fallback bridge.
type Provider struct {
	Status   discover.ProviderStatus
	Fallback *bridge.FallbackProvider
}

// ID returns the provider id.
func (p Provider) ID() string {
	if p.Fallback != nil && p.Status.ID == "" {
		return p.Fallback.ID
	}
	return p.Status.ID
}

// DiscoverProviders assembles the routable provider set: legacy bridges
// found on disk plus protocol-native kg-provider-* binaries, honoring
// explicit --provider-bin overrides. This is the one discovery assembly
// shared by every hub front end (kg CLI, kg-mcp server).
func DiscoverProviders(ctx context.Context, overrides discover.Overrides) []Provider {
	env := discover.DefaultEnv()
	var out []Provider
	seen := map[string]bool{}

	for _, fb := range bridge.Table() {
		path := discover.FindExecutable(fb.Bin, overrides, env)
		if path == "" {
			continue
		}
		fbCopy := fb
		st := discover.Probe(ctx, fb.ID, path)
		out = append(out, Provider{Status: st, Fallback: &fbCopy})
		seen[st.ID] = true
	}
	for name, path := range discover.ScanProviders(env) {
		if seen[name] {
			continue
		}
		st := discover.Probe(ctx, name, path)
		out = append(out, Provider{Status: st})
		seen[st.ID] = true
	}
	// Explicit overrides for providers unknown to the hub.
	for id, path := range overrides {
		if seen[id] || !discover.IsExecutable(path) {
			continue
		}
		st := discover.Probe(ctx, id, path)
		out = append(out, Provider{Status: st})
		seen[st.ID] = true
	}
	return out
}

// Resolved binds a capability_id to a concrete provider plus the effective
// spec (provider-authoritative when probed, fallback otherwise).
type Resolved struct {
	Provider     Provider
	CapabilityID string
	SideEffects  []string
	Output       protocol.OutputSpec
	InputSchema  json.RawMessage
	CLISpec      protocol.CLISpec
	Probed       bool
	Diagnostics  []protocol.Diagnostic
}

// Resolve finds the best provider for capabilityID. Probed providers win
// over fallback bridges; ties break on weight (desc) then provider id.
func Resolve(providers []Provider, capabilityID string) (*Resolved, error) {
	var cands []*Resolved
	for _, p := range providers {
		if r := resolveOne(p, capabilityID); r != nil {
			cands = append(cands, r)
		}
	}
	if len(cands) == 0 {
		return nil, fmt.Errorf("%s: no provider offers %q", protocol.ErrCapabilityNotFound, capabilityID)
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].Probed != cands[j].Probed {
			return cands[i].Probed
		}
		if cands[i].Provider.Status.Weight != cands[j].Provider.Status.Weight {
			return cands[i].Provider.Status.Weight > cands[j].Provider.Status.Weight
		}
		return cands[i].Provider.ID() < cands[j].Provider.ID()
	})
	return cands[0], nil
}

func resolveOne(p Provider, capabilityID string) *Resolved {
	fbCap := (*bridge.FallbackCapability)(nil)
	if p.Fallback != nil {
		fbCap = p.Fallback.Capability(capabilityID)
	}

	if p.Status.Probed && p.Status.Manifest != nil {
		for _, c := range p.Status.Manifest.Capabilities {
			if c.CapabilityID != capabilityID {
				continue
			}
			r := &Resolved{
				Provider:     p,
				CapabilityID: capabilityID,
				SideEffects:  c.SideEffects,
				Output:       c.Output,
				InputSchema:  c.InputSchema,
				CLISpec:      c.CLISpec,
				Probed:       true,
			}
			r.Diagnostics = append(r.Diagnostics, p.Status.Diagnostics...)
			// Provider cli_spec overrides the hub fallback table; a
			// difference is surfaced, never silently applied.
			if fbCap != nil && !reflect.DeepEqual(c.CLISpec, fbCap.CLISpec) {
				r.Diagnostics = append(r.Diagnostics, protocol.Diagnostic{
					Severity: "info", Message: bridge.CLISpecDiffDiagnostic})
			}
			return r
		}
	}

	if fbCap != nil {
		return &Resolved{
			Provider:     p,
			CapabilityID: capabilityID,
			SideEffects:  fbCap.SideEffects,
			Output:       fbCap.Output,
			InputSchema:  fbCap.InputSchema,
			CLISpec:      fbCap.CLISpec,
			Probed:       false,
			Diagnostics:  p.Status.Diagnostics,
		}
	}
	return nil
}

// ParseInput converts argv (hub flags already stripped) into an input
// object per the cli_spec: tokens matching a spec flag are parsed by kind,
// remaining bare tokens fill positionals in spec order.
func ParseInput(spec protocol.CLISpec, args []string) (map[string]any, error) {
	input := map[string]any{}
	byFlag := map[string]protocol.FlagSpec{}
	for _, f := range spec.Flags {
		byFlag[f.Flag] = f
	}
	var positionalValues []string

	for i := 0; i < len(args); i++ {
		tok := args[i]
		if strings.HasPrefix(tok, "-") && tok != "-" {
			f, ok := byFlag[tok]
			if !ok {
				return nil, fmt.Errorf("unknown flag %q for this capability", tok)
			}
			switch f.Kind {
			case protocol.FlagBoolean:
				input[f.Name] = true
			case protocol.FlagArray:
				if i+1 >= len(args) {
					return nil, fmt.Errorf("flag %q expects a value", tok)
				}
				i++
				input[f.Name] = append(asSlice(input[f.Name]), args[i])
			case protocol.FlagString:
				if i+1 >= len(args) {
					return nil, fmt.Errorf("flag %q expects a value", tok)
				}
				i++
				input[f.Name] = args[i]
			case protocol.FlagNumber:
				if i+1 >= len(args) {
					return nil, fmt.Errorf("flag %q expects a value", tok)
				}
				i++
				n, err := strconv.ParseFloat(args[i], 64)
				if err != nil {
					return nil, fmt.Errorf("flag %q: %q is not a number", tok, args[i])
				}
				input[f.Name] = n
			}
			continue
		}
		positionalValues = append(positionalValues, tok)
	}

	for i, v := range positionalValues {
		if i >= len(spec.Positionals) {
			return nil, fmt.Errorf("unexpected extra argument %q", v)
		}
		input[spec.Positionals[i].Name] = v
	}
	return input, nil
}

func asSlice(v any) []any {
	if s, ok := v.([]any); ok {
		return s
	}
	return nil
}

// Runner executes an external command; a seam for tests.
type Runner interface {
	Run(ctx context.Context, name string, args []string, stdin []byte) (stdout, stderr []byte, err error)
}

// ExecRunner runs real subprocesses.
type ExecRunner struct{}

// Run implements Runner.
func (ExecRunner) Run(ctx context.Context, name string, args []string, stdin []byte) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	return out.Bytes(), errb.Bytes(), err
}

// Execute runs the resolved capability. When dryRun is true it renders the
// execution plan with zero side effects instead of invoking anything.
func Execute(ctx context.Context, r *Resolved, input map[string]any, gates policy.Gates, dryRun bool, run Runner) (*protocol.Envelope, error) {
	if run == nil {
		run = ExecRunner{}
	}
	env := protocol.NewEnvelope(r.CapabilityID, r.Provider.ID())
	env.Diagnostics = append(env.Diagnostics, r.Diagnostics...)

	if err := schema.ValidateInput(r.InputSchema, input); err != nil {
		return protocol.ErrorEnvelope(r.CapabilityID, r.Provider.ID(), protocol.ErrInvalidInput, err.Error()), nil
	}

	argv, err := bridge.RenderArgv(r.CLISpec, input)
	if err != nil {
		return protocol.ErrorEnvelope(r.CapabilityID, r.Provider.ID(), protocol.ErrInvalidInput, err.Error()), nil
	}

	denied := gates.Denied(r.SideEffects)

	if dryRun {
		env.Status = "ok"
		env.Result = map[string]any{
			"dry_run":       true,
			"provider":      r.Provider.ID(),
			"provider_path": r.Provider.Status.Path,
			"probed":        r.Probed,
			"capability_id": r.CapabilityID,
			"argv":          append([]string{r.Provider.Status.Path}, argv...),
			"side_effects":  r.SideEffects,
			"denied":        denied,
			"would_execute": len(denied) == 0,
		}
		return env, nil
	}

	if err := gates.Check(r.SideEffects); err != nil {
		return protocol.ErrorEnvelope(r.CapabilityID, r.Provider.ID(), protocol.ErrPolicyDenied, err.Error()), nil
	}

	if r.Probed {
		return invokeProtocol(ctx, r, input, run)
	}
	return invokeFallback(ctx, r, argv, run)
}

// invokeProtocol speaks kg.execution/v1: stdin carries the JSON request,
// stdout carries exactly one envelope.
func invokeProtocol(ctx context.Context, r *Resolved, input map[string]any, run Runner) (*protocol.Envelope, error) {
	req, err := json.Marshal(map[string]any{
		"capability_id": r.CapabilityID,
		"input":         input,
	})
	if err != nil {
		return nil, err
	}
	out, _, err := run.Run(ctx, r.Provider.Status.Path,
		[]string{"invoke", r.CapabilityID, "--request", "-"}, req)
	if err != nil && len(out) == 0 {
		return protocol.ErrorEnvelope(r.CapabilityID, r.Provider.ID(),
			protocol.ErrInvocationFailed, fmt.Sprintf("invoke failed: %v", err)), nil
	}
	var env protocol.Envelope
	if err := json.Unmarshal(bytes.TrimSpace(out), &env); err != nil {
		return protocol.ErrorEnvelope(r.CapabilityID, r.Provider.ID(),
			protocol.ErrInvocationFailed, fmt.Sprintf("invoke stdout is not a valid envelope: %v", err)), nil
	}
	env.Diagnostics = append(r.Diagnostics, env.Diagnostics...)
	return &env, nil
}

// invokeFallback executes a legacy CLI through the hub bridge and wraps the
// raw output into an envelope.
func invokeFallback(ctx context.Context, r *Resolved, argv []string, run Runner) (*protocol.Envelope, error) {
	env := protocol.NewEnvelope(r.CapabilityID, r.Provider.ID())
	env.Diagnostics = append(env.Diagnostics, r.Diagnostics...)

	out, stderr, err := run.Run(ctx, r.Provider.Status.Path, argv, nil)
	if err != nil {
		msg := fmt.Sprintf("provider exited with error: %v", err)
		if s := strings.TrimSpace(string(stderr)); s != "" {
			msg += ": " + s
		}
		e := protocol.ErrorEnvelope(r.CapabilityID, r.Provider.ID(), protocol.ErrInvocationFailed, msg)
		e.Diagnostics = env.Diagnostics
		return e, nil
	}

	switch r.Output.Mode {
	case "artifact":
		path := stdoutPath(r.CLISpec, argv)
		if path == "" {
			env.Status = "ok"
			env.Result = map[string]any{"stdout": strings.TrimSpace(string(out))}
			return env, nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			e := protocol.ErrorEnvelope(r.CapabilityID, r.Provider.ID(),
				protocol.ErrInvocationFailed, fmt.Sprintf("reading artifact %q: %v", path, err))
			e.Diagnostics = env.Diagnostics
			return e, nil
		}
		sum := sha256.Sum256(data)
		env.Status = "ok"
		env.Artifacts = []protocol.Artifact{{
			Path: path, Kind: r.Output.Kind, Checksum: "sha256:" + hex.EncodeToString(sum[:]),
		}}
	default: // result-json
		env.Status = "ok"
		trimmed := bytes.TrimSpace(out)
		var payload any
		if len(trimmed) > 0 && json.Unmarshal(trimmed, &payload) == nil {
			env.Result = payload
		} else {
			env.Result = map[string]any{"stdout": string(trimmed)}
		}
	}
	return env, nil
}

// stdoutPath finds the value of the flag marked stdout:true in rendered argv.
func stdoutPath(spec protocol.CLISpec, argv []string) string {
	for _, f := range spec.Flags {
		if !f.Stdout {
			continue
		}
		for i, tok := range argv {
			if tok == f.Flag && i+1 < len(argv) {
				return argv[i+1]
			}
		}
	}
	return ""
}
