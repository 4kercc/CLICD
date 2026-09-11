package remote

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"clicd/internal/config"
)

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
			client, err := NewClient(p.Type, p.Config)
			if err != nil {
				fmt.Printf("Remote storage sync client init error for %s: %v\n", p.Name, err)
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()

			remoteBasePath := fmt.Sprintf("snapshots/%d/%s", snap.ContainerID, snap.ID)
			// Walk directory and upload files
			err = filepath.Walk(snap.Path, func(path string, info os.FileInfo, walkErr error) error {
				if walkErr != nil || info.IsDir() {
					return walkErr
				}
				relPath, _ := filepath.Rel(snap.Path, path)
				remoteFile := filepath.ToSlash(filepath.Join(remoteBasePath, relPath))
				return client.UploadFile(ctx, path, remoteFile)
			})

			if err == nil {
				// Mark as synced in config
				if s := config.FindSnapshot(snap.ID); s != nil {
					s.RemoteSynced = true
					s.RemoteStoragePoolID = p.ID
					s.RemotePath = remoteBasePath
					_ = config.SaveConfig()
				}
				fmt.Printf("Successfully synced snapshot %s to remote storage %s\n", snap.ID, p.Name)
			} else {
				fmt.Printf("Failed to sync snapshot %s to %s: %v\n", snap.ID, p.Name, err)
			}
		}(pool, *snapshot)
	}
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
			client, err := NewClient(p.Type, p.Config)
			if err != nil {
				fmt.Printf("Remote storage sync client init error for %s: %v\n", p.Name, err)
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
			defer cancel()

			remoteBasePath := fmt.Sprintf("backups/%d/%s", bkp.ContainerID, bkp.ID)
			// Walk directory and upload all files (xml, disk.qcow2, etc.)
			err = filepath.Walk(bkp.Path, func(path string, info os.FileInfo, walkErr error) error {
				if walkErr != nil || info.IsDir() {
					return walkErr
				}
				relPath, _ := filepath.Rel(bkp.Path, path)
				remoteFile := filepath.ToSlash(filepath.Join(remoteBasePath, relPath))
				return client.UploadFile(ctx, path, remoteFile)
			})

			if err == nil {
				if b := config.FindBackup(bkp.ID); b != nil {
					b.RemoteSynced = true
					b.RemoteStoragePoolID = p.ID
					b.RemotePath = remoteBasePath
					_ = config.SaveConfig()
				}
				fmt.Printf("Successfully synced backup %s to remote storage %s\n", bkp.ID, p.Name)
			} else {
				fmt.Printf("Failed to sync backup %s to %s: %v\n", bkp.ID, p.Name, err)
			}
		}(pool, *backup)
	}
}

// EnsureLocalSnapshotFromRemote downloads remote snapshot files if local copy is missing or remote source is requested.
func EnsureLocalSnapshotFromRemote(snapshot *config.Snapshot) error {
	if snapshot == nil {
		return fmt.Errorf("snapshot is nil")
	}
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
	// Download standard files
	for _, filename := range []string{"disk.qcow2", "domain.xml", "config", "rootfs.tar.gz"} {
		remoteFile := filepath.ToSlash(filepath.Join(snapshot.RemotePath, filename))
		localFile := filepath.Join(snapshot.Path, filename)
		_ = client.DownloadFile(ctx, remoteFile, localFile)
	}
	return nil
}

// EnsureLocalBackupFromRemote downloads remote backup files if local copy is missing or remote source is requested.
func EnsureLocalBackupFromRemote(backup *config.Backup) error {
	if backup == nil {
		return fmt.Errorf("backup is nil")
	}
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
	for _, filename := range []string{"disk.qcow2", "domain.xml", "unattend.iso", "seed.iso"} {
		remoteFile := filepath.ToSlash(filepath.Join(backup.RemotePath, filename))
		localFile := filepath.Join(backup.Path, filename)
		_ = client.DownloadFile(ctx, remoteFile, localFile)
	}
	return nil
}
