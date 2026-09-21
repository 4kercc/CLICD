package api

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"clicd/internal/config"
	"clicd/internal/kvm"
	"clicd/internal/lxc"
	"clicd/internal/storage/remote"
)

var kvmManager = kvm.NewManager()

func init() {
	config.StartContainerCallback = startByRuntime
	config.StopContainerCallback = stopByRuntime
	config.RestartContainerCallback = restartByRuntime
	config.ResetPasswordCallback = func(id int) (string, error) {
		return resetPasswordByRuntime(id, "")
	}
	config.CreateSnapshotCallback = func(id int, user string) (string, error) {
		snap, err := createSnapshotByRuntime(id, user, false, 0)
		if err != nil {
			return "", err
		}
		remote.SyncSnapshotToRemoteStorage(&snap)
		return snap.ID, nil
	}
	config.BatchActionCallback = func(ids []int, action string) error {
		for _, id := range ids {
			c := config.FindContainer(id)
			if c == nil {
				continue
			}
			var taskType TaskType
			switch action {
			case "start":
				taskType = TaskStart
			case "stop":
				taskType = TaskStop
			case "restart":
				taskType = TaskRestart
			case "snapshot":
				taskType = TaskSnapshot
			default:
				continue
			}
			globalQueue.enqueueSingleWithUser(id, c.Name, taskType, "", "telegram:admin")
		}
		return nil
	}

	config.BatchCreateQuickVMCallback = func(namePrefix string, count int, templateID string, vcpu float64, ramMB int, diskGB int, isKVM bool) (int, error) {
		if count <= 0 {
			count = 1
		}
		if count > 20 {
			count = 20
		}
		if vcpu <= 0 {
			vcpu = 1
		}
		if ramMB <= 0 {
			ramMB = 1024
		}
		if diskGB <= 0 {
			diskGB = 10
		}
		virt := config.VirtualizationLXC
		if isKVM {
			virt = config.VirtualizationKVM
		}
		if templateID == "" {
			if isKVM {
				templateID = "debian-12-generic-amd64"
			} else {
				templateID = "debian-bookworm"
			}
		}

		assignNAT := true
		var configs []lxc.ContainerConfig
		for i := 1; i <= count; i++ {
			cName := fmt.Sprintf("%s-%d", namePrefix, i)
			if count == 1 {
				cName = namePrefix
			}
			// Check duplicate name
			if config.FindContainerByName(cName) != nil {
				cName = fmt.Sprintf("%s-%d", namePrefix, time.Now().Unix()%10000+int64(i))
			}
			configs = append(configs, lxc.ContainerConfig{
				Name:               cName,
				Virtualization:     virt,
				TemplateID:         templateID,
				VCPU:               vcpu,
				RAMMB:              ramMB,
				DiskGB:             diskGB,
				AssignNAT:          &assignNAT,
				PortMappingCount:   3,
				SSHAuthMode:        lxc.SSHAuthAutoPassword,
				SnapshotLimit:      config.DefaultSnapshotLimit,
			})
		}
		planned, err := lxc.ReserveBatchCreateNATPorts(configs)
		if err != nil {
			return 0, err
		}
		ids := globalQueue.EnqueueBatchCreateWithAudit(planned, "telegram:admin", "127.0.0.1", "TelegramBot")
		return len(ids), nil
	}

	config.BatchAdjustQuickConfigCallback = func(ids []int, vcpu float64, ramMB int, diskGB int, downMbps int, upMbps int) (int, error) {
		count := 0
		for _, id := range ids {
			c := config.FindContainer(id)
			if c == nil {
				continue
			}
			if vcpu > 0 {
				c.VCPU = vcpu
			}
			if ramMB > 0 {
				c.RAMMB = ramMB
			}
			if diskGB > 0 && diskGB >= c.DiskGB {
				c.DiskGB = diskGB
			}
			if downMbps >= 0 {
				c.NetworkDownMbps = downMbps
			}
			if upMbps >= 0 {
				c.NetworkUpMbps = upMbps
			}
			if c.Status == "running" {
				_ = applyLimitsByRuntime(c)
			}
			count++
		}
		if count > 0 {
			_ = config.SaveConfig()
		}
		return count, nil
	}

	config.BatchRestoreLatestSnapshotCallback = func(ids []int) (int, error) {
		restoredCount := 0
		for _, id := range ids {
			snaps := config.ContainerSnapshots(id)
			if len(snaps) == 0 {
				continue
			}
			sortSnapshotsNewestFirst(snaps)
			latest := snaps[0]
			if err := restoreSnapshotByRuntime(latest.ID); err == nil {
				restoredCount++
			}
		}
		return restoredCount, nil
	}
}

const noNetworkSelectedMessage = "请勾选任意一个可用网络"

func runtimeFromRequest(value string) string {
	return config.NormalizeVirtualization(value)
}

func hasRequestedNetwork(cfg lxc.ContainerConfig) bool {
	return cfg.WantsNAT() || cfg.WantsLANIPv4() || cfg.AssignIPv4 || len(cfg.PublicIPv4s) > 0 || cfg.AssignIPv6 || len(cfg.IPv6Addresses) > 0
}

func runtimeFromTemplateID(templateID string) string {
	if kvm.FindImage(templateID) != nil {
		return config.VirtualizationKVM
	}
	return config.VirtualizationLXC
}

func createByRuntime(cfg lxc.ContainerConfig) error {
	cfg.Virtualization = runtimeFromRequest(cfg.Virtualization)
	cfg.NormalizeResourceAliases()
	if cfg.Virtualization == config.VirtualizationKVM {
		return kvmManager.CreateContainer(cfg)
	}
	return lxcManager.CreateContainer(cfg)
}

func validateCreateSSHAuth(cfg lxc.ContainerConfig) error {
	if cfg.Virtualization == config.VirtualizationKVM && kvm.IsWindowsImage(cfg.TemplateID) {
		return nil
	}
	_, err := lxc.ResolveCreateSSHAccess(cfg)
	return err
}

func validateReinstallSSHAuth(c *config.Container, templateID string, cfg lxc.ContainerConfig) error {
	if c != nil && c.IsKVM() && kvm.IsWindowsImage(templateID) {
		return nil
	}
	currentPassword := ""
	if c != nil {
		currentPassword = c.SSHPassword
	}
	_, err := lxc.ResolveReinstallSSHAccess(currentPassword, cfg)
	return err
}

func startByRuntime(id int) error {
	c := config.FindContainer(id)
	if c != nil && c.IsKVM() {
		return kvmManager.StartContainer(id)
	}
	return lxcManager.StartContainer(id)
}

func stopByRuntime(id int) error {
	c := config.FindContainer(id)
	if c != nil && c.IsKVM() {
		return kvmManager.StopContainer(id)
	}
	return lxcManager.StopContainer(id)
}

func restartByRuntime(id int) error {
	c := config.FindContainer(id)
	if c != nil && c.IsKVM() {
		return kvmManager.RestartContainer(id)
	}
	return lxcManager.RestartContainer(id)
}

func destroyByRuntime(id int) error {
	c := config.FindContainer(id)
	if c != nil && c.IsKVM() {
		return kvmManager.DestroyContainer(id)
	}
	return lxcManager.DestroyContainer(id)
}

func reinstallByRuntime(id int, templateID string, authConfig ...lxc.ContainerConfig) error {
	c := config.FindContainer(id)
	if c != nil && c.IsKVM() {
		return kvmManager.ReinstallContainer(id, templateID, authConfig...)
	}
	return lxcManager.ReinstallContainer(id, templateID, authConfig...)
}

func resetPasswordByRuntime(id int, password string) (string, error) {
	c := config.FindContainer(id)
	if c != nil && c.IsKVM() {
		return kvmManager.ResetSSHPassword(id, password)
	}
	return lxcManager.ResetSSHPassword(id, password)
}

func assignIPv6ByRuntime(id int) (*config.Container, error) {
	c := config.FindContainer(id)
	if c != nil && c.IsKVM() {
		return kvmManager.AssignIPv6(id)
	}
	return lxcManager.AssignIPv6(id)
}

func updatePublicIPv4ByRuntime(id int, requested []string, count int, auto bool) (*config.Container, error) {
	c := config.FindContainer(id)
	if c != nil && c.IsKVM() {
		return kvmManager.UpdatePublicIPv4Assignments(id, requested, count, auto)
	}
	return lxcManager.UpdatePublicIPv4Assignments(id, requested, count, auto)
}

func updateIPv6ByRuntime(id int, requested []string, count int, auto bool) (*config.Container, error) {
	c := config.FindContainer(id)
	if c != nil && c.IsKVM() {
		return kvmManager.UpdateIPv6Assignments(id, requested, count, auto)
	}
	return lxcManager.UpdateIPv6Assignments(id, requested, count, auto)
}

func usageByRuntime(id int) (map[string]interface{}, error) {
	c := config.FindContainer(id)
	if c == nil {
		return nil, fmt.Errorf("container not found: %d", id)
	}

	type result struct {
		usage map[string]interface{}
		err   error
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch := make(chan result, 1)
	go func() {
		if c.IsKVM() {
			u, err := kvmManager.GetResourceUsage(id)
			ch <- result{usage: u, err: err}
			return
		}
		u, err := lxcManager.GetResourceUsage(id)
		ch <- result{usage: u, err: err}
	}()

	select {
	case res := <-ch:
		return res.usage, res.err
	case <-ctx.Done():
		// 5秒超时直接返回兜底空指标，避免阻塞整体请求
		fallback := map[string]interface{}{
			"memory_usage_bytes": int64(0),
			"memory_total_bytes": int64(c.RAMMB) * 1024 * 1024,
			"cpu_usage_usec":     uint64(0),
			"cpu_usage_pct":      0.0,
			"disk_usage_bytes":   int64(0),
			"network_rx_bytes":   uint64(0),
			"network_tx_bytes":   uint64(0),
			"timed_out":          true,
		}
		return fallback, nil
	}
}

func trafficByRuntime(id int) map[string]interface{} {
	c := config.FindContainer(id)
	if c != nil && c.IsKVM() {
		return kvmManager.GetTrafficInfo(id)
	}
	return lxcManager.GetTrafficInfo(id)
}

func createSnapshotByRuntime(id int, createdBy string, scheduled bool, rotateLimit int, storagePoolID ...string) (config.Snapshot, error) {
	c := config.FindContainer(id)
	if c != nil && c.IsKVM() {
		return kvmManager.CreateSnapshot(id, createdBy, scheduled, rotateLimit, storagePoolID...)
	}
	return lxcManager.CreateSnapshot(id, createdBy, scheduled, rotateLimit, storagePoolID...)
}

func deleteSnapshotByRuntime(snapshotID string) error {
	snapshot := config.FindSnapshot(snapshotID)
	if snapshot != nil {
		if c := config.FindContainer(snapshot.ContainerID); c != nil && c.IsKVM() {
			return kvmManager.DeleteSnapshot(snapshotID)
		}
		if strings.Contains(snapshot.Path, string(os.PathSeparator)+"kvm"+string(os.PathSeparator)) {
			return kvmManager.DeleteSnapshot(snapshotID)
		}
	}
	return lxcManager.DeleteSnapshot(snapshotID)
}

func restoreSnapshotByRuntime(snapshotID string) error {
	snapshot := config.FindSnapshot(snapshotID)
	if snapshot != nil {
		if snapshot.RemoteSynced && snapshot.RemotePath != "" {
			// Pull remote copy if local disk is missing
			if remoteSnapshotDiskMissing(snapshot.Path) {
				if err := remote.EnsureLocalSnapshotFromRemote(snapshot); err != nil {
					return fmt.Errorf("failed to fetch snapshot from remote storage: %w", err)
				}
			}
		}
		if c := config.FindContainer(snapshot.ContainerID); c != nil && c.IsKVM() {
			return kvmManager.RestoreSnapshot(snapshotID)
		}
		if strings.Contains(snapshot.Path, string(os.PathSeparator)+"kvm"+string(os.PathSeparator)) {
			return kvmManager.RestoreSnapshot(snapshotID)
		}
	}
	return lxcManager.RestoreSnapshot(snapshotID)
}

func setSnapshotScheduleByRuntime(id int, enabled bool, intervalHours int, scheduleTime string, maxCopies int, createdBy string) (*config.Container, error) {
	c := config.FindContainer(id)
	if c != nil && c.IsKVM() {
		return kvmManager.SetSnapshotSchedule(id, enabled, intervalHours, scheduleTime, maxCopies, createdBy)
	}
	return lxcManager.SetSnapshotSchedule(id, enabled, intervalHours, scheduleTime, maxCopies, createdBy)
}

func createBackupByRuntime(id int, createdBy string, storagePoolID ...string) (config.Backup, error) {
	c := config.FindContainer(id)
	if c != nil && c.IsKVM() {
		return kvmManager.CreateBackup(id, createdBy, storagePoolID...)
	}
	return config.Backup{}, fmt.Errorf("backup is currently supported for KVM instances")
}

func deleteBackupByRuntime(backupID string) error {
	backup := config.FindBackup(backupID)
	if backup != nil {
		return kvmManager.DeleteBackup(backupID)
	}
	return fmt.Errorf("backup not found: %s", backupID)
}

func restoreBackupByRuntime(backupID string) error {
	backup := config.FindBackup(backupID)
	if backup != nil {
		if backup.RemoteSynced && backup.RemotePath != "" {
			if remoteSnapshotDiskMissing(backup.Path) {
				if err := remote.EnsureLocalBackupFromRemote(backup); err != nil {
					return fmt.Errorf("failed to fetch backup from remote storage: %w", err)
				}
			}
		}
		return kvmManager.RestoreBackup(backupID)
	}
	return fmt.Errorf("backup not found: %s", backupID)
}

func remoteSnapshotDiskMissing(path string) bool {
	if path == "" {
		return true
	}
	if _, err := os.Stat(filepath.Join(path, "disk.qcow2")); err == nil {
		return false
	}
	return true
}

func resizeDiskByRuntime(id int, newSizeGB int) error {
	c := config.FindContainer(id)
	if c != nil && c.IsKVM() {
		return kvmManager.ResizeDisk(id, newSizeGB)
	}
	return fmt.Errorf("online disk resize is currently supported for KVM instances")
}

func mountVirtioISOByRuntime(id int, mount bool) error {
	c := config.FindContainer(id)
	if c != nil && c.IsKVM() {
		return kvmManager.MountVirtioISO(id, mount)
	}
	return fmt.Errorf("mount VirtIO ISO is only supported for KVM instances")
}

func getGuestAgentStatusByRuntime(id int) (bool, map[string]interface{}, error) {
	c := config.FindContainer(id)
	if c != nil && c.IsKVM() {
		return kvmManager.GetGuestAgentStatus(id)
	}
	return false, nil, fmt.Errorf("guest agent status is only supported for KVM instances")
}

func importDiskImageByRuntime(id int, srcPath string, asOverlayBase bool) error {
	c := config.FindContainer(id)
	if c != nil && c.IsKVM() {
		return kvmManager.ImportDiskImage(id, srcPath, asOverlayBase)
	}
	return fmt.Errorf("disk import is currently supported for KVM instances")
}

func applyLimitsByRuntime(c *config.Container) error {
	if c != nil && c.IsKVM() {
		return kvmManager.ApplyContainerLimits(c)
	}
	return lxcManager.ApplyContainerLimits(c)
}

func listByRuntime() ([]config.Container, error) {
	containers, err := lxcManager.ListContainers()
	if err != nil {
		containers = config.AppConfig.Containers
	}
	containers = kvmManager.ListContainers(containers)
	return containers, err
}

func validateRuntimeResourceRequest(runtime string, vcpu float64, ramMB int, diskGB int) error {
	if runtime == config.VirtualizationKVM {
		if vcpu < 1 || math.Abs(vcpu-math.Round(vcpu)) > 0.000001 {
			return fmt.Errorf("KVM vCPU must be a whole number and at least 1")
		}
	}
	return validateContainerResourceRequest(vcpu, ramMB, diskGB)
}
