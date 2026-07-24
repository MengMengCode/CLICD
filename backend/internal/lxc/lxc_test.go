package lxc

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"clicd/internal/config"
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

func TestNormalizeCreateNATMappingsSupportsDifferentHostAndContainerPorts(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig = &config.ClicdConfig{NATPortStart: 20000, NATPortEnd: 65535}

	cfg := ContainerConfig{
		PortMappingCount: 2,
		NATPortMappings: []config.PortMapping{{
			HostPort:      30080,
			ContainerPort: 80,
			Protocol:      "TCP",
		}},
	}
	if err := cfg.NormalizeCreateNATMappings(); err != nil {
		t.Fatal(err)
	}
	if cfg.PortMappingCount != 2 || len(cfg.NATPortMappings) != 1 {
		t.Fatalf("normalized config = %+v", cfg)
	}
	mapping := cfg.NATPortMappings[0]
	if mapping.HostPort != 30080 || mapping.ContainerPort != 80 || mapping.Protocol != "tcp" {
		t.Fatalf("normalized mapping = %+v", mapping)
	}

	container := &config.Container{
		ID: -1,
		PortMappings: []config.PortMapping{{
			HostPort:      22000,
			ContainerPort: 22,
			Protocol:      "tcp",
			Description:   "SSH",
		}},
	}
	mappings, err := SetupCreatePortMappings(container, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(mappings) != 2 || mappings[1].HostPort != 30080 || mappings[1].ContainerPort != 80 {
		t.Fatalf("created mappings = %+v", mappings)
	}
}

func TestNormalizeCreateNATMappingsKeepsLegacyExtraPortsCompatible(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig = &config.ClicdConfig{NATPortStart: 20000, NATPortEnd: 65535}

	cfg := ContainerConfig{ExtraPorts: []int{30080, 30443}}
	if err := cfg.NormalizeCreateNATMappings(); err != nil {
		t.Fatal(err)
	}
	if len(cfg.ExtraPorts) != 0 || len(cfg.NATPortMappings) != 2 {
		t.Fatalf("legacy ports were not converted: %+v", cfg)
	}
	for _, mapping := range cfg.NATPortMappings {
		if mapping.HostPort != mapping.ContainerPort {
			t.Fatalf("legacy mapping changed semantics: %+v", mapping)
		}
	}
}

func TestNormalizeCreateNATMappingsRejectsDuplicateHostPort(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig = &config.ClicdConfig{NATPortStart: 20000, NATPortEnd: 65535}

	cfg := ContainerConfig{NATPortMappings: []config.PortMapping{
		{HostPort: 30080, ContainerPort: 80, Protocol: "tcp"},
		{HostPort: 30080, ContainerPort: 8080, Protocol: "tcp"},
	}}
	if err := cfg.NormalizeCreateNATMappings(); err == nil {
		t.Fatal("duplicate host port was accepted")
	}
}

func TestNormalizeCreateNATMappingsRejectsManagementPortConflict(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig = &config.ClicdConfig{NATPortStart: 20000, NATPortEnd: 65535}

	cfg := ContainerConfig{
		ManagementPort: 30022,
		NATPortMappings: []config.PortMapping{{
			HostPort:      30022,
			ContainerPort: 8080,
			Protocol:      "tcp",
		}},
	}
	if err := cfg.NormalizeCreateNATMappings(); err == nil || !strings.Contains(err.Error(), "management_port") {
		t.Fatalf("management port conflict returned %v", err)
	}
}

func TestReserveCreateNATPortsProtectsConcurrentTasks(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig = &config.ClicdConfig{
		NATPortStart: 20000,
		NATPortEnd:   65535,
		NextSSHPort:  22000,
	}

	createNATReservationMu.Lock()
	createNATReservations = map[uint64][]config.PortMapping{}
	createNATReservationMu.Unlock()
	t.Cleanup(func() {
		createNATReservationMu.Lock()
		createNATReservations = map[uint64][]config.PortMapping{}
		createNATReservationMu.Unlock()
	})

	cfg := ContainerConfig{NATPortMappings: []config.PortMapping{{
		HostPort:      22000,
		ContainerPort: 80,
		Protocol:      "tcp",
	}}}
	if err := cfg.NormalizeCreateNATMappings(); err != nil {
		t.Fatal(err)
	}

	managementPort, release, err := ReserveCreateNATPorts(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if managementPort == 22000 {
		t.Fatal("management port collided with the requested custom host port")
	}
	if _, _, err := ReserveCreateNATPorts(cfg); err == nil {
		t.Fatal("concurrent task reserved an already reserved custom host port")
	}

	release()
	if _, releaseAgain, err := ReserveCreateNATPorts(cfg); err != nil {
		t.Fatalf("released custom host port remained reserved: %v", err)
	} else {
		releaseAgain()
	}

	explicit := ContainerConfig{ManagementPort: 30022}
	if err := explicit.NormalizeCreateNATMappings(); err != nil {
		t.Fatal(err)
	}
	if port, releaseExplicit, err := ReserveCreateNATPorts(explicit); err != nil {
		t.Fatal(err)
	} else {
		defer releaseExplicit()
		if port != explicit.ManagementPort {
			t.Fatalf("reserved management port = %d, want %d", port, explicit.ManagementPort)
		}
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

func TestRootfsHasSSHD(t *testing.T) {
	rootfs := t.TempDir()
	if rootfsHasSSHD(rootfs) {
		t.Fatal("empty rootfs unexpectedly reports sshd")
	}
	sshd := filepath.Join(rootfs, "usr", "sbin", "sshd")
	if err := os.MkdirAll(filepath.Dir(sshd), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sshd, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if !rootfsHasSSHD(rootfs) {
		t.Fatal("executable sshd was not detected")
	}
}

func TestSameFilesystemPathResolvesContainerStorageSymlink(t *testing.T) {
	base := t.TempDir()
	storageContainer := filepath.Join(base, "storage", "ct-1")
	rootfs := filepath.Join(storageContainer, "rootfs")
	if err := os.MkdirAll(rootfs, 0755); err != nil {
		t.Fatal(err)
	}

	lxcPath := filepath.Join(base, "lxc")
	if err := os.MkdirAll(lxcPath, 0755); err != nil {
		t.Fatal(err)
	}
	containerLink := filepath.Join(lxcPath, "ct-1")
	if err := os.Symlink(storageContainer, containerLink); err != nil {
		t.Skipf("directory symlinks are unavailable: %v", err)
	}

	linkedRootfs := filepath.Join(containerLink, "rootfs")
	if !sameFilesystemPath(rootfs, linkedRootfs) {
		t.Fatalf("sameFilesystemPath(%q, %q) = false, want true", rootfs, linkedRootfs)
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
