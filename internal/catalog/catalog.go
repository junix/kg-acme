// Package catalog loads and validates the hub's stable command catalog.
//
// The catalog declares the hub's stable command surface (extract, dedup,
// communities [hierarchy|summaries], store, ask, parse, provider, pipeline).
// Each capability command maps to a provider capability_id in the
// provider-published namespace (extract.*/detect.*/summarize.*/resolve.*/
// store.*/retrieve.*/parse.*); builtin commands are implemented by the hub
// itself. The catalog never declares provider flags or enums — those come
// from provider self-description.
package catalog

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

//go:embed catalog.json
var embedded []byte

// Catalog is the parsed command catalog.
type Catalog struct {
	Version  int       `json:"version"`
	Commands []Command `json:"commands"`
}

// Command is one stable command entry.
type Command struct {
	CommandPath  []string `json:"command_path"`
	SemanticID   string   `json:"semantic_id"`
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	CapabilityID string   `json:"capability_id,omitempty"`
	Builtin      bool     `json:"builtin,omitempty"`
	Stub         bool     `json:"stub,omitempty"`
}

// Path renders the command path as "extract" / "provider" etc.
func (c Command) Path() string { return strings.Join(c.CommandPath, " ") }

var segmentRe = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Load parses and validates the embedded catalog.
func Load() (*Catalog, error) { return Parse(embedded) }

// Parse parses and validates catalog JSON. Validation rules:
//   - semantic_id must mirror command_path (segments joined by spaces);
//   - every path segment must be a lowercase kebab-case word;
//   - title must be non-empty and carry no ending punctuation;
//   - description must be a single sentence ending in a period;
//   - capability commands must declare a capability_id, builtins must not.
func Parse(data []byte) (*Catalog, error) {
	var c Catalog
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("catalog: invalid JSON: %w", err)
	}
	if len(c.Commands) == 0 {
		return nil, fmt.Errorf("catalog: no commands")
	}
	seen := map[string]bool{}
	for i, cmd := range c.Commands {
		where := fmt.Sprintf("command %d (%q)", i, cmd.SemanticID)
		if len(cmd.CommandPath) == 0 {
			return nil, fmt.Errorf("catalog: %s: empty command_path", where)
		}
		for _, seg := range cmd.CommandPath {
			if !segmentRe.MatchString(seg) {
				return nil, fmt.Errorf("catalog: %s: illegal segment %q", where, seg)
			}
		}
		if cmd.SemanticID != cmd.Path() {
			return nil, fmt.Errorf("catalog: %s: semantic_id %q must mirror command_path %q",
				where, cmd.SemanticID, cmd.Path())
		}
		if seen[cmd.SemanticID] {
			return nil, fmt.Errorf("catalog: %s: duplicate semantic_id", where)
		}
		seen[cmd.SemanticID] = true
		if cmd.Title == "" {
			return nil, fmt.Errorf("catalog: %s: empty title", where)
		}
		if strings.ContainsAny(cmd.Title[len(cmd.Title)-1:], ".!?。！？") {
			return nil, fmt.Errorf("catalog: %s: title %q must not end with punctuation", where, cmd.Title)
		}
		if cmd.Description == "" || !strings.HasSuffix(cmd.Description, ".") || strings.ContainsAny(cmd.Description, "\n") {
			return nil, fmt.Errorf("catalog: %s: description %q must be a single sentence ending in '.'", where, cmd.Description)
		}
		if cmd.Builtin && cmd.CapabilityID != "" {
			return nil, fmt.Errorf("catalog: %s: builtin command must not declare capability_id", where)
		}
		if !cmd.Builtin && cmd.CapabilityID == "" {
			return nil, fmt.Errorf("catalog: %s: capability command must declare capability_id", where)
		}
	}
	return &c, nil
}

// Find returns the command whose first path segment matches name.
func (c *Catalog) Find(name string) *Command {
	for i := range c.Commands {
		if len(c.Commands[i].CommandPath) > 0 && c.Commands[i].CommandPath[0] == name {
			return &c.Commands[i]
		}
	}
	return nil
}

// FindPath returns the command whose command_path is the longest prefix of
// args, plus the number of args consumed. Multi-segment commands
// (e.g. "communities hierarchy") win over their first-segment parent.
func (c *Catalog) FindPath(args []string) (*Command, int) {
	var best *Command
	bestLen := 0
	for i := range c.Commands {
		path := c.Commands[i].CommandPath
		if len(path) == 0 || len(path) > len(args) || len(path) <= bestLen {
			continue
		}
		match := true
		for j, seg := range path {
			if args[j] != seg {
				match = false
				break
			}
		}
		if match {
			best = &c.Commands[i]
			bestLen = len(path)
		}
	}
	return best, bestLen
}

// CapabilityCommands returns all non-builtin commands.
func (c *Catalog) CapabilityCommands() []Command {
	var out []Command
	for _, cmd := range c.Commands {
		if !cmd.Builtin {
			out = append(out, cmd)
		}
	}
	return out
}
