package policy

import (
	"strings"
	"testing"
)

func TestDefaultDenyAll(t *testing.T) {
	g := Gates{}
	effects := []string{Network, DataEgress, DownloadsModels, WritesDB}
	if denied := g.Denied(effects); len(denied) != 4 {
		t.Errorf("default gates must deny everything, denied %v", denied)
	}
	if err := g.Check(effects); err == nil {
		t.Error("Check must fail with default gates")
	}
}

func TestExplicitAllow(t *testing.T) {
	g := Gates{AllowNetwork: true, AllowModelDownload: true}
	if denied := g.Denied([]string{Network, DownloadsModels}); len(denied) != 0 {
		t.Errorf("explicitly allowed effects must pass, denied %v", denied)
	}
	if err := g.Check([]string{Network, DownloadsModels}); err != nil {
		t.Errorf("Check should pass: %v", err)
	}
	if denied := g.Denied([]string{Network, WritesDB}); len(denied) != 1 || denied[0] != WritesDB {
		t.Errorf("writes_db must still be denied, got %v", denied)
	}
}

func TestUnknownEffectDenied(t *testing.T) {
	g := Gates{AllowNetwork: true, AllowDataEgress: true, AllowModelDownload: true, AllowDBWrite: true}
	if denied := g.Denied([]string{"launch_missiles"}); len(denied) != 1 {
		t.Errorf("unknown side effects fail closed, denied %v", denied)
	}
}

func TestCheckMessageNamesFlags(t *testing.T) {
	g := Gates{}
	err := g.Check([]string{Network, WritesDB})
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	for _, want := range []string{"network", "writes_db", "--allow-network", "--allow-db-write"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message should mention %q: %s", want, msg)
		}
	}
}

func TestAllowFlagMapping(t *testing.T) {
	cases := map[string]string{
		Network:         "--allow-network",
		DataEgress:      "--allow-data-egress",
		DownloadsModels: "--allow-model-download",
		WritesDB:        "--allow-db-write",
		"other":         "",
	}
	for effect, flag := range cases {
		if got := AllowFlag(effect); got != flag {
			t.Errorf("AllowFlag(%q) = %q, want %q", effect, got, flag)
		}
	}
}

// Merge OR's two gate sets together: the result must allow anything either
// input allows, and the receiver is not mutated.
func TestMerge(t *testing.T) {
	a := Gates{AllowNetwork: true}
	b := Gates{AllowDataEgress: true, AllowDBWrite: true}
	merged := a.Merge(b)
	want := Gates{AllowNetwork: true, AllowDataEgress: true, AllowDBWrite: true}
	if merged != want {
		t.Errorf("Merge = %+v, want %+v", merged, want)
	}
	// Inputs are unchanged (Merge returns a new value).
	if a.AllowDataEgress || a.AllowDBWrite {
		t.Errorf("receiver mutated by Merge: %+v", a)
	}
	if b.AllowNetwork {
		t.Errorf("argument mutated by Merge: %+v", b)
	}
	// Merging two empty gate sets stays fully closed.
	if merged2 := (Gates{}).Merge(Gates{}); merged2 != (Gates{}) {
		t.Errorf("empty Merge should stay closed: %+v", merged2)
	}
	// Merging is idempotent for already-open gates.
	all := Gates{AllowNetwork: true, AllowDataEgress: true, AllowModelDownload: true, AllowDBWrite: true}
	if merged3 := all.Merge(all); merged3 != all {
		t.Errorf("idempotent Merge changed the set: %+v", merged3)
	}
}

func TestParseGates(t *testing.T) {
	cases := []struct {
		name    string
		spec    string
		want    Gates
		wantErr bool
	}{
		// Each named token opens exactly its gate.
		{"network", "network", Gates{AllowNetwork: true}, false},
		{"data_egress", "data_egress", Gates{AllowDataEgress: true}, false},
		{"downloads_models", "downloads_models", Gates{AllowModelDownload: true}, false},
		{"writes_db", "writes_db", Gates{AllowDBWrite: true}, false},
		// "*" opens every gate.
		{"all", "*", Gates{true, true, true, true}, false},
		// Comma-separated tokens accumulate.
		{"comma pair", "network,writes_db", Gates{AllowNetwork: true, AllowDBWrite: true}, false},
		// Semicolons and whitespace are all valid separators (including tabs/newlines).
		{"mixed separators", "network;\ndata_egress\twrites_db",
			Gates{AllowNetwork: true, AllowDataEgress: true, AllowDBWrite: true}, false},
		// Empty / whitespace-only spec parses to all-closed gates (no error).
		{"empty", "", Gates{}, false},
		{"whitespace only", "  \t\n ", Gates{}, false},
		// Duplicate tokens are harmless (idempotent set).
		{"duplicate", "network,network", Gates{AllowNetwork: true}, false},
		// An unknown token fails loudly — server config must never silently
		// allow or deny the wrong gate.
		{"unknown token", "network,launch_missiles", Gates{}, true},
		{"misspelled", "networ", Gates{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseGates(tc.spec)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseGates(%q): expected error, got %+v", tc.spec, got)
				}
				if !strings.Contains(err.Error(), "unknown side effect") {
					t.Errorf("ParseGates(%q) error should explain the unknown token: %v", tc.spec, err)
				}
				// On error the returned gates are the zero value (caller must
				// not use a partially-built set).
				if got != (Gates{}) {
					t.Errorf("ParseGates(%q) returned non-zero gates on error: %+v", tc.spec, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseGates(%q): unexpected error: %v", tc.spec, err)
			}
			if got != tc.want {
				t.Errorf("ParseGates(%q) = %+v, want %+v", tc.spec, got, tc.want)
			}
		})
	}
}

// An unknown side effect carries no AllowFlag, so Check must still name the
// denied effect but omit the "(allow explicitly with ...)" suffix.
func TestCheckUnknownEffectNoHint(t *testing.T) {
	err := Gates{}.Check([]string{"launch_missiles"})
	if err == nil {
		t.Fatal("unknown effect must be denied")
	}
	msg := err.Error()
	if !strings.Contains(msg, "launch_missiles") {
		t.Errorf("error should name the denied effect: %s", msg)
	}
	if strings.Contains(msg, "allow explicitly") {
		t.Errorf("error must omit the hint when no flag maps: %s", msg)
	}
}
