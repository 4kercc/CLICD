//go:build !linux

package api

import "clicd/internal/config"

// snapshotUniqueBytes is unavailable off Linux (extent maps are a Linux
// filesystem feature), so callers fall back to reporting the logical size only.
func snapshotUniqueBytes(snapshot *config.Snapshot) *int64 {
	return nil
}

func decorateSnapshotUsage(snapshots []config.Snapshot) []config.Snapshot {
	return snapshots
}
