package transfer

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"clicd/internal/config"
)

func smallManifest() Manifest {
	digest := sha256.Sum256([]byte("disk payload"))
	return Manifest{Version: FormatVersion, Runtime: "kvm", Arch: runtime.GOARCH, BootProfile: "linux", Container: config.Container{Name: "source", Virtualization: "kvm", VCPU: 1, RAMMB: 512, DiskGB: 1}, Files: []File{{Name: "disks/system.qcow2", Role: "system", Format: "qcow2", Size: 12, SHA256: hex.EncodeToString(digest[:])}}}
}

func archiveFor(t *testing.T, m Manifest, headers []*tar.Header, payloads []string) []byte {
	t.Helper()
	var result bytes.Buffer
	compressed := gzip.NewWriter(&result)
	writer := tar.NewWriter(compressed)
	data, _ := json.Marshal(m)
	if err := writer.WriteHeader(&tar.Header{Name: "manifest.json", Mode: 0600, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	writer.Write(data)
	for i, h := range headers {
		if err := writer.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if i < len(payloads) {
			writer.Write([]byte(payloads[i]))
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	return result.Bytes()
}

func TestPortableArchiveValidatesEveryDiskAndRejectsUnsafeEntries(t *testing.T) {
	m := smallManifest()
	ctx := context.Background()
	for _, test := range []struct {
		name   string
		header tar.Header
		body   string
		valid  bool
	}{
		{"regular", tar.Header{Name: m.Files[0].Name, Size: 12, Typeflag: tar.TypeReg}, "disk payload", true},
		{"parent traversal", tar.Header{Name: "../outside", Size: 12, Typeflag: tar.TypeReg}, "disk payload", false},
		{"absolute", tar.Header{Name: "/outside", Size: 12, Typeflag: tar.TypeReg}, "disk payload", false},
		{"windows drive", tar.Header{Name: "C:/outside", Size: 12, Typeflag: tar.TypeReg}, "disk payload", false},
		{"backslash", tar.Header{Name: `disks\system.qcow2`, Size: 12, Typeflag: tar.TypeReg}, "disk payload", false},
		{"symlink", tar.Header{Name: m.Files[0].Name, Typeflag: tar.TypeSymlink, Linkname: "/etc/shadow"}, "", false},
		{"hardlink", tar.Header{Name: m.Files[0].Name, Typeflag: tar.TypeLink, Linkname: "../outside"}, "", false},
		{"device", tar.Header{Name: m.Files[0].Name, Typeflag: tar.TypeChar, Devmajor: 1, Devminor: 3}, "", false},
		{"wrong checksum", tar.Header{Name: m.Files[0].Name, Size: 12, Typeflag: tar.TypeReg}, "bad payload!", false},
		{"wrong length", tar.Header{Name: m.Files[0].Name, Size: 1, Typeflag: tar.TypeReg}, "x", false},
		{"unlisted file", tar.Header{Name: "disks/extra.qcow2", Size: 12, Typeflag: tar.TypeReg}, "disk payload", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := archiveFor(t, m, []*tar.Header{&test.header}, []string{test.body})
			_, err := Extract(ctx, bytes.NewReader(data), t.TempDir())
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v err=%v", test.valid, err)
			}
		})
	}
	t.Run("duplicate", func(t *testing.T) {
		h := &tar.Header{Name: m.Files[0].Name, Size: 12, Typeflag: tar.TypeReg}
		_, err := Extract(ctx, bytes.NewReader(archiveFor(t, m, []*tar.Header{h, h}, []string{"disk payload", "disk payload"})), t.TempDir())
		if err == nil {
			t.Fatal("duplicate accepted")
		}
	})
	t.Run("missing disk", func(t *testing.T) {
		_, err := Extract(ctx, bytes.NewReader(archiveFor(t, m, nil, nil)), t.TempDir())
		if err == nil {
			t.Fatal("missing disk accepted")
		}
	})
	t.Run("gzip corruption", func(t *testing.T) {
		h := &tar.Header{Name: m.Files[0].Name, Size: 12, Typeflag: tar.TypeReg}
		data := archiveFor(t, m, []*tar.Header{h}, []string{"disk payload"})
		data[len(data)-4] ^= 0xff
		_, err := Extract(ctx, bytes.NewReader(data), t.TempDir())
		if err == nil {
			t.Fatal("corrupt compressed archive accepted")
		}
	})
}

func TestManifestRejectsUnsupportedArchitectureVersionAndExpansionBomb(t *testing.T) {
	for _, change := range []func(*Manifest){func(m *Manifest) { m.Version++ }, func(m *Manifest) { m.Arch = "different-arch" }, func(m *Manifest) { m.Files[0].Size = MaxExpandedBytes + 1 }, func(m *Manifest) { m.Files[0].Name = "../disk" }, func(m *Manifest) { m.Container.DataDisks = []config.DataDisk{{ID: "missing", SizeGB: 1}} }, func(m *Manifest) { m.Runtime = "unsupported" }} {
		m := smallManifest()
		change(&m)
		if err := ValidateManifest(&m); err == nil {
			t.Fatal("invalid manifest accepted")
		}
	}
}

func TestSparseKVMVirtualCapacityIsIndependentOfArchiveExpansionLimit(t *testing.T) {
	m := smallManifest()
	m.Container.DiskGB = 128
	if err := ValidateManifest(&m); err != nil {
		t.Fatal("128GB sparse QCOW logical disk rejected", err)
	}
	m.Container.DiskGB = 16385
	if err := ValidateManifest(&m); err == nil {
		t.Fatal("logical disk beyond 16TiB accepted")
	}
	m.Container.DiskGB = 128
	m.Files[0].Size = MaxExpandedBytes + 1
	if err := ValidateManifest(&m); err == nil {
		t.Fatal("physical payload expansion limit disabled")
	}
}

func TestArchiveCannotFollowPreexistingPayloadDirectorySymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privileges vary on Windows")
	}
	root, other := t.TempDir(), t.TempDir()
	if err := os.Symlink(other, filepath.Join(root, "disks")); err != nil {
		t.Fatal(err)
	}
	m := smallManifest()
	h := &tar.Header{Name: m.Files[0].Name, Size: 12, Typeflag: tar.TypeReg}
	if _, err := Extract(context.Background(), bytes.NewReader(archiveFor(t, m, []*tar.Header{h}, []string{"disk payload"})), root); err == nil {
		t.Fatal("payload symlink followed")
	}
	entries, _ := os.ReadDir(other)
	if len(entries) != 0 {
		t.Fatal("wrote outside staging root")
	}
}

func TestArtifactSurvivesReloadAndDoesNotMutateSourceDiskOwnership(t *testing.T) {
	pool := t.TempDir()
	root := filepath.Join(pool, "backups", "instances")
	oldRoot, oldConfig := selectBackupRoot, config.AppConfig
	selectBackupRoot = func(string, int64) (string, error) { return root, nil }
	defer func() { selectBackupRoot = oldRoot; config.AppConfig = oldConfig }()
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	config.AppConfig = &config.ClicdConfig{StoragePools: []config.StoragePool{{ID: "test", Path: pool, ContentTypes: []string{config.StorageContentBackups}}}}
	source := smallManifest().Container
	source.DataDisks = []config.DataDisk{{ID: "source-data", Path: "/source-owned/disk.qcow2", StoragePoolID: "original", SizeGB: 1, Format: "qcow2", Attached: false}}
	info, err := Create(context.Background(), "test", "portable test", "backup", source, func(_ context.Context, dir string, m *Manifest) error {
		os.MkdirAll(filepath.Join(dir, "disks"), 0700)
		os.WriteFile(filepath.Join(dir, "disks/system.qcow2"), []byte("disk payload"), 0600)
		os.WriteFile(filepath.Join(dir, "disks/data-0.qcow2"), []byte("data payload"), 0600)
		m.Files = []File{{Name: "disks/system.qcow2", Role: "system", Format: "qcow2"}, {Name: "disks/data-0.qcow2", Role: "data", DiskID: "source-data", Format: "qcow2"}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if source.DataDisks[0].Path != "/source-owned/disk.qcow2" || source.DataDisks[0].StoragePoolID != "original" {
		t.Fatal("export mutated live source disk descriptors")
	}
	if got := List("backup"); len(got) != 1 || got[0].ID != info.ID {
		t.Fatalf("disk index not discovered after reconstructing registry: %+v", got)
	}
	m, dir, cleanup, err := OpenVerified(context.Background(), info.ID, "backup", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if len(m.Files) != 2 || m.Container.DataDisks[0].Attached || m.Container.DataDisks[0].Path != "" {
		t.Fatal("detached disk missing or host path exported")
	}
	if _, err = os.Stat(filepath.Join(dir, "disks/data-0.qcow2")); err != nil {
		t.Fatal(err)
	}
	_, p, err := Resolve(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	file, _ := os.OpenFile(p, os.O_WRONLY, 0600)
	file.WriteAt([]byte("x"), 12)
	file.Close()
	if _, _, _, err = OpenVerified(context.Background(), info.ID, "backup", nil); err == nil {
		t.Fatal("archive checksum corruption not detected")
	}
}

func TestNewIdentityRetainsAccountsButNeverNetworkSchedulesOrDiskOwnership(t *testing.T) {
	oldConfig := config.AppConfig
	config.AppConfig = &config.ClicdConfig{}
	defer func() { config.AppConfig = oldConfig }()
	previous := allocateIdentityID
	allocateIdentityID = func() int { return 99 }
	defer func() { allocateIdentityID = previous }()
	source := smallManifest().Container
	source.ID = 1
	source.UUID = "source-id"
	source.SSHPassword = "test-account-kept"
	source.MACAddress = "52:54:00:01:02:03"
	source.IP = "192.0.2.5"
	source.PortMappings = []config.PortMapping{{HostPort: 20022, ContainerPort: 22}}
	source.SnapshotScheduleEnabled = true
	source.RestoreOnHostBoot = true
	source.Status = "running"
	source.DataDisks = []config.DataDisk{{ID: "old-data", Path: "/old/data"}}
	c := NewIdentity(source, "restored", "new-pool")
	if c.ID == source.ID || c.UUID == source.UUID || c.UUID == "" || c.IP != "" || c.MACAddress != "" || len(c.PortMappings) != 0 || c.Status != "stopped" || c.RestoreOnHostBoot || c.SnapshotScheduleEnabled || !c.Standalone || c.SSHPassword != source.SSHPassword {
		t.Fatal("identity was not safely isolated")
	}
	c.DataDisks[0].Path = "new"
	if source.DataDisks[0].Path != "/old/data" {
		t.Fatal("clone shares mutable disk descriptor slice")
	}
	if _, _, err := Resolve("../../etc/shadow"); err == nil {
		t.Fatal("invalid artifact lookup accepted")
	}
	if strings.Contains(c.UUID, "/") {
		t.Fatal("unsafe UUID")
	}
}
