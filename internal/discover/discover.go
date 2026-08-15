// Package discover finds provider executables and probes them for
// self-description (describe) and dependency status (available).
//
// Discovery order for a binary name:
//  1. explicit --provider-bin ID=PATH override;
//  2. ~/sync/<os>-<arch>-bin/;
//  3. ~/sync/bin/;
//  4. PATH lookup;
//  5. PATH scan for kg-provider-* executables.
//
// Only actual executable files are accepted.
package discover

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	coreprotocol "github.com/junix/acme-core/protocol"

	"kg-acme/internal/protocol"
	"kg-acme/internal/schema"
)

// Env abstracts the environment discovery depends on, for testing.
type Env struct {
	Home   string // user home directory
	Path   string // PATH value
	GOOS   string
	GOARCH string
}

// DefaultEnv builds Env from the real process environment.
func DefaultEnv() Env {
	home, _ := os.UserHomeDir()
	return Env{Home: home, Path: os.Getenv("PATH"), GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}
}

func (e Env) goos() string {
	if e.GOOS == "" {
		return runtime.GOOS
	}
	return e.GOOS
}

func (e Env) goarch() string {
	if e.GOARCH == "" {
		return runtime.GOARCH
	}
	return e.GOARCH
}

// Overrides maps provider/binary IDs to explicit executable paths
// (--provider-bin ID=PATH).
type Overrides map[string]string

// IsExecutable reports whether path is a regular, executable file.
func IsExecutable(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular() && st.Mode().Perm()&0o111 != 0
}

func (e Env) lookPath(name string) (string, bool) {
	for _, dir := range filepath.SplitList(e.Path) {
		if dir == "" {
			continue
		}
		p := filepath.Join(dir, name)
		if IsExecutable(p) {
			return p, true
		}
	}
	return "", false
}

// syncBins returns the per-platform sync bin directory names in preference
// order: the GOOS token first, then the "macos" alias this machine's
// ~/sync layout uses for darwin binaries.
func (e Env) syncBins() []string {
	names := []string{fmt.Sprintf("%s-%s-bin", e.goos(), e.goarch())}
	if e.goos() == "darwin" {
		names = append(names, fmt.Sprintf("macos-%s-bin", e.goarch()))
	}
	return names
}

// FindExecutable resolves a binary name to an executable path following the
// discovery order above. Returns "" when nothing executable is found.
func FindExecutable(name string, overrides Overrides, env Env) string {
	// 1. explicit override
	if p, ok := overrides[name]; ok && IsExecutable(p) {
		return p
	}
	if env.Home != "" {
		// 2. ~/sync/<os>-<arch>-bin/
		for _, dir := range env.syncBins() {
			p := filepath.Join(env.Home, "sync", dir, name)
			if IsExecutable(p) {
				return p
			}
		}
		// 3. ~/sync/bin/
		p := filepath.Join(env.Home, "sync", "bin", name)
		if IsExecutable(p) {
			return p
		}
	}
	// 4. PATH
	if p, ok := env.lookPath(name); ok {
		return p
	}
	return ""
}

// ScanProviders scans every PATH directory for kg-provider-* executables and
// returns them as name → path.
func ScanProviders(env Env) map[string]string {
	out := map[string]string{}
	for _, dir := range filepath.SplitList(env.Path) {
		if dir == "" {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, ent := range entries {
			name := ent.Name()
			if !strings.HasPrefix(name, "kg-provider-") {
				continue
			}
			p := filepath.Join(dir, name)
			if _, taken := out[name]; !taken && IsExecutable(p) {
				out[name] = p
			}
		}
	}
	return out
}

// ProviderStatus is what the hub knows about one provider binary.
type ProviderStatus struct {
	ID   string `json:"id"`
	Path string `json:"path"`

	// Weight biases routing when several providers offer the same
	// capability. Defaults to 1.0.
	Weight float64 `json:"weight"`

	// Manifest is non-nil when describe probing succeeded.
	Manifest *protocol.Manifest `json:"manifest,omitempty"`
	// Version is the negotiated protocol version (0 when unprobed).
	Version int `json:"version,omitempty"`

	// Available reflects `<provider> available --json`. Nil means unknown
	// (probe unsupported or failed) — fail-safe: unknown never downgrades
	// a provider.
	Available *protocol.AvailableReport `json:"available,omitempty"`

	// Probed reports whether describe probing succeeded.
	Probed bool `json:"probed"`

	// ProbeErrorCode records why describe probing failed:
	// malformed_manifest (bad JSON / schema violation) or
	// unsupported_schema_version (no common protocol version).
	// Empty when probing succeeded or the binary could not be run at all.
	ProbeErrorCode string `json:"probe_error_code,omitempty"`

	Diagnostics []protocol.Diagnostic `json:"diagnostics,omitempty"`
}

// ProbeTimeout bounds each probe subprocess.
const ProbeTimeout = 15 * time.Second

// Probe runs describe/available against a provider binary, best-effort, and
// fills a ProviderStatus. Failures degrade to diagnostics, never to a hard
// error: an unprobed provider stays usable via hub fallback data.
func Probe(ctx context.Context, id, path string) ProviderStatus {
	st := ProviderStatus{ID: id, Path: path, Weight: 1.0}

	// describe --json
	if out, err := runProbe(ctx, path, "describe", "--json"); err != nil {
		st.Diagnostics = append(st.Diagnostics, protocol.Diagnostic{
			Severity: "warning", Message: fmt.Sprintf("describe probe failed: %v", err)})
	} else if err := schema.ValidateManifest(out); err != nil {
		st.ProbeErrorCode = protocol.ErrMalformedManifest
		st.Diagnostics = append(st.Diagnostics, protocol.Diagnostic{
			Severity: "warning", Message: err.Error()})
	} else {
		var m protocol.Manifest
		if err := json.Unmarshal(out, &m); err != nil {
			st.ProbeErrorCode = protocol.ErrMalformedManifest
			st.Diagnostics = append(st.Diagnostics, protocol.Diagnostic{
				Severity: "warning", Message: fmt.Sprintf("describe output is not valid JSON: %v", err)})
		} else if err := validateDescriptionFloor(&m); err != nil {
			st.ProbeErrorCode = protocol.ErrMalformedManifest
			st.Diagnostics = append(st.Diagnostics, protocol.Diagnostic{
				Severity: "warning", Message: err.Error()})
		} else if v, err := protocol.Negotiate(m.ProtocolVersions); err != nil {
			st.ProbeErrorCode = protocol.ErrUnsupportedSchemaVersion
			st.Diagnostics = append(st.Diagnostics, protocol.Diagnostic{
				Severity: "warning", Message: err.Error()})
		} else {
			m.Provider.ID = firstNonEmpty(m.Provider.ID, id)
			st.Manifest = &m
			st.Version = v
			st.Probed = true
			st.ID = m.Provider.ID
		}
	}

	// available --json — fail-safe: failure never downgrades.
	if out, err := runProbe(ctx, path, "available", "--json"); err == nil {
		if err := schema.ValidateAvailable(out); err == nil {
			var a protocol.AvailableReport
			if err := json.Unmarshal(out, &a); err == nil {
				st.Available = &a
			}
		}
	}

	return st
}

func runProbe(ctx context.Context, path string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()
	cmd := probeCommand(ctx, path, args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return out, nil
}

func firstNonEmpty(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}

// validateDescriptionFloor applies the acme-core description-quality floor to
// every capability in a probed manifest. A violation rejects the manifest as
// malformed, like a schema breach: provider-published text is the provider's
// contract, and low-information descriptions must be fixed at the source.
// (The hub's own catalog style validator in internal/catalog is separate and
// unaffected.)
func validateDescriptionFloor(m *protocol.Manifest) error {
	for _, capability := range m.Capabilities {
		if err := coreprotocol.ValidateDescriptionFloor(coreprotocol.Capability{
			ID:          capability.CapabilityID,
			Title:       capability.Title,
			Description: capability.Description,
		}); err != nil {
			return err
		}
	}
	return nil
}
