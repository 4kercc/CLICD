package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"clicd/internal/config"
	"clicd/internal/storage/remote"
)

func HandleSnapshots(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	if !requireScope(w, r, "snapshot:read") {
		return
	}
	snapshots := append([]config.Snapshot(nil), config.AppConfig.Snapshots...)
	snapshots = filterSnapshotsForRequest(r, snapshots)
	sortSnapshotsNewestFirst(snapshots)
	snapshots = decorateSnapshotUsage(snapshots)
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: snapshots})
}

func handleContainerSnapshots(w http.ResponseWriter, r *http.Request, containerID int, action string) {
	switch {
	case action == "snapshots" && r.Method == http.MethodGet:
		if !requireScope(w, r, "snapshot:read") {
			return
		}
		listContainerSnapshots(w, r, containerID)
	case action == "snapshots" && r.Method == http.MethodPost:
		if !requireScope(w, r, "snapshot:create") {
			return
		}
		createContainerSnapshot(w, r, containerID)
	case action == "snapshots/schedule" && r.Method == http.MethodPost:
		if !requireScope(w, r, "snapshot:schedule") {
			return
		}
		updateSnapshotSchedule(w, r, containerID)
		case action == "snapshots/quota" && r.Method == http.MethodPut:
			if !requireScope(w, r, "snapshot:schedule") {
				return
			}
			updateSnapshotQuota(w, r, containerID)
		case action == "snapshots/sync-all" && r.Method == http.MethodPost:
			if !requireScope(w, r, "snapshot:create") {
				return
			}
			syncAllContainerSnapshots(w, r, containerID)
		case strings.HasPrefix(action, "snapshots/") && strings.HasSuffix(action, "/sync") && r.Method == http.MethodPost:
			if !requireScope(w, r, "snapshot:create") {
				return
			}
			snapshotID := strings.TrimSuffix(strings.TrimPrefix(action, "snapshots/"), "/sync")
			syncContainerSnapshot(w, r, containerID, snapshotID)
		case strings.HasPrefix(action, "snapshots/") && strings.HasSuffix(action, "/restore") && r.Method == http.MethodPost:
		if !requireScope(w, r, "snapshot:restore") {
			return
		}
		snapshotID := strings.TrimSuffix(strings.TrimPrefix(action, "snapshots/"), "/restore")
		restoreContainerSnapshot(w, r, containerID, snapshotID)
	case strings.HasPrefix(action, "snapshots/") && r.Method == http.MethodDelete:
		if !requireScope(w, r, "snapshot:delete") {
			return
		}
		snapshotID := strings.TrimPrefix(action, "snapshots/")
		deleteContainerSnapshot(w, r, containerID, snapshotID)
	default:
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Snapshot action not found"})
	}
}

func listContainerSnapshots(w http.ResponseWriter, r *http.Request, containerID int) {
	c := config.FindContainer(containerID)
	if c == nil {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Container not found"})
		return
	}
	snapshots := config.ContainerSnapshots(containerID)
	sortSnapshotsNewestFirst(snapshots)
	snapshots = decorateSnapshotUsage(snapshots)
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: map[string]interface{}{
		"snapshots": snapshots,
		"quota":     config.ContainerSnapshotLimit(c),
		"schedule": map[string]interface{}{
			"enabled":        c.SnapshotScheduleEnabled,
			"interval_hours": c.SnapshotScheduleIntervalHours,
			"time":           c.SnapshotScheduleTime,
			"max_copies":     c.SnapshotScheduleMaxCopies,
			"last_run":       c.SnapshotScheduleLastRun,
			"next_run":       c.SnapshotScheduleNextRun,
			"created_by":     c.SnapshotScheduleCreatedBy,
		},
	}})
}

func createContainerSnapshot(w http.ResponseWriter, r *http.Request, containerID int) {
	user := requestUser(r)
	var req struct {
		Description   string `json:"description"`
		StoragePoolID string `json:"storage_pool_id"`
	}
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
			return
		}
	}
	req.Description = strings.TrimSpace(req.Description)
	req.StoragePoolID = strings.TrimSpace(req.StoragePoolID)
	if _, err := config.SelectStoragePoolForContent(config.StorageContentSnapshots, req.StoragePoolID, 0); err != nil {
		jsonResponse(w, http.StatusConflict, APIResponse{Success: false, Message: err.Error()})
		return
	}
	if isSubUserRequest(r) {
		c := config.FindContainer(containerID)
		limit := config.ContainerSnapshotLimit(c)
		if len(config.ContainerSnapshots(containerID)) >= limit {
			jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "Snapshot quota reached. Delete an old snapshot first."})
			return
		}
	}
	snapshot, err := createSnapshotByRuntime(containerID, user, false, 0, req.Description, req.StoragePoolID)
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: err.Error()})
		return
	}
	// Auto sync snapshot to remote storage if enabled
	remote.SyncSnapshotToRemoteStorage(&snapshot)
	config.AddAuditLog("snapshot.create", snapshot.ContainerName, snapshot.ID, user)
	snapshot.UniqueBytes = snapshotUniqueBytes(&snapshot)
	jsonResponse(w, http.StatusCreated, APIResponse{Success: true, Data: snapshot})
}

func updateSnapshotQuota(w http.ResponseWriter, r *http.Request, containerID int) {
	if isSubUserRequest(r) {
		jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "Sub-users cannot change snapshot quota"})
		return
	}
	var req struct {
		SnapshotLimit int `json:"snapshot_limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
		return
	}
	if req.SnapshotLimit <= 0 {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Snapshot quota must be at least 1"})
		return
	}
	c := config.FindContainer(containerID)
	if c == nil {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Container not found"})
		return
	}
	c.SnapshotLimit = req.SnapshotLimit
	if err := config.SaveConfig(); err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Failed to save config"})
		return
	}
	user := requestUser(r)
	config.AddAuditLog("snapshot.quota", c.Name, "limit="+strconv.Itoa(req.SnapshotLimit), user)
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: map[string]interface{}{
		"container": c,
		"quota":     c.SnapshotLimit,
	}})
}

func updateSnapshotSchedule(w http.ResponseWriter, r *http.Request, containerID int) {
	var req struct {
		Enabled       bool   `json:"enabled"`
		IntervalHours int    `json:"interval_hours"`
		Time          string `json:"time"`
		MaxCopies     int    `json:"max_copies"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
		return
	}
	if req.IntervalHours <= 0 {
		req.IntervalHours = 24
	}
	if req.IntervalHours < 24 {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Snapshot schedule interval cannot be less than 24 hours"})
		return
	}
	if req.Time == "" {
		req.Time = "03:00"
	}
	if req.MaxCopies < 0 {
		req.MaxCopies = 0
	}
	if req.Enabled {
		if _, err := config.SelectStoragePoolForContent(config.StorageContentSnapshots, "", 0); err != nil {
			jsonResponse(w, http.StatusConflict, APIResponse{Success: false, Message: err.Error()})
			return
		}
	}
	user := requestUser(r)
	c, err := setSnapshotScheduleByRuntime(containerID, req.Enabled, req.IntervalHours, req.Time, req.MaxCopies, user)
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	}

	if req.Enabled {
		config.AddAuditLog("snapshot.schedule", c.Name, "enabled", user)
	} else {
		config.AddAuditLog("snapshot.schedule", c.Name, "disabled", user)
	}

	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: map[string]interface{}{
		"container": c,
	}})
}

func deleteContainerSnapshot(w http.ResponseWriter, r *http.Request, containerID int, snapshotID string) {
	snapshot := config.FindSnapshot(snapshotID)
	if snapshot == nil || snapshot.ContainerID != containerID {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Snapshot not found"})
		return
	}
	user := requestUser(r)
	if err := deleteSnapshotByRuntime(snapshotID); err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: err.Error()})
		return
	}
	config.AddAuditLog("snapshot.delete", snapshot.ContainerName, snapshot.ID, user)
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "Snapshot deleted"})
}

func restoreContainerSnapshot(w http.ResponseWriter, r *http.Request, containerID int, snapshotID string) {
	snapshot := config.FindSnapshot(snapshotID)
	if snapshot == nil || snapshot.ContainerID != containerID {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Snapshot not found"})
		return
	}
	user := requestUser(r)
	if err := restoreSnapshotByRuntime(snapshotID); err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: err.Error()})
		return
	}
		config.AddAuditLog("snapshot.restore", snapshot.ContainerName, snapshot.ID, user)
		jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "Snapshot restored"})
	}

	func syncContainerSnapshot(w http.ResponseWriter, r *http.Request, containerID int, snapshotID string) {
		snapshot := config.FindSnapshot(snapshotID)
		if snapshot == nil || snapshot.ContainerID != containerID {
			jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Snapshot not found"})
			return
		}
		var pool *config.StoragePool
		for _, p := range config.AppConfig.StoragePools {
			if p.Enabled && p.SyncSnapshots && p.Type != "local" && p.Type != "" {
				pool = &p
				break
			}
		}
		if pool == nil {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "未找到已启用「同步快照」的远程存储池，请先在存储设置中添加并开启"})
			return
		}
		if err := remote.SyncSingleSnapshotToPool(snapshot, pool); err != nil {
			jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "同步快照到远程存储失败: " + err.Error()})
			return
		}
		user := requestUser(r)
		config.AddAuditLog("snapshot.sync", snapshot.ContainerName, snapshot.ID, user)
		snapshot.UniqueBytes = snapshotUniqueBytes(snapshot)
		jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "快照已成功同步至远程存储", Data: snapshot})
	}

	func syncAllContainerSnapshots(w http.ResponseWriter, r *http.Request, containerID int) {
		snapshots := config.ContainerSnapshots(containerID)
		if len(snapshots) == 0 {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "当前容器暂无快照"})
			return
		}
		var pool *config.StoragePool
		for _, p := range config.AppConfig.StoragePools {
			if p.Enabled && p.SyncSnapshots && p.Type != "local" && p.Type != "" {
				pool = &p
				break
			}
		}
		if pool == nil {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "未找到已启用「同步快照」的远程存储池，请先在存储设置中添加并开启"})
			return
		}
		count := 0
		for _, s := range snapshots {
			snapCopy := s
			if err := remote.SyncSingleSnapshotToPool(&snapCopy, pool); err == nil {
				count++
			}
		}
		user := requestUser(r)
		c := config.FindContainer(containerID)
		cName := ""
		if c != nil {
			cName = c.Name
		}
		config.AddAuditLog("snapshot.sync_all", cName, strconv.Itoa(count)+" snapshots synced", user)
		jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: fmt.Sprintf("已成功同步 %d 个快照至远程存储", count)})
	}

func requestUser(r *http.Request) string {
	return requestActor(r)
}

func sortSnapshotsNewestFirst(snapshots []config.Snapshot) {
	sort.SliceStable(snapshots, func(i, j int) bool {
		ti, _ := time.Parse("2006-01-02 15:04:05", snapshots[i].CreatedAt)
		tj, _ := time.Parse("2006-01-02 15:04:05", snapshots[j].CreatedAt)
		return tj.Before(ti)
	})
}

func filterSnapshotsForRequest(r *http.Request, snapshots []config.Snapshot) []config.Snapshot {
	allowed, restricted := requestAllowedContainers(r)
	if !restricted {
		return snapshots
	}
	filtered := make([]config.Snapshot, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if c := config.FindContainer(snapshot.ContainerID); c != nil && isContainerAllowed(allowed, c) {
			filtered = append(filtered, snapshot)
		}
	}
	return filtered
}

// HandleBackups handles global backup listing
func HandleBackups(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	if !requireScope(w, r, "snapshot:read") {
		return
	}
	backups := append([]config.Backup(nil), config.AppConfig.Backups...)
	backups = filterBackupsForRequest(r, backups)
	sortBackupsNewestFirst(backups)
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: backups})
}

func handleContainerBackups(w http.ResponseWriter, r *http.Request, containerID int, action string) {
	switch {
	case action == "backups" && r.Method == http.MethodGet:
		if !requireScope(w, r, "snapshot:read") {
			return
		}
		listContainerBackups(w, r, containerID)
		case action == "backups" && r.Method == http.MethodPost:
			if !requireScope(w, r, "snapshot:create") {
				return
			}
			createContainerBackup(w, r, containerID)
		case action == "backups/sync-all" && r.Method == http.MethodPost:
			if !requireScope(w, r, "snapshot:create") {
				return
			}
			syncAllContainerBackups(w, r, containerID)
		case strings.HasPrefix(action, "backups/") && strings.HasSuffix(action, "/sync") && r.Method == http.MethodPost:
			if !requireScope(w, r, "snapshot:create") {
				return
			}
			backupID := strings.TrimSuffix(strings.TrimPrefix(action, "backups/"), "/sync")
			syncContainerBackup(w, r, containerID, backupID)
		case strings.HasPrefix(action, "backups/") && strings.HasSuffix(action, "/restore") && r.Method == http.MethodPost:
		if !requireScope(w, r, "snapshot:restore") {
			return
		}
		backupID := strings.TrimSuffix(strings.TrimPrefix(action, "backups/"), "/restore")
		restoreContainerBackup(w, r, containerID, backupID)
	case strings.HasPrefix(action, "backups/") && r.Method == http.MethodDelete:
		if !requireScope(w, r, "snapshot:delete") {
			return
		}
		backupID := strings.TrimPrefix(action, "backups/")
		deleteContainerBackup(w, r, containerID, backupID)
	default:
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Backup action not found"})
	}
}

func listContainerBackups(w http.ResponseWriter, r *http.Request, containerID int) {
	c := config.FindContainer(containerID)
	if c == nil {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Container not found"})
		return
	}
	backups := config.ContainerBackups(containerID)
	sortBackupsNewestFirst(backups)
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: map[string]interface{}{
		"backups": backups,
	}})
}

func createContainerBackup(w http.ResponseWriter, r *http.Request, containerID int) {
	user := requestUser(r)
	var req struct {
		StoragePoolID string `json:"storage_pool_id"`
	}
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
			return
		}
	}
	req.StoragePoolID = strings.TrimSpace(req.StoragePoolID)
	if _, err := config.SelectStoragePoolForContent(config.StorageContentBackups, req.StoragePoolID, 0); err != nil {
		jsonResponse(w, http.StatusConflict, APIResponse{Success: false, Message: err.Error()})
		return
	}
	backup, err := createBackupByRuntime(containerID, user, req.StoragePoolID)
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: err.Error()})
		return
	}
	// Auto sync backup to remote storage if enabled
	remote.SyncBackupToRemoteStorage(&backup)
	config.AddAuditLog("backup.create", backup.ContainerName, backup.ID, user)
	jsonResponse(w, http.StatusCreated, APIResponse{Success: true, Data: backup})
}

func deleteContainerBackup(w http.ResponseWriter, r *http.Request, containerID int, backupID string) {
	backup := config.FindBackup(backupID)
	if backup == nil || backup.ContainerID != containerID {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Backup not found"})
		return
	}
	user := requestUser(r)
	if err := deleteBackupByRuntime(backupID); err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: err.Error()})
		return
	}
	config.AddAuditLog("backup.delete", backup.ContainerName, backup.ID, user)
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "Backup deleted"})
}

func restoreContainerBackup(w http.ResponseWriter, r *http.Request, containerID int, backupID string) {
	backup := config.FindBackup(backupID)
	if backup == nil || backup.ContainerID != containerID {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Backup not found"})
		return
	}
	user := requestUser(r)
	if err := restoreBackupByRuntime(backupID); err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: err.Error()})
		return
	}
		config.AddAuditLog("backup.restore", backup.ContainerName, backup.ID, user)
		jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "Backup restored"})
	}

	func syncContainerBackup(w http.ResponseWriter, r *http.Request, containerID int, backupID string) {
		backup := config.FindBackup(backupID)
		if backup == nil || backup.ContainerID != containerID {
			jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Backup not found"})
			return
		}
		var pool *config.StoragePool
		for _, p := range config.AppConfig.StoragePools {
			if p.Enabled && p.SyncBackups && p.Type != "local" && p.Type != "" {
				pool = &p
				break
			}
		}
		if pool == nil {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "未找到已启用「同步备份」的远程存储池，请先在存储设置中添加并开启"})
			return
		}
		if err := remote.SyncSingleBackupToPool(backup, pool); err != nil {
			jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "同步备份到远程存储失败: " + err.Error()})
			return
		}
		user := requestUser(r)
		config.AddAuditLog("backup.sync", backup.ContainerName, backup.ID, user)
		jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "备份已成功同步至远程存储", Data: backup})
	}

	func syncAllContainerBackups(w http.ResponseWriter, r *http.Request, containerID int) {
		backups := config.ContainerBackups(containerID)
		if len(backups) == 0 {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "当前容器暂无全量备份"})
			return
		}
		var pool *config.StoragePool
		for _, p := range config.AppConfig.StoragePools {
			if p.Enabled && p.SyncBackups && p.Type != "local" && p.Type != "" {
				pool = &p
				break
			}
		}
		if pool == nil {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "未找到已启用「同步备份」的远程存储池，请先在存储设置中添加并开启"})
			return
		}
		count := 0
		for _, b := range backups {
			bkpCopy := b
			if err := remote.SyncSingleBackupToPool(&bkpCopy, pool); err == nil {
				count++
			}
		}
		user := requestUser(r)
		c := config.FindContainer(containerID)
		cName := ""
		if c != nil {
			cName = c.Name
		}
		config.AddAuditLog("backup.sync_all", cName, strconv.Itoa(count)+" backups synced", user)
		jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: fmt.Sprintf("已成功同步 %d 个备份至远程存储", count)})
	}

// HandleStorageSyncProgress returns active sync/restore progress by snapshot/backup ID
func HandleStorageSyncProgress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "id parameter is required"})
		return
	}
	progress := remote.GetProgress(id)
	if progress == nil {
		jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: map[string]interface{}{"status": "idle"}})
		return
	}
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: progress})
}

func sortBackupsNewestFirst(backups []config.Backup) {
	sort.SliceStable(backups, func(i, j int) bool {
		ti, _ := time.Parse("2006-01-02 15:04:05", backups[i].CreatedAt)
		tj, _ := time.Parse("2006-01-02 15:04:05", backups[j].CreatedAt)
		return tj.Before(ti)
	})
}

func filterBackupsForRequest(r *http.Request, backups []config.Backup) []config.Backup {
	allowed, restricted := requestAllowedContainers(r)
	if !restricted {
		return backups
	}
	filtered := make([]config.Backup, 0, len(backups))
	for _, backup := range backups {
		if c := config.FindContainer(backup.ContainerID); c != nil && isContainerAllowed(allowed, c) {
			filtered = append(filtered, backup)
		}
	}
	return filtered
}
