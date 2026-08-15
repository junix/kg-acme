package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	coreprotocol "github.com/junix/acme-core/protocol"

	"kg-acme/internal/surface"
)

type listOptions struct {
	Prefix    string
	PrefixSet bool
	Level     int
	LevelSet  bool
	Tree      bool
}
type listItem struct {
	CapabilityID string `json:"capability_id"`
	Description  string `json:"description"`
	Kind         string `json:"kind"`
	Available    bool   `json:"available"`
}

func (r Runner) rootHelp(snapshot surface.Snapshot) {
	fmt.Fprintln(r.Stdout, `kg — execute installed knowledge-graph capabilities

Usage:
  kg <capability-id> [positionals] [options]
  kg <capability-id-or-group> --describe
  kg list [--prefix PREFIX] [--level N] [--tree] [--json]

Global options:
  --params <JSON|@FILE>        Read one complete argument object.
  -o, --output <PATH>          Write a new output artifact.
  --dry-run                    Validate and plan without provider/model/file/network activity.
  --allow-network              Allow declared network access.
  --allow-data-egress          Allow declared data egress.
  --allow-model-download       Allow the selected capability to download missing weights.
  --allow-db-write             Allow declared graph-database writes.
  --json                       Emit one versioned JSON result.

Available capabilities:`)
	r.printCapabilities(snapshot.Capabilities)
	fmt.Fprintln(r.Stdout, "\nControl plane: kgctl --help")
}

func (r Runner) missingSnapshotHelp(path string) {
	fmt.Fprintf(r.Stdout, "kg — execute installed knowledge-graph capabilities\n\nNo capability snapshot is installed.\nRun: kgctl refresh\nSnapshot: %s\n", path)
}

func (r Runner) printCapabilities(values []surface.Capability) {
	w := tabwriter.NewWriter(r.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "CAPABILITY ID\tDESCRIPTION")
	for _, value := range values {
		if value.Available {
			fmt.Fprintf(w, "%s\t%s\n", surface.PublicID(value), oneLine(value.Description))
		}
	}
	_ = w.Flush()
}

func (r Runner) describeCapability(value surface.Capability) error {
	return writeJSON(r.Stdout, publicDescription(value))
}

func (r Runner) describeGroup(snapshot surface.Snapshot, reference string) error {
	if !validPrefix(reference) {
		return fmt.Errorf("invalid dotted capability prefix: %s", reference)
	}
	found := false
	for _, group := range snapshot.Groups {
		if group.ID == reference {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("unknown capability or group: %s", reference)
	}
	var result []map[string]any
	for _, value := range snapshot.Capabilities {
		if value.Available && matchesPrefix(surface.PublicID(value), reference) {
			result = append(result, publicDescription(value))
		}
	}
	return writeJSON(r.Stdout, result)
}

func publicDescription(value surface.Capability) map[string]any {
	return map[string]any{
		"schema_version": "kg.capability-description/v3", "capability_id": surface.PublicID(value), "title": value.Title, "description": value.Description,
		"input_schema": json.RawMessage(value.InputSchema), "output_schema": json.RawMessage(value.OutputSchema), "output": value.Output,
		"side_effects": candidateEffects(value), "source": value.Source,
		"error_contract": map[string]any{"schema_version": "kg.error/v1", "exit_codes": []map[string]any{{"exit_code": 0, "name": "ok"}, {"exit_code": 1, "name": "error"}}},
	}
}

func candidateEffects(value surface.Capability) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range value.Candidates {
		for _, effect := range c.SideEffects {
			if !seen[effect] {
				seen[effect] = true
				out = append(out, effect)
			}
		}
	}
	sort.Strings(out)
	return out
}

func (r Runner) capabilityHelp(value surface.Capability) {
	id := surface.PublicID(value)
	fmt.Fprintf(r.Stdout, "kg %s — %s\n\n%s\n\nUsage:\n  kg %s", id, value.Title, value.Description, id)
	for _, positional := range value.CLISpec.Positionals {
		fmt.Fprintf(r.Stdout, " <%s>", strings.ToUpper(positional.Name))
	}
	fmt.Fprintln(r.Stdout, " [options]")
	var schema struct {
		Properties map[string]struct {
			Description string `json:"description"`
		} `json:"properties"`
	}
	_ = json.Unmarshal(value.InputSchema, &schema)
	if len(value.CLISpec.Positionals) > 0 {
		fmt.Fprintln(r.Stdout, "\nArguments:")
		for _, positional := range value.CLISpec.Positionals {
			fmt.Fprintf(r.Stdout, "  %-27s %s\n", strings.ToUpper(positional.Name), oneLine(schema.Properties[positional.Name].Description))
		}
	}
	fmt.Fprintln(r.Stdout, "\nOptions:")
	fmt.Fprintln(r.Stdout, "  --params <JSON|@FILE>        Read the complete argument object.")
	ordered := make([]int, len(value.CLISpec.Flags))
	for index := range ordered {
		ordered[index] = index
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		left, right := value.CLISpec.Flags[ordered[i]], value.CLISpec.Flags[ordered[j]]
		if left.Order != right.Order {
			return left.Order < right.Order
		}
		return left.Flag < right.Flag
	})
	for _, index := range ordered {
		flag := value.CLISpec.Flags[index]
		kind := strings.ToUpper(flag.Kind)
		label := flag.Flag
		if flag.Kind != "boolean" {
			label += " <" + kind + ">"
		}
		fmt.Fprintf(r.Stdout, "  %-27s %s\n", label, oneLine(schema.Properties[flag.Name].Description))
	}
	fmt.Fprintln(r.Stdout, "  -o, --output <PATH>          Write a new output artifact.")
	fmt.Fprintln(r.Stdout, "  --describe                   Emit the machine-readable capability contract.")
	fmt.Fprintln(r.Stdout, "  --dry-run                    Validate without starting providers or models.")
	fmt.Fprintln(r.Stdout, "  --allow-network              Allow declared network access.")
	fmt.Fprintln(r.Stdout, "  --allow-data-egress          Allow declared data egress.")
	fmt.Fprintln(r.Stdout, "  --allow-model-download       Allow selected model downloads.")
	fmt.Fprintln(r.Stdout, "  --allow-db-write             Allow declared graph-database writes.")
	fmt.Fprintln(r.Stdout, "  --json                       Emit one versioned JSON result.")
	fmt.Fprintf(r.Stdout, "\nExamples:\n  kg %s", id)
	for _, positional := range value.CLISpec.Positionals {
		fmt.Fprintf(r.Stdout, " <%s>", strings.ToUpper(positional.Name))
	}
	fmt.Fprintln(r.Stdout, " --json")
	fmt.Fprintf(r.Stdout, "  kg %s --params @request.json --json\n", id)
	fmt.Fprintf(r.Stdout, "\nSource:\n  Integration: %s\n", value.Source.IntegrationPath)
	for _, path := range value.Source.ImplementationPaths {
		fmt.Fprintf(r.Stdout, "  Implementation: %s\n", path)
	}
}

func (r Runner) list(snapshot surface.Snapshot, opts options, arguments []string) error {
	if opts.Params != "" || opts.Output != "" || opts.DryRun || opts.Describe || opts.Gates != (policyZero()) {
		return fmt.Errorf("kg list accepts only --prefix, --level, --tree, and --json")
	}
	if len(arguments) == 1 && isHelp(arguments[0]) {
		fmt.Fprint(r.Stdout, `kg list — discover available knowledge-graph capabilities

Usage:
  kg list [--prefix PREFIX] [--level N] [--tree] [--json]

Options:
  --prefix <DOTTED-PREFIX>  Select one segment-aware capability subtree.
  --level <N>               Reveal N relative levels; 0 means all levels.
  --tree                    Render the selected capabilities as a description tree.
  --json                    Emit the stable list object instead of a table/tree.
`)
		return nil
	}
	parsed, err := parseListOptions(arguments)
	if err != nil {
		return err
	}
	if parsed.LevelSet && parsed.Level == 0 {
		parsed.Level = maxDepth(snapshot.Capabilities)
	}
	if !parsed.LevelSet {
		if parsed.PrefixSet && !parsed.Tree {
			parsed.Level = 1
		} else {
			parsed.Level = maxDepth(snapshot.Capabilities)
		}
	}
	items, err := projectList(snapshot, parsed)
	if err != nil {
		return err
	}
	if opts.JSON {
		return writeJSON(r.Stdout, map[string]any{"schema_version": "kg.capability-list/v1", "items": items})
	}
	if parsed.Tree {
		fmt.Fprintln(r.Stdout, renderTree(items, parsed, snapshot.Groups))
		return nil
	}
	w := tabwriter.NewWriter(r.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "CAPABILITY ID\tDESCRIPTION")
	for _, item := range items {
		fmt.Fprintf(w, "%s\t%s\n", item.CapabilityID, oneLine(item.Description))
	}
	return w.Flush()
}

func parseListOptions(arguments []string) (listOptions, error) {
	var result listOptions
	for index := 0; index < len(arguments); index++ {
		arg := arguments[index]
		switch {
		case arg == "--tree":
			result.Tree = true
		case arg == "--prefix":
			if index+1 >= len(arguments) {
				return result, fmt.Errorf("--prefix requires a value")
			}
			index++
			result.Prefix, result.PrefixSet = arguments[index], true
		case strings.HasPrefix(arg, "--prefix="):
			result.Prefix, result.PrefixSet = strings.TrimPrefix(arg, "--prefix="), true
		case arg == "--level":
			if index+1 >= len(arguments) {
				return result, fmt.Errorf("--level requires a value")
			}
			index++
			level, err := strconv.Atoi(arguments[index])
			if err != nil || level < 0 {
				return result, fmt.Errorf("--level must be non-negative; 0 means all levels")
			}
			result.Level, result.LevelSet = level, true
		case strings.HasPrefix(arg, "--level="):
			level, err := strconv.Atoi(strings.TrimPrefix(arg, "--level="))
			if err != nil || level < 0 {
				return result, fmt.Errorf("--level must be non-negative; 0 means all levels")
			}
			result.Level, result.LevelSet = level, true
		default:
			return result, fmt.Errorf("unknown kg list option: %s", arg)
		}
	}
	if result.PrefixSet && !validPrefix(result.Prefix) {
		return result, fmt.Errorf("invalid dotted capability prefix: %s", result.Prefix)
	}
	return result, nil
}

func projectList(snapshot surface.Snapshot, opts listOptions) ([]listItem, error) {
	groups := map[string]string{}
	for _, group := range snapshot.Groups {
		groups[group.ID] = group.Description
	}
	items := map[string]listItem{}
	matched := false
	prefixDepth := 0
	if opts.PrefixSet {
		prefixDepth = len(strings.Split(opts.Prefix, "."))
	}
	for _, value := range snapshot.Capabilities {
		if !value.Available {
			continue
		}
		id := surface.PublicID(value)
		if opts.PrefixSet && !matchesPrefix(id, opts.Prefix) {
			continue
		}
		matched = true
		if id == opts.Prefix || len(strings.Split(id, "."))-prefixDepth <= opts.Level {
			items[id] = listItem{id, value.Description, "capability", true}
			continue
		}
		parts := strings.Split(id, ".")
		groupID := strings.Join(parts[:prefixDepth+opts.Level], ".")
		description, ok := groups[groupID]
		if !ok {
			return nil, fmt.Errorf("snapshot group missing: %s", groupID)
		}
		items[groupID] = listItem{groupID, description, "group", true}
	}
	if !matched {
		return nil, fmt.Errorf("unknown or empty capability prefix: %s", opts.Prefix)
	}
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]listItem, 0, len(keys))
	for _, key := range keys {
		result = append(result, items[key])
	}
	return result, nil
}

type treeNode struct {
	Description string
	Children    map[string]*treeNode
}

func renderTree(items []listItem, opts listOptions, groups []coreprotocol.CapabilityGroup) string {
	root := &treeNode{Children: map[string]*treeNode{}}
	anchor := []string{}
	lines := []string{"Knowledge-graph capabilities"}
	groupDescriptions := map[string]string{}
	for _, group := range groups {
		groupDescriptions[group.ID] = group.Description
	}
	if opts.PrefixSet {
		anchor = strings.Split(opts.Prefix, ".")
		anchorLine := opts.Prefix
		if description := groupDescriptions[opts.Prefix]; description != "" {
			anchorLine += " — " + oneLine(description)
		}
		lines = []string{"Knowledge-graph capabilities under " + opts.Prefix, anchorLine}
	}
	for _, item := range items {
		parts := strings.Split(item.CapabilityID, ".")
		node := root
		for index, part := range parts[len(anchor):] {
			if node.Children[part] == nil {
				node.Children[part] = &treeNode{Children: map[string]*treeNode{}}
			}
			node = node.Children[part]
			fullID := strings.Join(parts[:len(anchor)+index+1], ".")
			if description := groupDescriptions[fullID]; description != "" {
				node.Description = description
			}
		}
		node.Description = item.Description
	}
	var walk func(*treeNode, string)
	walk = func(node *treeNode, indent string) {
		keys := make([]string, 0, len(node.Children))
		for key := range node.Children {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for index, key := range keys {
			branch := "├── "
			next := indent + "│   "
			if index == len(keys)-1 {
				branch = "└── "
				next = indent + "    "
			}
			child := node.Children[key]
			line := indent + branch + key
			if child.Description != "" {
				line += " — " + oneLine(child.Description)
			}
			lines = append(lines, line)
			walk(child, next)
		}
	}
	walk(root, "")
	return strings.Join(lines, "\n")
}

func maxDepth(values []surface.Capability) int {
	depth := 1
	for _, value := range values {
		if n := len(strings.Split(surface.PublicID(value), ".")); n > depth {
			depth = n
		}
	}
	return depth
}
func matchesPrefix(id, prefix string) bool { return id == prefix || strings.HasPrefix(id, prefix+".") }
func validPrefix(value string) bool {
	if value == "" || strings.HasPrefix(value, "kg.") {
		return false
	}
	for _, segment := range strings.Split(value, ".") {
		if segment == "" {
			return false
		}
		for _, char := range segment {
			if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '-') {
				return false
			}
		}
	}
	return true
}
func oneLine(value string) string { return strings.Join(strings.Fields(value), " ") }
