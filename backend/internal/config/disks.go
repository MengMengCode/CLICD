package config

import (
	"fmt"
	"math"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

const MaxDataDisks = 8

type DataDisk struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	SizeGB        float64 `json:"size_gb"`
	StoragePoolID string  `json:"storage_pool_id"`
	Path          string  `json:"path,omitempty"`
	Format        string  `json:"format"`
	Target        string  `json:"target"`
	Bus           string  `json:"bus,omitempty"`
	MountPath     string  `json:"mount_path,omitempty"`
	Attached      bool    `json:"attached"`
	CreatedAt     string  `json:"created_at"`
}

var diskComponent = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,79}$`)

func NewDataDiskID() string { return "disk-" + generateUUIDString() }

func DiskSizeBytes(sizeGB float64) (int64, error) {
	if math.IsNaN(sizeGB) || math.IsInf(sizeGB, 0) || sizeGB < 0.125 || sizeGB > 16384 {
		return 0, fmt.Errorf("disk size must be between 0.125 and 16384 GB")
	}
	return int64(math.Ceil(sizeGB*1024)) * 1024 * 1024, nil
}

// DataDiskPath derives ownership from pool, workload UUID and disk ID. Persisted
// paths must match exactly; no API accepts a host path.
func DataDiskPath(c *Container, d DataDisk) (string, error) {
	if c == nil || !diskComponent.MatchString(c.UUID) || !diskComponent.MatchString(d.ID) {
		return "", fmt.Errorf("invalid workload or disk identity")
	}
	pool := StoragePoolByID(d.StoragePoolID)
	if pool == nil || pool.Path == "" {
		return "", fmt.Errorf("data disk storage pool is unavailable")
	}
	extension := "disk.img"
	if c.IsKVM() {
		extension = "disk.qcow2"
	}
	expected := filepath.Join(pool.Path, "data-disks", c.Runtime(), c.UUID, d.ID, extension)
	if d.Path != "" && filepath.Clean(d.Path) != filepath.Clean(expected) {
		return "", fmt.Errorf("data disk path is outside its managed owner directory")
	}
	// A guest must never be able to replace a managed host component by a symlink.
	relative, _ := filepath.Rel(pool.Path, expected)
	cursor := filepath.Clean(pool.Path)
	for _, part := range strings.Split(relative, string(os.PathSeparator)) {
		cursor = filepath.Join(cursor, part)
		info, err := os.Lstat(cursor)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("managed disk paths cannot contain symlinks")
		}
		if cursor == expected && !info.Mode().IsRegular() {
			return "", fmt.Errorf("managed disk must be a regular file")
		}
	}
	return expected, nil
}

func ValidateGuestMountPath(value string) error {
	if value == "" || value != path.Clean(value) || !strings.HasPrefix(value, "/") || strings.Contains(value, "\\") || strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return fmt.Errorf("mount_path must be a clean absolute guest path without spaces")
	}
	allowed := false
	for _, base := range []string{"/data", "/mnt", "/srv"} {
		if value == base || strings.HasPrefix(value, base+"/") {
			allowed = true
		}
	}
	if !allowed {
		return fmt.Errorf("managed data disks must mount below /data, /mnt or /srv")
	}
	return nil
}

// DiskInfo is the public inventory. Host paths deliberately stay out of this API.
type DiskInfo struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	SizeGB         float64 `json:"size_gb"`
	AllocatedBytes int64   `json:"allocated_bytes"`
	Format         string  `json:"format"`
	Target         string  `json:"target"`
	Bus            string  `json:"bus,omitempty"`
	MountPath      string  `json:"mount_path,omitempty"`
	StoragePoolID  string  `json:"storage_pool_id"`
	Attached       bool    `json:"attached"`
	CanResize      bool    `json:"can_resize"`
	Error          string  `json:"error,omitempty"`
}
type DiskInventory struct {
	System    DiskInfo   `json:"system"`
	DataDisks []DiskInfo `json:"data_disks"`
	CanEdit   bool       `json:"can_edit"`
	Reason    string     `json:"reason,omitempty"`
	Limits    struct {
		MaxDataDisks int `json:"max_data_disks"`
	} `json:"limits"`
}

func PublicDataDisk(d DataDisk) DiskInfo {
	return DiskInfo{ID: d.ID, Name: d.Name, SizeGB: d.SizeGB, Format: d.Format, Target: d.Target, Bus: d.Bus, MountPath: d.MountPath, StoragePoolID: d.StoragePoolID, Attached: d.Attached, CanResize: true}
}
