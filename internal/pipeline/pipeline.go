// Package pipeline is the Phase 2 pipeline runner: it loads a declarative
// kg.pipeline/v1 definition, compiles stages into a DAG, validates typed
// edges between stages at plan time, pre-checks the union of side-effect
// gates, and executes stages in topological order through the Phase 1
// router (resolve / policy gates / envelope) — with dry-run plan rendering
// and work-dir-based resume.
//
// The hub still implements no KG algorithms: every stage is a routed
// provider capability invocation. Artifacts are the only data that flows
// between stages, matched by kind and injected into the downstream input
// under the property named by the edge's `as` field.
package pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"kg-acme/internal/policy"
	"kg-acme/internal/protocol"
	"kg-acme/internal/router"
	"kg-acme/internal/schema"
)

// Definition is a parsed kg.pipeline/v1 pipeline file.
type Definition struct {
	Pipeline string  `json:"pipeline"`
	Name     string  `json:"name"`
	Stages   []Stage `json:"stages"`
}

// Stage is one pipeline step: a routed capability invocation.
type Stage struct {
	ID         string         `json:"id"`
	Capability string         `json:"capability"`
	Optional   bool           `json:"optional,omitempty"`
	Input      map[string]any `json:"input,omitempty"`
	InputFrom  []Edge         `json:"input_from,omitempty"`
}

// UnmarshalJSON accepts input_from as a single edge object or an array of
// edges (single-edge pipelines stay terse; fan-in DAGs use the array form).
func (s *Stage) UnmarshalJSON(data []byte) error {
	type rawStage struct {
		ID         string          `json:"id"`
		Capability string          `json:"capability"`
		Optional   bool            `json:"optional,omitempty"`
		Input      map[string]any  `json:"input,omitempty"`
		InputFrom  json.RawMessage `json:"input_from,omitempty"`
	}
	var r rawStage
	if err := json.Unmarshal(data, &r); err != nil {
		return err
	}
	s.ID, s.Capability, s.Optional, s.Input = r.ID, r.Capability, r.Optional, r.Input
	if len(r.InputFrom) == 0 {
		return nil
	}
	if strings.HasPrefix(strings.TrimSpace(string(r.InputFrom)), "[") {
		return json.Unmarshal(r.InputFrom, &s.InputFrom)
	}
	var e Edge
	if err := json.Unmarshal(r.InputFrom, &e); err != nil {
		return err
	}
	s.InputFrom = []Edge{e}
	return nil
}

// Edge wires one upstream stage's artifact into this stage's input.
type Edge struct {
	// Stage is the upstream stage id.
	Stage string `json:"stage"`
	// ArtifactKind selects among the upstream artifacts by kind. Optional;
	// when given it must equal the upstream capability's declared output
	// kind (checked at plan time).
	ArtifactKind string `json:"artifact_kind,omitempty"`
	// As is the downstream input_schema property the artifact path is
	// injected into (e.g. "document_file").
	As string `json:"as"`
}

// LoadDefinition reads and parses a pipeline definition file.
func LoadDefinition(path string) (*Definition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &Error{Code: protocol.ErrInvalidPipeline, Message: fmt.Sprintf("reading pipeline definition: %v", err)}
	}
	return ParseDefinition(data)
}

// ParseDefinition parses a kg.pipeline/v1 definition document (the inline
// form used by kg-mcp's kg_pipeline_run tool).
func ParseDefinition(data []byte) (*Definition, error) {
	var def Definition
	if err := json.Unmarshal(data, &def); err != nil {
		return nil, &Error{Code: protocol.ErrInvalidPipeline, Message: fmt.Sprintf("pipeline definition is not valid JSON: %v", err)}
	}
	return &def, nil
}

// Error is a plan/validation failure carrying a hub error code.
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

// PlannedStage is a stage bound to its routed provider and upstream edges.
type PlannedStage struct {
	Stage    *Stage
	Resolved *router.Resolved
	Edges    []*PlannedEdge
}

// PlannedEdge is an edge with its upstream stage resolved.
type PlannedEdge struct {
	Edge     Edge
	Upstream *PlannedStage
	// Kind is the artifact kind expected from upstream (the upstream
	// capability's declared output kind), filled by checkEdge.
	Kind string
	// RuntimePath is the concrete artifact path injected at execution time.
	RuntimePath string
}

// Plan is a validated pipeline: stages in topological execution order,
// plus the union of side effects and the gates currently denied.
type Plan struct {
	Def     *Definition
	Order   []*PlannedStage
	Effects []string
	Denied  []string
}

func legalStageID(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

// Build validates the definition, resolves every stage's capability
// against the provider set, checks typed edges, and returns the execution
// plan. Gate denial is reported (Plan.Denied) but not fatal here — the run
// path fails fast on it, validate/dry-run paths just report it.
func Build(def *Definition, providers []router.Provider, gates policy.Gates) (*Plan, error) {
	if def.Pipeline != protocol.PipelineProtocol {
		return nil, &Error{protocol.ErrInvalidPipeline,
			fmt.Sprintf("pipeline field must be %q, got %q", protocol.PipelineProtocol, def.Pipeline)}
	}
	if def.Name == "" {
		return nil, &Error{protocol.ErrInvalidPipeline, "pipeline name must be non-empty"}
	}
	if len(def.Stages) == 0 {
		return nil, &Error{protocol.ErrInvalidPipeline, "pipeline must declare at least one stage"}
	}

	byID := map[string]*PlannedStage{}
	for i := range def.Stages {
		st := &def.Stages[i]
		if !legalStageID(st.ID) {
			return nil, &Error{protocol.ErrInvalidPipeline,
				fmt.Sprintf("stage id %q must be non-empty lowercase [a-z0-9-_]", st.ID)}
		}
		if st.Capability == "" {
			return nil, &Error{protocol.ErrInvalidPipeline, fmt.Sprintf("stage %q: capability must be non-empty", st.ID)}
		}
		if byID[st.ID] != nil {
			return nil, &Error{protocol.ErrInvalidPipeline, fmt.Sprintf("duplicate stage id %q", st.ID)}
		}
		byID[st.ID] = &PlannedStage{Stage: st}
	}

	// Resolve capabilities and bind edges.
	for _, ps := range byID {
		res, err := router.Resolve(providers, ps.Stage.Capability)
		if err != nil {
			return nil, &Error{protocol.ErrCapabilityNotFound,
				fmt.Sprintf("stage %q: %v", ps.Stage.ID, err)}
		}
		ps.Resolved = res
		for _, e := range ps.Stage.InputFrom {
			up := byID[e.Stage]
			if up == nil {
				return nil, &Error{protocol.ErrInvalidPipeline,
					fmt.Sprintf("stage %q: input_from references unknown stage %q", ps.Stage.ID, e.Stage)}
			}
			if e.As == "" {
				return nil, &Error{protocol.ErrInvalidPipeline,
					fmt.Sprintf("stage %q: input_from edge from %q must name a target property with \"as\"", ps.Stage.ID, e.Stage)}
			}
			ps.Edges = append(ps.Edges, &PlannedEdge{Edge: e, Upstream: up})
		}
	}

	order, err := topoSort(def, byID)
	if err != nil {
		return nil, err
	}

	// Typed-edge validation, now that capabilities are resolved.
	for _, ps := range order {
		for _, pe := range ps.Edges {
			if err := checkEdge(ps, pe); err != nil {
				return nil, err
			}
		}
		// Static input + injected placeholders must satisfy the downstream
		// input_schema (catches missing required properties at plan time).
		if err := schema.ValidateInput(ps.Resolved.InputSchema, mergedInput(ps, true)); err != nil {
			return nil, &Error{protocol.ErrInvalidInput, fmt.Sprintf("stage %q: %v", ps.Stage.ID, err)}
		}
	}

	plan := &Plan{Def: def, Order: order}
	seen := map[string]bool{}
	for _, ps := range order {
		for _, e := range ps.Resolved.SideEffects {
			if !seen[e] {
				seen[e] = true
				plan.Effects = append(plan.Effects, e)
			}
		}
	}
	sort.Strings(plan.Effects)
	plan.Denied = gates.Denied(plan.Effects)
	return plan, nil
}

// topoSort orders stages so every stage follows its upstreams. Kahn's
// algorithm, breaking ties by definition order to stay deterministic.
// Linear chains are just degenerate DAGs; branching needs no code change.
func topoSort(def *Definition, byID map[string]*PlannedStage) ([]*PlannedStage, error) {
	defIndex := map[*PlannedStage]int{}
	indegree := map[*PlannedStage]int{}
	for i := range def.Stages {
		ps := byID[def.Stages[i].ID]
		defIndex[ps] = i
		indegree[ps] = len(ps.Edges)
	}
	less := func(a, b *PlannedStage) bool { return defIndex[a] < defIndex[b] }

	var ready []*PlannedStage
	for _, ps := range byID {
		if indegree[ps] == 0 {
			ready = append(ready, ps)
		}
	}
	sort.Slice(ready, func(i, j int) bool { return less(ready[i], ready[j]) })

	var order []*PlannedStage
	for len(ready) > 0 {
		ps := ready[0]
		ready = ready[1:]
		order = append(order, ps)
		for _, other := range byID {
			for _, e := range other.Edges {
				if e.Upstream == ps {
					indegree[other]--
					if indegree[other] == 0 {
						ready = append(ready, other)
					}
				}
			}
		}
		sort.Slice(ready, func(i, j int) bool { return less(ready[i], ready[j]) })
	}
	if len(order) != len(byID) {
		return nil, &Error{protocol.ErrInvalidPipeline, "pipeline stages form a cycle"}
	}
	return order, nil
}

// kindChannel maps an artifact kind to the data channel it flows on.
// Edges are only allowed within one channel.
func kindChannel(kind string) string {
	switch kind {
	case "kg-document":
		return "graph"
	case "chunks":
		return "chunks"
	case "communities":
		return "communities"
	case "json":
		return "json"
	default:
		return ""
	}
}

// fieldChannel maps an input property to the channel it consumes, per the
// hub-wide naming convention: graph-in capabilities take their document via
// document/document_file. Properties outside the table are untyped and
// accept any artifact channel.
func fieldChannel(name string) string {
	switch name {
	case "document", "document_file":
		return "graph"
	case "chunks_file":
		return "chunks"
	default:
		return ""
	}
}

// checkEdge validates one typed edge at plan time: the upstream must
// declare an artifact-mode output of the referenced kind, the target
// property must exist in the downstream input_schema, and the artifact
// channel must match the target property's channel.
func checkEdge(ps *PlannedStage, pe *PlannedEdge) error {
	up := pe.Upstream
	kind := up.Resolved.Output.Kind
	if up.Resolved.Output.Mode != "artifact" {
		return &Error{protocol.ErrIncompatibleStageEdge, fmt.Sprintf(
			"stage %q: upstream stage %q (%s) has output mode %q — no artifact to wire; "+
				"only artifact-mode capabilities can feed input_from",
			ps.Stage.ID, up.Stage.ID, up.Stage.Capability, up.Resolved.Output.Mode)}
	}
	if pe.Edge.ArtifactKind != "" && pe.Edge.ArtifactKind != kind {
		return &Error{protocol.ErrIncompatibleStageEdge, fmt.Sprintf(
			"stage %q: edge expects artifact kind %q but stage %q (%s) produces kind %q",
			ps.Stage.ID, pe.Edge.ArtifactKind, up.Stage.ID, up.Stage.Capability, kind)}
	}
	pe.Kind = kind

	if !inputSchemaHasProperty(ps.Resolved.InputSchema, pe.Edge.As) {
		return &Error{protocol.ErrInvalidInput, fmt.Sprintf(
			"stage %q: input_schema of %s has no property %q (edge target \"as\")",
			ps.Stage.ID, ps.Stage.Capability, pe.Edge.As)}
	}

	upCh, downCh := kindChannel(kind), fieldChannel(pe.Edge.As)
	if downCh != "" && upCh != downCh {
		return &Error{protocol.ErrIncompatibleStageEdge, fmt.Sprintf(
			"stage %q: artifact kind %q (channel %q) cannot feed property %q (channel %q)",
			ps.Stage.ID, kind, orUnknown(upCh), pe.Edge.As, downCh)}
	}
	return nil
}

func orUnknown(ch string) string {
	if ch == "" {
		return "unknown"
	}
	return ch
}

// inputSchemaHasProperty reports whether the input_schema declares the
// property. Open schemas (no properties object, or additionalProperties
// not explicitly false) answer true — the hub cannot know, so it does not
// block.
func inputSchemaHasProperty(raw json.RawMessage, name string) bool {
	if len(raw) == 0 {
		return true
	}
	var sch struct {
		Properties           map[string]any `json:"properties"`
		AdditionalProperties any            `json:"additionalProperties"`
	}
	if err := json.Unmarshal(raw, &sch); err != nil || sch.Properties == nil {
		return true
	}
	if _, ok := sch.Properties[name]; ok {
		return true
	}
	if b, ok := sch.AdditionalProperties.(bool); ok && !b {
		return false
	}
	return true
}

// placeholder renders the plan-time stand-in for an injected artifact path.
func placeholder(upstreamID, kind string) string {
	return fmt.Sprintf("kg-pipeline://%s/%s", upstreamID, kind)
}

// mergedInput combines a stage's static input with its edge injections.
// In plan mode edge values are placeholder markers; in run mode they are
// the concrete artifact paths recorded on PlannedEdge.RuntimePath.
func mergedInput(ps *PlannedStage, planMode bool) map[string]any {
	in := map[string]any{}
	for k, v := range ps.Stage.Input {
		in[k] = v
	}
	for _, pe := range ps.Edges {
		if planMode {
			in[pe.Edge.As] = placeholder(pe.Upstream.Stage.ID, pe.Kind)
		} else if pe.RuntimePath != "" {
			in[pe.Edge.As] = pe.RuntimePath
		}
	}
	return in
}
