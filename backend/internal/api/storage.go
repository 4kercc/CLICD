package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	pathpkg "path"
	"path/filepath"
	"strings"

	"clicd/internal/config"
)

type storageInfoResponse struct {
	Pools        []storagePoolInfo `json:"pools"`
	Disks        []storageDiskInfo `json:"disks"`
	ContentTypes []string          `json:"content_types"`
}

type storagePoolInfo struct {
	config.StoragePool
	Available      bool                  `json:"available"`
	Exists         bool                  `json:"exists"`
	SizeBytes      int64                 `json:"size_bytes"`
	UsedBytes      int64                 `json:"used_bytes"`
	FreeBytes      int64                 `json:"free_bytes"`
	ClicdUsedBytes int64                 `json:"clicd_used_bytes"`
	ContentUsage   []storageContentUsage `json:"content_usage"`
	Error          string                `json:"error,omitempty"`
}

type storageContentUsage struct {
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
}

type storageDiskInfo struct {
	Name           string                `json:"name"`
	Path           string                `json:"path"`
	Type           string                `json:"type"`
	FSType         string                `json:"fstype"`
	MountPoint     string                `json:"mount_point"`
	Model          string                `json:"model"`
	SizeBytes      int64                 `json:"size_bytes"`
	UsedBytes      int64                 `json:"used_bytes"`
	FreeBytes      int64                 `json:"free_bytes"`
	StoragePoolID  string                `json:"storage_pool_id,omitempty"`
	StoragePath    string                `json:"storage_path,omitempty"`
	ClicdUsedBytes int64                 `json:"clicd_used_bytes"`
	ContentUsage   []storageContentUsage `json:"content_usage"`
}

func HandleStorage(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: buildStorageInfo()})
	case http.MethodPut:
		var req struct {
			Pools []config.StoragePool `json:"pools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
			return
		}
		pools, err := normalizeStoragePoolsRequest(req.Pools)
		if err != nil {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
			return
		}
		for _, pool := range pools {
			if err := os.MkdirAll(pool.Path, 0755); err != nil {
				jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: fmt.Sprintf("Failed to create %s: %v", pool.Path, err)})
				return
			}
		}
		config.AppConfig.StoragePools = pools
		if err := config.SaveConfig(); err != nil {
			jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Failed to save storage pools"})
			return
		}
		jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: buildStorageInfo()})
	default:
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
	}
}

func buildStorageInfo() storageInfoResponse {
	disks := detectStorageDisks()
	pools := make([]storagePoolInfo, 0, len(config.AppConfig.StoragePools))
	for _, pool := range config.AppConfig.StoragePools {
		info := storagePoolInfo{StoragePool: pool}
		if filepath.Clean(pool.MountPoint) == string(os.PathSeparator) {
			_ = os.MkdirAll(pool.Path, 0755)
		}
		if st, err := os.Stat(pool.Path); err == nil && st.IsDir() {
			info.Exists = true
		} else if err != nil {
			info.Error = err.Error()
		}
		detectedMountPoint := bestMountPointForPath(pool.Path, disks)
		if info.MountPoint == "" {
			info.MountPoint = detectedMountPoint
		}
		if detectedMountPoint != "" && filepath.Clean(info.MountPoint) == filepath.Clean(detectedMountPoint) {
			info.Available = info.Exists
			info.SizeBytes, info.UsedBytes, info.FreeBytes = dfPath(pool.Path)
			info.ContentUsage, info.ClicdUsedBytes = contentUsageForPool(pool.Path)
		} else if info.Error == "" {
			info.Error = "storage disk is not mounted"
		}
		pools = append(pools, info)
	}
	for i := range disks {
		for _, pool := range pools {
			if pool.MountPoint != disks[i].MountPoint {
				continue
			}
			disks[i].ClicdUsedBytes += pool.ClicdUsedBytes
			disks[i].ContentUsage = mergeContentUsage(disks[i].ContentUsage, pool.ContentUsage)
			if disks[i].StoragePoolID == "" {
				disks[i].StoragePoolID = pool.ID
				disks[i].StoragePath = pool.Path
			}
		}
	}
	return storageInfoResponse{
		Pools: pools,
		Disks: disks,
		ContentTypes: []string{
			config.StorageContentLXC,
			config.StorageContentKVM,
			config.StorageContentImages,
			config.StorageContentSnapshots,
			config.StorageContentBackups,
		},
	}
}

func normalizeStoragePoolsRequest(items []config.StoragePool) ([]config.StoragePool, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("at least one mounted storage disk configuration must be retained")
	}
	result := make([]config.StoragePool, 0, len(items))
	seen := map[string]bool{}
	defaultSeen := map[string]bool{}
	disks := detectStorageDisks()
	for _, item := range items {
		item.ID = strings.TrimSpace(item.ID)
		item.Name = strings.TrimSpace(item.Name)
		item.Path = filepath.Clean(strings.TrimSpace(item.Path))
		item.MountPoint = filepath.Clean(strings.TrimSpace(item.MountPoint))
		if item.MountPoint == "." {
			item.MountPoint = ""
		}
		if item.Name == "" {
			return nil, fmt.Errorf("storage pool name is required")
		}
		if item.ID == "" {
			item.ID = storageID(item.Name)
		}
		if seen[item.ID] {
			return nil, fmt.Errorf("duplicate storage pool ID: %s", item.ID)
		}
		seen[item.ID] = true
		if !filepath.IsAbs(item.Path) {
			return nil, fmt.Errorf("%s path must be absolute", item.Name)
		}
		detectedMountPoint := bestMountPointForPath(item.Path, disks)
		if detectedMountPoint == "" {
			return nil, fmt.Errorf("%s path is not on an available mounted storage disk", item.Name)
		}
		if item.MountPoint != "" && filepath.Clean(item.MountPoint) != filepath.Clean(detectedMountPoint) {
			return nil, fmt.Errorf("%s storage disk mount point has changed; refresh and try again", item.Name)
		}
		item.MountPoint = detectedMountPoint
		item.ContentTypes = normalizeStorageContentTypes(item.ContentTypes)
		item.DefaultContents = normalizeStorageContentTypes(item.DefaultContents)
		allowed := map[string]bool{}
		for _, content := range item.ContentTypes {
			allowed[content] = true
		}
		defaults := make([]string, 0, len(item.DefaultContents))
		for _, content := range item.DefaultContents {
			if !allowed[content] {
				continue
			}
			if defaultSeen[content] {
				return nil, fmt.Errorf("only one default storage disk is allowed for %s", content)
			}
			defaultSeen[content] = true
			defaults = append(defaults, content)
		}
		item.DefaultContents = defaults
		result = append(result, item)
	}
	return result, nil
}

func normalizeStorageContentTypes(values []string) []string {
	valid := map[string]bool{
		config.StorageContentLXC:       true,
		config.StorageContentKVM:       true,
		config.StorageContentImages:    true,
		config.StorageContentSnapshots: true,
		config.StorageContentBackups:   true,
	}
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		next := strings.ToLower(strings.TrimSpace(value))
		if !valid[next] || seen[next] {
			continue
		}
		seen[next] = true
		result = append(result, next)
	}
	return result
}

func storageID(name string) string {
	id := strings.ToLower(strings.TrimSpace(name))
	id = strings.NewReplacer(" ", "-", "_", "-", ".", "-", "/", "-").Replace(id)
	id = strings.Trim(id, "-")
	if id == "" {
		return "storage"
	}
	return id
}

func detectStorageDisks() []storageDiskInfo {
	type lsblkDevice struct {
		Name       string        `json:"name"`
		Path       string        `json:"path"`
		Type       string        `json:"type"`
		FSType     string        `json:"fstype"`
		MountPoint string        `json:"mountpoint"`
		Model      string        `json:"model"`
		Size       int64         `json:"size"`
		ReadOnly   bool          `json:"ro"`
		Children   []lsblkDevice `json:"children"`
	}
	var payload struct {
		BlockDevices []lsblkDevice `json:"blockdevices"`
	}
	out, err := exec.Command("lsblk", "-J", "-b", "-o", "NAME,PATH,SIZE,TYPE,FSTYPE,MOUNTPOINT,MODEL,RO").Output()
	if err != nil {
		return nil
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return nil
	}
	result := []storageDiskInfo{}
	var walk func(lsblkDevice)
	walk = func(dev lsblkDevice) {
		info := storageDiskInfo{
			Name:       dev.Name,
			Path:       dev.Path,
			Type:       dev.Type,
			FSType:     dev.FSType,
			MountPoint: dev.MountPoint,
			Model:      strings.TrimSpace(dev.Model),
			SizeBytes:  dev.Size,
		}
		if isUsableStorageMount(dev.Type, dev.FSType, dev.Path, dev.MountPoint, dev.ReadOnly) && !mountIsReadOnly(dev.MountPoint) {
			info.SizeBytes, info.UsedBytes, info.FreeBytes = dfPath(dev.MountPoint)
			result = append(result, info)
		}
		for _, child := range dev.Children {
			walk(child)
		}
	}
	for _, dev := range payload.BlockDevices {
		walk(dev)
	}
	return result
}

func isUsableStorageMount(deviceType, fsType, devicePath, mountPoint string, readOnly bool) bool {
	if readOnly || strings.TrimSpace(mountPoint) == "" || !strings.HasPrefix(mountPoint, "/") {
		return false
	}

	deviceType = strings.ToLower(strings.TrimSpace(deviceType))
	devicePath = strings.ToLower(strings.TrimSpace(devicePath))
	if deviceType == "loop" || deviceType == "rom" || deviceType == "zram" || strings.HasPrefix(devicePath, "/dev/loop") {
		return false
	}

	fsType = strings.ToLower(strings.TrimSpace(fsType))
	unsupportedFileSystems := map[string]bool{
		"":           true,
		"squashfs":   true,
		"iso9660":    true,
		"udf":        true,
		"swap":       true,
		"tmpfs":      true,
		"devtmpfs":   true,
		"overlay":    true,
		"proc":       true,
		"sysfs":      true,
		"cgroup":     true,
		"cgroup2":    true,
		"efivarfs":   true,
		"securityfs": true,
	}
	if unsupportedFileSystems[fsType] {
		return false
	}

	mountPoint = pathpkg.Clean(mountPoint)
	for _, reserved := range []string{"/snap", "/boot"} {
		if mountPoint == reserved || strings.HasPrefix(mountPoint, reserved+"/") {
			return false
		}
	}
	return true
}

func mountIsReadOnly(mountPoint string) bool {
	out, err := exec.Command("findmnt", "-n", "-o", "OPTIONS", "--target", mountPoint).Output()
	if err != nil {
		return false
	}
	for _, option := range strings.Split(strings.TrimSpace(string(out)), ",") {
		if strings.TrimSpace(option) == "ro" {
			return true
		}
	}
	return false
}

func contentUsageForPool(poolPath string) ([]storageContentUsage, int64) {
	mapping := map[string]string{
		config.StorageContentLXC:       "lxc",
		config.StorageContentKVM:       "kvm",
		config.StorageContentImages:    "images",
		config.StorageContentSnapshots: "snapshots",
		config.StorageContentBackups:   "backups",
	}
	result := make([]storageContentUsage, 0, len(mapping))
	var total int64
	for _, content := range []string{
		config.StorageContentLXC,
		config.StorageContentKVM,
		config.StorageContentImages,
		config.StorageContentSnapshots,
		config.StorageContentBackups,
	} {
		size := dirSizeBytes(filepath.Join(poolPath, mapping[content]))
		result = append(result, storageContentUsage{ContentType: content, SizeBytes: size})
		total += size
	}
	return result, total
}

func mergeContentUsage(current []storageContentUsage, next []storageContentUsage) []storageContentUsage {
	sizes := map[string]int64{}
	order := []string{}
	for _, item := range append(current, next...) {
		if _, ok := sizes[item.ContentType]; !ok {
			order = append(order, item.ContentType)
		}
		sizes[item.ContentType] += item.SizeBytes
	}
	result := make([]storageContentUsage, 0, len(order))
	for _, content := range order {
		result = append(result, storageContentUsage{ContentType: content, SizeBytes: sizes[content]})
	}
	return result
}

func dirSizeBytes(path string) int64 {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	// Count allocated blocks on this filesystem only. LXC rootfs directories can
	// contain active mounts such as proc/sys; traversing them is slow and reports
	// enormous virtual sizes that are not actually occupied by CLICD data.
	out, err := exec.Command("du", "-skx", path).Output()
	if err == nil {
		fields := strings.Fields(string(out))
		if len(fields) > 0 {
			var sizeKB int64
			if _, scanErr := fmt.Sscanf(fields[0], "%d", &sizeKB); scanErr == nil && sizeKB <= (1<<63-1)/1024 {
				return sizeKB * 1024
			}
		}
	}
	var size int64
	_ = filepath.WalkDir(path, func(_ string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, statErr := d.Info(); statErr == nil {
			size += info.Size()
		}
		return nil
	})
	return size
}

func dfPath(path string) (size int64, used int64, free int64) {
	out, err := exec.Command("df", "-B1", "-P", path).Output()
	if err != nil {
		return 0, 0, 0
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return 0, 0, 0
	}
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) < 6 {
		return 0, 0, 0
	}
	fmt.Sscanf(fields[1], "%d", &size)
	fmt.Sscanf(fields[2], "%d", &used)
	fmt.Sscanf(fields[3], "%d", &free)
	return size, used, free
}

func bestMountPointForPath(path string, disks []storageDiskInfo) string {
	path = strings.ReplaceAll(path, "\\", "/")
	path = pathpkg.Clean(path)
	best := ""
	for _, disk := range disks {
		mp := pathpkg.Clean(strings.ReplaceAll(disk.MountPoint, "\\", "/"))
		if disk.MountPoint == "" || mp == "." {
			continue
		}
		matches := path == mp
		if mp == "/" {
			matches = pathpkg.IsAbs(path)
		} else if strings.HasPrefix(path, mp+"/") {
			matches = true
		}
		if matches {
			if len(mp) > len(best) {
				best = mp
			}
		}
	}
	return best
}
