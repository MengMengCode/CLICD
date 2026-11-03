package lxc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"clicd/internal/config"
	"clicd/internal/transfer"
)

func copyTransferRaw(ctx context.Context, source, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return err
	}
	output, err := exec.CommandContext(ctx, "cp", "--sparse=always", "--reflink=auto", source, target).CombinedOutput()
	if err != nil {
		return fmt.Errorf("copy portable filesystem: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return validateExt4Image(target)
}

func ValidateTransfer(ctx context.Context, dir string, manifest *transfer.Manifest) error {
	if manifest.Runtime != "lxc" {
		return fmt.Errorf("not an LXC backup")
	}
	for _, file := range manifest.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := validateExt4Image(filepath.Join(dir, filepath.FromSlash(file.Name))); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) ExportTransfer(ctx context.Context, id int, dir string, manifest *transfer.Manifest) (err error) {
	snapshotMu.Lock()
	defer snapshotMu.Unlock()
	stored := config.FindContainer(id)
	if stored == nil || stored.IsKVM() {
		return fmt.Errorf("LXC container not found")
	}
	c := *stored
	state, err := m.GetContainerStatus(c.LxcName())
	if err != nil {
		return err
	}
	if state != "running" && state != "stopped" {
		return fmt.Errorf("container must be running or fully stopped for backup")
	}
	if state == "running" {
		if err = m.StopContainer(id); err != nil {
			return err
		}
		defer func() {
			restartErr := m.StartContainer(id)
			if restartErr == nil {
				config.UpdateContainerStatusAndRestore(id, "running", c.RestoreOnHostBoot)
			}
			err = errors.Join(err, restartErr)
		}()
	}
	if state, err = m.GetContainerStatus(c.LxcName()); err != nil || state != "stopped" {
		return fmt.Errorf("cannot confirm stopped LXC before copying")
	}
	if err = m.UnmountDataDisks(&c); err != nil {
		return err
	}
	containerDir := filepath.Join(m.LxcPath, c.LxcName())
	if _, err = m.prepareContainerForColdCopy(id, c.LxcName(), containerDir); err != nil {
		return err
	}
	configuration, _ := os.ReadFile(filepath.Join(containerDir, "config"))
	for _, item := range []struct {
		kind        string
		destination *uint32
	}{{"u", &manifest.RootUIDBase}, {"g", &manifest.RootGIDBase}} {
		expression := regexp.MustCompile(`(?m)^\s*lxc\.idmap\s*=\s*` + item.kind + `\s+0\s+(\d+)\s+`)
		match := expression.FindSubmatch(configuration)
		if len(match) > 1 {
			value, _ := strconv.ParseUint(string(match[1]), 10, 32)
			*item.destination = uint32(value)
		}
	}
	manifest.BootProfile = "linux"
	manifest.Files = []transfer.File{{Name: "disks/system.img", Role: "system", Format: "raw"}}
	image := filepath.Join(containerDir, "rootfs.img")
	target := filepath.Join(dir, "disks", "system.img")
	if _, statErr := os.Stat(image); statErr == nil {
		if err = copyTransferRaw(ctx, image, target); err != nil {
			return err
		}
	} else if !os.IsNotExist(statErr) {
		return statErr
	} else {
		if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		file, e := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return e
		}
		e = file.Truncate(int64(c.DiskGB * (1 << 30)))
		file.Close()
		if e != nil {
			return e
		}
		output, e := exec.CommandContext(ctx, "mkfs.ext4", "-F", "-d", filepath.Join(containerDir, "rootfs"), target).CombinedOutput()
		if e != nil {
			return fmt.Errorf("capture directory rootfs: %w: %s", e, strings.TrimSpace(string(output)))
		}
	}
	for index, d := range c.DataDisks {
		p, e := config.DataDiskPath(&c, d)
		if e != nil {
			return e
		}
		name := fmt.Sprintf("disks/data-%d.img", index)
		if err = copyTransferRaw(ctx, p, filepath.Join(dir, filepath.FromSlash(name))); err != nil {
			return err
		}
		manifest.Files = append(manifest.Files, transfer.File{Name: name, Role: "data", DiskID: d.ID, Format: "raw"})
	}
	return nil
}

func safeTransferWrite(root, name, value string, uid, gid int) error {
	name = strings.TrimPrefix(name, "/")
	parent := filepath.Dir(name)
	cursor := root
	for _, part := range strings.Split(parent, string(os.PathSeparator)) {
		if part == "." {
			continue
		}
		cursor = filepath.Join(cursor, part)
		st, err := os.Lstat(cursor)
		if os.IsNotExist(err) {
			if err = os.Mkdir(cursor, 0755); err != nil {
				return err
			}
			if err = os.Chown(cursor, uid, gid); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("guest configuration parent cannot be a symlink")
		}
	}
	p := filepath.Join(root, name)
	if st, err := os.Lstat(p); err == nil && st.Mode()&os.ModeSymlink != 0 {
		if err = os.Remove(p); err != nil {
			return err
		}
	}
	if err := os.WriteFile(p, []byte(value), 0644); err != nil {
		return err
	}
	return os.Chown(p, uid, gid)
}

func rewriteTransferIDs(ctx context.Context, root string, oldUID, oldGID uint32, newUID, newGID int) error {
	seen := map[[2]uint64]bool{}
	return filepath.WalkDir(root, func(p string, _ os.DirEntry, e error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if e != nil {
			return e
		}
		st, e := os.Lstat(p)
		if e != nil {
			return e
		}
		if key, ok := transferFileID(st); ok {
			if seen[key] {
				return nil
			}
			seen[key] = true
		}
		_, uid, gid, ok := fileStatFields(st)
		if !ok {
			return fmt.Errorf("cannot inspect guest filesystem ownership")
		}
		originalUID, originalGID := uid, gid
		if oldUID > 0 && uid >= int(oldUID) && uid < int(oldUID)+65536 {
			uid = uid - int(oldUID) + newUID
		} else if uid >= 0 && uid < 65536 {
			uid += newUID
		}
		if oldGID > 0 && gid >= int(oldGID) && gid < int(oldGID)+65536 {
			gid = gid - int(oldGID) + newGID
		} else if gid >= 0 && gid < 65536 {
			gid += newGID
		}
		if uid == originalUID && gid == originalGID {
			return nil
		}
		return remapTransferOwnership(p, st, uid, gid, oldUID, oldGID, newUID, newGID)
	})
}

func rewriteTransferDataImage(ctx context.Context, p string, oldUID, oldGID uint32, newUID, newGID int) (err error) {
	if int(oldUID) == newUID && int(oldGID) == newGID {
		return nil
	}
	root, err := os.MkdirTemp(filepath.Dir(p), ".identity-")
	if err != nil {
		return err
	}
	mounted := false
	defer func() {
		if mounted {
			if unmountErr := exec.Command("umount", root).Run(); unmountErr != nil {
				err = errors.Join(err, unmountErr)
				return
			}
		}
		_ = os.Remove(root)
	}()
	if output, e := exec.CommandContext(ctx, "mount", "-t", "ext4", "-o", "loop,nodev,nosuid,noexec", p, root).CombinedOutput(); e != nil {
		return fmt.Errorf("mount data disk for ownership mapping: %w: %s", e, strings.TrimSpace(string(output)))
	}
	mounted = true
	return rewriteTransferIDs(ctx, root, oldUID, oldGID, newUID, newGID)
}

func resetTransferRoot(root, name string, uid, gid int) error {
	for _, pattern := range []string{"etc/ssh/ssh_host_*", "etc/network/interfaces.d/*", "etc/netplan/*.yaml", "etc/netplan/*.yml", "etc/systemd/network/*", "etc/NetworkManager/system-connections/*", "var/lib/cloud/instances", "var/lib/cloud/instance", "var/lib/cloud/data", "var/lib/dhcp/*", "etc/udev/rules.d/70-persistent-net.rules"} {
		// Resolve the parent first. Guest symlinks must never direct host file IO.
		parent := filepath.Dir(filepath.Join(root, pattern))
		resolved, e := filepath.EvalSymlinks(parent)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return e
		}
		if e = safePathUnder(resolved, root); e != nil {
			return e
		}
		matches, _ := filepath.Glob(filepath.Join(root, pattern))
		for _, p := range matches {
			if e = os.RemoveAll(p); e != nil {
				return e
			}
		}
	}
	files := map[string]string{
		"etc/hostname": name + "\n", "etc/machine-id": "", "etc/hosts": "127.0.0.1 localhost\n127.0.1.1 " + name + "\n",
		"etc/network/interfaces":                           "auto lo\niface lo inet loopback\nauto eth0\niface eth0 inet dhcp\n",
		"etc/systemd/network/10-clicd-clone.network":       "[Match]\nName=eth* en*\n[Network]\nDHCP=ipv4\n",
		"etc/cloud/cloud.cfg.d/99-clicd-clone-network.cfg": "network: {config: disabled}\n",
		"etc/cloud/cloud-init.disabled":                    "",
		".clicd-unprivileged-shifted":                      "1\n",
	}
	if st, e := os.Stat(filepath.Join(root, "etc/netplan")); e == nil && st.IsDir() {
		files["etc/netplan/99-clicd-clone.yaml"] = "network:\n  version: 2\n  ethernets:\n    eth0:\n      dhcp4: true\n"
	}
	if st, e := os.Stat(filepath.Join(root, "etc/NetworkManager/system-connections")); e == nil && st.IsDir() {
		files["etc/NetworkManager/system-connections/clicd-clone.nmconnection"] = "[connection]\nid=clicd-clone\ntype=ethernet\ninterface-name=eth0\nautoconnect=true\n[ipv4]\nmethod=auto\n[ipv6]\nmethod=disabled\n"
	}
	for path, content := range files {
		if err := safeTransferWrite(root, path, content, uid, gid); err != nil {
			return err
		}
		if strings.Contains(path, ".nmconnection") || strings.HasPrefix(path, "etc/netplan/") {
			if err := os.Chmod(filepath.Join(root, path), 0600); err != nil {
				return err
			}
		}
	}
	if st, e := os.Stat(filepath.Join(root, "etc/ssh")); e == nil && st.IsDir() {
		if err := safeTransferWrite(root, "etc/ssh/.clicd-key-parent-check", "", uid, gid); err != nil {
			return err
		}
		_ = os.Remove(filepath.Join(root, "etc/ssh/.clicd-key-parent-check"))
		for _, kind := range []string{"ed25519", "rsa"} {
			p := filepath.Join(root, "etc/ssh/ssh_host_"+kind+"_key")
			output, e := exec.Command("ssh-keygen", "-q", "-t", kind, "-N", "", "-C", "", "-f", p).CombinedOutput()
			if e != nil {
				return fmt.Errorf("generate independent guest SSH host keys: %w: %s", e, strings.TrimSpace(string(output)))
			}
			if e = os.Chown(p, uid, gid); e != nil {
				return e
			}
			if e = os.Chown(p+".pub", uid, gid); e != nil {
				return e
			}
		}
	}
	return nil
}

func (m *Manager) RestoreTransfer(ctx context.Context, manifest *transfer.Manifest, dir, name, poolID string, assignNAT bool, portCount int) (_ *config.Container, err error) {
	transfer.CreationMu.Lock()
	defer transfer.CreationMu.Unlock()
	if err = transfer.ValidateManifest(manifest); err != nil {
		return nil, err
	}
	if err = ValidateTransfer(ctx, dir, manifest); err != nil {
		return nil, err
	}
	if !config.IsValidContainerName(name) || config.FindContainerByName(name) != nil {
		return nil, fmt.Errorf("invalid or already used instance name")
	}
	var required int64
	for _, f := range manifest.Files {
		required += f.Size
	}
	pool, err := config.SelectStoragePoolForContent(config.StorageContentLXC, poolID, required)
	if err != nil {
		return nil, err
	}
	if poolID != "" && pool.ID != poolID {
		return nil, fmt.Errorf("requested LXC storage pool is unavailable or lacks capacity")
	}
	c := transfer.NewIdentity(manifest.Container, name, pool.ID)
	sourceDataDisks := c.DataDisks
	c.DataDisks = nil
	c.LXCName = fmt.Sprintf("ct-%d", c.ID)
	c.StoragePath = filepath.Join(pool.Path, "lxc", c.LXCName)
	c.MACAddress = transferMAC()
	c.InstallationMediaImageID = ""
	c.InstallationMediaBoot = ""
	if _, e := os.Lstat(c.StoragePath); !os.IsNotExist(e) {
		return nil, fmt.Errorf("new instance storage path already exists")
	}
	link := filepath.Join(m.LxcPath, c.LXCName)
	separateLink := filepath.Clean(link) != filepath.Clean(c.StoragePath)
	if separateLink {
		if _, e := os.Lstat(link); !os.IsNotExist(e) {
			return nil, fmt.Errorf("new LXC name already has unmanaged storage")
		}
	}
	if err = os.MkdirAll(m.LxcPath, 0755); err != nil {
		return nil, err
	}
	if err = os.MkdirAll(c.StoragePath, 0755); err != nil {
		return nil, err
	}
	committed := false
	linked := false
	defer func() {
		if !committed {
			_ = m.UnmountDataDisks(&c)
			_ = m.CleanupDataDisks(&c)
			if linked {
				_ = os.Remove(link)
			}
			_ = os.RemoveAll(c.StoragePath)
		}
	}()
	if separateLink {
		if err = os.Symlink(c.StoragePath, link); err != nil {
			return nil, err
		}
		linked = true
	}
	root := filepath.Join(c.StoragePath, "rootfs")
	if err = os.MkdirAll(root, 0755); err != nil {
		return nil, err
	}
	image := filepath.Join(c.StoragePath, "rootfs.img")
	for _, f := range manifest.Files {
		if f.Role == "system" {
			if err = copyTransferRaw(ctx, filepath.Join(dir, filepath.FromSlash(f.Name)), image); err != nil {
				return nil, err
			}
		}
	}
	if output, e := exec.CommandContext(ctx, "mount", "-t", "ext4", "-o", "loop,nodev,nosuid,noexec", image, root).CombinedOutput(); e != nil {
		return nil, fmt.Errorf("mount restored rootfs for identity reset: %w: %s", e, strings.TrimSpace(string(output)))
	}
	mounted := true
	defer func() {
		if mounted {
			_ = exec.Command("umount", root).Run()
		}
	}()
	uid, gid, err := unprivilegedIDMap()
	if err != nil {
		return nil, err
	}
	if err = rewriteTransferIDs(ctx, root, manifest.RootUIDBase, manifest.RootGIDBase, uid, gid); err != nil {
		return nil, err
	}
	if err = resetTransferRoot(root, name, uid, gid); err != nil {
		return nil, err
	}
	if output, e := exec.Command("umount", root).CombinedOutput(); e != nil {
		return nil, fmt.Errorf("unmount restored rootfs: %w: %s", e, strings.TrimSpace(string(output)))
	}
	mounted = false
	configuration := fmt.Sprintf("lxc.include = /usr/share/lxc/config/common.conf\nlxc.uts.name = %s\nlxc.rootfs.path = dir:%s\nlxc.net.0.type = veth\nlxc.net.0.link = lxcbr0\nlxc.net.0.flags = up\nlxc.net.0.name = eth0\nlxc.net.0.hwaddr = %s\n", name, root, c.MACAddress)
	if err = os.WriteFile(filepath.Join(c.StoragePath, "config"), []byte(configuration), 0600); err != nil {
		return nil, err
	}
	if err = m.applyResourceLimits(c.LxcName(), ContainerConfig{Name: name, TemplateID: c.Template, VCPU: c.VCPU, RAMMB: c.RAMMB, DiskGB: c.DiskGB, IOReadMBps: c.IOReadMBps, IOWriteMBps: c.IOWriteMBps, NetworkDownMbps: c.NetworkDownMbps, NetworkUpMbps: c.NetworkUpMbps}); err != nil {
		return nil, err
	}
	for _, sourceDisk := range sourceDataDisks {
		oldID := sourceDisk.ID
		if err = config.ValidateGuestMountPath(sourceDisk.MountPath); err != nil {
			return nil, err
		}
		d := sourceDisk
		d.ID = config.NewDataDiskID()
		d.Path = ""
		d.StoragePoolID = pool.ID
		d.Format = "raw"
		d.CreatedAt = time.Now().UTC().Format(time.RFC3339)
		p, e := config.DataDiskPath(&c, d)
		if e != nil {
			return nil, e
		}
		d.Path = p
		c.DataDisks = append(c.DataDisks, d)
		for _, f := range manifest.Files {
			if f.Role == "data" && f.DiskID == oldID {
				if err = copyTransferRaw(ctx, filepath.Join(dir, filepath.FromSlash(f.Name)), p); err != nil {
					return nil, err
				}
				if err = rewriteTransferDataImage(ctx, p, manifest.RootUIDBase, manifest.RootGIDBase, uid, gid); err != nil {
					return nil, err
				}
			}
		}
	}
	if assignNAT {
		if portCount < 1 || portCount > 64 {
			return nil, fmt.Errorf("invalid port mapping quota")
		}
		port, release, e := ReserveCreateNATPorts(ContainerConfig{Name: name, Virtualization: "lxc", PortMappingCount: portCount})
		if e != nil {
			return nil, e
		}
		defer release()
		c.SSHPort = port
		c.PortMappingLimit = portCount
		c.PortMappings = SetupDefaultPortMappings(port)
	} else {
		c.PortMappingLimit = 0
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	config.AddContainer(c)
	if err = config.SaveConfig(); err != nil {
		config.RemoveContainer(c.ID)
		return nil, err
	}
	committed = true
	return &c, nil
}

func transferMAC() string {
	id := strings.ReplaceAll(config.NewContainerUUID(), "-", "")
	return fmt.Sprintf("00:16:3e:%s:%s:%s", id[:2], id[2:4], id[4:6])
}
