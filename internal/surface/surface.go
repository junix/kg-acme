// Package surface builds the immutable public capability inventory from
// provider self-descriptions. It contains integration metadata only; KG
// algorithms remain in provider projects.
package surface

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	coreprotocol "github.com/junix/acme-core/protocol"

	"kg-acme/internal/catalog"
	"kg-acme/internal/protocol"
	"kg-acme/internal/router"
)

const SnapshotSchema = "kg.snapshot/v3"

type Candidate struct {
	ProviderID         string   `json:"provider_id"`
	ProviderCapability string   `json:"provider_capability"`
	ProviderPath       string   `json:"provider_path"`
	ImplementationPath string   `json:"implementation_path,omitempty"`
	Available          bool     `json:"available"`
	Probed             bool     `json:"probed"`
	Weight             float64  `json:"weight"`
	SideEffects        []string `json:"side_effects,omitempty"`
}

type Source struct {
	IntegrationPath     string   `json:"integration_path"`
	ImplementationPaths []string `json:"implementation_paths"`
}

type Capability struct {
	SemanticID  string              `json:"semantic_id"`
	Title       string              `json:"title"`
	Description string              `json:"description"`
	Available   bool                `json:"available"`
	InputSchema json.RawMessage     `json:"input_schema"`
	Output      protocol.OutputSpec `json:"output"`
	CLISpec     protocol.CLISpec    `json:"cli_spec"`
	Candidates  []Candidate         `json:"candidates"`
	Source      Source              `json:"source"`
}

type Snapshot struct {
	SchemaVersion string                         `json:"schema_version"`
	Fingerprint   string                         `json:"fingerprint"`
	CreatedAt     time.Time                      `json:"created_at"`
	Providers     []router.Provider              `json:"providers"`
	Groups        []coreprotocol.CapabilityGroup `json:"groups"`
	Capabilities  []Capability                   `json:"capabilities"`
}

func Build(providers []router.Provider) Snapshot {
	cat, _ := catalog.Load()
	byProviderCapability := map[string]catalog.Command{}
	if cat != nil {
		for _, command := range cat.CapabilityCommands() {
			byProviderCapability[command.CapabilityID] = command
		}
	}
	integration := integrationPath()
	byID := map[string]*Capability{}
	for _, provider := range providers {
		for _, item := range providerCapabilities(provider) {
			publicID := normalizeID(item.CapabilityID)
			semanticID := "kg." + publicID
			view := byID[semanticID]
			if view == nil {
				title, description := item.Title, item.Description
				if command, ok := byProviderCapability[item.CapabilityID]; ok {
					if title == "" {
						title = command.Title
					}
					if description == "" {
						description = command.Description
					}
				}
				if title == "" {
					title = titleize(publicID)
				}
				if description == "" {
					description = "Execute the " + publicID + " knowledge-graph capability."
				}
				view = &Capability{SemanticID: semanticID, Title: title, Description: description, InputSchema: item.InputSchema, Output: item.Output, CLISpec: item.CLISpec, Source: Source{IntegrationPath: integration}}
				byID[semanticID] = view
			}
			available := provider.Status.Path != "" && (provider.Status.Available == nil || provider.Status.Available.Available)
			implementation := implementationPath(provider.ID())
			if provider.Status.Manifest != nil && provider.Status.Manifest.Source != nil && provider.Status.Manifest.Source.LocalCodePath != "" {
				implementation = provider.Status.Manifest.Source.LocalCodePath
			}
			candidate := Candidate{ProviderID: provider.ID(), ProviderCapability: item.CapabilityID, ProviderPath: provider.Status.Path, ImplementationPath: implementation, Available: available, Probed: provider.Status.Probed, Weight: provider.Status.Weight, SideEffects: item.SideEffects}
			view.Candidates = append(view.Candidates, candidate)
			view.Available = view.Available || available
			if candidate.ImplementationPath != "" && !contains(view.Source.ImplementationPaths, candidate.ImplementationPath) {
				view.Source.ImplementationPaths = append(view.Source.ImplementationPaths, candidate.ImplementationPath)
			}
		}
	}

	// Pipelines are orchestration capabilities of the hub, not KG algorithms.
	for _, value := range []Capability{
		{SemanticID: "kg.pipeline.run", Title: "Run a capability pipeline", Description: "Execute a declarative kg.pipeline/v1 pipeline definition.", Available: true, InputSchema: json.RawMessage(`{"type":"object","properties":{"definition":{"type":"string"},"work_dir":{"type":"string"},"resume":{"type":"string"}},"required":["definition"],"additionalProperties":false}`), CLISpec: protocol.CLISpec{Positionals: []protocol.PositionalSpec{{Name: "definition", Required: true}}}, Source: Source{IntegrationPath: integration}},
		{SemanticID: "kg.pipeline.validate", Title: "Validate a capability pipeline", Description: "Validate a declarative kg.pipeline/v1 pipeline definition without executing providers.", Available: true, InputSchema: json.RawMessage(`{"type":"object","properties":{"definition":{"type":"string"}},"required":["definition"],"additionalProperties":false}`), CLISpec: protocol.CLISpec{Positionals: []protocol.PositionalSpec{{Name: "definition", Required: true}}}, Source: Source{IntegrationPath: integration}},
	} {
		copy := value
		byID[value.SemanticID] = &copy
	}

	capabilities := make([]Capability, 0, len(byID))
	for _, capability := range byID {
		sort.Slice(capability.Candidates, func(i, j int) bool {
			left, right := capability.Candidates[i], capability.Candidates[j]
			if left.Probed != right.Probed {
				return left.Probed
			}
			if left.Weight != right.Weight {
				return left.Weight > right.Weight
			}
			return left.ProviderID < right.ProviderID
		})
		sort.Strings(capability.Source.ImplementationPaths)
		capabilities = append(capabilities, *capability)
	}
	sort.Slice(capabilities, func(i, j int) bool { return capabilities[i].SemanticID < capabilities[j].SemanticID })
	groups := buildGroups(capabilities)
	sort.Slice(providers, func(i, j int) bool { return providers[i].ID() < providers[j].ID() })
	snapshot := Snapshot{SchemaVersion: SnapshotSchema, CreatedAt: time.Now().UTC(), Providers: providers, Groups: groups, Capabilities: capabilities}
	data, _ := json.Marshal(struct {
		Providers    []router.Provider              `json:"providers"`
		Groups       []coreprotocol.CapabilityGroup `json:"groups"`
		Capabilities []Capability                   `json:"capabilities"`
	}{providers, groups, capabilities})
	digest := sha256.Sum256(data)
	snapshot.Fingerprint = hex.EncodeToString(digest[:])
	return snapshot
}

type providerCapability struct {
	CapabilityID string
	Title        string
	Description  string
	SideEffects  []string
	InputSchema  json.RawMessage
	Output       protocol.OutputSpec
	CLISpec      protocol.CLISpec
}

func providerCapabilities(provider router.Provider) []providerCapability {
	var result []providerCapability
	if provider.Status.Probed && provider.Status.Manifest != nil {
		for _, capability := range provider.Status.Manifest.Capabilities {
			result = append(result, providerCapability{capability.CapabilityID, capability.Title, capability.Description, capability.SideEffects, capability.InputSchema, capability.Output, capability.CLISpec})
		}
		return result
	}
	if provider.Fallback != nil {
		for _, capability := range provider.Fallback.Capabilities {
			result = append(result, providerCapability{CapabilityID: capability.CapabilityID, SideEffects: capability.SideEffects, InputSchema: capability.InputSchema, Output: capability.Output, CLISpec: capability.CLISpec})
		}
	}
	return result
}

func PublicID(capability Capability) string { return strings.TrimPrefix(capability.SemanticID, "kg.") }

func Find(snapshot Snapshot, id string) (Capability, bool) {
	id = strings.TrimPrefix(id, "kg.")
	for _, capability := range snapshot.Capabilities {
		if PublicID(capability) == id {
			return capability, true
		}
	}
	return Capability{}, false
}

func Select(capability Capability, explicit string, dryRun bool) (Candidate, error) {
	if len(capability.Candidates) == 0 {
		return Candidate{}, nil
	}
	for _, candidate := range capability.Candidates {
		if explicit != "" && candidate.ProviderID != explicit {
			continue
		}
		if candidate.Available || (dryRun && candidate.ProviderPath != "") {
			return candidate, nil
		}
	}
	if explicit != "" {
		return Candidate{}, &SelectionError{"provider " + explicit + " is not available for " + PublicID(capability)}
	}
	return Candidate{}, &SelectionError{"no available implementation for " + PublicID(capability)}
}

type SelectionError struct{ Message string }

func (e *SelectionError) Error() string { return e.Message }

func normalizeID(id string) string {
	parts := strings.Split(id, ".")
	for index := range parts {
		parts[index] = strings.ReplaceAll(parts[index], "_", "-")
	}
	return strings.Join(parts, ".")
}

func buildGroups(capabilities []Capability) []coreprotocol.CapabilityGroup {
	seen := map[string]bool{}
	var groups []coreprotocol.CapabilityGroup
	for _, capability := range capabilities {
		parts := strings.Split(PublicID(capability), ".")
		for length := 1; length < len(parts); length++ {
			id := strings.Join(parts[:length], ".")
			if seen[id] {
				continue
			}
			seen[id] = true
			groups = append(groups, coreprotocol.CapabilityGroup{ID: id, Title: titleize(id), Description: "Knowledge-graph capabilities in the " + id + " group.", Order: len(groups)})
		}
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].ID < groups[j].ID })
	return groups
}

func titleize(value string) string {
	value = strings.NewReplacer(".", " ", "-", " ").Replace(value)
	if value == "" {
		return "Capability"
	}
	return strings.ToUpper(value[:1]) + value[1:]
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func integrationPath() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func implementationPath(id string) string {
	home := "~/projects"
	switch id {
	case "kg-extract":
		return home + "/kg/kg-extract"
	case "kg-layout":
		return home + "/kg/kg-layout"
	case "graph-kg":
		return home + "/kg/kg-analyze"
	case "kg-algorithms":
		return home + "/kg/kg-algorithms"
	case "kg-mm":
		return home + "/kg/kg-mm"
	case "ygr":
		return home + "/kg/ygr"
	default:
		return ""
	}
}
