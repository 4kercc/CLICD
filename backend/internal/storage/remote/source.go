package remote

import (
	"crypto/md5"
	"encoding/hex"
	"net"
	"os"
	"regexp"
	"strings"
)

var pathSegmentSafe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// SourceHostID identifies which CLICD host produced a remote snapshot or
// backup. Multiple CLICD hosts can share one remote storage pool, and bare
// numeric container IDs would collide and say nothing about origin — the
// remote tree therefore starts with this identity.
//
// Resolution order: the CLICD_SOURCE_HOST_ID environment variable, the egress
// public IPv4 address, the hostname, and finally a literal placeholder. The
// UDP dial below never sends a packet; it only asks the kernel which interface
// the default route uses.
func SourceHostID() string {
	if id := strings.TrimSpace(os.Getenv("CLICD_SOURCE_HOST_ID")); id != "" {
		return SanitizePathSegment(id)
	}
	if ip := detectEgressIPv4(); ip != "" {
		return SanitizePathSegment(ip)
	}
	if hostname, err := os.Hostname(); err == nil && strings.TrimSpace(hostname) != "" {
		return SanitizePathSegment(hostname)
	}
	return "unknown-host"
}

func detectEgressIPv4() string {
	conn, err := net.Dial("udp", "8.8.8.8:53")
	if err != nil {
		return ""
	}
	defer conn.Close()
	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || addr.IP == nil || addr.IP.IsLoopback() || addr.IP.To4() == nil {
		return ""
	}
	return addr.IP.String()
}

// SanitizePathSegment makes a value safe as a single remote path segment:
// anything outside letters, digits, dot, underscore and dash collapses into a
// dash, leading dots/dashes are trimmed so ".." cannot sneak in, and a value
// that collapses to nothing (e.g. a purely non-ASCII name) falls back to a
// deterministic unnamed-<hash> segment so distinct inputs never merge.
func SanitizePathSegment(value string) string {
	safe := strings.Trim(pathSegmentSafe.ReplaceAllString(strings.TrimSpace(value), "-"), "-.")
	if safe == "" {
		sum := md5.Sum([]byte(value))
		return "unnamed-" + hex.EncodeToString(sum[:])[:8]
	}
	return safe
}
