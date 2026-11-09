package kvm

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// windowsInstallProfile mirrors what the install media actually contains so the
// unattended answer file fits the ISO instead of a hard-coded guess. Every field
// here has already burned us once:
//
//   - a UI language the image has no MUI resources for makes MUIUNATTEND.EXE
//     fail and msoobe.exe die with a PANTHR exception, which reboots forever;
//   - /IMAGE/INDEX 1 is the "Server Core" edition on every Server ISO, so the
//     "normal" install silently lands on a machine without a desktop;
//   - multi-edition ISOs refuse to install when the answer file carries no
//     <ProductKey> ("无法从无人参与应答文件读取 <ProductKey> 设置").
type windowsInstallProfile struct {
	Index          int
	Language       string
	EditionID      string
	DisplayName    string
	ProductKey     string
	RequiresBypass bool // 11th-generation media (build >= 22000): TPM/SecureBoot/CPU checks
	Source         string
}

// windowsWimImage is one index of the ISO's sources/install.wim, as reported by
// `wimlib-imagex info`.
type windowsWimImage struct {
	Index            int
	Name             string
	DisplayName      string
	EditionID        string
	InstallationType string
	ProductType      string
	Language         string
	Build            int
}

var (
	windowsProfileMu    sync.Mutex
	windowsProfileCache = map[string]windowsInstallProfile{}
)

const windowsProfileCacheLimit = 64

// windowsInstallProfileFor probes the ISO once per (path, size, mtime) and caches
// the result: a batch create of twenty VMs must not mount the media twenty times.
func windowsInstallProfileFor(isoPath string) windowsInstallProfile {
	isoPath = strings.TrimSpace(isoPath)
	if isoPath == "" {
		return defaultWindowsInstallProfile(isoPath)
	}
	cacheKey := isoPath
	if info, err := os.Stat(isoPath); err == nil {
		cacheKey = fmt.Sprintf("%s|%d|%d", isoPath, info.Size(), info.ModTime().Unix())
	}

	windowsProfileMu.Lock()
	if profile, ok := windowsProfileCache[cacheKey]; ok {
		windowsProfileMu.Unlock()
		return profile
	}
	windowsProfileMu.Unlock()

	profile := probeWindowsInstallProfile(isoPath)

	windowsProfileMu.Lock()
	if len(windowsProfileCache) > windowsProfileCacheLimit {
		windowsProfileCache = map[string]windowsInstallProfile{}
	}
	windowsProfileCache[cacheKey] = profile
	windowsProfileMu.Unlock()
	return profile
}

// invalidateWindowsInstallProfile drops the cached probe for an image, used when
// the media is re-downloaded (same path, different content).
func invalidateWindowsInstallProfile(isoPath string) {
	prefix := strings.TrimSpace(isoPath) + "|"
	windowsProfileMu.Lock()
	for key := range windowsProfileCache {
		if key == isoPath || strings.HasPrefix(key, prefix) {
			delete(windowsProfileCache, key)
		}
	}
	windowsProfileMu.Unlock()
}

func defaultWindowsInstallProfile(isoPath string) windowsInstallProfile {
	return windowsInstallProfile{
		Index:    1,
		Language: languageFromFileName(isoPath),
		Source:   "default",
	}
}

// probeWindowsInstallProfile mounts the ISO read-only and asks wimlib what is in
// sources/install.wim. Any failure falls back to the historical behaviour (index
// 1, filename-derived language, no product key) so nothing regresses on exotic
// media — the caller only loses the improvements, never the install.
func probeWindowsInstallProfile(isoPath string) windowsInstallProfile {
	fallback := defaultWindowsInstallProfile(isoPath)

	if _, err := exec.LookPath("wimlib-imagex"); err != nil {
		fmt.Printf("windows install profile: wimlib-imagex not installed, using index 1 / %s for %s\n", fallback.Language, isoPath)
		return fallback
	}

	mountDir, err := os.MkdirTemp("", "clicd-winiso-")
	if err != nil {
		return fallback
	}
	defer os.RemoveAll(mountDir)

	if out, err := exec.Command("mount", "-o", "loop,ro", isoPath, mountDir).CombinedOutput(); err != nil {
		fmt.Printf("windows install profile: mount %s failed: %v (%s)\n", isoPath, err, strings.TrimSpace(string(out)))
		return fallback
	}
	defer exec.Command("umount", mountDir).Run()

	wimPath := ""
	for _, name := range []string{"install.wim", "install.esd"} {
		candidate := filepath.Join(mountDir, "sources", name)
		if _, err := os.Stat(candidate); err == nil {
			wimPath = candidate
			break
		}
	}
	if wimPath == "" {
		fmt.Printf("windows install profile: no sources/install.wim in %s\n", isoPath)
		return fallback
	}

	out, err := exec.Command("wimlib-imagex", "info", wimPath).Output()
	if err != nil {
		fmt.Printf("windows install profile: wimlib info failed on %s: %v\n", isoPath, err)
		return fallback
	}
	images := parseWimlibInfo(string(out))
	if len(images) == 0 {
		return fallback
	}

	chosen := chooseWindowsImage(images)
	profile := windowsInstallProfile{
		Index:          chosen.Index,
		Language:       normalizeWindowsLanguage(chosen.Language, isoPath),
		EditionID:      chosen.EditionID,
		DisplayName:    firstNonEmptyTrimmed(chosen.DisplayName, chosen.Name),
		ProductKey:     windowsProductKey(chosen),
		RequiresBypass: chosen.Build >= 22000,
		Source:         "wimlib",
	}
	fmt.Printf("windows install profile: %s -> index %d (%s), language %s, build %d, product key %s\n",
		filepath.Base(isoPath), profile.Index, profile.DisplayName, profile.Language, chosen.Build, keyOrNone(profile.ProductKey))
	return profile
}

// parseWimlibInfo turns `wimlib-imagex info` output into one record per index.
// Keys are matched on the exact label because "Service Pack Build" also ends in
// "Build" and "Display Name" contains the word "Name".
func parseWimlibInfo(out string) []windowsWimImage {
	images := []windowsWimImage{}
	var current windowsWimImage
	flush := func() {
		if current.Index > 0 {
			images = append(images, current)
		}
		current = windowsWimImage{}
	}
	for _, line := range strings.Split(out, "\n") {
		label, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		label = strings.TrimSpace(label)
		value = strings.TrimSpace(value)
		switch label {
		case "Index":
			flush()
			current.Index, _ = strconv.Atoi(value)
		case "Name":
			current.Name = value
		case "Display Name":
			current.DisplayName = value
		case "Edition ID":
			current.EditionID = value
		case "Installation Type":
			current.InstallationType = value
		case "Product Type":
			current.ProductType = value
		case "Default Language":
			current.Language = value
		case "Build":
			current.Build, _ = strconv.Atoi(value)
		}
	}
	flush()
	return images
}

// chooseWindowsImage picks an edition that actually has a desktop. Server media
// lists ...STANDARDCORE / ...DATACENTERCORE next to the full editions, and client
// media lists Home/Core before Professional, so "first index" is always wrong.
func chooseWindowsImage(images []windowsWimImage) windowsWimImage {
	desktop := make([]windowsWimImage, 0, len(images))
	for _, image := range images {
		if strings.EqualFold(strings.TrimSpace(image.InstallationType), "Server Core") {
			continue
		}
		if strings.Contains(strings.ToLower(image.EditionID), "core") {
			continue
		}
		desktop = append(desktop, image)
	}
	candidates := desktop
	if len(candidates) == 0 {
		candidates = images
	}
	for _, preferred := range []string{"serverstandard", "professional", "enterprise", "education", "home"} {
		for _, image := range candidates {
			if strings.EqualFold(strings.TrimSpace(image.EditionID), preferred) {
				return image
			}
		}
	}
	return candidates[0]
}

// windowsProductKey returns the generic KMS client key for the chosen edition, or
// "" when the edition is unknown. Only used so Windows Setup accepts the answer
// file; it does not activate anything.
func windowsProductKey(image windowsWimImage) string {
	edition := strings.ToLower(strings.TrimSpace(image.EditionID))
	if strings.EqualFold(strings.TrimSpace(image.ProductType), "ServerNT") || strings.Contains(edition, "server") {
		switch {
		case strings.Contains(edition, "datacenter"):
			switch {
			case image.Build >= 26000:
				return "D764K-2NDRG-47T6Q-P8T8W-YP6DF" // Server 2025
			case image.Build >= 20000:
				return "WX4NM-KYWYW-QJJR4-XV3QB-6VM33" // Server 2022
			default:
				return "WMDGN-G9PQG-XVVXX-R3X43-63DFG" // Server 2019
			}
		case strings.Contains(edition, "standard"):
			switch {
			case image.Build >= 26000:
				return "TVRH6-WHNXV-R9WG3-9XRFY-MY832" // Server 2025
			case image.Build >= 20000:
				return "VDYBN-27WPP-V4HQT-9VMD4-VMK7H" // Server 2022
			default:
				return "N69G4-B89J2-4G8F4-WWYCC-J464C" // Server 2019
			}
		}
		return ""
	}
	switch edition {
	case "professional":
		return "VK7JG-NPHTM-C97JM-9MPGT-3V66T"
	case "enterprise":
		return "XGVPP-NMH47-7TTHJ-W3FW7-8HV2C"
	case "education":
		return "NW6C2-QMPVW-D7KKK-3GKT6-VCFB2"
	case "core", "home":
		return "YTMG3-N6DKC-DKB77-7M9GH-8HVX7"
	}
	return ""
}

// windowsLocalePattern matches a BCP-47 style tag such as "zh-cn" or "en-US".
// The trailing character class is spelled out instead of \b because official
// media names put the tag right before an underscore ("zh-cn_windows_..."), and
// "_" is a word character, so \b would never match there.
var windowsLocalePattern = regexp.MustCompile(`(?i)([a-z]{2})-([a-z]{2})(?:[^a-z]|$)`)

// normalizeWindowsLanguage prefers what the image reports and falls back to the
// file name (official media is named like zh-cn_windows_server_2025_...).
func normalizeWindowsLanguage(raw, isoPath string) string {
	if match := windowsLocalePattern.FindStringSubmatch(strings.TrimSpace(raw)); match != nil {
		return strings.ToLower(match[1]) + "-" + strings.ToUpper(match[2])
	}
	return languageFromFileName(isoPath)
}

func languageFromFileName(isoPath string) string {
	base := strings.ToLower(filepath.Base(isoPath))
	if match := windowsLocalePattern.FindStringSubmatch(base); match != nil {
		return strings.ToLower(match[1]) + "-" + strings.ToUpper(match[2])
	}
	switch {
	case strings.HasPrefix(base, "cn_"), strings.Contains(base, "_cn_"), strings.Contains(base, "chinese"):
		return "zh-CN"
	}
	return "en-US"
}

func firstNonEmptyTrimmed(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func keyOrNone(key string) string {
	if strings.TrimSpace(key) == "" {
		return "(none)"
	}
	return key
}
