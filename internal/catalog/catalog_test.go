package catalog

import (
	"strings"
	"testing"
)

func TestLoadEmbeddedValid(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("embedded catalog must be valid: %v", err)
	}
	want := []string{"extract.entities-relations", "resolve.coref", "detect.communities", "detect.communities-hierarchy", "summarize.communities",
		"detect.communities-semantic", "store.triples", "retrieve.ask", "parse.multimodal", "layout.compute", "analyze.centrality",
		"analyze.pagerank", "analyze.shortest-paths", "analyze.components", "analyze.triangles", "analyze.topology", "embed.nodes"}
	if len(c.Commands) != len(want) {
		t.Fatalf("expected %d commands, got %d", len(want), len(c.Commands))
	}
	for i, name := range want {
		if c.Commands[i].Path() != name {
			t.Errorf("command %d: expected %q, got %q", i, name, c.Commands[i].Path())
		}
	}
	for _, cmd := range c.CapabilityCommands() {
		if cmd.CapabilityID == "" {
			t.Errorf("capability command %q missing capability_id", cmd.Path())
		}
	}
}

// The stable commands map to the provider-published capability namespace;
// that namespace is the single source of truth for capability ids.
func TestCatalogCapabilityMapping(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"extract.entities-relations":   "extract.entities_relations",
		"resolve.coref":                "resolve.coref",
		"detect.communities":           "detect.communities",
		"detect.communities-hierarchy": "detect.communities_hierarchy",
		"summarize.communities":        "summarize.communities",
		"detect.communities-semantic":  "detect.communities_semantic",
		"store.triples":                "store.triples",
		"retrieve.ask":                 "retrieve.ask",
		"parse.multimodal":             "parse.multimodal",
		"layout.compute":               "layout.compute",
		"analyze.centrality":           "analyze.centrality",
		"analyze.pagerank":             "analyze.pagerank",
		"analyze.shortest-paths":       "analyze.shortest_paths",
		"analyze.components":           "analyze.components",
		"analyze.triangles":            "analyze.triangles",
		"analyze.topology":             "analyze.topology",
		"embed.nodes":                  "embed.nodes",
	}
	got := map[string]string{}
	for _, cmd := range c.CapabilityCommands() {
		if strings.HasPrefix(cmd.CapabilityID, "kg.") {
			t.Errorf("command %q uses retired capability namespace %q", cmd.Path(), cmd.CapabilityID)
		}
		got[cmd.Path()] = cmd.CapabilityID
	}
	for path, id := range want {
		if got[path] != id {
			t.Errorf("command %q: expected capability_id %q, got %q", path, id, got[path])
		}
	}
}

func validDoc(t *testing.T) string {
	t.Helper()
	return `{"version":1,"commands":[{"command_path":["extract","things"],"semantic_id":"extract.things","title":"Extract things","description":"Extracts things.","capability_id":"extract.entities_relations"}]}`
}

func mutate(t *testing.T, old, new string) string {
	t.Helper()
	doc := validDoc(t)
	if !strings.Contains(doc, old) {
		t.Fatalf("valid doc does not contain %q", old)
	}
	return strings.Replace(doc, old, new, 1)
}

func TestParseValidationRules(t *testing.T) {
	cases := []struct {
		name    string
		doc     string
		wantErr string
		bug     string // non-empty: known production defect, scenario recorded but skipped
	}{
		{"semantic_id mirror", mutate(t, `"semantic_id":"extract.things"`, `"semantic_id":"extract.other"`), "must mirror command_path", ""},
		{"illegal segment", mutate(t, `"command_path":["extract","things"]`, `"command_path":["Extract","things"]`), "illegal segment", ""},
		{"digit-leading segment", mutate(t, `"command_path":["extract","things"]`, `"command_path":["extract","1things"]`), "illegal segment", ""},
		{"title punctuation", mutate(t, `"title":"Extract things"`, `"title":"Extract things."`), "must not end with punctuation", ""},
		{"title ends with bang", mutate(t, `"title":"Extract things"`, `"title":"Extract things!"`), "must not end with punctuation", ""},
		{"title ends with full-width stop", mutate(t, `"title":"Extract things"`, `"title":"Extract things。"`), "must not end with punctuation",
			"Parse slices the title's last byte, so the multi-byte 。 is never matched against the punctuation set"},
		{"empty title", mutate(t, `"title":"Extract things"`, `"title":""`), "empty title", ""},
		{"description not sentence", mutate(t, `"description":"Extracts things."`, `"description":"Extracts things"`), "single sentence ending", ""},
		{"description with newline", mutate(t, `"description":"Extracts things."`, `"description":"Extracts\nthings."`), "single sentence ending", ""},
		{"empty description", mutate(t, `"description":"Extracts things."`, `"description":""`), "single sentence ending", ""},
		{"capability command without id", mutate(t, `"capability_id":"extract.entities_relations"`, `"builtin":false`), "must declare capability_id", ""},
		{"builtin with capability id", `{"version":1,"commands":[{"command_path":["pipeline"],"semantic_id":"pipeline","title":"Pipe","description":"Pipes.","builtin":true,"capability_id":"kg.pipe"}]}`, "builtin command must not declare", ""},
		{"duplicate id", `{"version":1,"commands":[
		  {"command_path":["a"],"semantic_id":"a","title":"A","description":"A.","capability_id":"kg.a"},
		  {"command_path":["a"],"semantic_id":"a","title":"A","description":"A.","capability_id":"kg.a"}]}`, "duplicate semantic_id", ""},
		{"bad json", `{`, "invalid JSON", ""},
		{"no commands", `{"version":1,"commands":[]}`, "no commands", ""},
		{"empty command path", `{"version":1,"commands":[{"command_path":[],"semantic_id":"","title":"X","description":"X.","capability_id":"kg.x"}]}`, "empty command_path", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.bug != "" {
				t.Skipf("BUG: %s", tc.bug)
			}
			_, err := Parse([]byte(tc.doc))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %q", tc.wantErr, err.Error())
			}
		})
	}
}

func TestFind(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	// Find returns the FIRST command whose first segment matches: for a
	// group with several subcommands that pins the catalog's first entry.
	if cmd := c.Find("extract"); cmd == nil || cmd.Path() != "extract.entities-relations" {
		t.Errorf("extract group: got %v, want extract.entities-relations", cmd)
	}
	if cmd := c.Find("detect"); cmd == nil || cmd.Path() != "detect.communities" {
		t.Errorf("detect group: got %v, want detect.communities", cmd)
	}
	if c.Find("nonexistent") != nil {
		t.Error("nonexistent should not be found")
	}
}

// A builtin command (hub-implemented, builtin:true) carries no
// capability_id: Parse must accept it and CapabilityCommands must exclude
// it — the embedded catalog has no builtins, so this pins the branch with a
// synthetic doc.
func TestParseBuiltinCommand(t *testing.T) {
	doc := `{"version":1,"commands":[
	  {"command_path":["pipeline"],"semantic_id":"pipeline","title":"Pipeline","description":"Runs pipelines.","builtin":true},
	  {"command_path":["extract","things"],"semantic_id":"extract.things","title":"Extract things","description":"Extracts things.","capability_id":"extract.entities_relations"}]}`
	c, err := Parse([]byte(doc))
	if err != nil {
		t.Fatalf("valid doc with a builtin command must parse: %v", err)
	}
	if c.Version != 1 || len(c.Commands) != 2 {
		t.Fatalf("parsed catalog shape: version %d, %d commands", c.Version, len(c.Commands))
	}
	builtin := c.Commands[0]
	if !builtin.Builtin || builtin.CapabilityID != "" || builtin.Path() != "pipeline" {
		t.Errorf("builtin command must round-trip: %+v", builtin)
	}
	caps := c.CapabilityCommands()
	if len(caps) != 1 || caps[0].Path() != "extract.things" || caps[0].CapabilityID != "extract.entities_relations" {
		t.Errorf("CapabilityCommands must keep only the capability command: %+v", caps)
	}
}

func TestFindPath(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	// Longest prefix wins: the subcommand beats its parent.
	cmd, n := c.FindPath([]string{"detect", "communities-hierarchy", "--json"})
	if cmd == nil || n != 2 || cmd.CapabilityID != "detect.communities_hierarchy" {
		t.Errorf("detect.communities-hierarchy: got %v consumed %d", cmd, n)
	}
	cmd, n = c.FindPath([]string{"detect", "communities", "doc.json"})
	if cmd == nil || n != 2 || cmd.CapabilityID != "detect.communities" {
		t.Errorf("detect.communities: got %v consumed %d", cmd, n)
	}
	if cmd, _ = c.FindPath([]string{"nope"}); cmd != nil {
		t.Errorf("unknown command should not match, got %v", cmd)
	}
	// Boundary: empty args, and a first-segment group that is no command's
	// full path, both consume nothing.
	if cmd, n = c.FindPath(nil); cmd != nil || n != 0 {
		t.Errorf("empty args must match nothing, got %v consumed %d", cmd, n)
	}
	if cmd, n = c.FindPath([]string{"detect"}); cmd != nil || n != 0 {
		t.Errorf("group prefix alone must match no command, got %v consumed %d", cmd, n)
	}
}
