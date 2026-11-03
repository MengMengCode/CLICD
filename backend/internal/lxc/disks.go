package lxc

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"clicd/internal/config"
)

var dataDiskMu sync.Mutex

const dataDiskMarker = "# clicd data disk "

func runDiskCommand(ctx context.Context, name string, args ...string) error {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s failed: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}
func (m *Manager) requireDiskStopped(c *config.Container) error {
	out, err := exec.Command("lxc-info", "-n", c.LxcName(), "-sH").Output()
	if err != nil {
		return fmt.Errorf("cannot inspect container state: %w", err)
	}
	if strings.TrimSpace(string(out)) != "STOPPED" {
		return fmt.Errorf("container must be fully stopped before changing disks (state %s)", strings.TrimSpace(string(out)))
	}
	return nil
}
func validateExt4Image(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var magic [2]byte
	if _, err = f.ReadAt(magic[:], 1080); err != nil {
		return err
	}
	if magic != [2]byte{0x53, 0xef} {
		return fmt.Errorf("only managed ext4 loop images are supported")
	}
	return nil
}
func (m *Manager) systemDiskPath(c *config.Container) (string, error) {
	dir := filepath.Join(m.LxcPath, c.LxcName())
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	base, _ := filepath.Abs(m.LxcPath)
	abs, _ := filepath.Abs(resolved)
	relative, _ := filepath.Rel(base, abs)
	if (relative == "." || strings.HasPrefix(relative, "..") || filepath.IsAbs(relative)) && !lxcStorageTargetAllowed(abs) {
		return "", fmt.Errorf("rootfs is not in managed storage")
	}
	image := filepath.Join(resolved, "rootfs.img")
	info, err := os.Lstat(image)
	if err != nil {
		return "", fmt.Errorf("rootfs layout has no managed loop image; quota/directory resizing is not supported: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("rootfs image must be a regular file")
	}
	if err = validateExt4Image(image); err != nil {
		return "", err
	}
	return image, nil
}
func rawDiskInfo(path string) (float64, int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, 0, err
	}
	if !info.Mode().IsRegular() {
		return 0, 0, fmt.Errorf("disk must be a regular file")
	}
	out, err := exec.Command("du", "-B1", "--", path).Output()
	if err != nil {
		return 0, 0, err
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return 0, 0, fmt.Errorf("cannot inspect allocated disk space")
	}
	allocated, err := strconv.ParseInt(fields[0], 10, 64)
	return float64(info.Size()) / (1 << 30), allocated, err
}
func (m *Manager) GetDisks(ctx context.Context, id int) (config.DiskInventory, error) {
	stored := config.FindContainer(id)
	if stored == nil || stored.IsKVM() {
		return config.DiskInventory{}, fmt.Errorf("LXC container not found")
	}
	c := *stored
	result := config.DiskInventory{System: config.DiskInfo{ID: "system", Name: "Root filesystem", SizeGB: c.DiskGB, Format: "raw/ext4", Target: "rootfs", StoragePoolID: c.StoragePoolID, Attached: true, CanResize: true}, DataDisks: []config.DiskInfo{}}
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
	} else {
		result.System.SizeGB, result.System.AllocatedBytes, err = rawDiskInfo(path)
		if err != nil {
			result.System.Error = err.Error()
		}
	}
	for _, d := range c.DataDisks {
		entry := config.PublicDataDisk(d)
		path, err := config.DataDiskPath(&c, d)
		if err == nil {
			entry.SizeGB, entry.AllocatedBytes, err = rawDiskInfo(path)
		}
		if err != nil {
			entry.Error = err.Error()
			entry.CanResize = false
		}
		result.DataDisks = append(result.DataDisks, entry)
	}
	return result, nil
}
func diskMountPoint(path string) string { return path + ".mount" }

// LXC resolves bind sources after entering an unprivileged user namespace.
// Permit traversal of managed ancestors without exposing directory listings or
// the raw image (0600). Never change the storage pool or its parent directories.
func prepareDataDiskDirectories(c *config.Container, d config.DataDisk) (string, error) {
	image, err := config.DataDiskPath(c, d)
	if err != nil {
		return "", err
	}
	pool := config.StoragePoolByID(d.StoragePoolID)
	relative, err := filepath.Rel(pool.Path, filepath.Dir(image))
	if err != nil || relative == "." || strings.HasPrefix(relative, "..") || filepath.IsAbs(relative) {
		return "", fmt.Errorf("invalid managed data disk directory")
	}
	cursor := pool.Path
	for _, part := range strings.Split(relative, string(os.PathSeparator)) {
		cursor = filepath.Join(cursor, part)
		if err = os.Mkdir(cursor, 0711); err != nil && !os.IsExist(err) {
			return "", err
		}
		info, err := os.Lstat(cursor)
		if err != nil {
			return "", err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("managed disk directories cannot contain symlinks")
		}
		if _, uid, _, ok := fileStatFields(info); ok && uid != os.Geteuid() {
			return "", fmt.Errorf("managed disk directory is owned by another host user")
		}
		if err = os.Chmod(cursor, 0711); err != nil {
			return "", err
		}
	}
	return image, nil
}

func imageMountedAt(image, target string) (bool, error) {
	if info, err := os.Lstat(target); os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, err
	} else if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("disk mountpoint must be a managed directory without symlinks")
	}
	targetValue, err := findmntValue(target, "TARGET")
	if err != nil {
		return false, err
	}
	if !sameFilesystemPath(targetValue, target) {
		return false, nil
	}
	source, err := findmntValue(target, "SOURCE")
	if err != nil {
		return false, err
	}
	source = strings.Split(source, "[")[0]
	backing, err := exec.Command("losetup", "-n", "-O", "BACK-FILE", source).Output()
	if err != nil {
		return false, fmt.Errorf("cannot verify loop-device ownership")
	}
	if !sameFilesystemPath(strings.TrimSpace(string(backing)), image) {
		return false, fmt.Errorf("mountpoint is occupied by a different filesystem")
	}
	return true, nil
}
func unmountOwnedImage(ctx context.Context, image, target string) error {
	mounted, err := imageMountedAt(image, target)
	if err != nil {
		return err
	}
	if mounted {
		return runDiskCommand(ctx, "umount", target)
	}
	return nil
}
func requireImageNotAttached(ctx context.Context, path string) error {
	out, err := exec.CommandContext(ctx, "losetup", "-j", path).Output()
	if err != nil {
		return fmt.Errorf("cannot inspect loop-device ownership: %w", err)
	}
	if strings.TrimSpace(string(out)) != "" {
		return fmt.Errorf("disk image is still attached to a loop device; unmount it before changing or deleting it")
	}
	return nil
}
func safeDataGuestDirectory(root, guest string) error {
	if err := config.ValidateGuestMountPath(guest); err != nil {
		return err
	}
	cursor := root
	for _, part := range strings.Split(strings.TrimPrefix(guest, "/"), "/") {
		cursor = filepath.Join(cursor, part)
		info, err := os.Lstat(cursor)
		if os.IsNotExist(err) {
			if err = os.Mkdir(cursor, 0755); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("guest mount path cannot cross symlinks or non-directories")
		}
	}
	entries, err := os.ReadDir(cursor)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("guest mount path %s is not empty; refusing to hide existing data", guest)
	}
	return nil
}
func (m *Manager) syncDataDiskConfig(c *config.Container) error {
	filename := filepath.Join(m.LxcPath, c.LxcName(), "config")
	content, err := os.ReadFile(filename)
	if err != nil {
		return err
	}
	next := []string{}
	skip := false
	for _, line := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), dataDiskMarker) {
			skip = true
			continue
		}
		if skip && strings.HasPrefix(strings.TrimSpace(line), "lxc.mount.entry") {
			skip = false
			continue
		}
		skip = false
		next = append(next, line)
	}
	for _, d := range c.DataDisks {
		if !d.Attached {
			continue
		}
		if err := config.ValidateGuestMountPath(d.MountPath); err != nil {
			return err
		}
		path, err := config.DataDiskPath(c, d)
		if err != nil {
			return err
		}
		source := diskMountPoint(path)
		if strings.ContainsAny(source, " \t\n\r") {
			return fmt.Errorf("data disk storage paths cannot contain whitespace")
		}
		next = append(next, dataDiskMarker+d.ID, fmt.Sprintf("lxc.mount.entry = %s %s none bind,create=dir 0 0", source, strings.TrimPrefix(d.MountPath, "/")))
	}
	return os.WriteFile(filename, []byte(strings.Join(next, "\n")), 0644)
}

// PrepareDataDisks mounts sources outside rootfs and only bind-mounts empty,
// symlink-free guest targets. It is also used by full backup restore.
func (m *Manager) PrepareDataDisks(c *config.Container) error {
	dataDiskMu.Lock()
	defer dataDiskMu.Unlock()
	return m.prepareDataDisks(c)
}
func (m *Manager) prepareDataDisks(c *config.Container) (result error) {
	if len(c.DataDisks) == 0 {
		return nil
	}
	root := filepath.Join(m.LxcPath, c.LxcName(), "rootfs")
	newMounts := []string{}
	defer func() {
		if result != nil {
			for i := len(newMounts) - 1; i >= 0; i-- {
				_ = unmountOwnedImage(context.Background(), newMounts[i], diskMountPoint(newMounts[i]))
			}
		}
	}()
	for _, d := range c.DataDisks {
		if !d.Attached {
			continue
		}
		path, err := prepareDataDiskDirectories(c, d)
		if err != nil {
			return err
		}
		if err = validateExt4Image(path); err != nil {
			return err
		}
		if err = safeDataGuestDirectory(root, d.MountPath); err != nil {
			return err
		}
		target := diskMountPoint(path)
		if err = os.MkdirAll(target, 0700); err != nil {
			return err
		}
		mounted, err := imageMountedAt(path, target)
		if err != nil {
			return err
		}
		if !mounted {
			if err = runDiskCommand(context.Background(), "mount", "-t", "ext4", "-o", "loop,nodev,nosuid", path, target); err != nil {
				return err
			}
			newMounts = append(newMounts, path)
		}
	}
	return m.syncDataDiskConfig(c)
}
func (m *Manager) UnmountDataDisks(c *config.Container) error {
	if len(c.DataDisks) == 0 {
		return nil
	}
	dataDiskMu.Lock()
	defer dataDiskMu.Unlock()
	if err := m.requireDiskStopped(c); err != nil {
		return err
	}
	return m.unmountDataDisks(c)
}
func (m *Manager) unmountDataDisks(c *config.Container) error {
	for _, d := range c.DataDisks {
		path, err := config.DataDiskPath(c, d)
		if err != nil {
			return err
		}
		if err = unmountOwnedImage(context.Background(), path, diskMountPoint(path)); err != nil {
			return err
		}
	}
	return nil
}
func (m *Manager) persistDataDisks(previous, next config.Container) error {
	filename := filepath.Join(m.LxcPath, previous.LxcName(), "config")
	oldConfig, err := os.ReadFile(filename)
	if err != nil {
		return err
	}
	if err = m.syncDataDiskConfig(&next); err != nil {
		return err
	}
	current := config.FindContainer(next.ID)
	if current == nil {
		return fmt.Errorf("container disappeared")
	}
	old := current.DataDisks
	current.DataDisks = next.DataDisks
	if err = config.SaveConfig(); err != nil {
		current.DataDisks = old
		_ = os.WriteFile(filename, oldConfig, 0644)
		return err
	}
	return nil
}
func (m *Manager) AddDataDisk(ctx context.Context, id int, name string, sizeGB float64, poolID, mountPath string) (config.DataDisk, error) {
	dataDiskMu.Lock()
	defer dataDiskMu.Unlock()
	stored := config.FindContainer(id)
	if stored == nil || stored.IsKVM() {
		return config.DataDisk{}, fmt.Errorf("LXC container not found")
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
	if err = config.ValidateGuestMountPath(mountPath); err != nil {
		return config.DataDisk{}, err
	}
	for _, d := range c.DataDisks {
		if d.MountPath == mountPath || strings.HasPrefix(d.MountPath, mountPath+"/") || strings.HasPrefix(mountPath, d.MountPath+"/") {
			return config.DataDisk{}, fmt.Errorf("guest mount paths cannot overlap")
		}
	}
	poolID = strings.TrimSpace(poolID)
	pool, err := config.SelectStoragePoolForContent(config.StorageContentLXC, poolID, bytes)
	if err != nil {
		return config.DataDisk{}, err
	}
	if poolID != "" && pool.ID != poolID {
		return config.DataDisk{}, fmt.Errorf("requested data disk storage pool is unavailable or lacks capacity")
	}
	if err = m.ensureDiskImageMounted(c.LxcName()); err != nil {
		return config.DataDisk{}, err
	}
	if err = safeDataGuestDirectory(filepath.Join(m.LxcPath, c.LxcName(), "rootfs"), mountPath); err != nil {
		return config.DataDisk{}, err
	}
	d := config.DataDisk{ID: config.NewDataDiskID(), Name: name, SizeGB: float64(bytes) / (1 << 30), StoragePoolID: pool.ID, Format: "raw", Target: mountPath, MountPath: mountPath, Attached: true, CreatedAt: time.Now().Format(time.RFC3339)}
	d.Path, err = prepareDataDiskDirectories(&c, d)
	if err != nil {
		return d, err
	}
	f, err := os.OpenFile(d.Path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return d, err
	}
	err = f.Truncate(bytes)
	_ = f.Close()
	if err != nil {
		_ = os.Remove(d.Path)
		return d, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			if err := unmountOwnedImage(context.Background(), d.Path, diskMountPoint(d.Path)); err == nil {
				_ = os.Remove(d.Path)
			}
		}
	}()
	if err = runDiskCommand(ctx, "mkfs.ext4", "-F", d.Path); err != nil {
		return d, err
	}
	target := diskMountPoint(d.Path)
	if err = os.MkdirAll(target, 0700); err != nil {
		return d, err
	}
	if mounted, err := imageMountedAt(d.Path, target); err != nil || mounted {
		return d, fmt.Errorf("new disk mountpoint is occupied or unsafe: %v", err)
	}
	if err = runDiskCommand(ctx, "mount", "-t", "ext4", "-o", "loop,nodev,nosuid", d.Path, target); err != nil {
		return d, err
	}
	uid, gid, err := unprivilegedIDMap()
	if err != nil {
		return d, err
	}
	if err = os.Chown(target, uid, gid); err != nil {
		return d, err
	}
	if err = os.Chmod(target, 0755); err != nil {
		return d, err
	}
	if err = unmountOwnedImage(ctx, d.Path, target); err != nil {
		return d, err
	}
	next := c
	next.DataDisks = append(append([]config.DataDisk(nil), c.DataDisks...), d)
	if err = m.persistDataDisks(c, next); err != nil {
		return d, err
	}
	cleanup = false
	return d, nil
}
func growExt4Image(ctx context.Context, path string, bytes int64) error {
	if err := validateExt4Image(path); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if bytes < info.Size() {
		return fmt.Errorf("disk shrinking is not supported; current size is %.3f GB", float64(info.Size())/(1<<30))
	}
	if err = requireImageNotAttached(ctx, path); err != nil {
		return err
	}
	out, err := exec.CommandContext(ctx, "e2fsck", "-f", "-p", path).CombinedOutput()
	if err != nil {
		exit, ok := err.(*exec.ExitError)
		if !ok || exit.ExitCode() != 1 {
			return fmt.Errorf("filesystem check failed: %v: %s", err, out)
		}
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	err = f.Truncate(bytes)
	_ = f.Close()
	if err != nil {
		return err
	}
	if err = runDiskCommand(ctx, "resize2fs", path); err != nil {
		return fmt.Errorf("image expanded but filesystem resize failed; retry to finish safely: %w", err)
	}
	return nil
}
func (m *Manager) ResizeDisk(ctx context.Context, id int, diskID string, sizeGB float64) error {
	dataDiskMu.Lock()
	defer dataDiskMu.Unlock()
	stored := config.FindContainer(id)
	if stored == nil || stored.IsKVM() {
		return fmt.Errorf("LXC container not found")
	}
	c := *stored
	if err := m.requireDiskStopped(&c); err != nil {
		return err
	}
	bytes, err := config.DiskSizeBytes(sizeGB)
	if err != nil {
		return err
	}
	if err = m.unmountDataDisks(&c); err != nil {
		return err
	}
	path := ""
	index := -1
	poolID := c.StoragePoolID
	if diskID == "system" {
		path, err = m.systemDiskPath(&c)
		if err == nil {
			err = unmountOwnedImage(ctx, path, filepath.Join(m.LxcPath, c.LxcName(), "rootfs"))
		}
	} else {
		for i, d := range c.DataDisks {
			if d.ID == diskID {
				index = i
				poolID = d.StoragePoolID
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
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if bytes < info.Size() {
		return fmt.Errorf("disk shrinking is not supported")
	}
	pool, err := config.SelectStoragePoolForContent(config.StorageContentLXC, poolID, bytes-info.Size())
	if err != nil {
		return err
	}
	if poolID != "" && pool.ID != poolID {
		return fmt.Errorf("disk storage pool is unavailable or lacks capacity")
	}
	if err = growExt4Image(ctx, path, bytes); err != nil {
		return err
	}
	current := config.FindContainer(id)
	if current == nil {
		return fmt.Errorf("container disappeared")
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
		return fmt.Errorf("disk expanded but metadata save failed: %w", err)
	}
	return nil
}
func (m *Manager) SetDataDiskAttached(ctx context.Context, id int, diskID string, attached bool) error {
	dataDiskMu.Lock()
	defer dataDiskMu.Unlock()
	stored := config.FindContainer(id)
	if stored == nil || stored.IsKVM() {
		return fmt.Errorf("LXC container not found")
	}
	c := *stored
	if err := m.requireDiskStopped(&c); err != nil {
		return err
	}
	if err := m.unmountDataDisks(&c); err != nil {
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
	dataDiskMu.Lock()
	defer dataDiskMu.Unlock()
	stored := config.FindContainer(id)
	if stored == nil || stored.IsKVM() {
		return fmt.Errorf("LXC container not found")
	}
	c := *stored
	if err := m.requireDiskStopped(&c); err != nil {
		return err
	}
	if err := m.unmountDataDisks(&c); err != nil {
		return err
	}
	for i, d := range c.DataDisks {
		if d.ID == diskID {
			path, err := config.DataDiskPath(&c, d)
			if err != nil {
				return err
			}
			if err = requireImageNotAttached(ctx, path); err != nil {
				return err
			}
			next := c
			next.DataDisks = append(append([]config.DataDisk(nil), c.DataDisks[:i]...), c.DataDisks[i+1:]...)
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
			_ = os.Remove(diskMountPoint(path))
			_ = os.Remove(filepath.Dir(path))
			return nil
		}
	}
	return fmt.Errorf("data disk not found")
}
func (m *Manager) CleanupDataDisks(c *config.Container) error {
	dataDiskMu.Lock()
	defer dataDiskMu.Unlock()
	if err := m.unmountDataDisks(c); err != nil {
		return err
	}
	paths := []string{}
	for _, d := range c.DataDisks {
		path, err := config.DataDiskPath(c, d)
		if err != nil {
			return err
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			if err = requireImageNotAttached(context.Background(), path); err != nil {
				return err
			}
		}
		paths = append(paths, path)
	}
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		_ = os.Remove(diskMountPoint(path))
		_ = os.Remove(filepath.Dir(path))
	}
	return nil
}
