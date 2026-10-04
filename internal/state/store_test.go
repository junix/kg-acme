package state

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	coreprotocol "github.com/junix/acme-core/protocol"
	corestate "github.com/junix/acme-core/state"

	"kg-acme/internal/protocol"
	"kg-acme/internal/surface"
)

// validSnapshot returns a minimal snapshot that satisfies every load-hardening
// rule (kg.snapshot/v3 schema, fingerprint, kg.* semantic IDs with a matching
// group taxonomy), so the strict-decode behavior can be isolated.
func validSnapshot() surface.Snapshot {
	return surface.Snapshot{
		SchemaVersion: surface.SnapshotSchema,
		Fingerprint:   "3f2a9c01",
		CreatedAt:     time.Date(2026, 10, 4, 6, 48, 0, 0, time.UTC),
		Groups: []coreprotocol.CapabilityGroup{{
			ID:          "store",
			Title:       "Store",
			Description: "Persist knowledge-graph data in supported graph databases.",
			Order:       0,
		}},
		Capabilities: []surface.Capability{{
			SemanticID:   "kg.store.triples",
			Title:        "Store triples",
			Description:  "Store triples in a supported graph database.",
			Available:    true,
			InputSchema:  json.RawMessage(`{"type":"object","properties":{"dataset":{"type":"string"}},"required":["dataset"],"additionalProperties":false}`),
			OutputSchema: json.RawMessage(`{"type":"object","description":"Structured JSON result returned by the selected capability."}`),
			Output:       protocol.OutputSpec{Mode: "result-json", Kind: "json"},
			Source:       surface.Source{IntegrationPath: "/projects/kg", ImplementationPaths: []string{"/projects/graphdb"}},
		}},
	}
}

// The snapshot on disk flows through acme-core DecodeStrict before the CLI or
// MCP server may use it, so a duplicated JSON key must fail the load even when
// last-wins decoding would still produce a hardening-passing document.
func TestLoadSnapshotRejectsDuplicateKeyBeforeUse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "capability-snapshot.json")
	if err := SaveSnapshot(path, validSnapshot()); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Duplicate the fingerprint pair so the document stays otherwise valid:
	// both last-wins values are non-empty fingerprints, so only the
	// duplicate-key check (not incidental hardening) can reject it.
	fingerprint := regexp.MustCompile(`"fingerprint"\s*:\s*"[^"]*"`)
	if len(fingerprint.FindAllString(string(data), -1)) != 1 {
		t.Fatal("test setup: expected exactly one fingerprint pair")
	}
	tampered := fingerprint.ReplaceAllString(string(data), "${0}, ${0}")
	if err := os.WriteFile(path, []byte(tampered), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot, err := LoadSnapshot(path)
	if err == nil {
		t.Fatal("duplicate JSON key in snapshot must be rejected")
	}
	if !strings.Contains(err.Error(), "duplicate object key") || !strings.Contains(err.Error(), "fingerprint") {
		t.Fatalf("expected duplicate-object-key error naming the fingerprint path, got: %v", err)
	}
	if !reflect.DeepEqual(snapshot, surface.Snapshot{}) {
		t.Fatalf("rejected snapshot must not be usable, got %+v", snapshot)
	}
}

// A valid persisted snapshot must still load and round-trip through the hub's
// own save/load pair once the strict decoder is in force.
func TestLoadSnapshotRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "capability-snapshot.json")
	want := validSnapshot()
	if err := SaveSnapshot(path, want); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}
	got, err := LoadSnapshot(path)
	if err != nil {
		t.Fatalf("valid snapshot must load: %v", err)
	}
	// The atomic writer indents embedded RawMessage schemas, so compare the
	// re-marshaled documents: loading must preserve the snapshot's content.
	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotJSON, wantJSON) {
		t.Fatalf("snapshot must round-trip unchanged:\ngot  %s\nwant %s", gotJSON, wantJSON)
	}
	if got.Fingerprint != want.Fingerprint || len(got.Capabilities) != 1 || got.Capabilities[0].SemanticID != "kg.store.triples" {
		t.Fatalf("loaded snapshot lost expected content: %+v", got)
	}
}

// routes.json is the second persisted control-plane document decoded via
// acme-core: a duplicated route entry must be rejected before routing uses it
// (with plain map decoding the second provider would silently win), while a
// valid document still loads and round-trips.
func TestRoutesDuplicateKeyRejectedAndRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routes.json")

	duplicate := "{\n  \"schema_version\": \"kg.routes/v1\",\n  \"routes\": {\n" +
		"    \"kg.store.triples\": \"graph-kg\",\n" +
		"    \"kg.store.triples\": \"kg-algorithms\"\n" +
		"  }\n}\n"
	if err := os.WriteFile(path, []byte(duplicate), 0o644); err != nil {
		t.Fatal(err)
	}
	routes, err := LoadRoutes(path)
	if err == nil {
		t.Fatal("duplicate JSON key in routes config must be rejected")
	}
	if !strings.Contains(err.Error(), "duplicate object key") || !strings.Contains(err.Error(), "kg.store.triples") {
		t.Fatalf("expected duplicate-object-key error naming the route path, got: %v", err)
	}
	if len(routes.Routes) != 0 {
		t.Fatalf("rejected routes must not be usable, got %+v", routes)
	}

	valid := corestate.Routes{Routes: map[string]string{"kg.store.triples": "graph-kg"}}
	if err := SaveRoutes(path, valid); err != nil {
		t.Fatalf("save routes: %v", err)
	}
	got, err := LoadRoutes(path)
	if err != nil {
		t.Fatalf("valid routes config must load: %v", err)
	}
	if got.SchemaVersion != corestate.RoutesSchema("kg") {
		t.Fatalf("schema version = %q, want %q", got.SchemaVersion, corestate.RoutesSchema("kg"))
	}
	if !reflect.DeepEqual(got.Routes, valid.Routes) {
		t.Fatalf("routes must round-trip: got %+v, want %+v", got.Routes, valid.Routes)
	}
}
