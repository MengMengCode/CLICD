package api

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"clicd/internal/config"
)

func TestIsUsableStorageMount(t *testing.T) {
	tests := []struct {
		name       string
		deviceType string
		fsType     string
		devicePath string
		mountPoint string
		readOnly   bool
		wantUsable bool
	}{
		{name: "root partition", deviceType: "part", fsType: "ext4", devicePath: "/dev/sda2", mountPoint: "/", wantUsable: true},
		{name: "mounted data disk", deviceType: "disk", fsType: "xfs", devicePath: "/dev/sdb", mountPoint: "/data", wantUsable: true},
		{name: "snap loop", deviceType: "loop", fsType: "squashfs", devicePath: "/dev/loop0", mountPoint: "/snap/core20/2105", readOnly: true},
		{name: "loop without ro flag", deviceType: "loop", fsType: "ext4", devicePath: "/dev/loop7", mountPoint: "/mnt/loop"},
		{name: "read only disk", deviceType: "part", fsType: "ext4", devicePath: "/dev/sdc1", mountPoint: "/archive", readOnly: true},
		{name: "optical image", deviceType: "rom", fsType: "iso9660", devicePath: "/dev/sr0", mountPoint: "/media/cdrom"},
		{name: "efi partition", deviceType: "part", fsType: "vfat", devicePath: "/dev/sda1", mountPoint: "/boot/efi"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isUsableStorageMount(tt.deviceType, tt.fsType, tt.devicePath, tt.mountPoint, tt.readOnly)
			if got != tt.wantUsable {
				t.Fatalf("isUsableStorageMount() = %v, want %v", got, tt.wantUsable)
			}
		})
	}
}

func TestBestMountPointForPath(t *testing.T) {
	disks := []storageDiskInfo{
		{Path: "/dev/sda2", MountPoint: "/"},
		{Path: "/dev/sdb1", MountPoint: "/mnt/clicd-data"},
	}
	tests := []struct {
		path string
		want string
	}{
		{path: "/var/lib/clicd", want: "/"},
		{path: "/mnt/clicd-data/clicd", want: "/mnt/clicd-data"},
		{path: "/mnt/clicd-data", want: "/mnt/clicd-data"},
	}
	for _, tt := range tests {
		if got := bestMountPointForPath(tt.path, disks); got != tt.want {
			t.Fatalf("bestMountPointForPath(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestNormalizeStoragePoolsUsesServerManagedPath(t *testing.T) {
	disks := []storageDiskInfo{
		{Path: "/dev/sda2", MountPoint: "/"},
		{Path: "/dev/sdb1", MountPoint: "/mnt/data"},
	}
	items := []config.StoragePool{{
		ID:              "disk-data",
		Name:            "data",
		Path:            "/mnt/data/clicd",
		MountPoint:      "/mnt/data",
		ContentTypes:    []string{config.StorageContentLXC},
		DefaultContents: []string{config.StorageContentLXC},
		Enabled:         true,
	}}
	pools, err := normalizeStoragePoolsRequestWithDisks(items, disks)
	if err != nil {
		t.Fatal(err)
	}
	wantPath := filepath.Join(filepath.Clean("/mnt/data"), "clicd")
	if len(pools) != 1 || pools[0].ID != "disk-data" || pools[0].Name != "data (/dev/sdb1)" || pools[0].Path != wantPath || pools[0].MountPoint != "/mnt/data" {
		t.Fatalf("unexpected normalized pools: %#v", pools)
	}
}

func TestNormalizeStoragePoolsRejectsUncontrolledPath(t *testing.T) {
	disks := []storageDiskInfo{{Path: "/dev/sdb1", MountPoint: "/mnt/data"}}
	for _, path := range []string{"/etc", "/mnt/data/clicd/../../etc", "/mnt/data/other"} {
		_, err := normalizeStoragePoolsRequestWithDisks([]config.StoragePool{{
			ID:         "disk-data",
			Name:       "data",
			Path:       path,
			MountPoint: "/mnt/data",
			Enabled:    true,
		}}, disks)
		if err == nil {
			t.Fatalf("path %q was accepted", path)
		}
	}
}

func TestMountedZFSDatasetsCanBeConfiguredAsStorage(t *testing.T) {
	disks := appendZFSStorageMounts([]storageDiskInfo{{Path: "/dev/sda2", FSType: "ext4", MountPoint: "/"}}, []byte(`{
		"filesystems": [
			{"source":"tank/root","fstype":"zfs","target":"/","options":"rw","fsroot":"/"},
			{"source":"tank/volume","fstype":"zfs","target":"/vol1","options":"rw,relatime","fsroot":"/"},
			{"source":"tank/volume/data","fstype":"zfs","target":"/vol1/data","options":"rw,relatime","fsroot":"/"},
			{"source":"other/data","fstype":"zfs","target":"/vol2/data","options":"rw,relatime","fsroot":"/"},
			{"source":"tank/with-space","fstype":"zfs","target":"/mnt/with space","options":"rw","fsroot":"/"},
			{"source":"tank/volume/data","fstype":"zfs","target":"/vol1/data","options":"rw","fsroot":"/"},
			{"source":"tank/read-only","fstype":"zfs","target":"/vol1/archive","options":"ro,relatime","fsroot":"/"},
			{"source":"tank/bind[/some-dir]","fstype":"zfs","target":"/vol1/bind","options":"rw","fsroot":"/some-dir"},
			{"source":"tank/boot","fstype":"zfs","target":"/boot/zfs","options":"rw","fsroot":"/"},
			{"source":"tank/not-mounted","fstype":"zfs","target":"","options":"rw","fsroot":"/"},
			{"source":"/dev/sdc1","fstype":"zfs_member","target":"/vol3","options":"rw","fsroot":"/"}
		]
	}`))
	want := []string{"/", "/vol1", "/vol1/data", "/vol2/data", "/mnt/with space"}
	if len(disks) != len(want) {
		t.Fatalf("unexpected storage candidates: %+v", disks)
	}
	var items []config.StoragePool
	for i, disk := range disks {
		if disk.MountPoint != want[i] {
			t.Fatalf("candidate %d mount = %q, want %q", i, disk.MountPoint, want[i])
		}
		items = append(items, config.StoragePool{MountPoint: disk.MountPoint, Enabled: true})
	}
	pools, err := normalizeStoragePoolsRequestWithDisks(items, disks)
	if err != nil {
		t.Fatal(err)
	}
	if pools[0].ID != "disk-root" || pools[2].ID == pools[3].ID || pools[2].Path != managedStoragePath("/vol1/data") {
		t.Fatalf("unexpected storage identities: %+v", pools)
	}
	if _, err := normalizeStoragePoolsRequestWithDisks([]config.StoragePool{{MountPoint: "/vol1/archive", Enabled: true}}, disks); err == nil {
		t.Fatal("read-only mount was accepted")
	}
}

func TestRootOnZFSRetainsSystemStorageIdentity(t *testing.T) {
	disks := appendZFSStorageMounts(nil, []byte(`{"filesystems":[
		{"source":"rpool/ROOT/system","fstype":"zfs","target":"/","options":"rw,relatime","fsroot":"/"}
	]}`))
	pools, err := normalizeStoragePoolsRequestWithDisks([]config.StoragePool{{MountPoint: "/", Enabled: true}}, disks)
	if err != nil {
		t.Fatal(err)
	}
	if pools[0].ID != "disk-root" || pools[0].Path != managedStoragePath("/") {
		t.Fatalf("unexpected root pool: %+v", pools[0])
	}
}

func TestDirSizeBytesUsesAllocatedBlocks(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("allocated-block behavior is provided by the Linux du command")
	}
	dir := t.TempDir()
	file, err := os.Create(filepath.Join(dir, "sparse.img"))
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(1 << 30); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	if got := dirSizeBytes(dir); got >= 128<<20 {
		t.Fatalf("dirSizeBytes() = %d, expected allocated size instead of 1 GiB apparent size", got)
	}
}
