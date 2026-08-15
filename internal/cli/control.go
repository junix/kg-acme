package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"kg-acme/internal/discover"
	"kg-acme/internal/policy"
	"kg-acme/internal/router"
	"kg-acme/internal/state"
	"kg-acme/internal/surface"
)

type ControlRunner struct {
	Stdin                    io.Reader
	Stdout, Stderr           io.Writer
	SnapshotPath, RoutesPath string
}

func (c ControlRunner) Run(ctx context.Context, arguments []string) int {
	r := Runner{Stdin: c.Stdin, Stdout: c.Stdout, Stderr: c.Stderr, SnapshotPath: c.SnapshotPath, RoutesPath: c.RoutesPath}
	r.defaults()
	opts, args, err := parseGlobal(arguments)
	if err != nil {
		return r.fail(err, opts.JSON)
	}
	if len(args) == 0 || isHelp(args[0]) {
		r.controlHelp()
		return 0
	}
	if opts.Params != "" || opts.Output != "" || opts.DryRun || opts.Describe || opts.Gates != (policyZero()) {
		return r.fail(fmt.Errorf("execution options are not accepted by kgctl"), opts.JSON)
	}
	path, err := r.snapshotPath()
	if err != nil {
		return r.fail(err, opts.JSON)
	}
	switch args[0] {
	case "refresh":
		if len(args) != 1 {
			err = fmt.Errorf("usage: kgctl refresh [--provider-bin ID=PATH]")
		} else {
			err = r.refresh(ctx, path, opts)
		}
	case "providers":
		err = r.controlProviders(ctx, path, opts, args[1:])
	case "capabilities":
		err = r.controlCapabilities(path, opts, args[1:])
	case "route":
		err = r.controlRoute(path, opts, args[1:])
	case "completion":
		err = r.completion(path, args[1:])
	default:
		err = fmt.Errorf("unknown kgctl command: %s", args[0])
	}
	return r.finish(err, opts.JSON)
}

// policyZero avoids importing implementation details into flag parsing.
func policyZero() policy.Gates { return policy.Gates{} }

func (r Runner) refresh(ctx context.Context, path string, opts options) error {
	providers := router.DiscoverProviders(ctx, discover.Overrides(opts.ProviderBins))
	snapshot := surface.Build(providers)
	if err := state.SaveSnapshot(path, snapshot); err != nil {
		return err
	}
	available := 0
	for _, capability := range snapshot.Capabilities {
		if capability.Available {
			available++
		}
	}
	if opts.JSON {
		return writeJSON(r.Stdout, map[string]any{"schema_version": "kg.refresh/v1", "snapshot_path": path, "fingerprint": snapshot.Fingerprint, "capabilities": len(snapshot.Capabilities), "available": available})
	}
	fmt.Fprintf(r.Stdout, "refreshed %d capabilities (%d available)\n%s\n", len(snapshot.Capabilities), available, path)
	return nil
}

func (r Runner) controlProviders(ctx context.Context, path string, opts options, args []string) error {
	action := "list"
	if len(args) > 0 {
		action = args[0]
	}
	var providers []router.Provider
	if action == "doctor" {
		providers = router.DiscoverProviders(ctx, discover.Overrides(opts.ProviderBins))
	} else {
		snapshot, err := state.LoadSnapshot(path)
		if err != nil {
			return err
		}
		providers = snapshot.Providers
	}
	if action == "show" {
		if len(args) != 2 {
			return fmt.Errorf("usage: kgctl providers show <provider-id>")
		}
		for _, provider := range providers {
			if provider.ID() == args[1] {
				return r.outputAny(opts.JSON, provider.Status)
			}
		}
		return fmt.Errorf("provider not found: %s", args[1])
	}
	if action != "list" && action != "doctor" {
		return fmt.Errorf("unknown providers command: %s", action)
	}
	if opts.JSON {
		statuses := make([]any, 0, len(providers))
		for _, provider := range providers {
			statuses = append(statuses, provider.Status)
		}
		return writeJSON(r.Stdout, map[string]any{"schema_version": "kg.providers-list/v1", "providers": statuses})
	}
	for _, provider := range providers {
		available := provider.Status.Path != "" && (provider.Status.Available == nil || provider.Status.Available.Available)
		if !opts.All && action != "doctor" && !available {
			continue
		}
		status := "available"
		if !available {
			status = "unavailable"
		}
		count := 0
		if provider.Status.Manifest != nil {
			count = len(provider.Status.Manifest.Capabilities)
		} else if provider.Fallback != nil {
			count = len(provider.Fallback.Capabilities)
		}
		fmt.Fprintf(r.Stdout, "%-18s %-12s %3d  %s\n", provider.ID(), status, count, provider.Status.Path)
		if action == "doctor" {
			for _, diagnostic := range provider.Status.Diagnostics {
				fmt.Fprintf(r.Stdout, "  - %s: %s\n", diagnostic.Severity, diagnostic.Message)
			}
		}
	}
	return nil
}

func (r Runner) controlCapabilities(path string, opts options, args []string) error {
	snapshot, err := state.LoadSnapshot(path)
	if err != nil {
		return err
	}
	action := "list"
	if len(args) > 0 {
		action = args[0]
	}
	values := snapshot.Capabilities
	if action == "search" {
		query := strings.ToLower(strings.Join(args[1:], " "))
		var filtered []surface.Capability
		for _, value := range values {
			if strings.Contains(strings.ToLower(surface.PublicID(value)+" "+value.Title+" "+value.Description), query) {
				filtered = append(filtered, value)
			}
		}
		values = filtered
	} else if action == "show" {
		if len(args) != 2 {
			return fmt.Errorf("usage: kgctl capabilities show <capability-id>")
		}
		value, ok := surface.Find(snapshot, args[1])
		if !ok {
			return fmt.Errorf("capability not found: %s", args[1])
		}
		return r.outputAny(opts.JSON, publicDescription(value))
	} else if action != "list" {
		return fmt.Errorf("unknown capabilities command: %s", action)
	}
	if opts.JSON {
		var items []map[string]any
		for _, value := range values {
			if !opts.All && !value.Available {
				continue
			}
			item := map[string]any{"semantic_id": surface.PublicID(value), "description": value.Description}
			if opts.All {
				item["available"] = value.Available
			}
			items = append(items, item)
		}
		return writeJSON(r.Stdout, map[string]any{"schema_version": "kg.capabilities-list/v3", "capabilities": items})
	}
	w := tabwriter.NewWriter(r.Stdout, 0, 4, 2, ' ', 0)
	if opts.All {
		fmt.Fprintln(w, "CAPABILITY ID\tDESCRIPTION\tSTATUS")
	} else {
		fmt.Fprintln(w, "CAPABILITY ID\tDESCRIPTION")
	}
	for _, value := range values {
		if !opts.All && !value.Available {
			continue
		}
		if opts.All {
			status := "unavailable"
			if value.Available {
				status = "available"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\n", surface.PublicID(value), oneLine(value.Description), status)
		} else {
			fmt.Fprintf(w, "%s\t%s\n", surface.PublicID(value), oneLine(value.Description))
		}
	}
	return w.Flush()
}

func (r Runner) controlRoute(path string, opts options, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: kgctl route <explain|set|clear> ...")
	}
	snapshot, err := state.LoadSnapshot(path)
	if err != nil {
		return err
	}
	routesPath, err := r.routesPath()
	if err != nil {
		return err
	}
	routes, err := state.LoadRoutes(routesPath)
	if err != nil {
		return err
	}
	action := args[0]
	if action == "explain" {
		if len(args) != 2 {
			return fmt.Errorf("usage: kgctl route explain <capability-id>")
		}
		value, ok := surface.Find(snapshot, args[1])
		if !ok {
			return fmt.Errorf("capability not found: %s", args[1])
		}
		candidate, err := surface.Select(value, routes.Routes[value.SemanticID], false)
		if err != nil {
			return err
		}
		return r.outputAny(opts.JSON, map[string]any{"schema_version": "kg.route-explanation/v3", "capability": publicDescription(value), "route": map[string]any{"capability_id": args[1], "implementation_path": candidate.ImplementationPath}})
	}
	if action == "set" {
		if len(args) != 3 {
			return fmt.Errorf("usage: kgctl route set <capability-id> <provider-id>")
		}
		value, ok := surface.Find(snapshot, args[1])
		if !ok {
			return fmt.Errorf("capability not found: %s", args[1])
		}
		found := false
		for _, candidate := range value.Candidates {
			if candidate.ProviderID == args[2] {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("provider is not an implementation of %s", args[1])
		}
		routes.Routes[value.SemanticID] = args[2]
		if err := state.SaveRoutes(routesPath, routes); err != nil {
			return err
		}
		return r.outputAny(opts.JSON, map[string]any{"schema_version": "kg.route-config-result/v1", "capability_id": args[1], "configured": true})
	}
	if action == "clear" {
		if len(args) != 2 {
			return fmt.Errorf("usage: kgctl route clear <capability-id>")
		}
		value, ok := surface.Find(snapshot, args[1])
		if !ok {
			return fmt.Errorf("capability not found: %s", args[1])
		}
		delete(routes.Routes, value.SemanticID)
		if err := state.SaveRoutes(routesPath, routes); err != nil {
			return err
		}
		return r.outputAny(opts.JSON, map[string]any{"schema_version": "kg.route-config-result/v1", "capability_id": args[1], "cleared": true})
	}
	return fmt.Errorf("unknown route command: %s", action)
}

func (r Runner) completion(path string, args []string) error {
	if len(args) != 1 || (args[0] != "bash" && args[0] != "zsh") {
		return fmt.Errorf("usage: kgctl completion <bash|zsh>")
	}
	snapshot, err := state.LoadSnapshot(path)
	if err != nil {
		return err
	}
	var ids []string
	for _, value := range snapshot.Capabilities {
		if value.Available {
			ids = append(ids, surface.PublicID(value))
		}
	}
	sort.Strings(ids)
	fmt.Fprintf(r.Stdout, "# kg %s completion\n_kg_capabilities='%s'\n", args[0], strings.Join(ids, " "))
	return nil
}
func (r Runner) outputAny(jsonMode bool, value any) error {
	if jsonMode {
		return writeJSON(r.Stdout, value)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(r.Stdout, string(data))
	return nil
}
func (r Runner) controlHelp() {
	fmt.Fprint(r.Stdout, `kgctl — manage the knowledge-graph capability control plane

Usage:
  kgctl refresh [--provider-bin ID=PATH]
  kgctl providers [list|show|doctor] [--all] [--json]
  kgctl capabilities [list|search|show] [--all] [--json]
  kgctl route [explain|set|clear]
  kgctl completion <bash|zsh>

Only refresh and providers doctor start providers. List, search, show, route,
completion, and every kg help/describe command read the immutable snapshot.
`)
}
