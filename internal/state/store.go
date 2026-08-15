package state

import (
	"fmt"
	"os"

	"github.com/junix/acme-core/navigation"
	corestate "github.com/junix/acme-core/state"

	"kg-acme/internal/surface"
)

func DefaultSnapshotPath() (string, error) { return corestate.DefaultSnapshotPath("kg") }
func DefaultRoutesPath() (string, error)   { return corestate.DefaultRoutesPath("kg") }

func SaveSnapshot(path string, snapshot surface.Snapshot) error {
	return corestate.WriteJSONAtomically(path, snapshot)
}

func LoadSnapshot(path string) (surface.Snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return surface.Snapshot{}, err
	}
	var snapshot surface.Snapshot
	if err := corestate.DecodeStrict(data, &snapshot); err != nil {
		return surface.Snapshot{}, fmt.Errorf("decode snapshot: %w", err)
	}
	views := make([]navigation.CapabilityView, 0, len(snapshot.Capabilities))
	for _, capability := range snapshot.Capabilities {
		views = append(views, navigation.CapabilityView{SemanticID: capability.SemanticID, Description: capability.Description, Available: capability.Available})
	}
	if err := corestate.HardenSnapshot("kg", surface.SnapshotSchema, snapshot.SchemaVersion, snapshot.Fingerprint, views, snapshot.Groups); err != nil {
		return surface.Snapshot{}, err
	}
	return snapshot, nil
}

func LoadRoutes(path string) (corestate.Routes, error) {
	return corestate.LoadRoutes(path, corestate.RoutesSchema("kg"))
}
func SaveRoutes(path string, routes corestate.Routes) error {
	return corestate.SaveRoutes(path, routes, corestate.RoutesSchema("kg"))
}
