package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"clicd/internal/config"
	"clicd/internal/version"
)

// SyncProgress represents live file transfer progress.
type SyncProgress struct {
	ID              string `json:"id"`
	Type            string `json:"type"` // "backup_upload", "backup_download", "snapshot_upload", "snapshot_download"
	Stage           string `json:"stage"` // "preparing", "transferring", "completed", "failed"
	CurrentFile     string `json:"current_file"`
	TransferredBytes int64 `json:"transferred_bytes"`
	TotalBytes      int64  `json:"total_bytes"`
	Percent         int    `json:"percent"`
	SpeedBps        int64  `json:"speed_bps"`
	Error           string `json:"error,omitempty"`
	UpdatedAt       int64  `json:"updated_at"`
}

var (
	progressMu sync.RWMutex
	progressMap = make(map[string]*SyncProgress)
	syncOpLocks sync.Map
)

// Transfer deadlines. A snapshot is tens of gigabytes, and a fixed half-hour
// budget kills the transfer mid-flight on any link slower than ~28 MB/s — which
// is what turned large nightly snapshots into "EOF" failures while the small
// ones kept succeeding. Resumable transfers make a generous deadline safe: a
// run that still fails continues where it stopped.
const (
	snapshotTransferTimeout = 6 * time.Hour
	backupTransferTimeout   = 12 * time.Hour
)

func acquireSyncLock(id string) func() {
	raw, _ := syncOpLocks.LoadOrStore(id, &sync.Mutex{})
	mu := raw.(*sync.Mutex)
	mu.Lock()
	return func() {
		mu.Unlock()
	}
}

// SetProgress updates or stores the sync progress for a given ID.
func SetProgress(p SyncProgress) {
	progressMu.Lock()
	defer progressMu.Unlock()
	p.UpdatedAt = time.Now().UnixMilli()
	if p.TotalBytes > 0 && p.Percent == 0 {
		p.Percent = int(p.TransferredBytes * 100 / p.TotalBytes)
	}
	if p.Percent > 100 {
		p.Percent = 100
	}
	progressMap[p.ID] = &p
}

// GetProgress returns the current sync progress for a given ID.
func GetProgress(id string) *SyncProgress {
	progressMu.RLock()
	defer progressMu.RUnlock()
	if p, ok := progressMap[id]; ok {
		cp := *p
		return &cp
	}
	return nil
}

// ClearProgress removes the sync progress entry.
func ClearProgress(id string) {
	progressMu.Lock()
	defer progressMu.Unlock()
	delete(progressMap, id)
}

// calculateDirSize computes the total size of files under a path.
func calculateDirSize(dir string) int64 {
	var total int64
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}

// remoteSnapshotBasePath builds the remote directory for a snapshot:
// snapshots/<source-host>/<container-name>/<snapshot-id>. The source host and
// the container name make the remote tree self-describing across multiple
// CLICD hosts — bare numeric container IDs collide between hosts and say
// nothing about what a snapshot belongs to.
func remoteSnapshotBasePath(snap *config.Snapshot) string {
	return fmt.Sprintf("snapshots/%s/%s/%s", SourceHostID(), SanitizePathSegment(snap.ContainerName), snap.ID)
}

// remoteBackupBasePath is the backup counterpart of remoteSnapshotBasePath.
func remoteBackupBasePath(bkp *config.Backup) string {
	return fmt.Sprintf("backups/%s/%s/%s", SourceHostID(), SanitizePathSegment(bkp.ContainerName), bkp.ID)
}

// writeSnapshotMeta drops a self-describing snapshot-meta.json next to the
// snapshot files, so the remote copy (and the local one) still says which
// host, instance, snapshot and description it belongs to even when the panel
// database is gone. The file is written before the directory walk, so it
// uploads with the rest.
func writeSnapshotMeta(dir string, snap *config.Snapshot) {
	if dir == "" {
		return
	}
	meta := map[string]interface{}{
		"snapshot_id":   snap.ID,
		"type":          "snapshot",
		"source_host":   SourceHostID(),
		"container_id":  snap.ContainerID,
		"container_name": snap.ContainerName,
		"description":   snap.Description,
		"created_at":    snap.CreatedAt,
		"created_by":    snap.CreatedBy,
		"size_bytes":    snap.SizeBytes,
		"clicd_version": version.Current(),
		"generated_at":  time.Now().Format("2006-01-02 15:04:05"),
	}
	// The overlay is useless without the base it sits on, so name it here: an
	// offsite restore reads this file, not the panel database.
	if snap.BaseImage != "" {
		meta["base_image"] = snap.BaseImage
		meta["base_image_remote"] = BaseImageRemotePath(snap.ContainerName, snap.BaseImage)
	}
	if snap.UniqueBytes != nil {
		meta["unique_bytes"] = *snap.UniqueBytes
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(filepath.Join(dir, "snapshot-meta.json"), data, 0o644); err != nil {
		fmt.Printf("Warning: failed to write snapshot meta for %s: %v\n", snap.ID, err)
	}
}

// writeBackupMeta is the backup counterpart of writeSnapshotMeta.
func writeBackupMeta(dir string, bkp *config.Backup) {
	if dir == "" {
		return
	}
	meta := map[string]interface{}{
		"backup_id":     bkp.ID,
		"type":          "backup",
		"source_host":   SourceHostID(),
		"container_id":  bkp.ContainerID,
		"container_name": bkp.ContainerName,
		"created_at":    bkp.CreatedAt,
		"created_by":    bkp.CreatedBy,
		"format":        bkp.Format,
		"compressed":    bkp.Compressed,
		"size_bytes":    bkp.SizeBytes,
		"clicd_version": version.Current(),
		"generated_at":  time.Now().Format("2006-01-02 15:04:05"),
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(filepath.Join(dir, "backup-meta.json"), data, 0o644); err != nil {
		fmt.Printf("Warning: failed to write backup meta for %s: %v\n", bkp.ID, err)
	}
}

// BaseImageRemotePath is where an instance's base image lives on the pool. It
// sits under the same source host and instance name as that instance's
// snapshots, because a snapshot overlay is useless without the base it was
// taken on.
func BaseImageRemotePath(containerName, baseFilePath string) string {
	return fmt.Sprintf("bases/%s/%s/%s", SourceHostID(), SanitizePathSegment(containerName), SanitizePathSegment(path.Base(baseFilePath)))
}

// SyncBaseImageToPool uploads an instance's base image to the pool that syncs
// snapshots, returning the remote path. It is idempotent and resumable, so
// calling it again after an interruption continues rather than restarts — a
// base image is tens of gigabytes.
func SyncBaseImageToPool(containerName, localPath string) (string, error) {
	localPath = strings.TrimSpace(localPath)
	if localPath == "" {
		return "", fmt.Errorf("base image path is empty")
	}
	if _, err := os.Stat(localPath); err != nil {
		return "", fmt.Errorf("base image not readable: %w", err)
	}
	pool, ok := pickRemotePool("", func(pool config.StoragePool) bool { return pool.SyncSnapshots })
	if !ok {
		return "", fmt.Errorf("no enabled remote pool syncs snapshots")
	}
	client, err := NewClient(pool.Type, pool.Config)
	if err != nil {
		return "", fmt.Errorf("remote storage sync client init error for %s: %w", pool.Name, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), snapshotTransferTimeout)
	defer cancel()

	remotePath := BaseImageRemotePath(containerName, localPath)
	if err := client.UploadFile(ctx, localPath, remotePath); err != nil {
		return "", err
	}
	fmt.Printf("Successfully synced base image %s to remote storage %s (%s)\n", path.Base(localPath), pool.Name, remotePath)
	return remotePath, nil
}

// SnapshotRemoteCandidates lists the remote directories a snapshot may occupy:
// the recorded path when one was stored, plus the paths the current and the
// legacy naming schemes imply. Snapshots synced before the host/instance layout
// (or whose recorded path was lost) would otherwise stay behind as orphans when
// they are deleted locally.
func SnapshotRemoteCandidates(snap *config.Snapshot) []string {
	if snap == nil {
		return nil
	}
	return remoteCandidates(
		snap.RemotePath,
		fmt.Sprintf("snapshots/%s/%s/%s", SourceHostID(), SanitizePathSegment(snap.ContainerName), snap.ID),
		fmt.Sprintf("snapshots/%d/%s", snap.ContainerID, snap.ID),
	)
}

// BackupRemoteCandidates is the backup counterpart of SnapshotRemoteCandidates.
func BackupRemoteCandidates(bkp *config.Backup) []string {
	if bkp == nil {
		return nil
	}
	return remoteCandidates(
		bkp.RemotePath,
		fmt.Sprintf("backups/%s/%s/%s", SourceHostID(), SanitizePathSegment(bkp.ContainerName), bkp.ID),
		fmt.Sprintf("backups/%d/%s", bkp.ContainerID, bkp.ID),
	)
}

func remoteCandidates(paths ...string) []string {
	out := make([]string, 0, len(paths))
	for _, candidate := range paths {
		candidate = strings.Trim(strings.TrimSpace(candidate), "/")
		if candidate == "" {
			continue
		}
		duplicate := false
		for _, existing := range out {
			if existing == candidate {
				duplicate = true
				break
			}
		}
		if !duplicate {
			out = append(out, candidate)
		}
	}
	return out
}

// remotePoolForSnapshot picks the pool to delete from: the one the snapshot was
// synced to when that is still known, otherwise the first enabled pool that
// syncs snapshots.
func remotePoolForSnapshot(snap *config.Snapshot) (config.StoragePool, bool) {
	return pickRemotePool(snap.RemoteStoragePoolID, func(pool config.StoragePool) bool {
		return pool.SyncSnapshots
	})
}

func remotePoolForBackup(bkp *config.Backup) (config.StoragePool, bool) {
	return pickRemotePool(bkp.RemoteStoragePoolID, func(pool config.StoragePool) bool {
		return pool.SyncBackups
	})
}

func pickRemotePool(preferred string, wanted func(config.StoragePool) bool) (config.StoragePool, bool) {
	if config.AppConfig == nil {
		return config.StoragePool{}, false
	}
	for _, pool := range config.AppConfig.StoragePools {
		if pool.ID == preferred && pool.Enabled && pool.Type != "" && pool.Type != "local" {
			return pool, true
		}
	}
	for _, pool := range config.AppConfig.StoragePools {
		if pool.Enabled && pool.Type != "" && pool.Type != "local" && wanted(pool) {
			return pool, true
		}
	}
	return config.StoragePool{}, false
}

// DeleteSnapshotFromRemoteStorage removes the remote copies of a snapshot that
// was just deleted locally, trying every path it could occupy. Best effort by
// design: the local snapshot is already gone, so a failure here is logged
// rather than turned into an error — it only means an orphan is left to clean.
func DeleteSnapshotFromRemoteStorage(snap *config.Snapshot) {
	candidates := SnapshotRemoteCandidates(snap)
	if len(candidates) == 0 {
		return
	}
	pool, ok := remotePoolForSnapshot(snap)
	if !ok {
		return
	}
	client, err := NewClient(pool.Type, pool.Config)
	if err != nil {
		fmt.Printf("Warning: cannot reach remote pool %s to delete snapshot %s: %v\n", pool.Name, snap.ID, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	for _, candidate := range candidates {
		removed, err := client.DeletePath(ctx, candidate)
		if err != nil {
			fmt.Printf("Warning: failed to delete remote snapshot %s at %s: %v\n", snap.ID, candidate, err)
			continue
		}
		if !removed {
			continue
		}
		fmt.Printf("Deleted remote snapshot %s from %s (%s)\n", snap.ID, pool.Name, candidate)
		pruneRemoteParents(ctx, client, candidate)
	}
}

// DeleteBackupFromRemoteStorage is the backup counterpart of
// DeleteSnapshotFromRemoteStorage.
func DeleteBackupFromRemoteStorage(bkp *config.Backup) {
	candidates := BackupRemoteCandidates(bkp)
	if len(candidates) == 0 {
		return
	}
	pool, ok := remotePoolForBackup(bkp)
	if !ok {
		return
	}
	client, err := NewClient(pool.Type, pool.Config)
	if err != nil {
		fmt.Printf("Warning: cannot reach remote pool %s to delete backup %s: %v\n", pool.Name, bkp.ID, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	for _, candidate := range candidates {
		removed, err := client.DeletePath(ctx, candidate)
		if err != nil {
			fmt.Printf("Warning: failed to delete remote backup %s at %s: %v\n", bkp.ID, candidate, err)
			continue
		}
		if !removed {
			continue
		}
		fmt.Printf("Deleted remote backup %s from %s (%s)\n", bkp.ID, pool.Name, candidate)
		pruneRemoteParents(ctx, client, candidate)
	}
}

// pruneRemoteParents drops the instance and host directories once their last
// snapshot is gone, so the pool does not fill up with empty shells.
func pruneRemoteParents(ctx context.Context, client StorageClient, candidate string) {
	pruner, ok := client.(emptyDirPruner)
	if !ok {
		return
	}
	dir := path.Dir(candidate)
	for depth := 0; depth < 2 && dir != "." && dir != "/" && dir != ""; depth++ {
		if err := pruner.PruneEmptyDir(ctx, dir); err != nil {
			return
		}
		fmt.Printf("Removed empty remote directory %s\n", dir)
		dir = path.Dir(dir)
	}
}

// SyncSnapshotToRemoteStorage uploads a newly created snapshot to configured remote storage pools.
func SyncSnapshotToRemoteStorage(snapshot *config.Snapshot) {
	if snapshot == nil || snapshot.Path == "" {
		return
	}
	for _, pool := range config.AppConfig.StoragePools {
		if !pool.Enabled || !pool.SyncSnapshots || pool.Type == "local" || pool.Type == "" {
			continue
		}
		go func(p config.StoragePool, snap config.Snapshot) {
			_ = SyncSingleSnapshotToPool(&snap, &p)
		}(pool, *snapshot)
	}
}

// SyncSingleSnapshotToPool syncs a snapshot to a specific pool synchronously and updates its config state.
func SyncSingleSnapshotToPool(snap *config.Snapshot, pool *config.StoragePool) error {
	if snap == nil || pool == nil {
		return fmt.Errorf("snapshot or pool is nil")
	}

	releaseLock := acquireSyncLock(snap.ID)
	defer releaseLock()

	client, err := NewClient(pool.Type, pool.Config)
	if err != nil {
		return fmt.Errorf("remote storage sync client init error for %s: %w", pool.Name, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), snapshotTransferTimeout)
	defer cancel()

	writeSnapshotMeta(snap.Path, snap)
	totalSize := calculateDirSize(snap.Path)
	var transferred int64
	startTime := time.Now()

	SetProgress(SyncProgress{
		ID: snap.ID,
		Type: "snapshot_upload",
		Stage: "transferring",
		TotalBytes: totalSize,
		Percent: 0,
	})

	remoteBasePath := remoteSnapshotBasePath(snap)
	err = filepath.Walk(snap.Path, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return walkErr
		}
		relPath, _ := filepath.Rel(snap.Path, path)
		remoteFile := filepath.ToSlash(filepath.Join(remoteBasePath, relPath))

		SetProgress(SyncProgress{
			ID: snap.ID,
			Type: "snapshot_upload",
			Stage: "transferring",
			CurrentFile: relPath,
			TransferredBytes: transferred,
			TotalBytes: totalSize,
			Percent: int(float64(transferred) / float64(maxInt64(1, totalSize)) * 100),
		})

		if uploadErr := client.UploadFile(ctx, path, remoteFile); uploadErr != nil {
			return uploadErr
		}
		transferred += info.Size()

		elapsed := time.Since(startTime).Seconds()
		var speed int64
		if elapsed > 0 {
			speed = int64(float64(transferred) / elapsed)
		}

		SetProgress(SyncProgress{
			ID: snap.ID,
			Type: "snapshot_upload",
			Stage: "transferring",
			CurrentFile: relPath,
			TransferredBytes: transferred,
			TotalBytes: totalSize,
			Percent: int(float64(transferred) / float64(maxInt64(1, totalSize)) * 100),
			SpeedBps: speed,
		})
		return nil
	})

	if err == nil {
		if s := config.FindSnapshot(snap.ID); s != nil {
			s.RemoteSynced = true
			s.RemoteStoragePoolID = pool.ID
			s.RemotePath = remoteBasePath
			_ = config.SaveConfig()
		}
		SetProgress(SyncProgress{
			ID: snap.ID,
			Type: "snapshot_upload",
			Stage: "completed",
			TransferredBytes: totalSize,
			TotalBytes: totalSize,
			Percent: 100,
		})
		fmt.Printf("Successfully synced snapshot %s to remote storage %s\n", snap.ID, pool.Name)
		return nil
	}

	SetProgress(SyncProgress{
		ID: snap.ID,
		Type: "snapshot_upload",
		Stage: "failed",
		Error: err.Error(),
		TotalBytes: totalSize,
	})
	fmt.Printf("Failed to sync snapshot %s to %s: %v\n", snap.ID, pool.Name, err)
	return err
}

// SyncBackupToRemoteStorage uploads a newly created backup to configured remote storage pools.
func SyncBackupToRemoteStorage(backup *config.Backup) {
	if backup == nil || backup.Path == "" {
		return
	}
	for _, pool := range config.AppConfig.StoragePools {
		if !pool.Enabled || !pool.SyncBackups || pool.Type == "local" || pool.Type == "" {
			continue
		}
		go func(p config.StoragePool, bkp config.Backup) {
			_ = SyncSingleBackupToPool(&bkp, &p)
		}(pool, *backup)
	}
}

// SyncSingleBackupToPool syncs a backup to a specific pool synchronously and updates its config state.
func SyncSingleBackupToPool(bkp *config.Backup, pool *config.StoragePool) error {
	if bkp == nil || pool == nil {
		return fmt.Errorf("backup or pool is nil")
	}

	releaseLock := acquireSyncLock(bkp.ID)
	defer releaseLock()

	client, err := NewClient(pool.Type, pool.Config)
	if err != nil {
		return fmt.Errorf("remote storage sync client init error for %s: %w", pool.Name, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), backupTransferTimeout)
	defer cancel()

	writeBackupMeta(bkp.Path, bkp)
	totalSize := calculateDirSize(bkp.Path)
	if totalSize <= 0 && bkp.SizeBytes > 0 {
		totalSize = bkp.SizeBytes
	}
	var transferred int64
	startTime := time.Now()

	SetProgress(SyncProgress{
		ID: bkp.ID,
		Type: "backup_upload",
		Stage: "transferring",
		TotalBytes: totalSize,
		Percent: 0,
	})

	remoteBasePath := remoteBackupBasePath(bkp)
	err = filepath.Walk(bkp.Path, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return walkErr
		}
		relPath, _ := filepath.Rel(bkp.Path, path)
		remoteFile := filepath.ToSlash(filepath.Join(remoteBasePath, relPath))

		SetProgress(SyncProgress{
			ID: bkp.ID,
			Type: "backup_upload",
			Stage: "transferring",
			CurrentFile: relPath,
			TransferredBytes: transferred,
			TotalBytes: totalSize,
			Percent: int(float64(transferred) / float64(maxInt64(1, totalSize)) * 100),
		})

		if uploadErr := client.UploadFile(ctx, path, remoteFile); uploadErr != nil {
			return uploadErr
		}
		transferred += info.Size()

		elapsed := time.Since(startTime).Seconds()
		var speed int64
		if elapsed > 0 {
			speed = int64(float64(transferred) / elapsed)
		}

		SetProgress(SyncProgress{
			ID: bkp.ID,
			Type: "backup_upload",
			Stage: "transferring",
			CurrentFile: relPath,
			TransferredBytes: transferred,
			TotalBytes: totalSize,
			Percent: int(float64(transferred) / float64(maxInt64(1, totalSize)) * 100),
			SpeedBps: speed,
		})
		return nil
	})

	if err == nil {
		if b := config.FindBackup(bkp.ID); b != nil {
			b.RemoteSynced = true
			b.RemoteStoragePoolID = pool.ID
			b.RemotePath = remoteBasePath
			_ = config.SaveConfig()
		}
		SetProgress(SyncProgress{
			ID: bkp.ID,
			Type: "backup_upload",
			Stage: "completed",
			TransferredBytes: totalSize,
			TotalBytes: totalSize,
			Percent: 100,
		})
		fmt.Printf("Successfully synced backup %s to remote storage %s\n", bkp.ID, pool.Name)
		return nil
	}

	SetProgress(SyncProgress{
		ID: bkp.ID,
		Type: "backup_upload",
		Stage: "failed",
		Error: err.Error(),
		TotalBytes: totalSize,
	})
	fmt.Printf("Failed to sync backup %s to %s: %v\n", bkp.ID, pool.Name, err)
	return err
}

// EnsureLocalSnapshotFromRemote downloads remote snapshot files if local copy is missing or remote source is requested.
func EnsureLocalSnapshotFromRemote(snapshot *config.Snapshot) error {
	if snapshot == nil {
		return fmt.Errorf("snapshot is nil")
	}

	releaseLock := acquireSyncLock(snapshot.ID)
	defer releaseLock()

	if snapshot.RemoteStoragePoolID == "" || snapshot.RemotePath == "" {
		return fmt.Errorf("no remote storage record for snapshot %s", snapshot.ID)
	}
	var pool *config.StoragePool
	for _, p := range config.AppConfig.StoragePools {
		if p.ID == snapshot.RemoteStoragePoolID {
			pool = &p
			break
		}
	}
	if pool == nil {
		return fmt.Errorf("remote storage pool %s not found", snapshot.RemoteStoragePoolID)
	}
	client, err := NewClient(pool.Type, pool.Config)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), snapshotTransferTimeout)
	defer cancel()

	_ = os.MkdirAll(snapshot.Path, 0700)
	files := []string{"disk.qcow2", "domain.xml", "config", "rootfs.tar.gz"}
	totalFiles := len(files)

	SetProgress(SyncProgress{
		ID: snapshot.ID,
		Type: "snapshot_download",
		Stage: "transferring",
		Percent: 10,
	})

		// Download standard files. Optional files may fail if absent remotely,
		// but core disk images must succeed.
		var criticalErr error
		for idx, filename := range files {
			remoteFile := filepath.ToSlash(filepath.Join(snapshot.RemotePath, filename))
			localFile := filepath.Join(snapshot.Path, filename)

			SetProgress(SyncProgress{
				ID: snapshot.ID,
				Type: "snapshot_download",
				Stage: "transferring",
				CurrentFile: filename,
				Percent: int(float64(idx+1) / float64(totalFiles) * 90),
			})
			if err := client.DownloadFile(ctx, remoteFile, localFile); err != nil {
				_ = os.Remove(localFile)
				if filename == "disk.qcow2" || filename == "rootfs.tar.gz" {
					criticalErr = fmt.Errorf("download critical file %s failed: %w", filename, err)
				}
			}
		}

		if criticalErr != nil {
			SetProgress(SyncProgress{
				ID: snapshot.ID,
				Type: "snapshot_download",
				Stage: "failed",
				Error: criticalErr.Error(),
				Percent: 100,
			})
			return criticalErr
		}

		SetProgress(SyncProgress{
			ID: snapshot.ID,
			Type: "snapshot_download",
			Stage: "completed",
			Percent: 100,
		})
		return nil
	}

	// EnsureLocalBackupFromRemote downloads remote backup files if local copy is missing or remote source is requested.
	func EnsureLocalBackupFromRemote(backup *config.Backup) error {
		if backup == nil {
			return fmt.Errorf("backup is nil")
		}

		releaseLock := acquireSyncLock(backup.ID)
		defer releaseLock()

		if backup.RemoteStoragePoolID == "" || backup.RemotePath == "" {
			return fmt.Errorf("no remote storage record for backup %s", backup.ID)
		}
		var pool *config.StoragePool
		for _, p := range config.AppConfig.StoragePools {
			if p.ID == backup.RemoteStoragePoolID {
				pool = &p
				break
			}
		}
		if pool == nil {
			return fmt.Errorf("remote storage pool %s not found", backup.RemoteStoragePoolID)
		}
		client, err := NewClient(pool.Type, pool.Config)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), backupTransferTimeout)
		defer cancel()

		_ = os.MkdirAll(backup.Path, 0700)
		files := []string{"disk.qcow2", "domain.xml", "unattend.iso", "seed.iso"}
		totalFiles := len(files)

		SetProgress(SyncProgress{
			ID: backup.ID,
			Type: "backup_download",
			Stage: "transferring",
			Percent: 10,
		})

		var criticalErr error
		for idx, filename := range files {
			remoteFile := filepath.ToSlash(filepath.Join(backup.RemotePath, filename))
			localFile := filepath.Join(backup.Path, filename)

			SetProgress(SyncProgress{
				ID: backup.ID,
				Type: "backup_download",
				Stage: "transferring",
				CurrentFile: filename,
				Percent: int(float64(idx+1) / float64(totalFiles) * 90),
			})
			if err := client.DownloadFile(ctx, remoteFile, localFile); err != nil {
				_ = os.Remove(localFile)
				if filename == "disk.qcow2" {
					criticalErr = fmt.Errorf("download critical file %s failed: %w", filename, err)
				}
			}
		}

		if criticalErr != nil {
			SetProgress(SyncProgress{
				ID: backup.ID,
				Type: "backup_download",
				Stage: "failed",
				Error: criticalErr.Error(),
				Percent: 100,
			})
			return criticalErr
		}

		SetProgress(SyncProgress{
			ID: backup.ID,
			Type: "backup_download",
			Stage: "completed",
			Percent: 100,
		})
		return nil
	}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
