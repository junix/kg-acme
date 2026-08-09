package discover

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
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
