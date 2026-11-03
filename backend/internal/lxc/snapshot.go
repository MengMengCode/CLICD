package lxc

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"clicd/internal/config"
)

var snapshotMu sync.Mutex

func (m *Manager) CreateSnapshot(id int, createdBy string, scheduled bool, rotateLimit int, storagePoolID ...string) (config.Snapshot, error) {
	snapshotMu.Lock()
	defer snapshotMu.Unlock()

	c := config.FindContainer(id)
	if c == nil {
		return config.Snapshot{}, fmt.Errorf("container not found: %d", id)
	}
	if len(c.DataDisks) > 0 {
		return config.Snapshot{}, fmt.Errorf("snapshots with data disks are not supported; use a full backup to include all disks")
	}
	if scheduled && rotateLimit > 0 {
		for {
			existing := config.ContainerSnapshots(id)
			if len(existing) < rotateLimit {
				break
			}
			sortSnapshotsOldestFirst(existing)
			if err := m.deleteSnapshotLocked(existing[0]); err != nil {
				return config.Snapshot{}, err
			}
		}
	}

	lxcName := c.LxcName()
	containerDir := filepath.Join(m.LxcPath, lxcName)
	if _, err := os.Stat(containerDir); err != nil {
		return config.Snapshot{}, fmt.Errorf("container storage not found: %v", err)
	}
	pool, err := config.SelectStoragePoolForContent(
		config.StorageContentSnapshots,
		firstString(storagePoolID),
		dirSizeBytes(containerDir),
	)
	if err != nil {
		return config.Snapshot{}, err
	}

	now := time.Now()
	snapshotID := fmt.Sprintf("snap-%d-%s", id, now.Format("20060102150405-000000000"))
	// Use container ID instead of lxcName to avoid collision when containers are recreated
	baseDir := filepath.Join(pool.Path, "snapshots")
	snapshotDir := filepath.Join(baseDir, strconv.Itoa(id), snapshotID)
	if err := safePathUnder(snapshotDir, baseDir); err != nil {
		return config.Snapshot{}, err
	}
	if err := os.MkdirAll(snapshotDir, 0700); err != nil {
		return config.Snapshot{}, err
	}

	wasRunning, err := m.prepareContainerForColdCopy(id, lxcName, containerDir)
	if err != nil {
		os.RemoveAll(snapshotDir)
		return config.Snapshot{}, err
	}
	if wasRunning {
		defer func() {
			if err := m.StartContainer(id); err != nil {
				fmt.Printf("Warning: failed to restart %s after snapshot: %v\n", lxcName, err)
			}
		}()
	}

	if err := copyTree(containerDir, snapshotDir); err != nil {
		os.RemoveAll(snapshotDir)
		return config.Snapshot{}, err
	}

	snapshot := config.Snapshot{
		ID:            snapshotID,
		ContainerID:   c.ID,
		ContainerName: c.Name,
		LXCName:       lxcName,
		CreatedAt:     now.Format("2006-01-02 15:04:05"),
		CreatedBy:     createdBy,
		Scheduled:     scheduled,
		Path:          snapshotDir,
		SizeBytes:     dirSizeBytes(snapshotDir),
	}
	config.AddSnapshot(snapshot)
	return snapshot, nil
}

func (m *Manager) DeleteSnapshot(id string) error {
	snapshotMu.Lock()
	defer snapshotMu.Unlock()

	snapshot := config.FindSnapshot(id)
	if snapshot == nil {
		return fmt.Errorf("snapshot not found: %s", id)
	}
	return m.deleteSnapshotLocked(*snapshot)
}

func (m *Manager) deleteSnapshotLocked(snapshot config.Snapshot) error {
	if snapshot.Path != "" {
		if err := safeSnapshotPath(snapshot.Path); err != nil {
			return err
		}
		if err := os.RemoveAll(snapshot.Path); err != nil {
			return fmt.Errorf("failed to delete snapshot files: %v", err)
		}
	}
	config.RemoveSnapshot(snapshot.ID)
	return nil
}

func (m *Manager) RestoreSnapshot(id string) error {
	snapshotMu.Lock()
	defer snapshotMu.Unlock()

	snapshot := config.FindSnapshot(id)
	if snapshot == nil {
		return fmt.Errorf("snapshot not found: %s", id)
	}
	if snapshot.Path == "" {
		return fmt.Errorf("snapshot path is empty")
	}
	if err := safeSnapshotPath(snapshot.Path); err != nil {
		return err
	}
	if _, err := os.Stat(snapshot.Path); err != nil {
		return fmt.Errorf("snapshot files not found: %v", err)
	}

	c := config.FindContainer(snapshot.ContainerID)
	if c == nil {
		return fmt.Errorf("container not found: %d", snapshot.ContainerID)
	}
	if len(c.DataDisks) > 0 {
		return fmt.Errorf("snapshots with data disks are not supported; use a full backup to include all disks")
	}
	lxcName := c.LxcName()
	containerDir := filepath.Join(m.LxcPath, lxcName)
	if err := safePathUnder(containerDir, m.LxcPath); err != nil {
		return err
	}

	wasRunning, err := m.prepareContainerForColdCopy(c.ID, lxcName, containerDir)
	if err != nil {
		return err
	}

	backupDir := filepath.Join(m.LxcPath, fmt.Sprintf(".%s-restore-backup-%d", lxcName, time.Now().UnixNano()))
	if err := safePathUnder(backupDir, m.LxcPath); err != nil {
		return err
	}
	if err := os.Rename(containerDir, backupDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to move current container aside: %v", err)
	}

	if err := copyTree(snapshot.Path, containerDir); err != nil {
		os.RemoveAll(containerDir)
		_ = os.Rename(backupDir, containerDir)
		return fmt.Errorf("failed to restore snapshot: %v", err)
	}
	_ = os.RemoveAll(backupDir)

	config.UpdateContainerStatus(c.ID, "stopped")
	if wasRunning {
		return m.StartContainer(c.ID)
	}
	return nil
}

func (m *Manager) SetSnapshotSchedule(id int, enabled bool, intervalHours int, scheduleTime string, createdBy string) (*config.Container, error) {
	dataDiskMu.Lock()
	defer dataDiskMu.Unlock()
	c := config.FindContainer(id)
	if c == nil {
		return nil, fmt.Errorf("container not found: %d", id)
	}
	if enabled && len(c.DataDisks) > 0 {
		return nil, fmt.Errorf("scheduled snapshots with data disks are not supported; use full backups to include all disks")
	}
	if intervalHours < 24 {
		return nil, fmt.Errorf("snapshot schedule interval cannot be less than 24 hours")
	}
	if _, err := parseScheduleClock(scheduleTime); err != nil {
		return nil, err
	}
	c.SnapshotScheduleEnabled = enabled
	c.SnapshotScheduleIntervalHours = intervalHours
	c.SnapshotScheduleTime = scheduleTime
	c.SnapshotScheduleCreatedBy = createdBy
	if enabled {
		c.SnapshotScheduleNextRun = nextSnapshotRun(time.Now(), intervalHours, scheduleTime).Format(time.RFC3339)
	} else {
		c.SnapshotScheduleNextRun = ""
	}
	if err := config.SaveConfig(); err != nil {
		return nil, err
	}
	return c, nil
}

func (m *Manager) StartSnapshotScheduler() {
	go func() {
		m.runDueSnapshotSchedules()
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			m.runDueSnapshotSchedules()
		}
	}()
}

func (m *Manager) runDueSnapshotSchedules() {
	now := time.Now()
	containers := append([]config.Container(nil), config.AppConfig.Containers...)
	for _, c := range containers {
		if c.IsKVM() || !c.SnapshotScheduleEnabled {
			continue
		}
		nextRun, err := time.Parse(time.RFC3339, c.SnapshotScheduleNextRun)
		if err != nil || c.SnapshotScheduleNextRun == "" {
			nextRun = now
		}
		if now.Before(nextRun) {
			continue
		}
		createdBy := c.SnapshotScheduleCreatedBy
		if createdBy == "" {
			createdBy = "admin"
		}
		rotateLimit := 0
		if strings.HasPrefix(createdBy, "user:") {
			rotateLimit = config.ContainerSnapshotLimit(&c)
		}
		if _, err := m.CreateSnapshot(c.ID, createdBy, true, rotateLimit); err != nil {
			fmt.Printf("Warning: scheduled snapshot failed for %s: %v\n", c.Name, err)
			continue
		}
		if current := config.FindContainer(c.ID); current != nil {
			interval := current.SnapshotScheduleIntervalHours
			if interval < 24 {
				interval = 24
			}
			next := nextRun.Add(time.Duration(interval) * time.Hour)
			for !next.After(now) {
				next = next.Add(time.Duration(interval) * time.Hour)
			}
			current.SnapshotScheduleLastRun = now.Format(time.RFC3339)
			current.SnapshotScheduleNextRun = next.Format(time.RFC3339)
			config.SaveConfig()
		}
	}
}

func parseScheduleClock(value string) (time.Duration, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return 0, fmt.Errorf("snapshot schedule time must be HH:MM")
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return 0, fmt.Errorf("snapshot schedule hour must be 00-23")
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return 0, fmt.Errorf("snapshot schedule minute must be 00-59")
	}
	return time.Duration(hour)*time.Hour + time.Duration(minute)*time.Minute, nil
}

func nextSnapshotRun(from time.Time, intervalHours int, scheduleTime string) time.Time {
	clock, err := parseScheduleClock(scheduleTime)
	if err != nil {
		clock = 3 * time.Hour
	}
	midnight := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, from.Location())
	next := midnight.Add(clock)
	interval := time.Duration(intervalHours) * time.Hour
	for !next.After(from) {
		next = next.Add(interval)
	}
	return next
}

func (m *Manager) prepareContainerForColdCopy(id int, lxcName string, containerDir string) (bool, error) {
	status, _ := m.GetContainerStatus(lxcName)
	wasRunning := status == "running"
	if wasRunning {
		if err := m.StopContainer(id); err != nil {
			return false, err
		}
		time.Sleep(time.Second)
	} else if c := config.FindContainer(id); c != nil {
		m.CleanPortMappings(id)
		m.cleanupBandwidthLimit(c.LxcName())
	}
	rootfs := filepath.Join(containerDir, "rootfs")
	exec.Command("umount", "-R", "-l", rootfs).Run()
	m.detachContainerMounts(containerDir)
	m.detachContainerLoopDevices(containerDir)
	return wasRunning, nil
}

func snapshotBaseDir() string {
	return snapshotBaseDirForPool("")
}

func snapshotBaseDirForPool(poolID string) string {
	if pool, err := config.SelectStoragePoolForContent(config.StorageContentSnapshots, poolID, 0); err == nil {
		return filepath.Join(pool.Path, "snapshots")
	}
	return ""
}

func safeSnapshotPath(path string) error {
	if err := safePathUnder(path, filepath.Join(config.AppConfig.DataDir, "snapshots")); err == nil {
		return nil
	}
	for _, pool := range config.StoragePoolsForContent(config.StorageContentSnapshots) {
		if err := safePathUnder(path, filepath.Join(pool.Path, "snapshots")); err == nil {
			return nil
		}
	}
	return fmt.Errorf("unsafe snapshot path: %s", path)
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[0])
}

func copyTree(src string, dst string) error {
	if err := os.MkdirAll(dst, 0700); err != nil {
		return err
	}
	output, err := exec.Command("cp", "-a", "--sparse=always", "--reflink=auto", src+string(os.PathSeparator)+".", dst+string(os.PathSeparator)).CombinedOutput()
	if err != nil {
		output, err = exec.Command("cp", "-a", "--sparse=always", src+string(os.PathSeparator)+".", dst+string(os.PathSeparator)).CombinedOutput()
		if err != nil {
			return fmt.Errorf("cp failed: %v, output: %s", err, string(output))
		}
	}
	return nil
}

func dirSizeBytes(path string) int64 {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	out, err := exec.Command("du", "-s", "-B1", path).Output()
	if err != nil {
		return 0
	}
	parts := strings.Fields(string(out))
	if len(parts) == 0 {
		return 0
	}
	var size int64
	fmt.Sscanf(parts[0], "%d", &size)
	return size
}

func safePathUnder(path string, base string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	absBase, err := filepath.Abs(base)
	if err != nil {
		return err
	}
	if absPath == absBase || strings.HasPrefix(absPath, absBase+string(os.PathSeparator)) {
		return nil
	}
	return fmt.Errorf("refusing unsafe path: %s", absPath)
}

func sortSnapshotsOldestFirst(snapshots []config.Snapshot) {
	sort.SliceStable(snapshots, func(i, j int) bool {
		ti, _ := time.Parse("2006-01-02 15:04:05", snapshots[i].CreatedAt)
		tj, _ := time.Parse("2006-01-02 15:04:05", snapshots[j].CreatedAt)
		return ti.Before(tj)
	})
}
