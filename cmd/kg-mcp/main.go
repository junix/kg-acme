// Command kg-mcp exposes the kg capability hub as an MCP server over stdio
// (newline-delimited JSON-RPC 2.0). It is the same hub as the kg CLI —
// same catalog, same provider discovery, same router, same policy gates,
// same pipeline runner — with the CLI's --allow-* flags supplied by server
// startup configuration instead:
//
//	kg-mcp [--allow-network] [--allow-data-egress] [--allow-model-download]
//	       [--allow-db-write] [--provider-bin ID=PATH]...
//
// The KG_ACME_ALLOW environment variable is an equivalent allow-list
// ("network,data_egress" or "*"); flags and env merge (OR).
//
// stdout carries only JSON-RPC frames; logs always go to stderr.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"kg-acme/internal/discover"
	"kg-acme/internal/mcp"
	"kg-acme/internal/policy"
	"kg-acme/internal/router"
)

const version = "0.1.0"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	gates := policy.Gates{}
	overrides := discover.Overrides{}

	for i := 0; i < len(args); i++ {
		tok := args[i]
		next := func() (string, error) {
			if i+1 >= len(args) {
				return "", fmt.Errorf("flag %s expects a value", tok)
			}
			i++
			return args[i], nil
		}
		switch {
		case tok == "--allow-network":
			gates.AllowNetwork = true
		case tok == "--allow-data-egress":
			gates.AllowDataEgress = true
		case tok == "--allow-model-download":
			gates.AllowModelDownload = true
		case tok == "--allow-db-write":
			gates.AllowDBWrite = true
		case strings.HasPrefix(tok, "--provider-bin="):
			if err := addOverride(overrides, strings.TrimPrefix(tok, "--provider-bin=")); err != nil {
				fmt.Fprintf(os.Stderr, "kg-mcp: %v\n", err)
				return 2
			}
		case tok == "--provider-bin":
			v, err := next()
			if err != nil {
				fmt.Fprintf(os.Stderr, "kg-mcp: %v\n", err)
				return 2
			}
			if err := addOverride(overrides, v); err != nil {
				fmt.Fprintf(os.Stderr, "kg-mcp: %v\n", err)
				return 2
			}
		case tok == "-h" || tok == "--help":
			usage()
			return 0
		default:
			fmt.Fprintf(os.Stderr, "kg-mcp: unknown flag %q\n", tok)
			usage()
			return 2
		}
	}

	if spec := os.Getenv("KG_ACME_ALLOW"); spec != "" {
		envGates, err := policy.ParseGates(spec)
		if err != nil {
			fmt.Fprintf(os.Stderr, "kg-mcp: KG_ACME_ALLOW: %v\n", err)
			return 2
		}
		gates = gates.Merge(envGates)
	}

	srv := mcp.New(mcp.Config{
		Version: version,
		Gates:   gates,
		Providers: func(ctx context.Context) []router.Provider {
			return router.DiscoverProviders(ctx, overrides)
		},
	})
	if err := srv.Serve(context.Background(), os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "kg-mcp: %v\n", err)
		return 1
	}
	return 0
}

func addOverride(overrides discover.Overrides, kv string) error {
	id, path, ok := strings.Cut(kv, "=")
	if !ok || id == "" || path == "" {
		return fmt.Errorf("--provider-bin expects ID=PATH, got %q", kv)
	}
	overrides[id] = path
	return nil
}

func usage() {
	fmt.Fprintln(os.Stderr, `kg-mcp — MCP server (stdio) for the kg capability hub

Usage:
  kg-mcp [--allow-*] [--provider-bin ID=PATH]...

Startup flags (the MCP form of the CLI's per-invocation hub flags):
  --allow-network         allow the network side effect
  --allow-data-egress     allow data egress side effect
  --allow-model-download  allow model downloads
  --allow-db-write        allow database writes
  --provider-bin ID=PATH  explicit provider executable (repeatable)

Environment:
  KG_ACME_ALLOW           comma/space-separated allow list
                          (network,data_egress,downloads_models,writes_db or *),
                          merged with the --allow-* flags`)
}
