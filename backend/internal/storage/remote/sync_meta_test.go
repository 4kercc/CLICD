package remote

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"clicd/internal/config"
)

func TestRemoteSnapshotBasePathUsesHostAndContainerName(t *testing.T) {
	t.Setenv("CLICD_SOURCE_HOST_ID", "192.3.170.78")
	snap := &config.Snapshot{
		ID:            "snap-25-20260927220916-000000000",
		ContainerID:   25,
		ContainerName: "jsq-win-2019",
	}
	got := remoteSnapshotBasePath(snap)
	want := "snapshots/192.3.170.78/jsq-win-2019/snap-25-20260927220916-000000000"
	if got != want {
		t.Fatalf("remoteSnapshotBasePath() = %q, want %q", got, want)
	}

	bkp := &config.Backup{
		ID:            "backup-25-20261002-000000000",
		ContainerID:   25,
		ContainerName: "jsq-win-2019",
	}
	if got := remoteBackupBasePath(bkp); got != "backups/192.3.170.78/jsq-win-2019/backup-25-20261002-000000000" {
		t.Fatalf("remoteBackupBasePath() = %q", got)
	}
}

func TestSanitizePathSegment(t *testing.T) {
	cases := []struct{ in, want string }{
		{"jsq-win-2019", "jsq-win-2019"},
		{"jsq win/2019", "jsq-win-2019"},
		{"../etc/passwd", "etc-passwd"},
		{"  spaced  ", "spaced"},
		{"my host/../name", "my-host-..-name"}, // interior ".." is inert: one flat segment, no separator
		{"", "unnamed-d41d8cd9"},
	}
	for _, tc := range cases {
		if got := SanitizePathSegment(tc.in); got != tc.want {
			t.Fatalf("SanitizePathSegment(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	// A purely non-ASCII name collapses to nothing and must not produce an
	// empty path segment; the fallback keeps distinct names distinct.
	a := SanitizePathSegment("主机名")
	b := SanitizePathSegment("容器名")
	if a == "" || b == "" || a == b {
		t.Fatalf("non-ASCII names must map to distinct non-empty segments, got %q and %q", a, b)
	}
	if !strings.HasPrefix(a, "unnamed-") {
		t.Fatalf("non-ASCII fallback should be unnamed-<hash>, got %q", a)
	}
}

func TestSourceHostIDPrefersEnvThenSanitizes(t *testing.T) {
	t.Setenv("CLICD_SOURCE_HOST_ID", "my host/../name")
	if got := SourceHostID(); got != "my-host-..-name" {
		t.Fatalf("SourceHostID() = %q, want my-host-..-name", got)
	}
}

func TestWriteSnapshotMeta(t *testing.T) {
	dir := t.TempDir()
	unique := int64(4096)
	snap := &config.Snapshot{
		ID:            "snap-25-20260927220916-000000000",
		ContainerID:   25,
		ContainerName: "jsq-win-2019",
		Description:   "升级内核前的备份",
		CreatedAt:     "2026-09-27 22:09:16",
		CreatedBy:     "admin",
		Path:          dir,
		SizeBytes:     64 << 30,
		UniqueBytes:   &unique,
	}
	t.Setenv("CLICD_SOURCE_HOST_ID", "192.3.170.78")
	writeSnapshotMeta(dir, snap)

	data, err := os.ReadFile(filepath.Join(dir, "snapshot-meta.json"))
	if err != nil {
		t.Fatalf("meta file missing: %v", err)
	}
	var meta map[string]interface{}
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("meta is not valid JSON: %v", err)
	}
	for key, want := range map[string]interface{}{
		"snapshot_id":    "snap-25-20260927220916-000000000",
		"type":           "snapshot",
		"source_host":    "192.3.170.78",
		"container_name": "jsq-win-2019",
		"description":    "升级内核前的备份",
		"unique_bytes":   float64(4096),
	} {
		if got := meta[key]; got != want {
			t.Fatalf("meta[%q] = %v, want %v", key, got, want)
		}
	}
}

func TestWriteBackupMeta(t *testing.T) {
	dir := t.TempDir()
	bkp := &config.Backup{
		ID:            "backup-3-20261002-000000000",
		ContainerID:   3,
		ContainerName: "kylin-v10",
		Format:        "qcow2",
		Compressed:    true,
		Path:          dir,
	}
	t.Setenv("CLICD_SOURCE_HOST_ID", "192.3.170.78")
	writeBackupMeta(dir, bkp)

	data, err := os.ReadFile(filepath.Join(dir, "backup-meta.json"))
	if err != nil {
		t.Fatalf("meta file missing: %v", err)
	}
	var meta map[string]interface{}
	_ = json.Unmarshal(data, &meta)
	if meta["type"] != "backup" || meta["container_name"] != "kylin-v10" || meta["compressed"] != true {
		t.Fatalf("unexpected meta content: %s", data)
	}
}
