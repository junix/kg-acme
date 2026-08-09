package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"kg-acme/internal/policy"
	"kg-acme/internal/protocol"
	"kg-acme/internal/router"
)

// StageResult is one stage's entry in the pipeline execution envelope.
type StageResult struct {
	ID          string              `json:"id"`
	Capability  string              `json:"capability"`
	Provider    string              `json:"provider,omitempty"`
	Status      string              `json:"status"` // ok | error | skipped | planned
	Input       map[string]any      `json:"input,omitempty"`
	SideEffects []string            `json:"side_effects,omitempty"`
	Artifacts   []protocol.Artifact `json:"artifacts,omitempty"`
	Error       *protocol.ErrorInfo `json:"error,omitempty"`
}

// Envelope is the single stdout payload of `kg pipeline run`
// (kg.pipeline.execution/v1).
type Envelope struct {
	Protocol    string                `json:"protocol"`
	Pipeline    string                `json:"pipeline"`
	Status      string                `json:"status"` // ok | error
	WorkDir     string                `json:"work_dir,omitempty"`
	DryRun      bool                  `json:"dry_run,omitempty"`
	Stages      []StageResult         `json:"stages"`
	Diagnostics []protocol.Diagnostic `json:"diagnostics,omitempty"`
	Error       *protocol.ErrorInfo   `json:"error,omitempty"`
}

func newEnvelope(def *Definition) *Envelope {
	return &Envelope{
		Protocol: protocol.PipelineExecutionProtocol,
		Pipeline: def.Name,
		Status:   "ok",
	}
}

// ErrorEnvelope builds a failed pipeline envelope (plan/validation errors,
// policy pre-check denial) with no stage results.
func ErrorEnvelope(name, code, message string) *Envelope {
	env := &Envelope{
		Protocol: protocol.PipelineExecutionProtocol,
		Pipeline: name,
		Status:   "error",
		Error:    &protocol.ErrorInfo{Code: code, Message: message},
	}
	return env
}

// RenderDryRun renders the full execution plan with zero side effects:
// topological order, per-stage provider/capability/resolved input (with
// injection placeholders), typed edges, and the required gates.
func RenderDryRun(plan *Plan) *Envelope {
	env := newEnvelope(plan.Def)
	env.DryRun = true
	for _, ps := range plan.Order {
		env.Stages = append(env.Stages, StageResult{
			ID:          ps.Stage.ID,
			Capability:  ps.Stage.Capability,
			Provider:    ps.Resolved.Provider.ID(),
			Status:      "planned",
			Input:       mergedInput(ps, true),
			SideEffects: ps.Resolved.SideEffects,
		})
	}
	if len(plan.Denied) > 0 {
		var flags []string
		for _, e := range plan.Denied {
			if f := policy.AllowFlag(e); f != "" {
				flags = append(flags, f)
			}
		}
		msg := fmt.Sprintf("side effects denied by policy: %s", strings.Join(plan.Denied, ", "))
		if len(flags) > 0 {
			msg += fmt.Sprintf(" (allow explicitly with %s)", strings.Join(flags, " "))
		}
		env.Diagnostics = append(env.Diagnostics, protocol.Diagnostic{Severity: "warning", Message: msg})
	}
	return env
}

// RunOptions controls Execute.
type RunOptions struct {
	// WorkDir is where stage envelopes and copied artifacts land. Empty
	// selects the default kg-pipeline-<timestamp> under the cwd. Ignored
	// when Resume is set.
	WorkDir string
	// Resume points at an existing work dir: stages whose recorded
	// envelope says ok (with checksum-verified artifacts) are skipped.
	Resume string
	Gates  policy.Gates
	// Runner is the subprocess seam (nil → real exec).
	Runner router.Runner
}

// Execute runs the plan stage by stage in topological order. Gate denial
// fails fast before any provider starts. A failed stage aborts the run
// unless the stage is optional, in which case it is skipped with a
// diagnostic. Each stage's result is recorded to
// <work-dir>/stage-<id>.envelope.json as it completes, and the final
// pipeline envelope to <work-dir>/pipeline.envelope.json.
func Execute(ctx context.Context, plan *Plan, opts RunOptions) *Envelope {
	env := newEnvelope(plan.Def)

	// Pipeline-level policy pre-check: the union of all stage side effects
	// must pass before anything executes.
	if len(plan.Denied) > 0 {
		env.Status = "error"
		env.Error = &protocol.ErrorInfo{Code: protocol.ErrPolicyDenied, Message: gateMessage(plan.Denied)}
		return env
	}

	workDir := opts.WorkDir
	if opts.Resume != "" {
		workDir = opts.Resume
	}
	if workDir == "" {
		workDir = "kg-pipeline-" + time.Now().Format("20060102-150405")
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		env.Status = "error"
		env.Error = &protocol.ErrorInfo{Code: protocol.ErrInvocationFailed, Message: fmt.Sprintf("creating work dir: %v", err)}
		return env
	}
	env.WorkDir = workDir

	results := map[string]*StageResult{}
	for _, ps := range plan.Order {
		res := StageResult{
			ID:          ps.Stage.ID,
			Capability:  ps.Stage.Capability,
			Provider:    ps.Resolved.Provider.ID(),
			SideEffects: ps.Resolved.SideEffects,
		}

		// Resume: reuse a previously completed stage whose artifacts still
		// verify against their recorded checksums.
		if opts.Resume != "" {
			if prev, ok := loadReusable(workDir, ps.Stage.ID); ok {
				env.Diagnostics = append(env.Diagnostics, protocol.Diagnostic{
					Severity: "info",
					Message:  fmt.Sprintf("stage %q: reused from %s (checksum verified)", ps.Stage.ID, stageEnvelopePath(workDir, ps.Stage.ID)),
				})
				results[ps.Stage.ID] = prev
				env.Stages = append(env.Stages, *prev)
				continue
			}
		}

		// Wire upstream artifacts into this stage's input.
		wireErr := false
		for _, pe := range ps.Edges {
			up := results[pe.Upstream.Stage.ID]
			path := ""
			if up != nil && up.Status == "ok" {
				for _, a := range up.Artifacts {
					if pe.Edge.ArtifactKind == "" || a.Kind == pe.Edge.ArtifactKind {
						path = a.Path
						break
					}
				}
			}
			if path == "" {
				res.Status = "error"
				res.Error = &protocol.ErrorInfo{Code: protocol.ErrInvocationFailed, Message: fmt.Sprintf(
					"upstream stage %q has no usable artifact of kind %q (stage status: %s)",
					pe.Upstream.Stage.ID, pe.Kind, stageStatus(up))}
				wireErr = true
				break
			}
			pe.RuntimePath = path
		}
		if !wireErr {
			res.Input = mergedInput(ps, false)
			stageEnv, err := router.Execute(ctx, ps.Resolved, res.Input, opts.Gates, false, opts.Runner)
			if err != nil {
				res.Status = "error"
				res.Error = &protocol.ErrorInfo{Code: protocol.ErrInvocationFailed, Message: err.Error()}
			} else if stageEnv.Status == "error" {
				res.Status = "error"
				res.Error = stageEnv.Error
			} else {
				res.Status = "ok"
				res.Artifacts, err = collectArtifacts(workDir, ps.Stage.ID, stageEnv.Artifacts)
				if err != nil {
					res.Status = "error"
					res.Error = &protocol.ErrorInfo{Code: protocol.ErrInvocationFailed, Message: err.Error()}
				}
			}
			for _, d := range ps.Resolved.Diagnostics {
				env.Diagnostics = append(env.Diagnostics, d)
			}
		}

		if res.Status == "error" && ps.Stage.Optional {
			env.Diagnostics = append(env.Diagnostics, protocol.Diagnostic{
				Severity: "warning",
				Message:  fmt.Sprintf("optional stage %q failed, skipping: %s", ps.Stage.ID, errorText(res.Error)),
			})
			res.Status = "skipped"
		}

		results[ps.Stage.ID] = &res
		env.Stages = append(env.Stages, res)
		writeStageEnvelope(workDir, res)

		if res.Status == "error" {
			env.Status = "error"
			env.Error = res.Error
			break
		}
	}

	writePipelineEnvelope(workDir, env)
	return env
}

func gateMessage(denied []string) string {
	var flags []string
	for _, e := range denied {
		if f := policy.AllowFlag(e); f != "" {
			flags = append(flags, f)
		}
	}
	msg := fmt.Sprintf("side effects denied by policy: %s", strings.Join(denied, ", "))
	if len(flags) > 0 {
		msg += fmt.Sprintf(" (allow explicitly with %s)", strings.Join(flags, " "))
	}
	return msg
}

func stageStatus(r *StageResult) string {
	if r == nil {
		return "not run"
	}
	return r.Status
}

func errorText(e *protocol.ErrorInfo) string {
	if e == nil {
		return "unknown error"
	}
	return e.Message
}

// collectArtifacts copies a stage's artifacts into the work dir, verifying
// each source file against its envelope checksum before copying, and
// returns the rewritten artifact list (work-dir paths, fresh checksums).
// Keeping artifacts under the work dir makes resume self-contained: a
// provider's temp dir may vanish between runs, the work dir does not.
func collectArtifacts(workDir, stageID string, arts []protocol.Artifact) ([]protocol.Artifact, error) {
	var out []protocol.Artifact
	for _, a := range arts {
		data, err := os.ReadFile(a.Path)
		if err != nil {
			return nil, fmt.Errorf("stage %q: reading artifact %q: %v", stageID, a.Path, err)
		}
		sum := sha256.Sum256(data)
		got := "sha256:" + hex.EncodeToString(sum[:])
		if a.Checksum != "" && a.Checksum != got {
			return nil, fmt.Errorf("stage %q: artifact %q checksum mismatch (envelope %s, actual %s)",
				stageID, a.Path, a.Checksum, got)
		}
		dst := filepath.Join(workDir, fmt.Sprintf("stage-%s-%s", stageID, filepath.Base(a.Path)))
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return nil, fmt.Errorf("stage %q: writing artifact to work dir: %v", stageID, err)
		}
		out = append(out, protocol.Artifact{Path: dst, Kind: a.Kind, Checksum: got})
	}
	return out, nil
}

func stageEnvelopePath(workDir, stageID string) string {
	return filepath.Join(workDir, fmt.Sprintf("stage-%s.envelope.json", stageID))
}

func writeStageEnvelope(workDir string, res StageResult) {
	data, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(stageEnvelopePath(workDir, res.ID), data, 0o644)
}

func writePipelineEnvelope(workDir string, env *Envelope) {
	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(workDir, "pipeline.envelope.json"), data, 0o644)
}

// loadReusable reads a recorded stage envelope and returns it when the
// stage completed ok and every recorded artifact still exists with a
// matching checksum.
func loadReusable(workDir, stageID string) (*StageResult, bool) {
	data, err := os.ReadFile(stageEnvelopePath(workDir, stageID))
	if err != nil {
		return nil, false
	}
	var res StageResult
	if err := json.Unmarshal(data, &res); err != nil || res.Status != "ok" {
		return nil, false
	}
	for _, a := range res.Artifacts {
		if !verifyChecksum(a) {
			return nil, false
		}
	}
	return &res, true
}

// verifyChecksum re-hashes an artifact file and compares against its
// recorded checksum. Artifacts without a checksum verify by existence.
func verifyChecksum(a protocol.Artifact) bool {
	data, err := os.ReadFile(a.Path)
	if err != nil {
		return false
	}
	if a.Checksum == "" {
		return true
	}
	sum := sha256.Sum256(data)
	return a.Checksum == "sha256:"+hex.EncodeToString(sum[:])
}
