package kvm

import (
	"clicd/internal/config"
	"context"
	"encoding/xml"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDataDiskXMLIncludesAttachedOwnedDisksOnly(t *testing.T) {
	_, c := manualISOFixture(t)
	c.UUID = "test-owner"
	d := config.DataDisk{ID: "disk-data", Name: "Data", StoragePoolID: "test", Format: "qcow2", Target: "sdc", Bus: "sata", Attached: true}
	var err error
	d.Path, err = config.DataDiskPath(c, d)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(d.Path), 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(d.Path, []byte("QFI\xfb"), 0644); err != nil {
		t.Fatal(err)
	}
	c.DataDisks = []config.DataDisk{d}
	text, err := NewManager().containerDomainXML(c)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Disks []struct {
			Source struct {
				File string `xml:"file,attr"`
			} `xml:"source"`
		} `xml:"devices>disk"`
	}
	if err = xml.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Disks) != 3 || doc.Disks[2].Source.File != d.Path {
		t.Fatalf("data disk missing: %s", text)
	}
	c.DataDisks[0].Attached = false
	text, err = NewManager().containerDomainXML(c)
	if err != nil || strings.Contains(text, d.Path) {
		t.Fatal("detached disk is still in XML")
	}
	c.DataDisks[0].Attached = true
	c.DataDisks = append(c.DataDisks, d)
	if _, err = NewManager().containerDomainXML(c); err == nil {
		t.Fatal("duplicate target accepted")
	}
}
func TestDataDiskCleanupValidatesAllOwnersBeforeDeleting(t *testing.T) {
	_, c := manualISOFixture(t)
	c.UUID = "test-owner"
	good := config.DataDisk{ID: "disk-owned", StoragePoolID: "test"}
	good.Path, _ = config.DataDiskPath(c, good)
	_ = os.MkdirAll(filepath.Dir(good.Path), 0755)
	_ = os.WriteFile(good.Path, []byte("data"), 0600)
	bad := good
	bad.ID = "disk-other"
	bad.Path = filepath.Join(t.TempDir(), "important")
	c.DataDisks = []config.DataDisk{good, bad}
	if NewManager().CleanupDataDisks(c) == nil {
		t.Fatal("unowned path accepted")
	}
	if _, err := os.Stat(good.Path); err != nil {
		t.Fatal("deleted an owned file before validating the complete set")
	}
}
func TestDiskResizeRejectsActualRunningAndShrinking(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake hypervisor uses POSIX commands")
	}
	_, c := manualISOFixture(t)
	c.UUID = "test-owner"
	c.StoragePoolID = "test"
	m := NewManagerForStoragePool("test")
	c.DiskImage = filepath.Join(m.instancesDir(), c.VirshName(), "disk.qcow2")
	_ = os.MkdirAll(filepath.Dir(c.DiskImage), 0755)
	_ = os.WriteFile(c.DiskImage, []byte("QFI\xfb"), 0600)
	config.AppConfig.Containers = []config.Container{*c}
	commands := t.TempDir()
	log := filepath.Join(commands, "resize.log")
	writeKVMTestCommand(t, commands, "virsh", "echo \"$KVM_TEST_STATE\"\n")
	writeKVMTestCommand(t, commands, "qemu-img", "case \"$1\" in info) echo '{\"format\":\"qcow2\",\"virtual-size\":4294967296,\"actual-size\":4096}' ;; resize) echo resized >> \"$KVM_COMMAND_LOG\" ;; esac\n")
	t.Setenv("PATH", commands+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("KVM_COMMAND_LOG", log)
	for _, state := range []string{"running", "paused", "in shutdown"} {
		t.Setenv("KVM_TEST_STATE", state)
		if err := m.ResizeDisk(context.Background(), c.ID, "system", 8); err == nil {
			t.Fatal("resized active VM")
		}
	}
	t.Setenv("KVM_TEST_STATE", "shut off")
	if err := m.ResizeDisk(context.Background(), c.ID, "system", 2); err == nil || !strings.Contains(err.Error(), "shrinking") {
		t.Fatalf("shrink: %v", err)
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatal("unsafe qemu resize executed")
	}
}

func TestDataDiskSnapshotSafetyGuards(t *testing.T) {
	_, c := manualISOFixture(t)
	c.DataDisks = []config.DataDisk{{ID: "disk-existing", Attached: false}}
	config.AppConfig.Containers = []config.Container{*c}
	if _, err := NewManager().CreateSnapshot(c.ID, "admin", false, 0); err == nil || !strings.Contains(err.Error(), "full backup") {
		t.Fatalf("snapshot ignored data disk: %v", err)
	}
	if _, err := NewManager().SetSnapshotSchedule(c.ID, true, 24, "03:00", "admin"); err == nil || !strings.Contains(err.Error(), "full backups") {
		t.Fatalf("scheduled snapshot accepted: %v", err)
	}
	config.AppConfig.DataDir = t.TempDir()
	path := filepath.Join(config.AppConfig.DataDir, "snapshots", "legacy")
	_ = os.MkdirAll(path, 0700)
	config.AppConfig.Snapshots = []config.Snapshot{{ID: "legacy", ContainerID: c.ID, Path: path}}
	if err := NewManager().RestoreSnapshot("legacy"); err == nil || !strings.Contains(err.Error(), "full backup") {
		t.Fatalf("old snapshot restored over data disk inventory: %v", err)
	}
}

func TestDataDiskRejectsExplicitPoolFallbackBeforeCreatingOrResizing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake hypervisor uses POSIX commands")
	}
	_, c := manualISOFixture(t)
	c.UUID = "pool-test-owner"
	disabled := config.AppConfig.StoragePools[0]
	disabled.ID, disabled.Path, disabled.Enabled = "disabled", t.TempDir(), false
	config.AppConfig.StoragePools = append(config.AppConfig.StoragePools, disabled)
	config.AppConfig.Containers = []config.Container{*c}
	commands := t.TempDir()
	log := filepath.Join(commands, "unsafe.log")
	writeKVMTestCommand(t, commands, "virsh", "echo 'shut off'\n")
	writeKVMTestCommand(t, commands, "df", "printf 'Filesystem 1B-blocks Used Available Use%% Mounted\\nfake 107374182400 0 107374182400 0%% /\\n'\n")
	writeKVMTestCommand(t, commands, "qemu-img", "if [ \"$1\" = info ]; then echo '{\"format\":\"qcow2\",\"virtual-size\":134217728,\"actual-size\":4096}'; else echo unsafe >> \"$KVM_COMMAND_LOG\"; exit 88; fi\n")
	t.Setenv("PATH", commands+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("KVM_COMMAND_LOG", log)
	if pool, err := config.SelectStoragePoolForContent("kvm", "missing", 128<<20); err != nil || pool.ID != "test" {
		t.Fatalf("fallback precondition: %v %v", pool, err)
	}
	for _, requested := range []string{"missing", "disabled"} {
		if _, err := NewManager().AddDataDisk(context.Background(), c.ID, "data", .125, requested); err == nil || !strings.Contains(err.Error(), "requested data disk storage pool") {
			t.Fatalf("explicit %s pool was not rejected: %v", requested, err)
		}
	}
	if len(config.FindContainer(c.ID).DataDisks) != 0 {
		t.Fatal("failed addition mutated disk metadata")
	}
	d := config.DataDisk{ID: "disk-disabled", StoragePoolID: "disabled", SizeGB: .125}
	d.Path, _ = config.DataDiskPath(c, d)
	_ = os.MkdirAll(filepath.Dir(d.Path), 0755)
	_ = os.WriteFile(d.Path, []byte("QFI\xfb"), 0600)
	config.FindContainer(c.ID).DataDisks = []config.DataDisk{d}
	if err := NewManager().ResizeDisk(context.Background(), c.ID, d.ID, .25); err == nil || !strings.Contains(err.Error(), "disk storage pool") {
		t.Fatalf("resize checked capacity on another pool: %v", err)
	}
	if config.FindContainer(c.ID).DataDisks[0].SizeGB != .125 {
		t.Fatal("failed resize mutated disk metadata")
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatal("created or resized a disk in an unintended pool")
	}
}
