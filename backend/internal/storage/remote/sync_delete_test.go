package remote

import (
	"reflect"
	"testing"

	"clicd/internal/config"
)

// Deleting a snapshot has to reach copies recorded under either naming scheme,
// because snapshots that predate the host/instance layout (and any whose
// recorded path was lost) would otherwise survive as remote orphans.
func TestSnapshotRemoteCandidatesCoverBothSchemes(t *testing.T) {
	t.Setenv("CLICD_SOURCE_HOST_ID", "192.3.170.78")
	snap := &config.Snapshot{
		ID:            "snap-25-20260927220916-000000000",
		ContainerID:   25,
		ContainerName: "jsq-win-2019",
	}
	got := SnapshotRemoteCandidates(snap)
	want := []string{
		"snapshots/192.3.170.78/jsq-win-2019/snap-25-20260927220916-000000000",
		"snapshots/25/snap-25-20260927220916-000000000",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SnapshotRemoteCandidates() = %#v, want %#v", got, want)
	}
}

func TestSnapshotRemoteCandidatesIncludeRecordedPathWithoutDuplicates(t *testing.T) {
	t.Setenv("CLICD_SOURCE_HOST_ID", "192.3.170.78")
	snap := &config.Snapshot{
		ID:            "snap-3-20260914213934-000000000",
		ContainerID:   3,
		ContainerName: "kylin-v10",
		RemotePath:    "snapshots/3/snap-3-20260914213934-000000000",
	}
	got := SnapshotRemoteCandidates(snap)
	want := []string{
		"snapshots/3/snap-3-20260914213934-000000000",
		"snapshots/192.3.170.78/kylin-v10/snap-3-20260914213934-000000000",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates with a recorded path = %#v, want %#v", got, want)
	}
}

func TestBackupRemoteCandidates(t *testing.T) {
	t.Setenv("CLICD_SOURCE_HOST_ID", "192.3.170.78")
	bkp := &config.Backup{ID: "backup-8-20260912125851", ContainerID: 8, ContainerName: "dm-1"}
	got := BackupRemoteCandidates(bkp)
	want := []string{
		"backups/192.3.170.78/dm-1/backup-8-20260912125851",
		"backups/8/backup-8-20260912125851",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BackupRemoteCandidates() = %#v, want %#v", got, want)
	}
}

// An empty container name must never collapse into a path that could match
// another instance's directory.
func TestRemoteCandidatesSkipEmptySegments(t *testing.T) {
	t.Setenv("CLICD_SOURCE_HOST_ID", "192.3.170.78")
	snap := &config.Snapshot{ID: "", ContainerID: 0, ContainerName: ""}
	for _, candidate := range SnapshotRemoteCandidates(snap) {
		if candidate == "" || candidate == "/" {
			t.Fatalf("candidate %q is not a real path", candidate)
		}
	}
}

func TestPickRemotePoolPrefersRecordedThenEnabled(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig = &config.ClicdConfig{StoragePools: []config.StoragePool{
		{ID: "disk-root", Type: "local", Enabled: true},
		{ID: "pool-a", Type: "sftp", Enabled: true, SyncSnapshots: true},
		{ID: "pool-b", Type: "webdav", Enabled: true, SyncSnapshots: false},
		{ID: "pool-c", Type: "sftp", Enabled: false, SyncSnapshots: true},
	}}

	if pool, ok := remotePoolForSnapshot(&config.Snapshot{RemoteStoragePoolID: "pool-b"}); !ok || pool.ID != "pool-b" {
		t.Fatalf("recorded pool should win, got %v ok=%v", pool.ID, ok)
	}
	if pool, ok := remotePoolForSnapshot(&config.Snapshot{}); !ok || pool.ID != "pool-a" {
		t.Fatalf("fallback should be the first enabled snapshot pool, got %v ok=%v", pool.ID, ok)
	}
	// Backups sync to a different set: only pool-b asks for them, and it is
	// disabled, so nothing matches.
	if pool, ok := remotePoolForBackup(&config.Backup{}); ok {
		t.Fatalf("no backup pool is enabled, got %v", pool.ID)
	}
}
