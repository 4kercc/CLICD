package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"clicd/internal/config"
	"clicd/internal/lxc"
	"clicd/internal/version"
)

var lxcManager = lxc.NewManager()

// HandleContainers handles container list and creation
func HandleContainers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !requireScope(w, r, "container:read") {
			return
		}
		listContainers(w, r)
	case http.MethodPost:
		if !requireScope(w, r, "container:create") {
			return
		}
		if isAccessRestrictedRequest(r) {
			jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "Container-bound API keys cannot create containers"})
			return
		}
		createContainer(w, r)
	default:
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
	}
}

// HandleContainerListAlias supports legacy integrations that call
// /api/containers/list or /api/v1/containers/list.
func HandleContainerListAlias(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	if !requireScope(w, r, "container:read") {
		return
	}
	listContainers(w, r)
}

// HandleSingleContainer handles individual container operations by ID or name: /api/containers/{id-or-name}/...
func HandleSingleContainer(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/containers/")
	path = strings.TrimPrefix(path, "/api/containers/")
	parts := strings.SplitN(path, "/", 2)
	c := containerByIdentifier(parts[0])
	id := 0
	if c != nil {
		id = c.ID
	}
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	// Snapshot delete/restore operations: allow even if the container was deleted
	isSnapshotDelete := strings.HasPrefix(action, "snapshots/") && r.Method == http.MethodDelete
	isSnapshotRestore := strings.HasPrefix(action, "snapshots/") && strings.HasSuffix(action, "/restore") && r.Method == http.MethodPost
	isSnapshotAction := isSnapshotDelete || isSnapshotRestore
	if !isSnapshotAction && c == nil {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Container not found"})
		return
	}
	if !isSnapshotAction && !isContainerAllowedForRequest(r, parts[0]) {
		jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "Access denied to this container"})
		return
	}
	if isSnapshotAction && id == 0 {
		// For orphaned snapshots, resolve containerID from the snapshot itself
		snapshotID := strings.TrimPrefix(action, "snapshots/")
		snapshotID = strings.TrimSuffix(snapshotID, "/restore")
		snapshot := config.FindSnapshot(snapshotID)
		if snapshot == nil {
			jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Snapshot not found"})
			return
		}
		id = snapshot.ContainerID
	}
	if isSnapshotAction {
		if c := config.FindContainer(id); c != nil && !isContainerAllowedForRequest(r, c.UUID) {
			jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "Access denied to this container"})
			return
		}
	}

	switch {
	case action == "start" && r.Method == http.MethodPost:
		if !requireScope(w, r, "container:power") {
			return
		}
		HandleSingleTaskAction(w, r, id, "start")
	case action == "stop" && r.Method == http.MethodPost:
		if !requireScope(w, r, "container:power") {
			return
		}
		HandleSingleTaskAction(w, r, id, "stop")
	case action == "restart" && r.Method == http.MethodPost:
		if !requireScope(w, r, "container:power") {
			return
		}
		HandleSingleTaskAction(w, r, id, "restart")
	case action == "reinstall" && r.Method == http.MethodPost:
		if !requireScope(w, r, "container:reinstall") {
			return
		}
		HandleSingleTaskAction(w, r, id, "reinstall")
	case action == "delete" && r.Method == http.MethodDelete:
		if !requireScope(w, r, "container:delete") {
			return
		}
		HandleSingleTaskAction(w, r, id, "delete")
	case action == "reset-password" && r.Method == http.MethodPost:
		if !requireScope(w, r, "container:password") {
			return
		}
		resetSSHPassword(w, r, id)
	case action == "usage" && r.Method == http.MethodGet:
		if !requireScope(w, r, "container:read") {
			return
		}
		getUsage(w, r, id)
	case action == "history" && r.Method == http.MethodGet:
		if !requireScope(w, r, "container:read") {
			return
		}
		jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: getContainerMetricHistory(c)})
	case action == "traffic" && r.Method == http.MethodGet:
		if !requireScope(w, r, "container:read") {
			return
		}
		getTraffic(w, r, id)
	case action == "traffic-reset" && r.Method == http.MethodPost:
		if !requireScope(w, r, "container:traffic") {
			return
		}
		resetTraffic(w, r, id)
	case action == "traffic-limit" && r.Method == http.MethodPut:
		if !requireScope(w, r, "container:traffic") {
			return
		}
		updateTrafficLimit(w, r, id)
	case action == "resource-limit" && r.Method == http.MethodPut:
		if !requireScope(w, r, "container:resize") {
			return
		}
		updateResourceLimit(w, r, id)
	case action == "random-port" && r.Method == http.MethodGet:
		if !requireScope(w, r, "container:network") {
			return
		}
		getRandomPort(w, r, id)
		case action == "rename" && r.Method == http.MethodPut:
			if !requireScope(w, r, "container:resize") {
				return
			}
			updateContainerName(w, r, id)
		case action == "template" && r.Method == http.MethodPut:
			if !requireScope(w, r, "container:resize") {
				return
			}
			updateContainerTemplate(w, r, id)
		case action == "expiry" && r.Method == http.MethodPut:
		if !requireScope(w, r, "container:resize") {
			return
		}
		updateExpiry(w, r, id)
	case action == "ipv6" && r.Method == http.MethodPost:
		if !requireScope(w, r, "ipv6:assign") {
			return
		}
		assignIPv6(w, r, id)
	case action == "public-ipv4" && r.Method == http.MethodPut:
		if !requireScope(w, r, "container:network") {
			return
		}
		updatePublicIPv4(w, r, id)
	case action == "ipv6-addresses" && r.Method == http.MethodPut:
		if !requireScope(w, r, "ipv6:assign") {
			return
		}
		updateIPv6Addresses(w, r, id)
		case action == "snapshots" || strings.HasPrefix(action, "snapshots/"):
			handleContainerSnapshots(w, r, id, action)
		case action == "backups" || strings.HasPrefix(action, "backups/"):
			handleContainerBackups(w, r, id, action)
		case action == "resize-disk" && r.Method == http.MethodPost:
			if !requireScope(w, r, "container:resize") {
				return
			}
			handleResizeDisk(w, r, id)
		case action == "import-disk" && r.Method == http.MethodPost:
			if !requireScope(w, r, "container:reinstall") {
				return
			}
			handleImportDisk(w, r, id)
			case action == "hardware-config" && r.Method == http.MethodPut:
				if !requireScope(w, r, "container:resize") {
					return
				}
				handleUpdateHardwareConfig(w, r, id)
			case action == "mount-virtio" && r.Method == http.MethodPost:
				if !requireScope(w, r, "container:reinstall") {
					return
				}
				handleMountVirtioISO(w, r, id)
			case action == "guest-agent" && r.Method == http.MethodGet:
				if !requireScope(w, r, "container:read") {
					return
				}
				handleGuestAgentStatus(w, r, id)
	case action == "port-mappings" && r.Method == http.MethodPost:
		if !requireScope(w, r, "container:network") {
			return
		}
		addPortMapping(w, r, id)
	case strings.HasPrefix(action, "port-mappings/") && r.Method == http.MethodPut:
		if !requireScope(w, r, "container:network") {
			return
		}
		updatePortMapping(w, r, id, strings.TrimPrefix(action, "port-mappings/"))
	case strings.HasPrefix(action, "port-mappings/") && r.Method == http.MethodDelete:
		if !requireScope(w, r, "container:network") {
			return
		}
		deletePortMapping(w, r, id, strings.TrimPrefix(action, "port-mappings/"))
	case action == "firewall" && r.Method == http.MethodGet:
		if !requireScope(w, r, "container:network") {
			return
		}
		getFirewall(w, r, id)
	case action == "firewall" && r.Method == http.MethodPut:
		if !requireScope(w, r, "container:network") {
			return
		}
		updateFirewall(w, r, id)
	case r.Method == http.MethodGet:
		if !requireScope(w, r, "container:read") {
			return
		}
		getContainer(w, r, id)
	default:
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Action not found"})
	}
}

func listContainers(w http.ResponseWriter, r *http.Request) {
	containers, _ := listByRuntime()
	containers = filterContainersForRequest(r, containers)
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: containers})
}

func createContainer(w http.ResponseWriter, r *http.Request) {
	var cfg lxc.ContainerConfig
	body, err := io.ReadAll(r.Body)
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
		return
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &cfg); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
		return
	}
	_ = json.Unmarshal(body, &fields)
	if err := normalizeCreateResourceLimits(&cfg, fields); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	}
	if cfg.Name == "" {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Container name is required"})
		return
	}
	cfg.Virtualization = runtimeFromRequest(cfg.Virtualization)
	if cfg.TemplateID == "" {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Template is required"})
		return
	}
	if !isImageEnabledAndDownloaded(cfg.TemplateID, cfg.Virtualization) {
		jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "Template is not enabled or downloaded"})
		return
	}
	if ids, err := normalizeAllowedImageIDs(cfg.AllowedImageIDs); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	} else {
		cfg.AllowedImageIDs = ids
	}
	if cfg.VCPU <= 0 {
		cfg.VCPU = 1
	}
	if cfg.RAMMB < 128 {
		cfg.RAMMB = 512
	}
	if cfg.DiskGB < 1 {
		cfg.DiskGB = 5
	}
	if cfg.PortMappingCount < 0 {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Port mapping count cannot be negative"})
		return
	}
	if err := cfg.NormalizeCreateNATMappings(); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	}
	if err := lxc.ValidateCreateNATPortAvailability(cfg); err != nil {
		jsonResponse(w, http.StatusConflict, APIResponse{Success: false, Message: err.Error()})
		return
	}
	if cfg.PortMappingCount > 64 {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Port mapping count cannot exceed 64"})
		return
	}
	if cfg.IPv4Count < 0 || cfg.IPv6Count < 0 {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "IP address count cannot be negative"})
		return
	}
	if cfg.IPv4Count > 64 || cfg.IPv6Count > 64 {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "IP address count cannot exceed 64"})
		return
	}
	if !cfg.AssignIPv4 && len(cfg.PublicIPv4s) == 0 {
		cfg.IPv4Count = 0
	}
	if !cfg.AssignIPv6 && len(cfg.IPv6Addresses) == 0 {
		cfg.IPv6Count = 0
	}
	if !hasRequestedNetwork(cfg) {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: noNetworkSelectedMessage})
		return
	}
	if cfg.SnapshotLimit <= 0 {
		cfg.SnapshotLimit = config.DefaultSnapshotLimit
	}
	if err := validateRuntimeResourceRequest(cfg.Virtualization, cfg.VCPU, cfg.RAMMB, cfg.DiskGB); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	}
	if err := validateCreateStoragePool(&cfg); err != nil {
		jsonResponse(w, http.StatusConflict, APIResponse{Success: false, Message: err.Error()})
		return
	}
	if err := validateCreateSSHAuth(cfg); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	}
	if cfg.ExpiresAt != "" {
		expiresAt, ok := lxc.ParseExpiration(cfg.ExpiresAt)
		if !ok {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid expiration date"})
			return
		}
		if !time.Now().Before(expiresAt) {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Expiration date must be in the future"})
			return
		}
	}

	if err := createByRuntime(cfg); err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: err.Error()})
		return
	}
	jsonResponse(w, http.StatusCreated, APIResponse{Success: true, Message: "Container created successfully"})
}

func getContainer(w http.ResponseWriter, r *http.Request, id int) {
	c := config.FindContainer(id)
	if c == nil {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Container not found"})
		return
	}
	if c.IsKVM() && c.Status == "running" {
		_, _ = kvmManager.RefreshVNCPort(c.ID)
		_, _ = kvmManager.RefreshNetwork(c.ID)
	}
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: c})
}

func getUsage(w http.ResponseWriter, r *http.Request, id int) {
	usage, err := usageByRuntime(id)
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: err.Error()})
		return
	}
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: usage})
}

func getTraffic(w http.ResponseWriter, r *http.Request, id int) {
	info := trafficByRuntime(id)
	if info == nil {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Container not found"})
		return
	}
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: info})
}

func updateExpiry(w http.ResponseWriter, r *http.Request, id int) {
	var req struct {
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request"})
		return
	}
	c := config.FindContainer(id)
	if c == nil {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Container not found"})
		return
	}
	c.ExpiresAt = req.ExpiresAt
	config.SaveConfig()
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "Expiry updated"})
}

// validateDisplayName bounds user-editable labels (container name, template tag):
// non-empty, at most 64 runes, no control characters.
func validateDisplayName(value string) error {
	if value == "" {
		return fmt.Errorf("value cannot be empty")
	}
	runes := []rune(value)
	if len(runes) > 64 {
		return fmt.Errorf("value is too long (max 64 characters)")
	}
	for _, r := range runes {
		if unicode.IsControl(r) {
			return fmt.Errorf("value contains invalid control characters")
		}
	}
	return nil
}

func updateContainerName(w http.ResponseWriter, r *http.Request, id int) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request"})
		return
	}
	name := strings.TrimSpace(req.Name)
	if err := validateDisplayName(name); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	}
	c := config.FindContainer(id)
	if c == nil {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Container not found"})
		return
	}
	c.Name = name
	config.SaveConfig()
	user := requestUser(r)
	config.AddAuditLog("container.rename", c.Name, fmt.Sprintf("Renamed to %s", name), user)
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "Name updated successfully", Data: c})
}

func updateContainerTemplate(w http.ResponseWriter, r *http.Request, id int) {
	var req struct {
		Template string `json:"template"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request"})
		return
	}
	template := strings.TrimSpace(req.Template)
	if err := validateDisplayName(template); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	}
	c := config.FindContainer(id)
	if c == nil {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Container not found"})
		return
	}
	c.Template = template
	config.SaveConfig()
	user := requestUser(r)
	config.AddAuditLog("container.update_template", c.Name, fmt.Sprintf("Updated template label to %s", template), user)
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "Template updated successfully", Data: c})
}

func resetTraffic(w http.ResponseWriter, r *http.Request, id int) {
	c := config.FindContainer(id)
	if c == nil {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Container not found"})
		return
	}
	c.TrafficUsedRX = 0
	c.TrafficUsedTX = 0
	c.TrafficResetDate = time.Now().Format("2006-01")
	config.SaveConfig()
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "Traffic reset"})
}

func updateTrafficLimit(w http.ResponseWriter, r *http.Request, id int) {
	var req struct {
		Mode         string `json:"traffic_mode"`
		MonthlyGB    int    `json:"monthly_traffic_gb"`
		TrafficInGB  int    `json:"traffic_in_gb"`
		TrafficOutGB int    `json:"traffic_out_gb"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request"})
		return
	}
	c := config.FindContainer(id)
	if c == nil {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Container not found"})
		return
	}
	c.TrafficMode = req.Mode
	c.MonthlyTrafficGB = req.MonthlyGB
	c.TrafficInGB = req.TrafficInGB
	c.TrafficOutGB = req.TrafficOutGB
	config.SaveConfig()
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "Traffic limit updated"})
}

func updateResourceLimit(w http.ResponseWriter, r *http.Request, id int) {
	var req struct {
		VCPU            *float64 `json:"vcpu"`
		RAMMB           *int     `json:"ram_mb"`
		IOMBps          *int     `json:"io_speed_mbps"`
		IOReadMBps      *int     `json:"io_read_mbps"`
		IOWriteMBps     *int     `json:"io_write_mbps"`
		BWMbps          *int     `json:"network_bw_mbps"`
		NetworkDownMbps *int     `json:"network_down_mbps"`
		NetworkUpMbps   *int     `json:"network_up_mbps"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request"})
		return
	}
	c := config.FindContainer(id)
	if c == nil {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Container not found"})
		return
	}

	// Update config
	nextVCPU := c.VCPU
	nextRAMMB := c.RAMMB
	if req.VCPU != nil {
		nextVCPU = *req.VCPU
	}
	if req.RAMMB != nil {
		nextRAMMB = *req.RAMMB
	}
	if err := validateRuntimeResourceRequest(c.Runtime(), nextVCPU, nextRAMMB, c.DiskGB); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	}
	for name, value := range map[string]*int{
		"network_bw_mbps":   req.BWMbps,
		"network_down_mbps": req.NetworkDownMbps,
		"network_up_mbps":   req.NetworkUpMbps,
		"io_speed_mbps":     req.IOMBps,
		"io_read_mbps":      req.IOReadMBps,
		"io_write_mbps":     req.IOWriteMBps,
	} {
		if err := rejectNegativeLimit(name, value); err != nil {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
			return
		}
	}

	c.VCPU = nextVCPU
	c.RAMMB = nextRAMMB
	applyNetworkLimitPatch(c, req.BWMbps, req.NetworkDownMbps, req.NetworkUpMbps)
	applyIOLimitPatch(c, req.IOMBps, req.IOReadMBps, req.IOWriteMBps)
	config.NormalizeContainerResourceAliases(c)
	config.SaveConfig()

	// Re-apply persisted/runtime limits. LXC also uses this path to migrate
	// old managed config lines such as lxc.prlimit.nproc.
	if err := applyLimitsByRuntime(c); err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: err.Error()})
		return
	}

	msg := "Resource limits updated"
	if c.IsKVM() && c.Status == "running" {
		msg = "资源已保存，请关机重启虚拟机后生效"
	}
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: msg})
}

func normalizeCreateResourceLimits(cfg *lxc.ContainerConfig, fields map[string]json.RawMessage) error {
	if cfg == nil {
		return nil
	}
	if err := rejectNegativeCreateLimits(*cfg); err != nil {
		return err
	}
	bwSet := hasJSONField(fields, "network_bw_mbps")
	downSet := hasJSONField(fields, "network_down_mbps")
	upSet := hasJSONField(fields, "network_up_mbps")
	if bwSet {
		if !downSet {
			cfg.NetworkDownMbps = cfg.NetworkBWMbps
		}
		if !upSet {
			cfg.NetworkUpMbps = cfg.NetworkBWMbps
		}
	}
	ioSet := hasJSONField(fields, "io_speed_mbps")
	readSet := hasJSONField(fields, "io_read_mbps")
	writeSet := hasJSONField(fields, "io_write_mbps")
	if ioSet {
		if !readSet {
			cfg.IOReadMBps = cfg.IOSpeedMBps
		}
		if !writeSet {
			cfg.IOWriteMBps = cfg.IOSpeedMBps
		}
	}
	cfg.NormalizeResourceAliases()
	return nil
}

func rejectNegativeCreateLimits(cfg lxc.ContainerConfig) error {
	for name, value := range map[string]int{
		"network_bw_mbps":   cfg.NetworkBWMbps,
		"network_down_mbps": cfg.NetworkDownMbps,
		"network_up_mbps":   cfg.NetworkUpMbps,
		"io_speed_mbps":     cfg.IOSpeedMBps,
		"io_read_mbps":      cfg.IOReadMBps,
		"io_write_mbps":     cfg.IOWriteMBps,
	} {
		if value < 0 {
			return fmt.Errorf("%s cannot be negative", name)
		}
	}
	return nil
}

func hasJSONField(fields map[string]json.RawMessage, name string) bool {
	if fields == nil {
		return false
	}
	_, ok := fields[name]
	return ok
}

func rejectNegativeLimit(name string, value *int) error {
	if value != nil && *value < 0 {
		return fmt.Errorf("%s cannot be negative", name)
	}
	return nil
}

func applyNetworkLimitPatch(c *config.Container, legacy *int, down *int, up *int) {
	if c == nil {
		return
	}
	config.NormalizeContainerResourceAliases(c)
	nextDown := c.NetworkDownMbps
	nextUp := c.NetworkUpMbps
	if legacy != nil {
		nextDown = *legacy
		nextUp = *legacy
	}
	if down != nil {
		nextDown = *down
	}
	if up != nil {
		nextUp = *up
	}
	c.NetworkDownMbps = nextDown
	c.NetworkUpMbps = nextUp
	c.NetworkBWMbps = config.LegacySymmetricLimit(nextDown, nextUp)
}

func applyIOLimitPatch(c *config.Container, legacy *int, read *int, write *int) {
	if c == nil {
		return
	}
	config.NormalizeContainerResourceAliases(c)
	nextRead := c.IOReadMBps
	nextWrite := c.IOWriteMBps
	if legacy != nil {
		nextRead = *legacy
		nextWrite = *legacy
	}
	if read != nil {
		nextRead = *read
	}
	if write != nil {
		nextWrite = *write
	}
	c.IOReadMBps = nextRead
	c.IOWriteMBps = nextWrite
	c.IOSpeedMBps = config.LegacySymmetricLimit(nextRead, nextWrite)
}

func handleResizeDisk(w http.ResponseWriter, r *http.Request, id int) {
	var req struct {
		DiskGB int `json:"disk_gb"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
		return
	}
	if req.DiskGB <= 0 {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Disk size must be greater than 0"})
		return
	}
	if err := resizeDiskByRuntime(id, req.DiskGB); err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: err.Error()})
		return
	}
	c := config.FindContainer(id)
	user := requestUser(r)
	config.AddAuditLog("container.resize_disk", c.Name, fmt.Sprintf("%d GB", req.DiskGB), user)
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "Disk resized successfully", Data: c})
}

func handleImportDisk(w http.ResponseWriter, r *http.Request, id int) {
	// Security: importing reads an arbitrary host file into a VM disk, which would
	// let a sub-user exfiltrate host files they don't own. Admin only.
	if isSubUserRequest(r) {
		jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "Administrator permission required for disk import"})
		return
	}
	var req struct {
		SourcePath    string `json:"source_path"`
		AsOverlayBase bool   `json:"as_overlay_base"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
		return
	}
	req.SourcePath = strings.TrimSpace(req.SourcePath)
	if req.SourcePath == "" {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "source_path is required"})
		return
	}
	if err := importDiskImageByRuntime(id, req.SourcePath, req.AsOverlayBase); err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: err.Error()})
		return
	}
	c := config.FindContainer(id)
	user := requestUser(r)
	config.AddAuditLog("container.import_disk", c.Name, req.SourcePath, user)
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "Disk imported successfully", Data: c})
}

func handleUpdateHardwareConfig(w http.ResponseWriter, r *http.Request, id int) {
	var req struct {
		BootOrder string `json:"boot_order"`
		BootMedia string `json:"boot_media"`
		Firmware  string `json:"firmware"`
		NICModel  string `json:"nic_model"`
		DiskBus   string `json:"disk_bus"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
		return
	}
	c := config.FindContainer(id)
	if c == nil {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Container not found"})
		return
	}
	if req.BootOrder != "" {
		c.BootOrder = strings.ToLower(strings.TrimSpace(req.BootOrder))
	}
	c.BootMedia = strings.TrimSpace(req.BootMedia)
	if req.Firmware != "" {
		c.Firmware = strings.ToLower(strings.TrimSpace(req.Firmware))
	}
	if req.NICModel != "" {
		c.NICModel = strings.ToLower(strings.TrimSpace(req.NICModel))
	}
	if req.DiskBus != "" {
		c.DiskBus = strings.ToLower(strings.TrimSpace(req.DiskBus))
	}
	if err := config.SaveConfig(); err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: err.Error()})
		return
	}
	_ = applyLimitsByRuntime(c)
	user := requestUser(r)
	config.AddAuditLog("container.hardware_update", c.Name, fmt.Sprintf("boot=%s, nic=%s, bus=%s", c.BootOrder, c.NICModel, c.DiskBus), user)
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "Hardware settings updated", Data: c})
}

func handleMountVirtioISO(w http.ResponseWriter, r *http.Request, id int) {
	var req struct {
		Mount bool `json:"mount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		req.Mount = true
	}
	if err := mountVirtioISOByRuntime(id, req.Mount); err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: err.Error()})
		return
	}
	msg := "VirtIO 驱动与 Guest Agent 光盘已成功挂载到虚拟机光驱 (CD-ROM)"
	if !req.Mount {
		msg = "VirtIO 驱动光盘已成功弹出"
	}
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: msg})
}

func handleGuestAgentStatus(w http.ResponseWriter, r *http.Request, id int) {
	connected, fsInfo, err := getGuestAgentStatusByRuntime(id)
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: err.Error()})
		return
	}
	jsonResponse(w, http.StatusOK, APIResponse{
		Success: true,
		Data: map[string]interface{}{
			"connected": connected,
			"fs_info":   fsInfo,
		},
	})
}

func getRandomPort(w http.ResponseWriter, r *http.Request, id int) {
	c := config.FindContainer(id)
	if c == nil {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Container not found"})
		return
	}
	hostIP := strings.TrimSpace(r.URL.Query().Get("host_ip"))
	start, end := config.NATPortRange()
	capacity := end - start + 1
	offset := 0
	if capacity > 0 {
		offset = int(time.Now().UnixNano() % int64(capacity))
	}
	for tries := 0; tries < capacity; tries++ {
		port := start + ((offset + tries) % capacity)
		if lxc.HostPortAvailable(c, hostIP, port, "tcp") {
			jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: map[string]int{"port": port}})
			return
		}
	}
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: map[string]int{"port": 0}})
}

// HandleTemplates returns available LXC templates
func HandleTemplates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	if !requireScope(w, r, "image:read") {
		return
	}
	if isSubUserRequest(r) {
		HandleEnabledImages(w, r)
		return
	}
	templates := lxc.GetTemplates()
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: templates})
}

// HandleDashboard returns dashboard stats
func HandleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	if !requireScope(w, r, "dashboard:read") {
		return
	}
	containers, _ := listByRuntime()
	containers = filterContainersForRequest(r, containers)
	running := 0
	stopped := 0
	for _, c := range containers {
		if c.Status == "running" {
			running++
		} else {
			stopped++
		}
	}
	stats := map[string]interface{}{
		"total_containers": len(containers),
		"running":          running,
		"stopped":          stopped,
	}
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: stats})
}

// HandleHostInfo returns host machine resource info
func HandleHostInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	if !requireScope(w, r, "host:read") {
		return
	}
	info := getHostInfo()
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: info})
}

// HandleHostHistory returns host resource samples collected by the server.
func HandleHostHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	if !requireScope(w, r, "host:read") {
		return
	}
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: getHostMetricHistory()})
}

func resetSSHPassword(w http.ResponseWriter, r *http.Request, id int) {
	c := config.FindContainer(id)
	if c != nil && lxc.IsExpired(*c) {
		jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "容器已到期，不允许此操作"})
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if r.Body != nil {
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&req); err != nil && err.Error() != "EOF" {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
			return
		}
	}
	password := strings.TrimSpace(req.Password)
	if password != "" {
		if err := validateSSHPassword(password); err != nil {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
			return
		}
	}
	newPassword, err := resetPasswordByRuntime(id, password)
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: err.Error()})
		return
	}
	jsonResponse(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "SSH password reset successfully",
		Data:    map[string]string{"password": newPassword},
	})
}

func validateSSHPassword(password string) error {
	return lxc.ValidateCustomSSHPassword(password)
}

func addPortMapping(w http.ResponseWriter, r *http.Request, id int) {
	var pm config.PortMapping
	if err := json.NewDecoder(r.Body).Decode(&pm); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
		return
	}
	mappings, err := lxcManager.AddPortMapping(id, pm)
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	}
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: mappings})
}

func updatePortMapping(w http.ResponseWriter, r *http.Request, id int, indexStr string) {
	index, err := strconv.Atoi(indexStr)
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid port mapping index"})
		return
	}
	var pm config.PortMapping
	if err := json.NewDecoder(r.Body).Decode(&pm); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
		return
	}
	if isSubUserRequest(r) {
		c := config.FindContainer(id)
		if c == nil {
			jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Container not found"})
			return
		}
		if index < 0 || index >= len(c.PortMappings) {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid port mapping index"})
			return
		}
		if pm.ContainerPort < 1 || pm.ContainerPort > 65535 {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "container port must be 1-65535"})
			return
		}
		existing := c.PortMappings[index]
		pm = config.PortMapping{
			ContainerPort: pm.ContainerPort,
			HostPort:      existing.HostPort,
			Protocol:      existing.Protocol,
			Description:   existing.Description,
		}
	}
	mappings, err := lxcManager.UpdatePortMapping(id, index, pm)
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	}
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: mappings})
}

func deletePortMapping(w http.ResponseWriter, r *http.Request, id int, indexStr string) {
	index, err := strconv.Atoi(indexStr)
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid port mapping index"})
		return
	}
	mappings, err := lxcManager.DeletePortMapping(id, index)
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	}
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: mappings})
}

// HandleVersion returns the current CLICD version.
func HandleVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: map[string]string{
		"version": version.Current(),
	}})
}
