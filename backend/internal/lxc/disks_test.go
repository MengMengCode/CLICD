package lxc

import (
	"clicd/internal/config"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDataMountRefusesGuestSymlinkAndNonEmptyDirectory(t *testing.T) {
	root := t.TempDir()
	if err := safeDataGuestDirectory(root, "/mnt/data"); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "mnt", "data", "existing")
	_ = os.WriteFile(file, []byte("keep"), 0600)
	if safeDataGuestDirectory(root, "/mnt/data") == nil {
		t.Fatal("hid existing guest data")
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "srv")); err == nil {
		if safeDataGuestDirectory(root, "/srv/escape") == nil {
			t.Fatal("followed guest symlink")
		}
	}
}
func TestLXCStrictDiskStopAndImageShrinkGuard(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake runtime uses POSIX commands")
	}
	commands := t.TempDir()
	_ = os.WriteFile(filepath.Join(commands, "lxc-info"), []byte("#!/bin/sh\necho FROZEN\n"), 0755)
	t.Setenv("PATH", commands+string(os.PathListSeparator)+os.Getenv("PATH"))
	m := NewManager()
	if m.requireDiskStopped(&config.Container{ID: 1, Name: "guest"}) == nil {
		t.Fatal("frozen guest was treated as stopped")
	}
	image := filepath.Join(t.TempDir(), "disk.img")
	f, _ := os.OpenFile(image, os.O_CREATE|os.O_RDWR, 0600)
	_ = f.Truncate(256 * 1024 * 1024)
	_, _ = f.WriteAt([]byte{0x53, 0xef}, 1080)
	_ = f.Close()
	if err := growExt4Image(context.Background(), image, 128*1024*1024); err == nil || !strings.Contains(err.Error(), "shrinking") {
		t.Fatalf("shrink: %v", err)
	}
	info, _ := os.Stat(image)
	if info.Size() != 256*1024*1024 {
		t.Fatal("shrink touched image")
	}
}
func TestDataDiskConfigOnlyChangesManagedMountEntries(t *testing.T) {
	old := config.AppConfig
	t.Cleanup(func() { config.AppConfig = old })
	pool := t.TempDir()
	config.AppConfig = &config.ClicdConfig{StoragePools: []config.StoragePool{{ID: "p", Path: pool, Enabled: true}}}
	m := &Manager{LxcPath: t.TempDir()}
	c := &config.Container{ID: 1, UUID: "test-owner", LXCName: "guest", Virtualization: config.VirtualizationLXC}
	dir := filepath.Join(m.LxcPath, "guest")
	_ = os.MkdirAll(dir, 0755)
	file := filepath.Join(dir, "config")
	original := "lxc.net.0.type = veth\nlxc.mount.entry = /host/existing srv/existing none bind 0 0\n"
	_ = os.WriteFile(file, []byte(original), 0644)
	d := config.DataDisk{ID: "disk-data", StoragePoolID: "p", MountPath: "/mnt/data", Attached: true}
	c.DataDisks = []config.DataDisk{d}
	if err := m.syncDataDiskConfig(c); err != nil {
		t.Fatal(err)
	}
	text, _ := os.ReadFile(file)
	if !strings.Contains(string(text), original) || !strings.Contains(string(text), "mnt/data none bind,create=dir") {
		t.Fatal(string(text))
	}
	c.DataDisks[0].Attached = false
	if err := m.syncDataDiskConfig(c); err != nil {
		t.Fatal(err)
	}
	text, _ = os.ReadFile(file)
	if strings.Contains(string(text), "clicd data disk") || !strings.Contains(string(text), "/host/existing") {
		t.Fatal("detach changed unrelated mounts")
	}
}

func TestDataDiskSnapshotSafetyGuards(t *testing.T) {
	old := config.AppConfig
	t.Cleanup(func() { config.AppConfig = old })
	config.AppConfig = &config.ClicdConfig{Containers: []config.Container{{ID: 1, UUID: "disk-owner", DataDisks: []config.DataDisk{{ID: "disk-existing", Attached: false}}}}}
	m := NewManager()
	if _, err := m.CreateSnapshot(1, "admin", false, 0); err == nil || !strings.Contains(err.Error(), "full backup") {
		t.Fatalf("snapshot ignored data disk: %v", err)
	}
	if _, err := m.SetSnapshotSchedule(1, true, 24, "03:00", "admin"); err == nil || !strings.Contains(err.Error(), "full backups") {
		t.Fatalf("scheduled snapshot accepted: %v", err)
	}
	config.AppConfig.DataDir = t.TempDir()
	path := filepath.Join(config.AppConfig.DataDir, "snapshots", "legacy")
	_ = os.MkdirAll(path, 0700)
	config.AppConfig.Snapshots = []config.Snapshot{{ID: "legacy", ContainerID: 1, Path: path}}
	if err := m.RestoreSnapshot("legacy"); err == nil || !strings.Contains(err.Error(), "full backup") {
		t.Fatalf("old snapshot restored over data disk inventory: %v", err)
	}
}
func TestDataImageStillAttachedCannotBeDeleted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake runtime uses POSIX commands")
	}
	commands := t.TempDir()
	_ = os.WriteFile(filepath.Join(commands, "losetup"), []byte("#!/bin/sh\necho '/dev/loop123: [1]:2 (/other/mount/disk.img)'\n"), 0755)
	t.Setenv("PATH", commands+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := requireImageNotAttached(context.Background(), "/owned/disk.img"); err == nil {
		t.Fatal("accepted an attached image")
	}
}

func TestDataDiskDirectoriesAllowUserNamespaceTraversalWithoutExposingImages(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits are required")
	}
	old := config.AppConfig
	t.Cleanup(func() { config.AppConfig = old })
	pool := t.TempDir()
	if err := os.Chmod(pool, 0700); err != nil {
		t.Fatal(err)
	}
	config.AppConfig = &config.ClicdConfig{StoragePools: []config.StoragePool{{ID: "p", Path: pool, Enabled: true}}}
	c := &config.Container{UUID: "disk-owner", Virtualization: config.VirtualizationLXC}
	d := config.DataDisk{ID: "disk-one", StoragePoolID: "p"}
	image, err := prepareDataDiskDirectories(c, d)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(image, []byte("private raw image"), 0600); err != nil {
		t.Fatal(err)
	}
	d.Path = image
	parents := []string{}
	for dir := filepath.Dir(image); dir != pool; dir = filepath.Dir(dir) {
		parents = append(parents, dir)
		if err = os.Chmod(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	// Repair legacy Add and imported backup directories before the next start.
	if _, err = prepareDataDiskDirectories(c, d); err != nil {
		t.Fatal(err)
	}
	for _, dir := range parents {
		info, err := os.Stat(dir)
		if err != nil || info.Mode().Perm() != 0711 {
			t.Fatalf("user namespace cannot traverse %s: %v %v", dir, info, err)
		}
	}
	info, _ := os.Stat(image)
	if info.Mode().Perm() != 0600 {
		t.Fatal("raw disk contents became readable")
	}
	info, _ = os.Stat(pool)
	if info.Mode().Perm() != 0700 {
		t.Fatal("modified storage pool permissions")
	}
	outside := t.TempDir()
	if err = os.Chmod(outside, 0700); err != nil {
		t.Fatal(err)
	}
	d.ID, d.Path = "disk-symlink", ""
	link := filepath.Join(filepath.Dir(filepath.Dir(image)), d.ID)
	if err = os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err = prepareDataDiskDirectories(c, d); err == nil {
		t.Fatal("followed a symlink while repairing traversal permissions")
	}
	info, _ = os.Stat(outside)
	if info.Mode().Perm() != 0700 {
		t.Fatal("changed permissions outside the managed owner tree")
	}
}

func TestDataDiskRejectsExplicitPoolFallbackBeforeCreatingOrResizing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake runtime uses POSIX commands")
	}
	old := config.AppConfig
	t.Cleanup(func() { config.AppConfig = old })
	available, disabled := t.TempDir(), t.TempDir()
	c := config.Container{ID: 1, UUID: "pool-test-owner", LXCName: "ct-fixture", DiskGB: 2}
	config.AppConfig = &config.ClicdConfig{Containers: []config.Container{c}, StoragePools: []config.StoragePool{
		{ID: "available", Path: available, Enabled: true, ContentTypes: []string{"lxc"}},
		{ID: "disabled", Path: disabled, Enabled: false, ContentTypes: []string{"lxc"}},
	}}
	commands := t.TempDir()
	log := filepath.Join(commands, "unsafe.log")
	write := func(name, script string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(commands, name), []byte("#!/bin/sh\n"+script), 0755); err != nil {
			t.Fatal(err)
		}
	}
	write("lxc-info", "echo STOPPED\n")
	write("df", "printf 'Filesystem 1B-blocks Used Available Use%% Mounted\\nfake 107374182400 0 107374182400 0%% /\\n'\n")
	for _, name := range []string{"mkfs.ext4", "e2fsck", "resize2fs", "mount"} {
		write(name, "echo unsafe >> \"$LXC_DISK_TEST_LOG\"; exit 88\n")
	}
	t.Setenv("PATH", commands+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("LXC_DISK_TEST_LOG", log)
	if pool, err := config.SelectStoragePoolForContent("lxc", "missing", 128<<20); err != nil || pool.ID != "available" {
		t.Fatalf("fallback precondition: %v %v", pool, err)
	}
	m := &Manager{LxcPath: t.TempDir()}
	for _, requested := range []string{"missing", "disabled"} {
		if _, err := m.AddDataDisk(context.Background(), c.ID, "data", .125, requested, "/data/qa"); err == nil || !strings.Contains(err.Error(), "requested data disk storage pool") {
			t.Fatalf("explicit %s pool was not rejected: %v", requested, err)
		}
	}
	if len(config.FindContainer(c.ID).DataDisks) != 0 {
		t.Fatal("failed addition mutated disk metadata")
	}
	if _, err := os.Stat(filepath.Join(m.LxcPath, c.LxcName())); !os.IsNotExist(err) {
		t.Fatal("invalid selection touched the guest rootfs")
	}
	d := config.DataDisk{ID: "disk-disabled", StoragePoolID: "disabled", SizeGB: .125}
	d.Path, _ = config.DataDiskPath(&c, d)
	_ = os.MkdirAll(filepath.Dir(d.Path), 0755)
	f, err := os.OpenFile(d.Path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Truncate(128 << 20)
	_, _ = f.WriteAt([]byte{0x53, 0xef}, 1080)
	_ = f.Close()
	config.FindContainer(c.ID).DataDisks = []config.DataDisk{d}
	if err := m.ResizeDisk(context.Background(), c.ID, d.ID, .25); err == nil || !strings.Contains(err.Error(), "disk storage pool") {
		t.Fatalf("resize checked capacity on another pool: %v", err)
	}
	info, _ := os.Stat(d.Path)
	if info.Size() != 128<<20 || config.FindContainer(c.ID).DataDisks[0].SizeGB != .125 {
		t.Fatal("failed resize changed the image or metadata")
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatal("formatted, mounted or resized a disk in an unintended pool")
	}
}
