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

// probeFixture writes a fake provider script whose describe output carries one
// capability with the given description, and returns its path.
func probeFixture(t *testing.T, description string) string {
	t.Helper()
	manifest := fmt.Sprintf(`{"protocol":"kg.provider/v1","protocol_versions":[1],"provider":{"id":"fake","version":"1.0.0","description":"Fake provider"},"capabilities":[{"capability_id":"test.echo","title":"Echo a value","description":%s,"side_effects":[],"input_schema":{"type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false},"output":{"mode":"result-json","kind":"json"},"cli_spec":{"subcommand":[],"always":[],"positionals":[],"flags":[]}}]}`,
		strconv.Quote(description))
	script := "#!/bin/sh\ncase \"$1\" in\n" +
		"  describe) printf '%s\\n' '" + manifest + "' ;;\n" +
		"  available) printf '%s\\n' '{\"available\":true,\"ready\":[],\"missing\":[]}' ;;\n" +
		"esac\n"
	path := filepath.Join(t.TempDir(), "fake-provider")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
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
