package kvm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"clicd/internal/config"
)

func diskCommand(ctx context.Context, name string, args ...string) error {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s failed: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}
func qcowDiskInfo(ctx context.Context, path string) (int64, int64, error) {
	out, err := exec.CommandContext(ctx, "qemu-img", "info", "-U", "--output=json", path).CombinedOutput()
	if err != nil {
		return 0, 0, fmt.Errorf("inspect disk: %w: %s", err, strings.TrimSpace(string(out)))
	}
	var info struct {
		VirtualSize int64  `json:"virtual-size"`
		ActualSize  int64  `json:"actual-size"`
		Format      string `json:"format"`
	}
	if err := json.Unmarshal(out, &info); err != nil {
		return 0, 0, err
	}
	if info.Format != "qcow2" || info.VirtualSize <= 0 {
		return 0, 0, fmt.Errorf("only managed QCOW2 disks can be resized")
	}
	return info.VirtualSize, info.ActualSize, nil
}
func (m *Manager) requireDiskStopped(c *config.Container) error {
	state, err := m.GetContainerStatus(c.VirshName())
	if err != nil {
		return fmt.Errorf("cannot inspect VM state: %w", err)
	}
	if state != "stopped" {
		return fmt.Errorf("VM must be fully shut down before changing disks (state %s)", state)
	}
	return nil
}
func (m *Manager) systemDiskPath(c *config.Container) (string, error) {
	expected := filepath.Join(NewManagerForStoragePool(c.StoragePoolID).BasePath, "instances", c.VirshName(), "disk.qcow2")
	if filepath.Clean(c.DiskImage) != filepath.Clean(expected) {
		return "", fmt.Errorf("this system disk is not in its managed instance directory")
	}
	if err := rejectDiskSymlinks(expected); err != nil {
		return "", err
	}
	return expected, nil
}
func rejectDiskSymlinks(path string) error {
	for cursor := filepath.Clean(path); ; cursor = filepath.Dir(cursor) {
		if info, err := os.Lstat(cursor); err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("disk paths cannot contain symlinks")
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		if filepath.Dir(cursor) == cursor {
			break
		}
	}
	return nil
}
func (m *Manager) GetDisks(ctx context.Context, id int) (config.DiskInventory, error) {
	stored := config.FindContainer(id)
	if stored == nil || !stored.IsKVM() {
		return config.DiskInventory{}, fmt.Errorf("KVM VM not found")
	}
	c := *stored
	result := config.DiskInventory{System: config.DiskInfo{ID: "system", Name: "System disk", SizeGB: c.DiskGB, Format: "qcow2", Target: "system", StoragePoolID: c.StoragePoolID, Attached: true, CanResize: true}, DataDisks: []config.DiskInfo{}}
	result.Limits.MaxDataDisks = config.MaxDataDisks
	if err := m.requireDiskStopped(&c); err != nil {
		result.Reason = err.Error()
	} else {
		result.CanEdit = true
	}
	path, err := m.systemDiskPath(&c)
	if err != nil {
		result.System.CanResize = false
		result.System.Error = err.Error()
	} else if size, allocated, err := qcowDiskInfo(ctx, path); err == nil {
		result.System.SizeGB = float64(size) / (1 << 30)
		result.System.AllocatedBytes = allocated
	} else {
		result.System.Error = err.Error()
		result.System.CanResize = false
	}
	for _, d := range c.DataDisks {
		entry := config.PublicDataDisk(d)
		path, err := config.DataDiskPath(&c, d)
		if err == nil {
			var size, allocated int64
			size, allocated, err = qcowDiskInfo(ctx, path)
			entry.SizeGB = float64(size) / (1 << 30)
			entry.AllocatedBytes = allocated
		}
		if err != nil {
			entry.Error = err.Error()
			entry.CanResize = false
		}
		result.DataDisks = append(result.DataDisks, entry)
	}
	return result, nil
}
func (m *Manager) persistDataDisks(previous, next config.Container) error {
	if err := m.ensureDomainDefinition(&next); err != nil {
		return err
	}
	current := config.FindContainer(next.ID)
	if current == nil {
		return fmt.Errorf("VM disappeared")
	}
	old := current.DataDisks
	current.DataDisks = next.DataDisks
	if err := config.SaveConfig(); err != nil {
		current.DataDisks = old
		rollback := m.ensureDomainDefinition(&previous)
		return fmt.Errorf("save disk configuration: %v (domain rollback: %v)", err, rollback)
	}
	return nil
}
func (m *Manager) AddDataDisk(ctx context.Context, id int, name string, sizeGB float64, poolID string) (config.DataDisk, error) {
	installationMediaMu.Lock()
	defer installationMediaMu.Unlock()
	stored := config.FindContainer(id)
	if stored == nil || !stored.IsKVM() {
		return config.DataDisk{}, fmt.Errorf("KVM VM not found")
	}
	c := *stored
	if err := m.requireDiskStopped(&c); err != nil {
		return config.DataDisk{}, err
	}
	if len(c.DataDisks) >= config.MaxDataDisks {
		return config.DataDisk{}, fmt.Errorf("data disk limit is %d", config.MaxDataDisks)
	}
	if c.SnapshotScheduleEnabled {
		return config.DataDisk{}, fmt.Errorf("disable scheduled snapshots before adding data disks; use full backups to include all disks")
	}
	bytes, err := config.DiskSizeBytes(sizeGB)
	if err != nil {
		return config.DataDisk{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 80 {
		return config.DataDisk{}, fmt.Errorf("disk name must contain 1 to 80 characters")
	}
	poolID = strings.TrimSpace(poolID)
	pool, err := config.SelectStoragePoolForContent(config.StorageContentKVM, poolID, bytes)
	if err != nil {
		return config.DataDisk{}, err
	}
	if poolID != "" && pool.ID != poolID {
		return config.DataDisk{}, fmt.Errorf("requested data disk storage pool is unavailable or lacks capacity")
	}
	bus, prefix := "virtio", "vd"
	offset := 1
	if runtime.GOARCH == "amd64" && (c.ManualInstall || IsWindowsImage(c.Template) || strings.Contains(c.Template, "windows")) {
		bus, prefix, offset = "sata", "sd", 2
	}
	used := map[string]bool{}
	for _, d := range c.DataDisks {
		used[d.Target] = true
	}
	target := ""
	for i := offset; i < 26; i++ {
		candidate := prefix + string(rune('a'+i))
		if !used[candidate] {
			target = candidate
			break
		}
	}
	if target == "" {
		return config.DataDisk{}, fmt.Errorf("no disk target is available")
	}
	d := config.DataDisk{ID: config.NewDataDiskID(), Name: name, SizeGB: float64(bytes) / (1 << 30), StoragePoolID: pool.ID, Format: "qcow2", Target: target, Bus: bus, Attached: true, CreatedAt: time.Now().Format(time.RFC3339)}
	d.Path, err = config.DataDiskPath(&c, d)
	if err != nil {
		return d, err
	}
	if err = os.MkdirAll(filepath.Dir(d.Path), 0755); err != nil {
		return d, err
	}
	if _, err := os.Lstat(d.Path); !os.IsNotExist(err) {
		return d, fmt.Errorf("new data disk path already exists")
	}
	if err = diskCommand(ctx, "qemu-img", "create", "-f", "qcow2", d.Path, fmt.Sprintf("%dM", bytes/(1<<20))); err != nil {
		_ = os.Remove(d.Path)
		return d, err
	}
	_ = os.Chmod(d.Path, 0644)
	next := c
	next.DataDisks = append(append([]config.DataDisk(nil), c.DataDisks...), d)
	if err = m.persistDataDisks(c, next); err != nil {
		if rollback := m.ensureDomainDefinition(&c); rollback != nil {
			return d, fmt.Errorf("%w; new disk file retained because domain rollback failed: %v", err, rollback)
		}
		_ = os.Remove(d.Path)
		return d, err
	}
	return d, nil
}
func (m *Manager) ResizeDisk(ctx context.Context, id int, diskID string, sizeGB float64) error {
	installationMediaMu.Lock()
	defer installationMediaMu.Unlock()
	stored := config.FindContainer(id)
	if stored == nil || !stored.IsKVM() {
		return fmt.Errorf("KVM VM not found")
	}
	c := *stored
	if err := m.requireDiskStopped(&c); err != nil {
		return err
	}
	bytes, err := config.DiskSizeBytes(sizeGB)
	if err != nil {
		return err
	}
	path := ""
	index := -1
	if diskID == "system" {
		path, err = m.systemDiskPath(&c)
	} else {
		for i, d := range c.DataDisks {
			if d.ID == diskID {
				index = i
				path, err = config.DataDiskPath(&c, d)
				break
			}
		}
		if index < 0 {
			return fmt.Errorf("data disk not found")
		}
	}
	if err != nil {
		return err
	}
	actual, _, err := qcowDiskInfo(ctx, path)
	if err != nil {
		return err
	}
	if bytes < actual {
		return fmt.Errorf("disk shrinking is not supported; current size is %.3f GB", float64(actual)/(1<<30))
	}
	if bytes > actual {
		poolID := c.StoragePoolID
		if index >= 0 {
			poolID = c.DataDisks[index].StoragePoolID
		}
		pool, err := config.SelectStoragePoolForContent(config.StorageContentKVM, poolID, bytes-actual)
		if err != nil {
			return err
		}
		if poolID != "" && pool.ID != poolID {
			return fmt.Errorf("disk storage pool is unavailable or lacks capacity")
		}
		if err = diskCommand(ctx, "qemu-img", "resize", path, fmt.Sprintf("%dM", bytes/(1<<20))); err != nil {
			return err
		}
	}
	current := config.FindContainer(id)
	if current == nil {
		return fmt.Errorf("VM disappeared")
	}
	if index < 0 {
		current.DiskGB = float64(bytes) / (1 << 30)
	} else {
		for i := range current.DataDisks {
			if current.DataDisks[i].ID == diskID {
				current.DataDisks[i].SizeGB = float64(bytes) / (1 << 30)
			}
		}
	}
	if err = config.SaveConfig(); err != nil {
		return fmt.Errorf("disk was expanded but metadata could not be persisted: %w", err)
	}
	return nil
}
func (m *Manager) SetDataDiskAttached(ctx context.Context, id int, diskID string, attached bool) error {
	installationMediaMu.Lock()
	defer installationMediaMu.Unlock()
	stored := config.FindContainer(id)
	if stored == nil {
		return fmt.Errorf("VM not found")
	}
	c := *stored
	if err := m.requireDiskStopped(&c); err != nil {
		return err
	}
	next := c
	next.DataDisks = append([]config.DataDisk(nil), c.DataDisks...)
	for i, d := range next.DataDisks {
		if d.ID == diskID {
			if _, err := config.DataDiskPath(&c, d); err != nil {
				return err
			}
			next.DataDisks[i].Attached = attached
			return m.persistDataDisks(c, next)
		}
	}
	return fmt.Errorf("data disk not found")
}
func (m *Manager) DeleteDataDisk(ctx context.Context, id int, diskID string) error {
	installationMediaMu.Lock()
	defer installationMediaMu.Unlock()
	stored := config.FindContainer(id)
	if stored == nil {
		return fmt.Errorf("VM not found")
	}
	c := *stored
	if err := m.requireDiskStopped(&c); err != nil {
		return err
	}
	for index, d := range c.DataDisks {
		if d.ID == diskID {
			path, err := config.DataDiskPath(&c, d)
			if err != nil {
				return err
			}
			next := c
			next.DataDisks = append(append([]config.DataDisk(nil), c.DataDisks[:index]...), c.DataDisks[index+1:]...)
			if err = m.persistDataDisks(c, next); err != nil {
				return err
			}
			if err = os.Remove(path); err != nil && !os.IsNotExist(err) {
				d.Attached = false
				current := config.FindContainer(id)
				current.DataDisks = append(current.DataDisks, d)
				_ = config.SaveConfig()
				return fmt.Errorf("disk detached but file deletion failed: %w", err)
			}
			_ = os.Remove(filepath.Dir(path))
			return nil
		}
	}
	return fmt.Errorf("data disk not found")
}

var dataDiskTarget = regexp.MustCompile(`^(vd[b-z]|sd[c-z])$`)

func attachDataDisksXML(xml string, c *config.Container) (string, error) {
	seen := map[string]bool{}
	if len(c.DataDisks) > config.MaxDataDisks {
		return "", fmt.Errorf("too many data disks")
	}
	for _, d := range c.DataDisks {
		if !d.Attached {
			continue
		}
		if !dataDiskTarget.MatchString(d.Target) || (d.Bus != "virtio" && d.Bus != "sata") || (d.Bus == "virtio" && !strings.HasPrefix(d.Target, "vd")) || (d.Bus == "sata" && !strings.HasPrefix(d.Target, "sd")) || seen[d.Target] {
			return "", fmt.Errorf("invalid or duplicate data disk target")
		}
		seen[d.Target] = true
		path, err := config.DataDiskPath(c, d)
		if err != nil {
			return "", err
		}
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			return "", fmt.Errorf("data disk %s is unavailable", d.ID)
		}
		disk := fmt.Sprintf("<disk type='file' device='disk'><driver name='qemu' type='qcow2' cache='none'/><source file='%s'/><target dev='%s' bus='%s'/><serial>%s</serial></disk>\n", xmlEscape(path), d.Target, d.Bus, xmlEscape(d.ID))
		xml = strings.Replace(xml, "  </devices>", "    "+disk+"  </devices>", 1)
	}
	return xml, nil
}
func (m *Manager) containerDomainXML(c *config.Container) (string, error) {
	xml, err := m.baseContainerDomainXML(c)
	if err != nil {
		return "", err
	}
	return attachDataDisksXML(xml, c)
}
func (m *Manager) CleanupDataDisks(c *config.Container) error {
	paths := []string{}
	for _, d := range c.DataDisks {
		path, err := config.DataDiskPath(c, d)
		if err != nil {
			return err
		}
		paths = append(paths, path)
	}
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		_ = os.Remove(filepath.Dir(path))
	}
	return nil
}
