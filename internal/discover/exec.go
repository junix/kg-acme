package discover

import (
	"context"
	"os/exec"
)

// probeCommand is a seam for tests and platform differences.
func probeCommand(ctx context.Context, path string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, path, args...)
}
