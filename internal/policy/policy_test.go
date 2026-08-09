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
