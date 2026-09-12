package remote

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
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

		if uploadErr := uploadWithRetry(ctx, client, path, remoteFile, 3); uploadErr != nil {
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

		if uploadErr := uploadWithRetry(ctx, client, path, remoteFile, 3); uploadErr != nil {
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
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

	// Download standard files. Metadata files are optional (may not exist remotely),
	// but the disk image must succeed and, for backups, match the recorded checksum.
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
		if err := downloadRemoteFile(ctx, client, remoteFile, localFile, filename, ""); err != nil {
			if filename == "disk.qcow2" {
				criticalErr = err
			}
			continue
		}
	}

	SetProgress(SyncProgress{
		ID: snapshot.ID,
		Type: "snapshot_download",
		Stage: func() string {
			if criticalErr != nil {
				return "failed"
			}
			return "completed"
		}(),
		Percent: 100,
		Error: func() string {
			if criticalErr != nil {
				return criticalErr.Error()
			}
			return ""
		}(),
	})
	if criticalErr != nil {
		fmt.Printf("Failed to restore snapshot %s from remote storage %s: %v\n", snapshot.ID, pool.Name, criticalErr)
		return criticalErr
	}
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
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
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

	// Download standard files. Metadata files are optional (may not exist remotely),
	// but the disk image must succeed and match the recorded checksum.
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
		if err := downloadRemoteFile(ctx, client, remoteFile, localFile, filename, backup.Checksum); err != nil {
			if filename == "disk.qcow2" {
				criticalErr = err
			}
			continue
		}
	}

	SetProgress(SyncProgress{
		ID: backup.ID,
		Type: "backup_download",
		Stage: func() string {
			if criticalErr != nil {
				return "failed"
			}
			return "completed"
		}(),
		Percent: 100,
		Error: func() string {
			if criticalErr != nil {
				return criticalErr.Error()
			}
			return ""
		}(),
	})
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
