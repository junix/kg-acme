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
	want := []string{"extract", "dedup", "communities", "communities hierarchy", "communities summaries",
		"store", "ask", "parse", "provider", "pipeline"}
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
		"extract":               "extract.entities_relations",
		"dedup":                 "resolve.coref",
		"communities":           "detect.communities",
		"communities hierarchy": "detect.communities_hierarchy",
		"communities summaries": "summarize.communities",
		"store":                 "store.triples",
		"ask":                   "retrieve.ask",
		"parse":                 "parse.multimodal",
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
	return `{"version":1,"commands":[{"command_path":["extract"],"semantic_id":"extract","title":"Extract things","description":"Extracts things.","capability_id":"extract.entities_relations"}]}`
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
	}{
		{"semantic_id mirror", mutate(t, `"semantic_id":"extract"`, `"semantic_id":"ext"`), "must mirror command_path"},
		{"illegal segment", mutate(t, `"command_path":["extract"]`, `"command_path":["Extract"]`), "illegal segment"},
		{"title punctuation", mutate(t, `"title":"Extract things"`, `"title":"Extract things."`), "must not end with punctuation"},
		{"empty title", mutate(t, `"title":"Extract things"`, `"title":""`), "empty title"},
		{"description not sentence", mutate(t, `"description":"Extracts things."`, `"description":"Extracts things"`), "single sentence ending"},
		{"capability command without id", mutate(t, `"capability_id":"extract.entities_relations"`, `"builtin":false`), "must declare capability_id"},
		{"builtin with capability id", `{"version":1,"commands":[{"command_path":["pipeline"],"semantic_id":"pipeline","title":"Pipe","description":"Pipes.","builtin":true,"capability_id":"kg.pipe"}]}`, "builtin command must not declare"},
		{"duplicate id", `{"version":1,"commands":[
		  {"command_path":["a"],"semantic_id":"a","title":"A","description":"A.","capability_id":"kg.a"},
		  {"command_path":["a"],"semantic_id":"a","title":"A","description":"A.","capability_id":"kg.a"}]}`, "duplicate semantic_id"},
		{"bad json", `{`, "invalid JSON"},
		{"no commands", `{"version":1,"commands":[]}`, "no commands"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
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
	if c.Find("extract") == nil {
		t.Error("extract should be found")
	}
	if c.Find("nonexistent") != nil {
		t.Error("nonexistent should not be found")
	}
}

func TestFindPath(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	// Longest prefix wins: the subcommand beats its parent.
	cmd, n := c.FindPath([]string{"communities", "hierarchy", "--json"})
	if cmd == nil || n != 2 || cmd.CapabilityID != "detect.communities_hierarchy" {
		t.Errorf("communities hierarchy: got %v consumed %d", cmd, n)
	}
	cmd, n = c.FindPath([]string{"communities", "doc.json"})
	if cmd == nil || n != 1 || cmd.CapabilityID != "detect.communities" {
		t.Errorf("communities: got %v consumed %d", cmd, n)
	}
	if cmd, _ = c.FindPath([]string{"nope"}); cmd != nil {
		t.Errorf("unknown command should not match, got %v", cmd)
	}
}
