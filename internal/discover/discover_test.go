package discover

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"kg-acme/internal/protocol"
)

// setupEnv builds a fake filesystem layout:
//
//	home/sync/<os>-<arch>-bin/<names in archBin>
//	home/sync/bin/<names in syncBin>
//	pathDir/<names in pathBin>
//
// All listed binaries are created executable unless suffixed with ":noexec".
func setupEnv(t *testing.T, archBin, syncBin, pathBin []string) Env {
	t.Helper()
	home := t.TempDir()
	pathDir := t.TempDir()
	mk := func(dir string, names []string) {
		t.Helper()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, n := range names {
			mode := os.FileMode(0o755)
			name := n
			if base, ok := cutSuffix(n, ":noexec"); ok {
				name = base
				mode = 0o644
			}
			p := filepath.Join(dir, name)
			if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), mode); err != nil {
				t.Fatal(err)
			}
		}
	}
	mk(filepath.Join(home, "sync", runtime.GOOS+"-"+runtime.GOARCH+"-bin"), archBin)
	mk(filepath.Join(home, "sync", "bin"), syncBin)
	mk(pathDir, pathBin)
	return Env{Home: home, Path: pathDir}
}

func cutSuffix(s, suffix string) (string, bool) {
	if len(s) > len(suffix) && s[len(s)-len(suffix):] == suffix {
		return s[:len(s)-len(suffix)], true
	}
	return s, false
}

func TestFindExecutableOrder(t *testing.T) {
	env := setupEnv(t,
		[]string{"tool", "arch-only"},
		[]string{"tool", "sync-only"},
		[]string{"tool", "path-only", "tool-noexec:noexec"})

	got := FindExecutable("tool", nil, env)
	want := filepath.Join(env.Home, "sync", runtime.GOOS+"-"+runtime.GOARCH+"-bin", "tool")
	if got != want {
		t.Errorf("arch bin should win: got %q want %q", got, want)
	}

	got = FindExecutable("sync-only", nil, env)
	want = filepath.Join(env.Home, "sync", "bin", "sync-only")
	if got != want {
		t.Errorf("sync bin should be second: got %q want %q", got, want)
	}

	got = FindExecutable("path-only", nil, env)
	want = filepath.Join(env.Path, "path-only")
	if got != want {
		t.Errorf("PATH should be last: got %q want %q", got, want)
	}

	if got := FindExecutable("tool-noexec", nil, env); got != "" {
		t.Errorf("non-executable must not be accepted, got %q", got)
	}
	if got := FindExecutable("missing", nil, env); got != "" {
		t.Errorf("missing binary should resolve to empty, got %q", got)
	}
	// Empty Home skips the sync directories entirely; PATH still resolves.
	got = FindExecutable("path-only", nil, Env{Home: "", Path: env.Path})
	if want := filepath.Join(env.Path, "path-only"); got != want {
		t.Errorf("empty home must fall through to PATH: got %q want %q", got, want)
	}
}

func TestFindExecutableOverrideWins(t *testing.T) {
	env := setupEnv(t, []string{"tool"}, nil, nil)
	overrideDir := t.TempDir()
	overridePath := filepath.Join(overrideDir, "tool")
	if err := os.WriteFile(overridePath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := FindExecutable("tool", Overrides{"tool": overridePath}, env)
	if got != overridePath {
		t.Errorf("explicit --provider-bin override must win: got %q want %q", got, overridePath)
	}

	// A non-executable override falls through to normal discovery.
	bad := filepath.Join(overrideDir, "bad")
	if err := os.WriteFile(bad, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got = FindExecutable("tool", Overrides{"tool": bad}, env)
	want := filepath.Join(env.Home, "sync", runtime.GOOS+"-"+runtime.GOARCH+"-bin", "tool")
	if got != want {
		t.Errorf("non-executable override should fall through: got %q want %q", got, want)
	}

	// An override naming a missing file also falls through to discovery.
	got = FindExecutable("tool", Overrides{"tool": filepath.Join(overrideDir, "absent")}, env)
	if got != want {
		t.Errorf("missing override should fall through: got %q want %q", got, want)
	}
}

func TestFindExecutableMacOSAlias(t *testing.T) {
	env := setupEnv(t, nil, nil, nil)
	alias := filepath.Join(env.Home, "sync", "macos-"+runtime.GOARCH+"-bin")
	if err := os.MkdirAll(alias, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(alias, "tool")
	if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := FindExecutable("tool", nil, env)
	if runtime.GOOS == "darwin" {
		if got != p {
			t.Errorf("macos-<arch>-bin alias should be found on darwin: got %q want %q", got, p)
		}
	} else if got != "" {
		t.Errorf("macos alias must not apply on %s: got %q", runtime.GOOS, got)
	}
}

func TestScanProviders(t *testing.T) {
	env := setupEnv(t, nil, nil, []string{"kg-provider-a", "kg-provider-b", "other", "kg-provider-c:noexec"})
	found := ScanProviders(env)
	if len(found) != 2 {
		t.Fatalf("expected 2 kg-provider-* executables, got %v", found)
	}
	for _, name := range []string{"kg-provider-a", "kg-provider-b"} {
		if _, ok := found[name]; !ok {
			t.Errorf("expected %s to be found", name)
		}
	}
}

// When the same kg-provider-* name exists in two PATH directories, the
// earlier directory wins and later duplicates are ignored.
func TestScanProvidersFirstPathDirWins(t *testing.T) {
	dirA, dirB := t.TempDir(), t.TempDir()
	for _, dir := range []string{dirA, dirB} {
		if err := os.WriteFile(filepath.Join(dir, "kg-provider-x"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	env := Env{Path: strings.Join([]string{dirA, dirB}, string(os.PathListSeparator))}
	found := ScanProviders(env)
	want := filepath.Join(dirA, "kg-provider-x")
	if len(found) != 1 || found["kg-provider-x"] != want {
		t.Errorf("first PATH dir must win: got %v, want %q", found, want)
	}
}

func TestIsExecutable(t *testing.T) {
	dir := t.TempDir()
	execFile := filepath.Join(dir, "x")
	if err := os.WriteFile(execFile, []byte(""), 0o755); err != nil {
		t.Fatal(err)
	}
	plainFile := filepath.Join(dir, "y")
	if err := os.WriteFile(plainFile, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if !IsExecutable(execFile) {
		t.Error("0755 file should be executable")
	}
	if IsExecutable(plainFile) {
		t.Error("0644 file should not be executable")
	}
	if IsExecutable(dir) {
		t.Error("directory should not be executable")
	}
	if IsExecutable(filepath.Join(dir, "missing")) {
		t.Error("missing file should not be executable")
	}
}

// probeManifest builds a one-capability describe manifest with the given
// description, protocol_versions and side_effects.
func probeManifest(description, protocolVersions, sideEffects string) string {
	return fmt.Sprintf(`{"protocol":"kg.provider/v1","protocol_versions":%s,"provider":{"id":"fake","version":"1.0.0","description":"Fake provider"},"capabilities":[{"capability_id":"test.echo","title":"Echo a value","description":%s,"side_effects":%s,"input_schema":{"type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false},"output":{"mode":"result-json","kind":"json"},"cli_spec":{"subcommand":[],"always":[],"positionals":[],"flags":[]}}]}`,
		protocolVersions, strconv.Quote(description), sideEffects)
}

// probeFixtureRaw writes a fake provider script whose describe and available
// actions run the given shell snippets, and returns its path.
func probeFixtureRaw(t *testing.T, describe, available string) string {
	t.Helper()
	script := "#!/bin/sh\ncase \"$1\" in\n" +
		"  describe) " + describe + " ;;\n" +
		"  available) " + available + " ;;\n" +
		"esac\n"
	path := filepath.Join(t.TempDir(), "fake-provider")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// probeFixture writes a fake provider script whose describe output carries one
// capability with the given description, and returns its path.
func probeFixture(t *testing.T, description string) string {
	t.Helper()
	return probeFixtureRaw(t,
		"printf '%s\\n' '"+probeManifest(description, "[1]", "[]")+"'",
		`printf '%s\n' '{"available":true,"ready":[],"missing":[]}'`)
}

func TestProbeDescriptionFloor(t *testing.T) {
	t.Run("title restatement rejected as malformed manifest", func(t *testing.T) {
		st := Probe(context.Background(), "fake", probeFixture(t, "Echo a value."))
		if st.Probed {
			t.Fatal("a description that just restates the title must fail the floor")
		}
		if st.ProbeErrorCode != protocol.ErrMalformedManifest {
			t.Fatalf("want probe error %q, got %q", protocol.ErrMalformedManifest, st.ProbeErrorCode)
		}
		var messages []string
		for _, d := range st.Diagnostics {
			messages = append(messages, d.Message)
		}
		if !strings.Contains(strings.Join(messages, "\n"), `"test.echo"`) {
			t.Fatalf("floor diagnostic must name the capability id: %v", messages)
		}
	})

	t.Run("missing terminal period rejected", func(t *testing.T) {
		st := Probe(context.Background(), "fake", probeFixture(t, "Return the supplied value without changing it"))
		if st.Probed || st.ProbeErrorCode != protocol.ErrMalformedManifest {
			t.Fatalf("description without terminal period must fail: probed=%v code=%q", st.Probed, st.ProbeErrorCode)
		}
	})

	t.Run("clean description accepted", func(t *testing.T) {
		st := Probe(context.Background(), "fake", probeFixture(t, "Return the supplied value without changing it."))
		if !st.Probed || st.ProbeErrorCode != "" {
			t.Fatalf("clean manifest must probe: probed=%v code=%q diags=%v", st.Probed, st.ProbeErrorCode, st.Diagnostics)
		}
	})
}

// A successful probe fills the whole status: negotiated version, kept
// manifest, provider id taken from the manifest (not the discovery id), and
// the availability report attached — with zero diagnostics.
func TestProbeSuccessStatus(t *testing.T) {
	st := Probe(context.Background(), "discovery-id", probeFixture(t, "Return the supplied value without changing it."))
	if !st.Probed || st.ProbeErrorCode != "" {
		t.Fatalf("clean manifest must probe: probed=%v code=%q diags=%v", st.Probed, st.ProbeErrorCode, st.Diagnostics)
	}
	if st.Version != 1 {
		t.Errorf("negotiated version = %d, want 1", st.Version)
	}
	if st.ID != "fake" {
		t.Errorf("provider id must come from the manifest, not the discovery id: got %q", st.ID)
	}
	if st.Manifest == nil || len(st.Manifest.Capabilities) != 1 ||
		st.Manifest.Capabilities[0].CapabilityID != "test.echo" {
		t.Fatalf("manifest must be kept with its capability: %+v", st.Manifest)
	}
	if st.Available == nil || !st.Available.Available {
		t.Errorf("availability report must be attached: %+v", st.Available)
	}
	if len(st.Diagnostics) != 0 {
		t.Errorf("clean probe must emit no diagnostics: %v", st.Diagnostics)
	}
}

// Probe failure modes degrade to ProbeErrorCode plus diagnostics, never a
// hard error. Each row pins one documented branch:
//   - describe cannot run at all → empty code (no manifest verdict);
//   - manifest valid but no common version → unsupported_schema_version;
//   - manifest violates the schema → malformed_manifest;
//   - available fails → fail-safe: never downgrades a probed provider.
func TestProbeFailureModes(t *testing.T) {
	availableOK := `printf '%s\n' '{"available":true,"ready":[],"missing":[]}'`
	describe := func(manifest string) string {
		return "printf '%s\\n' '" + manifest + "'"
	}
	clean := "Return the supplied value without changing it."
	cases := []struct {
		name       string
		describe   string
		available  string
		wantProbed bool
		wantCode   string
		wantDiag   string // substring one diagnostic must contain; "" = any diagnostic
		wantAvail  bool
	}{
		{"describe exits nonzero", "exit 1", availableOK,
			false, "", "describe probe failed", true},
		{"no common protocol version", describe(probeManifest(clean, "[9]", "[]")), availableOK,
			false, protocol.ErrUnsupportedSchemaVersion, "provider offers", true},
		{"manifest violates schema", describe(probeManifest(clean, "[1]", `["teleport"]`)), availableOK,
			false, protocol.ErrMalformedManifest, "", true},
		{"available failure never downgrades", describe(probeManifest(clean, "[1]", "[]")), "exit 1",
			true, "", "", false},
		{"available report violates schema", describe(probeManifest(clean, "[1]", "[]")),
			`printf '%s\n' '{"available":"yes","ready":[],"missing":[]}'`,
			true, "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := Probe(context.Background(), "fake", probeFixtureRaw(t, tc.describe, tc.available))
			if st.Probed != tc.wantProbed {
				t.Errorf("Probed = %v, want %v (diags=%v)", st.Probed, tc.wantProbed, st.Diagnostics)
			}
			if st.ProbeErrorCode != tc.wantCode {
				t.Errorf("ProbeErrorCode = %q, want %q", st.ProbeErrorCode, tc.wantCode)
			}
			if !tc.wantProbed && len(st.Diagnostics) == 0 {
				t.Errorf("a failed describe must leave a diagnostic")
			}
			if tc.wantDiag != "" {
				found := false
				for _, d := range st.Diagnostics {
					if strings.Contains(d.Message, tc.wantDiag) {
						found = true
					}
				}
				if !found {
					t.Errorf("a diagnostic must contain %q: %v", tc.wantDiag, st.Diagnostics)
				}
			}
			if tc.wantAvail && st.Available == nil {
				t.Errorf("availability probing is independent of describe; report missing: %+v", st.Available)
			}
			if !tc.wantAvail && st.Available != nil {
				t.Errorf("failed available probe must leave Available nil: %+v", st.Available)
			}
		})
	}
}
