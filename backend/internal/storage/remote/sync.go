package remote

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"clicd/internal/config"
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

// uploadWithRetry retries transient upload failures with a short linear backoff;
// the shared sync context bounds the total time.
func uploadWithRetry(ctx context.Context, client StorageClient, localPath, remotePath string, attempts int) error {
	var lastErr error
	for i := 0; i < attempts; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := client.UploadFile(ctx, localPath, remotePath); err == nil {
			return nil
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(i+1) * 5 * time.Second):
		}
	}
	return lastErr
}

// sha256File computes the hex SHA256 of a file with a streaming 1MB buffer.
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	buf := make([]byte, 1024*1024)
	if _, err := io.CopyBuffer(h, f, buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// downloadRemoteFile fetches one file and verifies critical disk images.
// A failed download removes any partial local file so a restore never uses
// truncated data; disk.qcow2 additionally verifies the recorded checksum.
func downloadRemoteFile(ctx context.Context, client StorageClient, remoteFile, localFile, filename, checksum string) error {
	err := client.DownloadFile(ctx, remoteFile, localFile)
	if err != nil {
		_ = os.Remove(localFile)
		return fmt.Errorf("download %s failed: %w", filename, err)
	}
	if filename == "disk.qcow2" && checksum != "" {
		sum, sumErr := sha256File(localFile)
		if sumErr != nil {
			_ = os.Remove(localFile)
			return fmt.Errorf("verify %s failed: %w", filename, sumErr)
		}
		if sum != checksum {
			_ = os.Remove(localFile)
			return fmt.Errorf("checksum mismatch for %s: remote copy is corrupted", filename)
		}
	}
	return nil
}

// DirUploader is an optional StorageClient capability for implementations that
// can upload a whole directory tree in one transport (e.g. tar over SSH). Trees
// with many small files (LXC rootfs) would otherwise need one connection per
// file, which is orders of magnitude slower.
type DirUploader interface {
	UploadDir(ctx context.Context, localDir, remotePath string) error
}

// DeleteDir is an optional StorageClient capability that removes a remote
// directory tree in one operation (SFTP rm -rf, WebDAV collection DELETE).
// Object stores without directory semantics must use per-file deletion.
type DeleteDir interface {
	DeleteDir(ctx context.Context, remotePath string) error
}

// knownRemoteCopyFiles is the fallback file enumeration for remote deletion when
// the local snapshot/backup directory is already gone.
var knownRemoteCopyFiles = []string{
	"disk.qcow2", "domain.xml", "meta-data", "user-data", "network-config",
	"seed.iso", "unattend.iso", "config", "rootfs.tar.gz",
}

// localFileNames enumerates file names under localDir (flat copy layout).
func localFileNames(localDir string) []string {
	if localDir == "" {
		return nil
	}
	names := []string{}
	_ = filepath.Walk(localDir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			if rel, relErr := filepath.Rel(localDir, p); relErr == nil {
				names = append(names, filepath.ToSlash(rel))
			}
		}
		return nil
	})
	return names
}

// remoteRecordMu serializes snapshot/backup record mutations performed by
// concurrent per-pool sync goroutines.
var remoteRecordMu sync.Mutex

// recordRemoteCopy persists a successful offsite copy. The legacy single-value
// fields stay in sync (last successful writer) for backward compatibility.
func recordRemoteCopy(kind, id, poolID, remotePath string) {
	remoteRecordMu.Lock()
	defer remoteRecordMu.Unlock()
	switch kind {
	case "snapshot":
		if s := config.FindSnapshot(id); s != nil {
			s.RemoteSynced = true
			s.RemoteStoragePoolID = poolID
			s.RemotePath = remotePath
			upsertRemoteCopy(&s.RemoteCopies, poolID, remotePath)
			_ = config.SaveConfig()
		}
	case "backup":
		if b := config.FindBackup(id); b != nil {
			b.RemoteSynced = true
			b.RemoteStoragePoolID = poolID
			b.RemotePath = remotePath
			upsertRemoteCopy(&b.RemoteCopies, poolID, remotePath)
			_ = config.SaveConfig()
		}
	}
}

func upsertRemoteCopy(copies *[]config.RemoteCopy, poolID, remotePath string) {
	for i := range *copies {
		if (*copies)[i].PoolID == poolID {
			(*copies)[i].RemotePath = remotePath
			return
		}
	}
	*copies = append(*copies, config.RemoteCopy{PoolID: poolID, RemotePath: remotePath})
}

// remoteCopyTargets returns the unique (poolID, remotePath) pairs holding a
// copy of an item, merging the multi-copy list with the legacy single fields.
func remoteCopyTargets(copies []config.RemoteCopy, legacyPoolID, legacyPath string) [][2]string {
	seen := map[string]bool{}
	targets := [][2]string{}
	add := func(poolID, remotePath string) {
		poolID = strings.TrimSpace(poolID)
		remotePath = strings.TrimSpace(remotePath)
		if poolID == "" || remotePath == "" || seen[poolID] {
			return
		}
		seen[poolID] = true
		targets = append(targets, [2]string{poolID, remotePath})
	}
	for _, rc := range copies {
		add(rc.PoolID, rc.RemotePath)
	}
	add(legacyPoolID, legacyPath)
	return targets
}

// DeleteSnapshotFromRemoteStorage deletes the remote copy of a snapshot from its
// recorded remote storage pool. It should be called BEFORE the local files are
// removed, so the remote file list can be enumerated from the local directory.
func DeleteSnapshotFromRemoteStorage(snapshot *config.Snapshot) error {
	if snapshot == nil {
		return fmt.Errorf("snapshot is nil")
	}
	return deleteAllRemoteCopies(snapshot.Path, "snapshot", snapshot.ID,
		remoteCopyTargets(snapshot.RemoteCopies, snapshot.RemoteStoragePoolID, snapshot.RemotePath))
}

// DeleteBackupFromRemoteStorage deletes the remote copy of a backup from its
// recorded remote storage pool.
func DeleteBackupFromRemoteStorage(backup *config.Backup) error {
	if backup == nil {
		return fmt.Errorf("backup is nil")
	}
	return deleteAllRemoteCopies(backup.Path, "backup", backup.ID,
		remoteCopyTargets(backup.RemoteCopies, backup.RemoteStoragePoolID, backup.RemotePath))
}

// deleteAllRemoteCopies deletes the item from every recorded remote pool.
// All pools are attempted even when one fails; an error is returned when no
// target existed or every deletion failed.
func deleteAllRemoteCopies(localDir, kind, id string, targets [][2]string) error {
	if len(targets) == 0 {
		return fmt.Errorf("no remote storage record for %s %s", kind, id)
	}
	var firstErr error
	deleted := 0
	for _, target := range targets {
		if err := deleteRemoteCopy(localDir, target[0], target[1], kind, id); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		deleted++
	}
	if deleted == 0 {
		return firstErr
	}
	if firstErr != nil {
		return fmt.Errorf("deleted %d remote %s copies but some failed: %w", deleted, kind, firstErr)
	}
	return nil
}

func deleteRemoteCopy(localDir, poolID, remoteBasePath, kind, id string) error {
	if poolID == "" || remoteBasePath == "" {
		return fmt.Errorf("no remote storage record for %s %s", kind, id)
	}
	var pool *config.StoragePool
	for _, p := range config.AppConfig.StoragePools {
		if p.ID == poolID {
			pool = &p
			break
		}
	}
	if pool == nil {
		return fmt.Errorf("remote storage pool %s not found", poolID)
	}
	client, err := NewClient(pool.Type, pool.Config)
	if err != nil {
		return fmt.Errorf("remote client init error for %s: %w", pool.Name, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Prefer a one-shot tree delete (single ssh session / collection DELETE /
	// Drive folder delete): per-file deletion dials a new connection per file
	// and would take hours for LXC rootfs trees.
	if dd, ok := client.(DeleteDir); ok {
		if err := dd.DeleteDir(ctx, remoteBasePath); err != nil {
			fmt.Printf("Failed to delete remote %s copy %s on pool %s: %v\n", kind, id, pool.Name, err)
			return fmt.Errorf("delete %s failed: %w", remoteBasePath, err)
		}
		fmt.Printf("Deleted remote %s copy %s on pool %s\n", kind, id, pool.Name)
		return nil
	}

	names := localFileNames(localDir)
	if len(names) == 0 {
		names = knownRemoteCopyFiles
	}
	remoteBasePath = strings.Trim(remoteBasePath, "/")
	var firstErr error
	for _, name := range names {
		remoteFile := remoteBasePath + "/" + name
		if err := client.DeleteFile(ctx, remoteFile); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("delete %s failed: %w", remoteFile, err)
		}
	}
	// Remove the remote directory itself: SFTP uses rm -rf, WebDAV deletes the
	// collection; for object stores this is a harmless no-op on a "directory" key.
	if err := client.DeleteFile(ctx, remoteBasePath); err != nil && firstErr == nil {
		firstErr = fmt.Errorf("delete %s failed: %w", remoteBasePath, err)
	}
	if firstErr != nil {
		fmt.Printf("Failed to delete remote %s copy %s on pool %s: %v\n", kind, id, pool.Name, firstErr)
		return firstErr
	}
	fmt.Printf("Deleted remote %s copy %s on pool %s\n", kind, id, pool.Name)
	return nil
}

// TrackDirCopyProgress polls the destination directory size every 1.5s and
// reports copy progress for snapshot/backup creation operations until done is
// closed. Callers report the final "completed"/"failed" stage themselves.
func TrackDirCopyProgress(progressID, progressType, dstDir string, totalBytes int64, done <-chan struct{}) {
	ticker := time.NewTicker(1500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			used := calculateDirSize(dstDir)
			pct := 0
			if totalBytes > 0 {
				pct = int(float64(used) / float64(totalBytes) * 100)
				if pct > 99 {
					pct = 99
				}
			}
			SetProgress(SyncProgress{
				ID:               progressID,
				Type:             progressType,
				Stage:            "transferring",
				CurrentFile:      dstDir,
				TransferredBytes: used,
				TotalBytes:       totalBytes,
				Percent:          pct,
			})
		}
	}
}

// SetCreateProgress is a small helper for snapshot/backup creation flows to
// report lifecycle stages under the shared progress store.
func SetCreateProgress(progressID, progressType, stage, currentFile string, percent, transferred, total int64) {
	SetProgress(SyncProgress{
		ID:               progressID,
		Type:             progressType,
		Stage:            stage,
		CurrentFile:      currentFile,
		Percent:          int(percent),
		TransferredBytes: transferred,
		TotalBytes:       total,
	})
}

// snapDirHasTree reports whether the local snapshot/backup directory contains
// subdirectories (i.e. a container rootfs tree), which should be transported
// via DirUploader when available instead of per-file uploads.
func snapDirHasTree(localDir string) bool {
	found := false
	_ = filepath.Walk(localDir, func(p string, info os.FileInfo, err error) error {
		if err == nil && info.IsDir() && p != localDir {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// uploadWalk uploads every regular file under localDir to remoteBasePath with
// per-file progress updates. Symlinks are never dereferenced.
func uploadWalk(ctx context.Context, client StorageClient, localDir, remoteBasePath, progressID, progressType string, totalSize int64, transferred *int64, startTime time.Time) error {
	return filepath.Walk(localDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return walkErr
		}
		// Never dereference symlinks (e.g. rootfs/bin -> usr/bin in LXC rootfs):
		// following them either fails with "is a directory" or silently uploads
		// whole duplicated trees to the remote storage.
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		relPath, _ := filepath.Rel(localDir, path)
		remoteFile := filepath.ToSlash(filepath.Join(remoteBasePath, relPath))

		SetProgress(SyncProgress{
			ID:               progressID,
			Type:             progressType,
			Stage:            "transferring",
			CurrentFile:      relPath,
			TransferredBytes: *transferred,
			TotalBytes:       totalSize,
			Percent:          int(float64(*transferred) / float64(maxInt64(1, totalSize)) * 100),
		})

		if uploadErr := uploadWithRetry(ctx, client, path, remoteFile, 3); uploadErr != nil {
			return uploadErr
		}
		*transferred += info.Size()

		elapsed := time.Since(startTime).Seconds()
		var speed int64
		if elapsed > 0 {
			speed = int64(float64(*transferred) / elapsed)
		}

		SetProgress(SyncProgress{
			ID:               progressID,
			Type:             progressType,
			Stage:            "transferring",
			CurrentFile:      relPath,
			TransferredBytes: *transferred,
			TotalBytes:       totalSize,
			Percent:          int(float64(*transferred) / float64(maxInt64(1, totalSize)) * 100),
			SpeedBps:         speed,
		})
		return nil
	})
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

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

	remoteBasePath := fmt.Sprintf("snapshots/%d/%s", snap.ContainerID, snap.ID)
	if du, ok := client.(DirUploader); ok && snapDirHasTree(snap.Path) {
		SetProgress(SyncProgress{
			ID: snap.ID, Type: "snapshot_upload", Stage: "transferring",
			CurrentFile: remoteBasePath + "/ (tree)", TotalBytes: totalSize, Percent: 0,
		})
		if err = du.UploadDir(ctx, snap.Path, remoteBasePath); err == nil {
			transferred = totalSize
		}
	} else {
		err = uploadWalk(ctx, client, snap.Path, remoteBasePath, snap.ID, "snapshot_upload", totalSize, &transferred, startTime)
	}

	if err == nil {
		recordRemoteCopy("snapshot", snap.ID, pool.ID, remoteBasePath)
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
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()

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

	remoteBasePath := fmt.Sprintf("backups/%d/%s", bkp.ContainerID, bkp.ID)
	err = uploadWalk(ctx, client, bkp.Path, remoteBasePath, bkp.ID, "backup_upload", totalSize, &transferred, startTime)

	if err == nil {
		recordRemoteCopy("backup", bkp.ID, pool.ID, remoteBasePath)
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
// Multiple recorded copies are tried in order; the first pool that serves a
// complete disk image wins.
func EnsureLocalSnapshotFromRemote(snapshot *config.Snapshot) error {
	if snapshot == nil {
		return fmt.Errorf("snapshot is nil")
	}

	releaseLock := acquireSyncLock(snapshot.ID)
	defer releaseLock()

	targets := remoteCopyTargets(snapshot.RemoteCopies, snapshot.RemoteStoragePoolID, snapshot.RemotePath)
	if len(targets) == 0 {
		return fmt.Errorf("no remote storage record for snapshot %s", snapshot.ID)
	}

	SetProgress(SyncProgress{
		ID: snapshot.ID,
		Type: "snapshot_download",
		Stage: "transferring",
		Percent: 10,
	})

	var firstErr error
	for _, target := range targets {
		pool, client, err := clientForPool(target[0])
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		err = downloadSnapshotFilesFromPool(ctx, client, pool, snapshot, target[1])
		cancel()
		if err == nil {
			SetProgress(SyncProgress{
				ID: snapshot.ID,
				Type: "snapshot_download",
				Stage: "completed",
				Percent: 100,
			})
			return nil
		}
		if firstErr == nil {
			firstErr = err
		}
		fmt.Printf("Snapshot %s restore from pool %s failed, trying next copy: %v\n", snapshot.ID, pool.Name, err)
	}

	SetProgress(SyncProgress{
		ID: snapshot.ID,
		Type: "snapshot_download",
		Stage: "failed",
		Percent: 100,
		Error: func() string {
			if firstErr != nil {
				return firstErr.Error()
			}
			return ""
		}(),
	})
	if firstErr == nil {
		firstErr = fmt.Errorf("no remote copy could serve snapshot %s", snapshot.ID)
	}
	return firstErr
}

func clientForPool(poolID string) (*config.StoragePool, StorageClient, error) {
	var pool *config.StoragePool
	for _, p := range config.AppConfig.StoragePools {
		if p.ID == poolID {
			pool = &p
			break
		}
	}
	if pool == nil {
		return nil, nil, fmt.Errorf("remote storage pool %s not found", poolID)
	}
	client, err := NewClient(pool.Type, pool.Config)
	if err != nil {
		return nil, nil, fmt.Errorf("remote client init error for %s: %w", pool.Name, err)
	}
	return pool, client, nil
}

func downloadSnapshotFilesFromPool(ctx context.Context, client StorageClient, pool *config.StoragePool, snapshot *config.Snapshot, remotePath string) error {
	_ = os.MkdirAll(snapshot.Path, 0700)
	files := []string{"disk.qcow2", "domain.xml", "config", "rootfs.tar.gz"}
	totalFiles := len(files)

	// Download standard files. Metadata files are optional (may not exist remotely),
	// but the disk image must succeed.
	var criticalErr error
	for idx, filename := range files {
		remoteFile := filepath.ToSlash(filepath.Join(remotePath, filename))
		localFile := filepath.Join(snapshot.Path, filename)

		SetProgress(SyncProgress{
			ID: snapshot.ID,
			Type: "snapshot_download",
			Stage: "transferring",
			CurrentFile: filename,
			Percent: int(float64(idx+1) / float64(totalFiles) * 90),
		})
		if err := downloadRemoteFile(ctx, client, remoteFile, localFile, filename, ""); err != nil {
			if filename == "disk.qcow2" {
				criticalErr = err
			}
			continue
		}
	}
	if criticalErr != nil {
		fmt.Printf("Failed to restore snapshot %s from remote storage %s: %v\n", snapshot.ID, pool.Name, criticalErr)
		return criticalErr
	}
	return nil
}

// EnsureLocalBackupFromRemote downloads remote backup files if local copy is missing or remote source is requested.
// Multiple recorded copies are tried in order; the first pool that serves a
// complete, checksum-verified disk image wins.
func EnsureLocalBackupFromRemote(backup *config.Backup) error {
	if backup == nil {
		return fmt.Errorf("backup is nil")
	}

	releaseLock := acquireSyncLock(backup.ID)
	defer releaseLock()

	targets := remoteCopyTargets(backup.RemoteCopies, backup.RemoteStoragePoolID, backup.RemotePath)
	if len(targets) == 0 {
		return fmt.Errorf("no remote storage record for backup %s", backup.ID)
	}

	SetProgress(SyncProgress{
		ID: backup.ID,
		Type: "backup_download",
		Stage: "transferring",
		Percent: 10,
	})

	var firstErr error
	for _, target := range targets {
		pool, client, err := clientForPool(target[0])
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
		err = downloadBackupFilesFromPool(ctx, client, pool, backup, target[1])
		cancel()
		if err == nil {
			SetProgress(SyncProgress{
				ID: backup.ID,
				Type: "backup_download",
				Stage: "completed",
				Percent: 100,
			})
			return nil
		}
		if firstErr == nil {
			firstErr = err
		}
		fmt.Printf("Backup %s restore from pool %s failed, trying next copy: %v\n", backup.ID, pool.Name, err)
	}

	SetProgress(SyncProgress{
		ID: backup.ID,
		Type: "backup_download",
		Stage: "failed",
		Percent: 100,
		Error: func() string {
			if firstErr != nil {
				return firstErr.Error()
			}
			return ""
		}(),
	})
	if firstErr == nil {
		firstErr = fmt.Errorf("no remote copy could serve backup %s", backup.ID)
	}
	return firstErr
}

func downloadBackupFilesFromPool(ctx context.Context, client StorageClient, pool *config.StoragePool, backup *config.Backup, remotePath string) error {
	_ = os.MkdirAll(backup.Path, 0700)
	files := []string{"disk.qcow2", "domain.xml", "unattend.iso", "seed.iso"}
	totalFiles := len(files)

	// Download standard files. Metadata files are optional (may not exist remotely),
	// but the disk image must succeed and match the recorded checksum.
	var criticalErr error
	for idx, filename := range files {
		remoteFile := filepath.ToSlash(filepath.Join(remotePath, filename))
		localFile := filepath.Join(backup.Path, filename)

		SetProgress(SyncProgress{
			ID: backup.ID,
			Type: "backup_download",
			Stage: "transferring",
			CurrentFile: filename,
			Percent: int(float64(idx+1) / float64(totalFiles) * 90),
		})
		if err := downloadRemoteFile(ctx, client, remoteFile, localFile, filename, backup.Checksum); err != nil {
			if filename == "disk.qcow2" {
				criticalErr = err
			}
			continue
		}
	}
	if criticalErr != nil {
		fmt.Printf("Failed to restore backup %s from remote storage %s: %v\n", backup.ID, pool.Name, criticalErr)
		return criticalErr
	}
	return nil
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
