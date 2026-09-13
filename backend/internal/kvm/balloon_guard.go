package kvm

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"clicd/internal/config"
)

// BalloonGuard tracks dynamically adjusted VMs when host memory is under pressure.
var (
	balloonGuardMu     sync.Mutex
	reclaimedVMOriginal = make(map[string]int) // vmName -> originalRAMMB
)

// StartBalloonGuard starts a background supervisor (running every 20 seconds)
// that monitors host physical memory pressure.
//
//   - Normal mode (Host RAM < 85%): VMs enjoy their full allocated memory
//     without aggressive ballooning or page-stealing, keeping guest CPU usage
//     at pure baseline (0%~1%).
//   - Emergency mode (Host RAM >= 85%): Automatically reclaims safe unused
//     memory from running VMs that report idle RAM via VirtIO Balloon,
//     preventing host OOM and swapping.
//   - Recovery mode (Host RAM <= 75%): Once host pressure clears, all VMs
//     are gracefully restored back to their full allocated memory.
func (m *Manager) StartBalloonGuard() {
	go func() {
		// Run once shortly after startup
		time.Sleep(5 * time.Second)
		m.checkAndReconcileBalloonMemory()

		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			m.checkAndReconcileBalloonMemory()
		}
	}()
}

func (m *Manager) checkAndReconcileBalloonMemory() {
	totalMB, usedMB, ok := readHostMemoryMB()
	if !ok || totalMB <= 0 {
		return
	}

	usedPct := float64(usedMB) / float64(totalMB) * 100.0
	threshold := 85
	recoveryThreshold := 75
	if config.AppConfig != nil && config.AppConfig.AutoBalloonThresholdPct > 0 {
		threshold = config.AppConfig.AutoBalloonThresholdPct
		recoveryThreshold = threshold - 10
		if recoveryThreshold < 50 {
			recoveryThreshold = 50
		}
	}

	balloonGuardMu.Lock()
	defer balloonGuardMu.Unlock()

	if usedPct >= float64(threshold) {
		// Pressure detected: reclaim safe unused memory from KVM VMs
		m.reclaimUnusedMemoryUnderPressure(usedPct, threshold)
	} else if usedPct <= float64(recoveryThreshold) && len(reclaimedVMOriginal) > 0 {
		// Memory recovered: restore VMs back to their configured maximum
		m.restoreReclaimedMemoryToNormal(usedPct)
	}
}

func (m *Manager) reclaimUnusedMemoryUnderPressure(usedPct float64, threshold int) {
	containers := append([]config.Container(nil), config.AppConfig.Containers...)
	for _, c := range containers {
		if !c.IsKVM() || c.Status != "running" {
			continue
		}
		name := c.VirshName()
		stats := m.getVMMemoryStats(name)
		if len(stats) == 0 {
			continue
		}

		unusedKiB, hasUnused := stats["unused"]
		actualKiB, hasActual := stats["actual"]
		if !hasUnused || !hasActual || unusedKiB <= 512*1024 {
			// Skip VMs with no balloon driver or with less than 512MB free RAM
			continue
		}

		// Calculate safe target: leave at least 512MB buffer inside the guest
		// so guest applications do not face immediate memory starvation.
		safeReclaimKiB := unusedKiB - 512*1024
		targetKiB := actualKiB - safeReclaimKiB
		minSafeKiB := int64(c.RAMMB) * 1024 / 2 // Do not shrink below 50% of configured size
		if targetKiB < minSafeKiB {
			targetKiB = minSafeKiB
		}

		if targetKiB >= actualKiB-64*1024 {
			// Less than 64MB reclaimable; not worth ballooning
			continue
		}

		// Apply target via virsh setmem
		cmd := exec.Command("virsh", "setmem", name, strconv.FormatInt(targetKiB, 10), "--live")
		if err := cmd.Run(); err == nil {
			if _, exists := reclaimedVMOriginal[name]; !exists {
				reclaimedVMOriginal[name] = c.RAMMB
			}
			reclaimedMB := (actualKiB - targetKiB) / 1024
			fmt.Printf("[Memory Guard] Host memory pressure at %.1f%% (>=%d%%). Dynamically reclaimed %d MB unused RAM from %s (target: %d MB).\n",
				usedPct, threshold, reclaimedMB, name, targetKiB/1024)
		}
	}
}

func (m *Manager) restoreReclaimedMemoryToNormal(usedPct float64) {
	for name, originalMB := range reclaimedVMOriginal {
		targetKiB := int64(originalMB) * 1024
		cmd := exec.Command("virsh", "setmem", name, strconv.FormatInt(targetKiB, 10), "--live")
		if err := cmd.Run(); err == nil {
			fmt.Printf("[Memory Guard] Host memory recovered to %.1f%%. Restored full %d MB RAM allocation to %s.\n",
				usedPct, originalMB, name)
			delete(reclaimedVMOriginal, name)
		}
	}
}

func (m *Manager) getVMMemoryStats(name string) map[string]int64 {
	out, err := exec.Command("virsh", "dommemstat", name).Output()
	if err != nil {
		return nil
	}
	stats := make(map[string]int64)
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 {
			if val, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
				stats[fields[0]] = val
			}
		}
	}
	return stats
}

// readHostMemoryMB parses /proc/meminfo to get accurate physical memory usage
func readHostMemoryMB() (totalMB int64, usedMB int64, ok bool) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()

	var total, available, free int64
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		val, _ := strconv.ParseInt(fields[1], 10, 64)
		switch fields[0] {
		case "MemTotal:":
			total = val / 1024
		case "MemAvailable:":
			available = val / 1024
		case "MemFree:":
			free = val / 1024
		}
	}
	if total <= 0 {
		return 0, 0, false
	}
	used := total - available
	if available == 0 {
		used = total - free
	}
	return total, used, true
}
