package kvm

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"clicd/internal/config"
)

func TestStandaloneDomainDoesNotRequireDeletedTemplateOrSeed(t *testing.T) {
	previous := config.AppConfig
	config.AppConfig = &config.ClicdConfig{}
	defer func() { config.AppConfig = previous }()
	for _, profile := range []string{"linux", "manual", "windows"} {
		t.Run(profile, func(t *testing.T) {
			c := config.Container{ID: 77, UUID: "independent-77", KVMName: "vm-77", Virtualization: "kvm", Standalone: true, Template: "standalone-" + profile, DiskImage: "/owned/independent/disk.qcow2", MACAddress: "52:54:00:00:11:22", VCPU: 1, RAMMB: 512, DiskGB: 4}
			xml, err := NewManager().containerDomainXML(&c)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(xml, c.DiskImage) || strings.Contains(xml, "seed.iso") || strings.Contains(xml, "unattend.iso") || strings.Contains(xml, "device='cdrom'") {
				t.Fatal("standalone domain still references provisioning media")
			}
			if err = validateProvisioningTools(c.Template, true); err != nil {
				t.Fatal("standalone still requires original provisioning tools")
			}
		})
	}
}

func TestFlattenedBackupSurvivesRemovalOfQCOWBackingFile(t *testing.T) {
	if _, err := exec.LookPath("qemu-img"); err != nil {
		t.Skip("qemu-img integration runs on Linux validation host")
	}
	ctx := context.Background()
	dir := t.TempDir()
	base := filepath.Join(dir, "base.qcow2")
	source := filepath.Join(dir, "source.qcow2")
	flat := filepath.Join(dir, "independent.qcow2")
	run := func(name string, args ...string) {
		t.Helper()
		if output, err := exec.Command(name, args...).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v %s", name, err, output)
		}
	}
	run("qemu-img", "create", "-f", "qcow2", base, "128G")
	if _, err := exec.LookPath("qemu-io"); err == nil {
		run("qemu-io", "-c", "write -P 0x5a 0 4096", base)
	}
	run("qemu-img", "create", "-f", "qcow2", "-F", "qcow2", "-b", base, source)
	if required, err := measureTransferDisk(ctx, source); err != nil || required >= 1<<30 {
		t.Fatalf("sparse 128GB disk confused with physical capacity: required=%d err=%v", required, err)
	}
	if err := validateTransferDisk(ctx, source); err == nil {
		t.Fatal("portable disk validation accepted external backing dependency")
	}
	if err := flattenTransferDisk(ctx, source, flat); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(base); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	if err := validateTransferDisk(ctx, flat); err != nil {
		t.Fatal("flattened disk depends on deleted source", err)
	}
	if _, err := exec.LookPath("qemu-io"); err == nil {
		run("qemu-io", "-r", "-c", "read -P 0x5a 0 4096", flat)
	}
}

func TestManualDiskTakesExplicitProvisioningMode(t *testing.T) {
	image := Image{ID: "custom-preinstalled", Distro: "windows", Provisioner: "manual-disk"}
	if !image.IsManualDisk() || image.IsManualISO() {
		t.Fatal("manual preinstalled disk metadata lost")
	}
}
