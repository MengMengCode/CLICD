package lxc

import (
	"strings"
	"testing"
)

func TestNestedLXCAllowsDefaultDockerCapabilities(t *testing.T) {
	dockerDefaults := strings.Fields("chown dac_override fowner fsetid kill setgid setuid setpcap net_bind_service net_raw sys_chroot mknod audit_write setfcap")
	dropped := strings.Fields(strings.TrimPrefix(managedCapabilityDropLine(true), "lxc.cap.drop = "))
	for _, capability := range dockerDefaults {
		for _, drop := range dropped {
			if capability == drop {
				t.Errorf("nested LXC drops %s, required by Docker's default OCI capability set", capability)
			}
		}
	}
}

func TestLXCUsesConservativeCapabilityDropPolicy(t *testing.T) {
	ordinary := managedCapabilityDropLine(false)
	want := "lxc.cap.drop = mac_admin mac_override sys_module sys_rawio sys_time sys_boot sys_nice sys_resource sys_ptrace sys_pacct mknod audit_control audit_read"
	if ordinary != want {
		t.Fatalf("ordinary container capability policy changed: %s", ordinary)
	}
	if nested := managedCapabilityDropLine(true); nested != strings.Replace(ordinary, " mknod", "", 1) {
		t.Fatalf("nesting changed capabilities beyond MKNOD: %s", nested)
	}
}
