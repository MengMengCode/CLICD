package config

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestManagedDiskOwnershipAndSizeValidation(t *testing.T) {
	old := AppConfig
	t.Cleanup(func() { AppConfig = old })
	pool := t.TempDir()
	AppConfig = &ClicdConfig{StoragePools: []StoragePool{{ID: "p", Path: pool, Enabled: true}}}
	c := &Container{UUID: "owner-one", Virtualization: VirtualizationKVM}
	d := DataDisk{ID: "disk-one", StoragePoolID: "p"}
	path, err := DataDiskPath(c, d)
	if err != nil {
		t.Fatal(err)
	}
	d.Path = path
	if _, err = DataDiskPath(&Container{UUID: "owner-two", Virtualization: VirtualizationKVM}, d); err == nil {
		t.Fatal("accepted another workload's disk")
	}
	d.ID = "../outside"
	if _, err = DataDiskPath(c, d); err == nil {
		t.Fatal("accepted traversal")
	}
	for _, size := range []float64{0, -1, math.NaN(), math.Inf(1), 16385} {
		if _, err := DiskSizeBytes(size); err == nil {
			t.Fatalf("accepted size %v", size)
		}
	}
	if got, err := DiskSizeBytes(.125); err != nil || got != 128*1024*1024 {
		t.Fatalf("size=%d err=%v", got, err)
	}
	target := filepath.Join(pool, "data-disks")
	if err := os.Symlink(t.TempDir(), target); err == nil {
		d.ID = "disk-one"
		d.Path = ""
		if _, err := DataDiskPath(c, d); err == nil {
			t.Fatal("accepted symlink-owned disk")
		}
	}
}
func TestGuestMountPathValidation(t *testing.T) {
	for _, path := range []string{"/", "/etc/config", "/proc/x", "/mnt/../etc", "/mnt//a", "relative", "/data/a b", "/srv/a\nvalue", "/mnt/a\\b"} {
		if ValidateGuestMountPath(path) == nil {
			t.Fatalf("accepted unsafe mount %q", path)
		}
	}
	for _, path := range []string{"/data", "/mnt/database", "/srv/app/data"} {
		if err := ValidateGuestMountPath(path); err != nil {
			t.Fatal(err)
		}
	}
}
