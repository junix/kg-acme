// Package policy implements the hub's side-effect gates.
//
// Every capability declares side_effects (network, data_egress,
// downloads_models, writes_db). All gates are closed by default; each must
// be opened explicitly with its --allow-* flag. --dry-run renders the
// execution plan with zero side effects.
package policy

import (
	"fmt"
	"strings"
)

// Side effect identifiers (as declared by providers).
const (
	Network         = "network"
	DataEgress      = "data_egress"
	DownloadsModels = "downloads_models"
	WritesDB        = "writes_db"
)

// Gates holds the operator's explicit allowances.
type Gates struct {
	AllowNetwork        bool
	AllowDataEgress     bool
	AllowModelDownload  bool
	AllowDBWrite        bool
}

// AllowFlag maps a side effect to the CLI flag that opens its gate.
func AllowFlag(effect string) string {
	switch effect {
	case Network:
		return "--allow-network"
	case DataEgress:
		return "--allow-data-egress"
	case DownloadsModels:
		return "--allow-model-download"
	case WritesDB:
		return "--allow-db-write"
	default:
		return ""
	}
}

func (g Gates) allowed(effect string) bool {
	switch effect {
	case Network:
		return g.AllowNetwork
	case DataEgress:
		return g.AllowDataEgress
	case DownloadsModels:
		return g.AllowModelDownload
	case WritesDB:
		return g.AllowDBWrite
	default:
		// Unknown side effects are denied by default (fail-closed).
		return false
	}
}

// Denied returns the declared effects whose gates are closed.
func (g Gates) Denied(effects []string) []string {
	var denied []string
	for _, e := range effects {
		if !g.allowed(e) {
			denied = append(denied, e)
		}
	}
	return denied
}

// Check returns nil when every declared effect is allowed, else an error
// naming the denied effects and the flags that would open them.
func (g Gates) Check(effects []string) error {
	denied := g.Denied(effects)
	if len(denied) == 0 {
		return nil
	}
	var hints []string
	for _, e := range denied {
		if f := AllowFlag(e); f != "" {
			hints = append(hints, f)
		}
	}
	msg := fmt.Sprintf("side effects denied by policy: %s", strings.Join(denied, ", "))
	if len(hints) > 0 {
		msg += fmt.Sprintf(" (allow explicitly with %s)", strings.Join(hints, " "))
	}
	return fmt.Errorf("%s", msg)
}
