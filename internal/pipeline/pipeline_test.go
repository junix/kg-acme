package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kg-acme/internal/discover"
	"kg-acme/internal/policy"
	"kg-acme/internal/protocol"
	"kg-acme/internal/router"
)

// fakeProvider builds a probed provider with the given capabilities, no
// subprocess involved.
func fakeProvider(id string, caps ...protocol.Capability) router.Provider {
	return router.Provider{Status: discover.ProviderStatus{
		ID:      id,
		Path:    "/fake/" + id,
		Weight:  1,
		Probed:  true,
		Version: 1,
		Manifest: &protocol.Manifest{
			Protocol:         protocol.ProviderProtocol,
			ProtocolVersions: []int{1},
			Provider:         protocol.ProviderInfo{ID: id, Version: "0.0.1"},
			Capabilities:     caps,
		},
	}}
}

func capSpec(id string, side []string, mode, kind string, schemaProps string) protocol.Capability {
	return protocol.Capability{
		CapabilityID: id,
		Title:        id,
		Description:  id + ".",
		SideEffects:  side,
		InputSchema:  json.RawMessage(schemaProps),
		Output:       protocol.OutputSpec{Mode: mode, Kind: kind},
	}
}

const graphInSchema = `{"type":"object","properties":{"document":{"type":"string"},"document_file":{"type":"string"},"merge_strategy":{"type":"string"}},"additionalProperties":false}`
const extractSchema = `{"type":"object","properties":{"file":{"type":"string"},"text":{"type":"string"}},"additionalProperties":false}`
const parseSchema = `{"type":"object","properties":{"sidecar":{"type":"string"}},"required":["sidecar"],"additionalProperties":false}`

// chainProviders mirrors the real ecosystem shapes: parse (chunks),
// extract (kg-document), coref (graph-in/out), store (graph-in),
// communities (result-json).
func chainProviders() []router.Provider {
	return []router.Provider{
		fakeProvider("kg-mm",
			capSpec("parse.multimodal", []string{"network"}, "artifact", "chunks", parseSchema)),
		fakeProvider("kg-extract",
			capSpec("extract.entities_relations", []string{"network", "data_egress"}, "artifact", "kg-document", extractSchema),
			capSpec("resolve.coref", nil, "artifact", "kg-document", graphInSchema),
			capSpec("detect.communities", nil, "result-json", "communities", graphInSchema)),
		fakeProvider("kg-neo4j-cli",
			capSpec("store.triples", []string{"writes_db"}, "result-json", "json", graphInSchema)),
	}
}

func parseDef(t *testing.T, defJSON string) *Definition {
	t.Helper()
	var def Definition
	if err := json.Unmarshal([]byte(defJSON), &def); err != nil {
		t.Fatalf("definition JSON: %v", err)
	}
	return &def
}

const chainDef = `{
  "pipeline": "kg.pipeline/v1",
  "name": "doc-to-graph",
  "stages": [
    {"id": "parse", "capability": "parse.multimodal", "optional": true,
     "input": {"sidecar": "sidecar.json"}},
    {"id": "extract", "capability": "extract.entities_relations",
     "input": {"file": "doc.md"}},
    {"id": "dedup", "capability": "resolve.coref",
     "input_from": {"stage": "extract", "artifact_kind": "kg-document", "as": "document_file"}},
    {"id": "store", "capability": "store.triples",
     "input_from": {"stage": "dedup", "artifact_kind": "kg-document", "as": "document_file"}}
  ]
}`

func TestBuildChainTopoOrder(t *testing.T) {
	plan, err := Build(parseDef(t, chainDef), chainProviders(), policy.Gates{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	var ids []string
	for _, ps := range plan.Order {
		ids = append(ids, ps.Stage.ID)
	}
	want := []string{"parse", "extract", "dedup", "store"}
	if fmt.Sprint(ids) != fmt.Sprint(want) {
		t.Errorf("topo order = %v, want %v", ids, want)
	}
	// Gate union: network + data_egress + writes_db, all denied by default.
	if fmt.Sprint(plan.Denied) != fmt.Sprint([]string{"data_egress", "network", "writes_db"}) {
		t.Errorf("denied = %v", plan.Denied)
	}
}

// Stages listed out of dependency order must still execute in topological
// order (DAG, not "definition order").
func TestTopoSortReordersDefinition(t *testing.T) {
	def := parseDef(t, `{
	  "pipeline": "kg.pipeline/v1", "name": "shuffled",
	  "stages": [
	    {"id": "store", "capability": "store.triples",
	     "input_from": {"stage": "dedup", "as": "document_file"}},
	    {"id": "dedup", "capability": "resolve.coref",
	     "input_from": {"stage": "extract", "as": "document_file"}},
	    {"id": "extract", "capability": "extract.entities_relations", "input": {"file": "d.md"}}
	  ]
	}`)
	plan, err := Build(def, chainProviders(), policy.Gates{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	var ids []string
	for _, ps := range plan.Order {
		ids = append(ids, ps.Stage.ID)
	}
	want := []string{"extract", "dedup", "store"}
	if fmt.Sprint(ids) != fmt.Sprint(want) {
		t.Errorf("topo order = %v, want %v", ids, want)
	}
}

func TestTopoSortDetectsCycle(t *testing.T) {
	def := parseDef(t, `{
	  "pipeline": "kg.pipeline/v1", "name": "cyclic",
	  "stages": [
	    {"id": "a", "capability": "resolve.coref",
	     "input_from": {"stage": "b", "as": "document_file"}},
	    {"id": "b", "capability": "resolve.coref",
	     "input_from": {"stage": "a", "as": "document_file"}}
	  ]
	}`)
	_, err := Build(def, chainProviders(), policy.Gates{})
	pe, ok := err.(*Error)
	if !ok || pe.Code != protocol.ErrInvalidPipeline {
		t.Fatalf("expected invalid_pipeline cycle error, got %v", err)
	}
}

// chunks (parse output) cannot feed a graph-in property (document_file).
func TestIncompatibleStageEdge(t *testing.T) {
	def := parseDef(t, `{
	  "pipeline": "kg.pipeline/v1", "name": "bad-edge",
	  "stages": [
	    {"id": "parse", "capability": "parse.multimodal", "input": {"sidecar": "s.json"}},
	    {"id": "dedup", "capability": "resolve.coref",
	     "input_from": {"stage": "parse", "artifact_kind": "chunks", "as": "document_file"}}
	  ]
	}`)
	_, err := Build(def, chainProviders(), policy.Gates{})
	pe, ok := err.(*Error)
	if !ok || pe.Code != protocol.ErrIncompatibleStageEdge {
		t.Fatalf("expected incompatible_stage_edge, got %v", err)
	}
}

// A result-json upstream has no artifacts to wire.
func TestEdgeFromResultJsonRejected(t *testing.T) {
	def := parseDef(t, `{
	  "pipeline": "kg.pipeline/v1", "name": "bad-edge",
	  "stages": [
	    {"id": "extract", "capability": "extract.entities_relations", "input": {"file": "d.md"}},
	    {"id": "comm", "capability": "detect.communities",
	     "input_from": {"stage": "extract", "as": "document_file"}},
	    {"id": "store", "capability": "store.triples",
	     "input_from": {"stage": "comm", "as": "document_file"}}
	  ]
	}`)
	_, err := Build(def, chainProviders(), policy.Gates{})
	pe, ok := err.(*Error)
	if !ok || pe.Code != protocol.ErrIncompatibleStageEdge {
		t.Fatalf("expected incompatible_stage_edge, got %v", err)
	}
}

// artifact_kind that disagrees with the upstream's declared output kind is
// rejected at plan time.
func TestEdgeKindMismatchRejected(t *testing.T) {
	def := parseDef(t, `{
	  "pipeline": "kg.pipeline/v1", "name": "bad-kind",
	  "stages": [
	    {"id": "extract", "capability": "extract.entities_relations", "input": {"file": "d.md"}},
	    {"id": "dedup", "capability": "resolve.coref",
	     "input_from": {"stage": "extract", "artifact_kind": "chunks", "as": "document_file"}}
	  ]
	}`)
	_, err := Build(def, chainProviders(), policy.Gates{})
	pe, ok := err.(*Error)
	if !ok || pe.Code != protocol.ErrIncompatibleStageEdge {
		t.Fatalf("expected incompatible_stage_edge, got %v", err)
	}
}

// "as" must name a property of the downstream input_schema (closed schema).
func TestEdgeTargetPropertyUnknown(t *testing.T) {
	def := parseDef(t, `{
	  "pipeline": "kg.pipeline/v1", "name": "bad-as",
	  "stages": [
	    {"id": "extract", "capability": "extract.entities_relations", "input": {"file": "d.md"}},
	    {"id": "dedup", "capability": "resolve.coref",
	     "input_from": {"stage": "extract", "as": "nope"}}
	  ]
	}`)
	_, err := Build(def, chainProviders(), policy.Gates{})
	pe, ok := err.(*Error)
	if !ok || pe.Code != protocol.ErrInvalidInput {
		t.Fatalf("expected invalid_input, got %v", err)
	}
}

// Static input violating the downstream input_schema fails at plan time.
func TestStaticInputSchemaChecked(t *testing.T) {
	def := parseDef(t, `{
	  "pipeline": "kg.pipeline/v1", "name": "bad-input",
	  "stages": [
	    {"id": "parse", "capability": "parse.multimodal", "input": {"unknown": 1}}
	  ]
	}`)
	_, err := Build(def, chainProviders(), policy.Gates{})
	pe, ok := err.(*Error)
	if !ok || pe.Code != protocol.ErrInvalidInput {
		t.Fatalf("expected invalid_input, got %v", err)
	}
}

func TestInputFromSingleObjectOrArray(t *testing.T) {
	single := parseDef(t, `{"pipeline":"kg.pipeline/v1","name":"x","stages":[
	  {"id":"a","capability":"c","input_from":{"stage":"b","as":"document_file"}}]}`)
	if len(single.Stages[0].InputFrom) != 1 {
		t.Fatalf("single object input_from should normalize to one edge: %+v", single.Stages[0].InputFrom)
	}
	array := parseDef(t, `{"pipeline":"kg.pipeline/v1","name":"x","stages":[
	  {"id":"a","capability":"c","input_from":[{"stage":"b","as":"document_file"},{"stage":"d","as":"document"}]}]}`)
	if len(array.Stages[0].InputFrom) != 2 {
		t.Fatalf("array input_from should keep both edges: %+v", array.Stages[0].InputFrom)
	}
}

// scriptRunner is a router.Runner fake that behaves like a protocol-native
// provider: it writes a per-capability artifact into dir and answers with
// a kg.execution/v1 envelope.
type scriptRunner struct {
	t       *testing.T
	dir     string
	calls   map[string]int
	failCap string // capability that answers an error envelope
}

func (r *scriptRunner) Run(_ context.Context, _ string, args []string, stdin []byte) ([]byte, []byte, error) {
	capID := args[1]
	r.calls[capID]++
	var req struct {
		Input map[string]any `json:"input"`
	}
	if err := json.Unmarshal(stdin, &req); err != nil {
		return nil, nil, err
	}
	if capID == r.failCap {
		env := protocol.ErrorEnvelope(capID, "fake", "invocation_failed", "boom")
		data, _ := json.Marshal(env)
		return data, nil, nil
	}
	if capID == "store.triples" {
		env := protocol.NewEnvelope(capID, "kg-neo4j-cli")
		env.Status = "ok"
		env.Result = map[string]any{"stored": true, "document_file": req.Input["document_file"]}
		data, _ := json.Marshal(env)
		return data, nil, nil
	}
	kind := map[string]string{
		"parse.multimodal":           "chunks",
		"extract.entities_relations": "kg-document",
		"resolve.coref":              "kg-document",
	}[capID]
	path := filepath.Join(r.dir, capID+".json")
	if err := os.WriteFile(path, []byte(`{"from":"`+capID+`"}`), 0o644); err != nil {
		r.t.Fatal(err)
	}
	env := protocol.NewEnvelope(capID, "fake")
	env.Status = "ok"
	env.Artifacts = []protocol.Artifact{{Path: path, Kind: kind}} // no checksum: hub computes it
	data, _ := json.Marshal(env)
	return data, nil, nil
}

func openGates() policy.Gates {
	return policy.Gates{AllowNetwork: true, AllowDataEgress: true, AllowModelDownload: true, AllowDBWrite: true}
}

func TestExecuteFullChain(t *testing.T) {
	plan, err := Build(parseDef(t, chainDef), chainProviders(), openGates())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	runner := &scriptRunner{t: t, dir: t.TempDir(), calls: map[string]int{}}
	workDir := filepath.Join(t.TempDir(), "work")
	env := Execute(context.Background(), plan, RunOptions{WorkDir: workDir, Gates: openGates(), Runner: runner})
	if env.Status != "ok" {
		t.Fatalf("pipeline failed: %+v", env.Error)
	}
	if len(env.Stages) != 4 {
		t.Fatalf("expected 4 stage results, got %d", len(env.Stages))
	}
	for _, s := range env.Stages {
		if s.Status != "ok" {
			t.Errorf("stage %s status %s: %+v", s.ID, s.Status, s.Error)
		}
	}
	// store received the dedup artifact path (inside the work dir).
	if runner.calls["store.triples"] != 1 {
		t.Errorf("store.triples calls = %d", runner.calls["store.triples"])
	}
	storeRes := env.Stages[3]
	if len(storeRes.Artifacts) != 0 {
		t.Errorf("result-json stage should carry no artifacts, got %+v", storeRes.Artifacts)
	}
	// Every artifact-mode stage produced a work-dir copy; per-stage
	// envelope files exist for resume.
	for _, id := range []string{"parse", "extract", "dedup"} {
		var found bool
		for _, s := range env.Stages {
			if s.ID == id && len(s.Artifacts) == 1 && filepath.Dir(s.Artifacts[0].Path) == workDir && s.Artifacts[0].Checksum != "" {
				found = true
			}
		}
		if !found {
			t.Errorf("stage %s should have one checksummed work-dir artifact: %+v", id, env.Stages)
		}
		if _, err := os.Stat(stageEnvelopePath(workDir, id)); err != nil {
			t.Errorf("stage envelope file missing for %s: %v", id, err)
		}
	}
	if _, err := os.Stat(filepath.Join(workDir, "pipeline.envelope.json")); err != nil {
		t.Errorf("pipeline envelope file missing: %v", err)
	}
}

func TestExecuteGatePrecheckFailsFast(t *testing.T) {
	plan, err := Build(parseDef(t, chainDef), chainProviders(), policy.Gates{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	runner := &scriptRunner{t: t, dir: t.TempDir(), calls: map[string]int{}}
	env := Execute(context.Background(), plan, RunOptions{WorkDir: filepath.Join(t.TempDir(), "w"), Gates: policy.Gates{}, Runner: runner})
	if env.Status != "error" || env.Error == nil || env.Error.Code != protocol.ErrPolicyDenied {
		t.Fatalf("expected policy_denied, got %+v", env.Error)
	}
	if len(env.Stages) != 0 {
		t.Errorf("no stage may execute under gate denial, got %+v", env.Stages)
	}
	if len(runner.calls) != 0 {
		t.Errorf("provider must not start under gate denial, calls = %v", runner.calls)
	}
	for _, want := range []string{"--allow-network", "--allow-data-egress", "--allow-db-write"} {
		if !contains(env.Error.Message, want) {
			t.Errorf("error should name %s: %q", want, env.Error.Message)
		}
	}
}

func contains(hay, needle string) bool { return strings.Contains(hay, needle) }

func TestExecuteOptionalStageSkipped(t *testing.T) {
	plan, err := Build(parseDef(t, chainDef), chainProviders(), openGates())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	runner := &scriptRunner{t: t, dir: t.TempDir(), calls: map[string]int{}, failCap: "parse.multimodal"}
	env := Execute(context.Background(), plan, RunOptions{WorkDir: filepath.Join(t.TempDir(), "w"), Gates: openGates(), Runner: runner})
	if env.Status != "ok" {
		t.Fatalf("optional failure must not fail the pipeline: %+v", env.Error)
	}
	if env.Stages[0].Status != "skipped" {
		t.Errorf("parse should be skipped, got %s", env.Stages[0].Status)
	}
	found := false
	for _, d := range env.Diagnostics {
		if d.Severity == "warning" && contains(d.Message, "optional stage \"parse\" failed, skipping") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected optional-skip diagnostic, got %+v", env.Diagnostics)
	}
}

func TestExecuteRequiredFailureAborts(t *testing.T) {
	plan, err := Build(parseDef(t, chainDef), chainProviders(), openGates())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	runner := &scriptRunner{t: t, dir: t.TempDir(), calls: map[string]int{}, failCap: "resolve.coref"}
	env := Execute(context.Background(), plan, RunOptions{WorkDir: filepath.Join(t.TempDir(), "w"), Gates: openGates(), Runner: runner})
	if env.Status != "error" {
		t.Fatalf("required stage failure must fail the pipeline")
	}
	if len(env.Stages) != 3 {
		t.Fatalf("pipeline must abort at the failed stage, got %d stage results", len(env.Stages))
	}
	if env.Stages[0].Status != "ok" || env.Stages[1].Status != "ok" || env.Stages[2].Status != "error" {
		t.Errorf("stage statuses: %+v", env.Stages)
	}
	if runner.calls["store.triples"] != 0 {
		t.Errorf("store must not run after upstream failure")
	}
}

func TestResumeSkipsCompletedStages(t *testing.T) {
	plan, err := Build(parseDef(t, chainDef), chainProviders(), openGates())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	workDir := filepath.Join(t.TempDir(), "work")
	runner := &scriptRunner{t: t, dir: t.TempDir(), calls: map[string]int{}}
	env := Execute(context.Background(), plan, RunOptions{WorkDir: workDir, Gates: openGates(), Runner: runner})
	if env.Status != "ok" {
		t.Fatalf("first run failed: %+v", env.Error)
	}
	firstCalls := runner.calls["extract.entities_relations"]

	// Second run against the same work dir: nothing executes again.
	plan2, err := Build(parseDef(t, chainDef), chainProviders(), openGates())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	env2 := Execute(context.Background(), plan2, RunOptions{Resume: workDir, Gates: openGates(), Runner: runner})
	if env2.Status != "ok" {
		t.Fatalf("resume run failed: %+v", env2.Error)
	}
	if runner.calls["extract.entities_relations"] != firstCalls {
		t.Errorf("resume must skip completed stages, extract calls = %d (first run %d)",
			runner.calls["extract.entities_relations"], firstCalls)
	}
	reused := 0
	for _, d := range env2.Diagnostics {
		if contains(d.Message, "reused from") {
			reused++
		}
	}
	if reused != 4 {
		t.Errorf("expected 4 reuse diagnostics, got %d: %+v", reused, env2.Diagnostics)
	}

	// Corrupt one artifact: its stage (and only downstream-consistent
	// behavior) re-executes on the next resume.
	plan3, _ := Build(parseDef(t, chainDef), chainProviders(), openGates())
	arts := env2.Stages[1].Artifacts // extract
	if err := os.WriteFile(arts[0].Path, []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	env3 := Execute(context.Background(), plan3, RunOptions{Resume: workDir, Gates: openGates(), Runner: runner})
	if env3.Status != "ok" {
		t.Fatalf("resume after tamper failed: %+v", env3.Error)
	}
	if runner.calls["extract.entities_relations"] != firstCalls+1 {
		t.Errorf("tampered artifact must force re-execution of extract, calls = %d", runner.calls["extract.entities_relations"])
	}
}

func TestRenderDryRun(t *testing.T) {
	plan, err := Build(parseDef(t, chainDef), chainProviders(), policy.Gates{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	env := RenderDryRun(plan)
	if !env.DryRun || env.Status != "ok" {
		t.Fatalf("dry-run envelope: %+v", env)
	}
	if len(env.Stages) != 4 {
		t.Fatalf("expected 4 planned stages, got %d", len(env.Stages))
	}
	for _, s := range env.Stages {
		if s.Status != "planned" || s.Provider == "" || s.Capability == "" {
			t.Errorf("planned stage: %+v", s)
		}
	}
	dedup := env.Stages[2]
	if dedup.Input["document_file"] != "kg-pipeline://extract/kg-document" {
		t.Errorf("dedup input placeholder: %v", dedup.Input)
	}
	if len(env.Diagnostics) == 0 {
		t.Errorf("closed gates should surface a warning diagnostic")
	}
}
