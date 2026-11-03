package kvm

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"clicd/internal/config"
)

func TestTransferRootUUIDHandlesCSVAndRejectsUnsafeOrMissingRoot(t *testing.T) {
	valid := "Name,Type,VFS,Label,Size,Parent,UUID\n/dev/sdb,filesystem,ext4,\"label,with-comma\",42,,data-uuid\n/dev/sda1,filesystem,ext4,,42,,root-uuid\n"
	if uuid, err := transferRootUUID([]byte(valid), "/dev/sda1"); err != nil || uuid != "root-uuid" {
		t.Fatalf("root UUID: %q %v", uuid, err)
	}
	for _, invalid := range []string{strings.ReplaceAll(valid, "root-uuid", "bad uuid"), "Name,UUID\n/dev/sda1\n", "Name,UUID\n/dev/sdb,data-uuid\n"} {
		if _, err := transferRootUUID([]byte(invalid), "/dev/sda1"); err == nil {
			t.Fatal("accepted missing or unsafe root UUID")
		}
	}
}

func resetTransferFixture(t *testing.T, osName string, rootMapped bool) (config.Container, string, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake libguestfs tools use POSIX executables")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is needed for fake libguestfs tools")
	}
	dir := t.TempDir()
	fstab := filepath.Join(dir, "fstab")
	if err := os.WriteFile(fstab, []byte("PARTUUID=original / ext4 defaults 0 1\nUUID=data /data/qa ext4 defaults,nofail 0 2\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KVM_FSTAB", fstab)
	t.Setenv("KVM_EDIT_LOG", filepath.Join(dir, "edit.log"))
	t.Setenv("KVM_CUSTOMIZE_LOG", filepath.Join(dir, "customize.log"))
	t.Setenv("KVM_CUSTOMIZE_STARTED", filepath.Join(dir, "started"))
	t.Setenv("KVM_EXPECT_TEMP_ROOT", "1")
	if rootMapped || osName == "windows" {
		t.Setenv("KVM_EXPECT_TEMP_ROOT", "0")
	}
	mountpoints := "<mountpoint dev='/dev/sdb'>/data/qa</mountpoint>"
	if rootMapped {
		mountpoints += "<mountpoint dev='/dev/sda1'>/</mountpoint>"
	}
	writeKVMTestCommand(t, dir, "virt-inspector", "printf '%s' \"$KVM_INSPECTION\"\n")
	t.Setenv("KVM_INSPECTION", "<operatingsystems><operatingsystem><root>/dev/sda1</root><name>"+osName+"</name><mountpoints>"+mountpoints+"</mountpoints></operatingsystem></operatingsystems>")
	writeKVMTestCommand(t, dir, "virt-filesystems", "printf 'Name,Type,VFS,Label,Size,Parent,UUID\\n/dev/sda1,filesystem,ext4,,42,,root-uuid\\n'\n")
	writeKVMTestCommand(t, dir, "virt-cat", "cat \"$KVM_FSTAB\"\n")
	for name, script := range map[string]string{
		"virt-edit": `import os,sys,re,pathlib
p=pathlib.Path(os.environ['KVM_FSTAB'])
log=pathlib.Path(os.environ['KVM_EDIT_LOG'])
count=len(log.read_text().splitlines()) if log.exists() else 0
with log.open('a') as f:f.write(repr(sys.argv[1:])+'\n')
if count and os.environ.get('KVM_RESTORE_FAIL')=='1':sys.exit(21)
expression=sys.argv[sys.argv.index('-e')+1]
data=bytes.fromhex(re.search(r'pack\("H\*","([0-9a-f]+)"\)',expression).group(1))
p.write_bytes(data+p.read_bytes() if ' . $_ if' in expression else data)
`,
		"virt-customize": `import os,sys,pathlib,time
pathlib.Path(os.environ['KVM_CUSTOMIZE_LOG']).write_text(repr(sys.argv[1:]))
if os.environ['KVM_EXPECT_TEMP_ROOT']=='1':
 assert b'UUID=root-uuid / auto defaults 0 0\n' in pathlib.Path(os.environ['KVM_FSTAB']).read_bytes()
pathlib.Path(os.environ['KVM_CUSTOMIZE_STARTED']).touch()
mode=os.environ.get('KVM_CUSTOMIZE_MODE','pass')
if mode=='wait':time.sleep(30)
if mode=='fail':print('simulated customization failure',file=sys.stderr);sys.exit(37)
`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/usr/bin/env python3\n"+script), 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	c := config.Container{DiskImage: filepath.Join(dir, "system.qcow2"), DataDisks: []config.DataDisk{{Path: filepath.Join(dir, "attached.qcow2"), Attached: true}, {Path: filepath.Join(dir, "detached.qcow2")}}}
	return c, fstab, dir
}

func TestResetTransferGuestRestoresPARTUUIDFstabAfterSuccessFailureAndCancellation(t *testing.T) {
	for _, mode := range []string{"pass", "fail", "wait"} {
		t.Run(mode, func(t *testing.T) {
			c, fstab, dir := resetTransferFixture(t, "linux", false)
			original, _ := os.ReadFile(fstab)
			t.Setenv("KVM_CUSTOMIZE_MODE", mode)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if mode == "wait" {
				go func() {
					for ctx.Err() == nil {
						if _, err := os.Stat(filepath.Join(dir, "started")); err == nil {
							cancel()
							return
						}
						time.Sleep(5 * time.Millisecond)
					}
				}()
			}
			err := resetTransferGuest(ctx, &c, "new-name")
			if (err == nil) != (mode == "pass") {
				t.Fatalf("customization %s: %v", mode, err)
			}
			restored, _ := os.ReadFile(fstab)
			if string(restored) != string(original) {
				t.Fatal("original fstab was not restored exactly")
			}
			edits, _ := os.ReadFile(filepath.Join(dir, "edit.log"))
			if strings.Count(string(edits), "\n") != 2 || !strings.Contains(string(edits), "'/dev/sda1:/'") {
				t.Fatal("repair/restore did not use explicit appliance root mounts")
			}
			command, _ := os.ReadFile(filepath.Join(dir, "customize.log"))
			if !strings.Contains(string(command), "attached.qcow2") || strings.Contains(string(command), "detached.qcow2") {
				t.Fatal("customization attached an incorrect data disk inventory")
			}
		})
	}
}

func TestResetTransferGuestWindowsAndMappedLinuxDoNotEditFstab(t *testing.T) {
	for _, osName := range []string{"linux", "windows"} {
		t.Run(osName, func(t *testing.T) {
			c, _, dir := resetTransferFixture(t, osName, true)
			if err := resetTransferGuest(context.Background(), &c, "new-name"); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(dir, "edit.log")); !os.IsNotExist(err) {
				t.Fatal("edited fstab although an explicit repair was unnecessary")
			}
		})
	}
}

func TestResetTransferGuestCannotSucceedAfterFstabRestorationFailure(t *testing.T) {
	c, _, _ := resetTransferFixture(t, "linux", false)
	t.Setenv("KVM_RESTORE_FAIL", "1")
	if err := resetTransferGuest(context.Background(), &c, "new-name"); err == nil || !strings.Contains(err.Error(), "restore cloned guest fstab") {
		t.Fatalf("failed restoration was reported as success: %v", err)
	}
}
