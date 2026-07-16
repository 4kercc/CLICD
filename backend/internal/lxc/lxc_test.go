package lxc

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRootfsCommandAddsSeparatorForAllowedCommand(t *testing.T) {
	base := t.TempDir()
	rootfs := filepath.Join(base, "ct-1", "rootfs")
	if err := os.MkdirAll(rootfs, 0755); err != nil {
		t.Fatal(err)
	}

	m := &Manager{LxcPath: base}
	cmd, err := m.rootfsCommand(rootfs, "chpasswd")
	if err != nil {
		t.Fatalf("rootfsCommand returned error: %v", err)
	}

	want := []string{"chroot", "--", rootfs, "chpasswd"}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("cmd.Args = %#v, want %#v", cmd.Args, want)
	}
}

func TestRootfsCommandRejectsUnmanagedCommand(t *testing.T) {
	base := t.TempDir()
	rootfs := filepath.Join(base, "ct-1", "rootfs")
	if err := os.MkdirAll(rootfs, 0755); err != nil {
		t.Fatal(err)
	}

	m := &Manager{LxcPath: base}
	if _, err := m.rootfsCommand(rootfs, "true"); err == nil {
		t.Fatal("rootfsCommand allowed unmanaged command")
	}
}

func TestRootfsCommandRejectsLeadingDashContainerName(t *testing.T) {
	base := t.TempDir()
	rootfs := filepath.Join(base, "-ct", "rootfs")
	if err := os.MkdirAll(rootfs, 0755); err != nil {
		t.Fatal(err)
	}

	m := &Manager{LxcPath: base}
	if _, err := m.rootfsCommand(rootfs, "chpasswd"); err == nil {
		t.Fatal("rootfsCommand allowed leading-dash container name")
	}
}

func TestRootfsCommandRejectsUnsafeRootfsPaths(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()
	m := &Manager{LxcPath: base}

	tests := []struct {
		name string
		path string
	}{
		{name: "outside base", path: filepath.Join(outside, "ct-1", "rootfs")},
		{name: "base path", path: base},
		{name: "not rootfs", path: filepath.Join(base, "ct-1", "not-rootfs")},
		{name: "rootfs directly under base", path: filepath.Join(base, "rootfs")},
		{name: "relative rootfs", path: filepath.Join("ct-1", "rootfs")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := m.rootfsCommand(tc.path, "chpasswd"); err == nil {
				t.Fatalf("rootfsCommand(%q) returned nil error", tc.path)
			}
		})
	}
}

func TestSafeRootfsPathRejectsSiblingPrefix(t *testing.T) {
	parent := t.TempDir()
	base := filepath.Join(parent, "lxc")
	siblingRootfs := filepath.Join(parent, "lxc-evil", "ct-1", "rootfs")
	m := &Manager{LxcPath: base}

	if _, err := m.safeRootfsPath(siblingRootfs); err == nil || !strings.Contains(err.Error(), "unsafe rootfs path") {
		t.Fatalf("safeRootfsPath returned %v, want unsafe rootfs path error", err)
	}
}

func TestIsLXCVDenylistSeccompProfile(t *testing.T) {
	tests := []string{`
# base profile
2
denylist
[all]
open_by_handle_at errno 1
`, `
2
blacklist allow
[all]
open_by_handle_at errno 1
`}

	for _, profile := range tests {
		if !isLXCVDenylistSeccompProfile(profile) {
			t.Fatalf("expected v2 denylist profile for\n%s", profile)
		}
	}
	if isLXCVDenylistSeccompProfile("1\nallowlist\n1\n") {
		t.Fatal("did not expect v1 allowlist profile")
	}
}

func TestManagedPrlimitLinesDoNotSetNproc(t *testing.T) {
	for _, line := range managedPrlimitLines() {
		if strings.HasPrefix(strings.TrimSpace(line), "lxc.prlimit.nproc") {
			t.Fatalf("managed prlimit lines must not set nproc: %q", line)
		}
	}
}

func TestAppendMissingSeccompRulesAddsFutexMitigationOnce(t *testing.T) {
	base := "2\ndenylist\n[all]\nopen_by_handle_at errno 1\n"

	once := appendMissingSeccompRules(base, cve202643499FutexSeccompRules)
	twice := appendMissingSeccompRules(once, cve202643499FutexSeccompRules)

	for _, want := range []string{
		"futex errno 1 [1,0x6,SCMP_CMP_MASKED_EQ,0x7f]",
		"futex errno 1 [1,0xb,SCMP_CMP_MASKED_EQ,0x7f]",
		"futex errno 1 [1,0xc,SCMP_CMP_MASKED_EQ,0x7f]",
		"futex errno 1 [1,0xd,SCMP_CMP_MASKED_EQ,0x7f]",
	} {
		if !strings.Contains(once, want) {
			t.Fatalf("missing seccomp rule %q in\n%s", want, once)
		}
		if strings.Count(twice, want) != 1 {
			t.Fatalf("rule %q duplicated in\n%s", want, twice)
		}
	}
}
