package kvm

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"clicd/internal/config"
	"clicd/internal/lxc"
	"clicd/internal/transfer"
)

func flattenTransferDisk(ctx context.Context, source, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	if output, err := exec.CommandContext(ctx, "qemu-img", "convert", "-O", "qcow2", source, target).CombinedOutput(); err != nil {
		return fmt.Errorf("flatten disk: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if err := os.Chmod(target, 0644); err != nil {
		return err
	}
	return validateTransferDisk(ctx, target)
}

func validateTransferDisk(ctx context.Context, p string) error {
	output, err := exec.CommandContext(ctx, "qemu-img", "info", "--output=json", p).CombinedOutput()
	if err != nil {
		return fmt.Errorf("inspect portable disk: %w: %s", err, strings.TrimSpace(string(output)))
	}
	var info struct {
		Format      string          `json:"format"`
		Virtual     int64           `json:"virtual-size"`
		Backing     string          `json:"backing-filename"`
		FullBacking string          `json:"full-backing-filename"`
		Specific    json.RawMessage `json:"format-specific"`
	}
	if err = json.Unmarshal(output, &info); err != nil {
		return err
	}
	if info.Format != "qcow2" || info.Virtual <= 0 || info.Virtual > transfer.MaxVirtualDiskBytes || info.Backing != "" || info.FullBacking != "" || strings.Contains(string(info.Specific), `"data-file"`) {
		return fmt.Errorf("backup disks must be bounded standalone QCOW2 without backing/external data files")
	}
	if output, err = exec.CommandContext(ctx, "qemu-img", "check", "-q", p).CombinedOutput(); err != nil {
		return fmt.Errorf("portable disk consistency check: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func ValidateTransfer(ctx context.Context, dir string, m *transfer.Manifest) error {
	if m.Runtime != "kvm" {
		return fmt.Errorf("not a KVM backup")
	}
	for _, file := range m.Files {
		if err := validateTransferDisk(ctx, filepath.Join(dir, filepath.FromSlash(file.Name))); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) ExportTransfer(ctx context.Context, id int, dir string, manifest *transfer.Manifest) (err error) {
	installationMediaMu.Lock()
	defer installationMediaMu.Unlock()
	kvmSnapshotMu.Lock()
	defer kvmSnapshotMu.Unlock()
	stored := config.FindContainer(id)
	if stored == nil || !stored.IsKVM() {
		return fmt.Errorf("KVM VM not found")
	}
	c := *stored
	state, err := m.GetContainerStatus(c.VirshName())
	if err != nil {
		return err
	}
	if state != "running" && state != "stopped" {
		return fmt.Errorf("VM must be running or fully stopped for backup")
	}
	if state == "running" {
		if err = m.StopContainer(id); err != nil {
			return err
		}
		defer func() {
			restoreErr := m.StartContainer(id)
			if restoreErr == nil {
				config.UpdateContainerStatusAndRestore(id, "running", c.RestoreOnHostBoot)
			}
			err = errors.Join(err, restoreErr)
		}()
	}
	if state, err = m.GetContainerStatus(c.VirshName()); err != nil || state != "stopped" {
		return fmt.Errorf("cannot confirm stopped VM before copying")
	}
	var measured int64
	sources := []string{c.DiskImage}
	for _, disk := range c.DataDisks {
		p, e := config.DataDiskPath(&c, disk)
		if e != nil {
			return e
		}
		sources = append(sources, p)
	}
	for _, source := range sources {
		size, e := measureTransferDisk(ctx, source)
		if e != nil {
			return e
		}
		measured += size
		if measured > transfer.MaxExpandedBytes {
			return fmt.Errorf("flattened QCOW2 payload exceeds 64 GiB archive limit")
		}
	}
	if err = transfer.EnsureSpace(dir, measured*2); err != nil {
		return err
	}
	manifest.BootProfile = "linux"
	if c.Template == "standalone-windows" || IsWindowsImage(c.Template) {
		manifest.BootProfile = "windows"
	} else if c.ManualInstall || c.Template == "standalone-manual" {
		manifest.BootProfile = "manual"
	}
	manifest.Files = []transfer.File{{Name: "disks/system.qcow2", Role: "system", Format: "qcow2"}}
	if err = flattenTransferDisk(ctx, c.DiskImage, filepath.Join(dir, "disks", "system.qcow2")); err != nil {
		return err
	}
	for index, d := range c.DataDisks {
		p, e := config.DataDiskPath(&c, d)
		if e != nil {
			return e
		}
		name := fmt.Sprintf("disks/data-%d.qcow2", index)
		if err = flattenTransferDisk(ctx, p, filepath.Join(dir, filepath.FromSlash(name))); err != nil {
			return err
		}
		manifest.Files = append(manifest.Files, transfer.File{Name: name, Role: "data", DiskID: d.ID, Format: "qcow2"})
	}
	return nil
}

func measureTransferDisk(ctx context.Context, p string) (int64, error) {
	output, err := exec.CommandContext(ctx, "qemu-img", "measure", "--output=json", "-O", "qcow2", p).CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("measure flattened disk capacity: %w: %s", err, strings.TrimSpace(string(output)))
	}
	var measured struct {
		Required int64 `json:"required"`
	}
	if err = json.Unmarshal(output, &measured); err != nil {
		return 0, err
	}
	if measured.Required <= 0 {
		return 0, fmt.Errorf("invalid flattened disk capacity estimate")
	}
	return measured.Required, nil
}

// Reset both cloud and manually installed Linux network identity offline. A
// fresh MAC alone is insufficient for netplan files matching the old MAC.
// libguestfs runs inside its appliance; guest links cannot escape to the host.
func resetTransferGuest(ctx context.Context, c *config.Container, name string) (result error) {
	if _, err := exec.LookPath("virt-customize"); err != nil {
		return fmt.Errorf("virt-customize (libguestfs-tools) is required to reset cloned guest network identity")
	}
	disks := []string{"--format=qcow2", "-a", c.DiskImage}
	for _, d := range c.DataDisks {
		if d.Attached {
			disks = append(disks, "-a", d.Path)
		}
	}
	inspectionArgs := append([]string{"--no-applications", "--no-icon"}, disks...)
	output, err := exec.CommandContext(ctx, "virt-inspector", inspectionArgs...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("inspect cloned guest OS: %w: %s", err, strings.TrimSpace(string(output)))
	}
	var inspected struct {
		Systems []struct {
			Name        string `xml:"name"`
			Root        string `xml:"root"`
			Mountpoints []struct {
				Path string `xml:",chardata"`
			} `xml:"mountpoints>mountpoint"`
		} `xml:"operatingsystem"`
	}
	if err = xml.Unmarshal(output, &inspected); err != nil || len(inspected.Systems) != 1 {
		return fmt.Errorf("backup must contain one inspectable Linux or Windows installation")
	}
	operatingSystem := inspected.Systems[0].Name
	if operatingSystem != "linux" && operatingSystem != "windows" {
		return fmt.Errorf("unsupported cloned guest OS: %s", operatingSystem)
	}
	if operatingSystem == "linux" {
		rootMounted := false
		for _, mount := range inspected.Systems[0].Mountpoints {
			rootMounted = rootMounted || strings.TrimSpace(mount.Path) == "/"
		}
		if !rootMounted {
			restore, err := prepareTransferRootMount(ctx, disks, inspected.Systems[0].Root)
			if restore != nil {
				defer func() { result = errors.Join(result, restore()) }()
			}
			if err != nil {
				return err
			}
		}
	}
	args := append(disks, "--no-network")
	if operatingSystem == "windows" {
		command := `powershell -NoProfile -ExecutionPolicy Bypass -Command "Get-NetAdapter | ForEach-Object { Get-NetIPAddress -InterfaceIndex $_.ifIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue | Where-Object PrefixOrigin -eq 'Manual' | Remove-NetIPAddress -Confirm:$false -ErrorAction SilentlyContinue; Set-NetIPInterface -InterfaceIndex $_.ifIndex -Dhcp Enabled -ErrorAction Stop; Set-DnsClientServerAddress -InterfaceIndex $_.ifIndex -ResetServerAddresses -ErrorAction Stop }"`
		args = append(args, "--firstboot-command", command)
	} else {
		script := `set -eu
rm -rf /var/lib/cloud/instances /var/lib/cloud/instance /var/lib/cloud/data /var/lib/dhcp/* /var/lib/NetworkManager/*lease*
rm -rf /var/lib/dhclient/* /var/lib/systemd/network/*
rm -f /etc/dhcpcd.duid /var/lib/dhcpcd/duid /var/lib/NetworkManager/secret_key
rm -f /etc/ssh/ssh_host_* /etc/udev/rules.d/70-persistent-net.rules
: > /etc/machine-id
rm -f /var/lib/dbus/machine-id
mkdir -p /etc/network/interfaces.d /etc/systemd/network
printf 'auto lo\niface lo inet loopback\nauto eth0\niface eth0 inet dhcp\n' > /etc/network/interfaces
find /etc/network/interfaces.d -maxdepth 1 -type f -delete
find /etc/systemd/network -maxdepth 1 -type f -delete
if [ -d /etc/sysconfig/network-scripts ]; then find /etc/sysconfig/network-scripts -maxdepth 1 -type f -name 'ifcfg-*' ! -name 'ifcfg-lo' -delete; fi
printf '[Match]\nName=eth* en*\n[Network]\nDHCP=ipv4\n' > /etc/systemd/network/10-clicd-clone.network
if [ -d /etc/netplan ]; then rm -f /etc/netplan/*.yaml /etc/netplan/*.yml; printf 'network:\n  version: 2\n  ethernets:\n    clicd:\n      match:\n        name: "en*"\n      dhcp4: true\n    eth:\n      match:\n        name: "eth*"\n      dhcp4: true\n' > /etc/netplan/99-clicd-clone.yaml; chmod 600 /etc/netplan/99-clicd-clone.yaml; fi
if [ -d /etc/NetworkManager/system-connections ]; then rm -f /etc/NetworkManager/system-connections/*; printf '[connection]\nid=clicd-clone\ntype=ethernet\nautoconnect=true\n[ipv4]\nmethod=auto\n[ipv6]\nmethod=disabled\n' > /etc/NetworkManager/system-connections/clicd-clone.nmconnection; chmod 600 /etc/NetworkManager/system-connections/clicd-clone.nmconnection; fi
mkdir -p /etc/cloud/cloud.cfg.d
printf 'network: {config: disabled}\n' > /etc/cloud/cloud.cfg.d/99-clicd-clone-network.cfg
touch /etc/cloud/cloud-init.disabled
if command -v ssh-keygen >/dev/null 2>&1; then ssh-keygen -A; fi
`
		args = append(args, "--hostname", name, "--run-command", script)
	}
	output, err = exec.CommandContext(ctx, "virt-customize", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("reset guest identity before restore: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// Old libguestfs releases can identify a PARTUUID-rooted Linux installation but
// omit / from its mount list. Edit only the new clone through explicit appliance
// mounts, then restore its original fstab even if customization is interrupted.
func prepareTransferRootMount(ctx context.Context, disks []string, root string) (func() error, error) {
	if !regexp.MustCompile(`^/dev/[a-zA-Z0-9_./+-]+$`).MatchString(root) || strings.Contains(root, "..") {
		return nil, fmt.Errorf("unsupported inspected guest root device")
	}
	for _, tool := range []string{"virt-filesystems", "virt-cat", "virt-edit"} {
		if _, err := exec.LookPath(tool); err != nil {
			return nil, fmt.Errorf("%s (libguestfs tools) is required to mount this cloned guest root", tool)
		}
	}
	args := append(append([]string(nil), disks...), "--filesystems", "--long", "--uuid", "--csv")
	output, err := transferCommandOutput(ctx, "virt-filesystems", args, 1<<20)
	if err != nil {
		return nil, err
	}
	uuid, err := transferRootUUID(output, root)
	if err != nil {
		return nil, err
	}
	mountArgs := append(append([]string(nil), disks...), "-m", root+":/")
	read := func(ctx context.Context) ([]byte, error) {
		return transferCommandOutput(ctx, "virt-cat", append(append([]string(nil), mountArgs...), "/etc/fstab"), 16<<10)
	}
	original, err := read(ctx)
	if err != nil {
		return nil, err
	}
	if len(original) == 0 || bytes.IndexByte(original, 0) >= 0 {
		return nil, fmt.Errorf("cloned guest fstab is empty or invalid; cannot safely repair its root mount")
	}
	edit := func(ctx context.Context, expression string) error {
		_, err := transferCommandOutput(ctx, "virt-edit", append(append([]string(nil), mountArgs...), "/etc/fstab", "-e", expression), 16<<10)
		return err
	}
	restore := func() error {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
		defer cancel()
		if err := edit(cleanupCtx, `$_ = $. == 1 ? pack("H*","`+hex.EncodeToString(original)+`") : ""`); err != nil {
			return fmt.Errorf("restore cloned guest fstab: %w", err)
		}
		actual, err := read(cleanupCtx)
		if err != nil || !bytes.Equal(actual, original) {
			return fmt.Errorf("cloned guest fstab was not restored exactly: %v", err)
		}
		return nil
	}
	prefix := "# CLICD temporary appliance root mount\nUUID=" + uuid + " / auto defaults 0 0\n"
	err = edit(ctx, `$_ = pack("H*","`+hex.EncodeToString([]byte(prefix))+`") . $_ if $. == 1`)
	return restore, err
}

func transferRootUUID(output []byte, root string) (string, error) {
	rows, err := csv.NewReader(bytes.NewReader(output)).ReadAll()
	if err != nil || len(rows) < 2 {
		return "", fmt.Errorf("cannot read cloned guest filesystem UUIDs")
	}
	nameColumn, uuidColumn := -1, -1
	for i, name := range rows[0] {
		if name == "Name" {
			nameColumn = i
		}
		if name == "UUID" {
			uuidColumn = i
		}
	}
	if nameColumn >= 0 && uuidColumn >= 0 {
		for _, row := range rows[1:] {
			if row[nameColumn] == root && regexp.MustCompile(`^[a-zA-Z0-9-]{1,128}$`).MatchString(row[uuidColumn]) {
				return row[uuidColumn], nil
			}
		}
	}
	return "", fmt.Errorf("inspected guest root has no usable filesystem UUID")
}

func transferCommandOutput(ctx context.Context, tool string, args []string, limit int64) ([]byte, error) {
	command := exec.CommandContext(ctx, tool, args...)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err = command.Start(); err != nil {
		return nil, err
	}
	output, readErr := io.ReadAll(io.LimitReader(stdout, limit+1))
	if readErr != nil || int64(len(output)) > limit {
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, fmt.Errorf("%s output is invalid or exceeds the safety limit", tool)
	}
	if err = command.Wait(); err != nil {
		return nil, fmt.Errorf("%s: %w: %s", tool, err, strings.TrimSpace(stderr.String()))
	}
	return output, nil
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
	pool, err := config.SelectStoragePoolForContent(config.StorageContentKVM, poolID, required)
	if err != nil {
		return nil, err
	}
	if poolID != "" && pool.ID != poolID {
		return nil, fmt.Errorf("requested KVM storage pool is unavailable or lacks capacity")
	}
	c := transfer.NewIdentity(manifest.Container, name, pool.ID)
	sourceDataDisks := c.DataDisks
	c.DataDisks = nil
	c.KVMName = fmt.Sprintf("vm-%d", c.ID)
	c.MACAddress = randomMAC()
	c.Template = "standalone-" + manifest.BootProfile
	c.ManualInstall = manifest.BootProfile == "manual"
	c.InstallationMediaImageID = ""
	c.InstallationMediaBoot = "disk"
	m = NewManagerForStoragePool(pool.ID)
	c.StoragePath = m.instanceDir(c.KVMName)
	c.DiskImage = filepath.Join(c.StoragePath, "disk.qcow2")
	if _, e := os.Lstat(c.StoragePath); !os.IsNotExist(e) {
		return nil, fmt.Errorf("new instance storage path already exists")
	}
	if err = os.MkdirAll(c.StoragePath, 0755); err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = undefineDomain(c.KVMName)
			_ = os.RemoveAll(c.StoragePath)
			_ = m.CleanupDataDisks(&c)
		}
	}()
	for _, file := range manifest.Files {
		if file.Role == "system" {
			if err = flattenTransferDisk(ctx, filepath.Join(dir, filepath.FromSlash(file.Name)), c.DiskImage); err != nil {
				return nil, err
			}
		}
	}
	for _, sourceDisk := range sourceDataDisks {
		oldID := sourceDisk.ID
		d := sourceDisk
		d.ID = config.NewDataDiskID()
		d.StoragePoolID = pool.ID
		d.Path = ""
		d.Format = "qcow2"
		d.CreatedAt = time.Now().UTC().Format(time.RFC3339)
		p, e := config.DataDiskPath(&c, d)
		if e != nil {
			return nil, e
		}
		d.Path = p
		c.DataDisks = append(c.DataDisks, d)
		for _, file := range manifest.Files {
			if file.Role == "data" && file.DiskID == oldID {
				if err = flattenTransferDisk(ctx, filepath.Join(dir, filepath.FromSlash(file.Name)), p); err != nil {
					return nil, err
				}
			}
		}
	}
	if err = resetTransferGuest(ctx, &c, name); err != nil {
		return nil, err
	}
	if err = m.validateHost(c.Template, true); err != nil {
		return nil, err
	}
	if assignNAT {
		if portCount < 1 || portCount > 64 {
			return nil, fmt.Errorf("port mapping quota must be between 1 and 64")
		}
		cfg := lxc.ContainerConfig{Name: name, Virtualization: "kvm", PortMappingCount: portCount}
		port, release, e := lxc.ReserveCreateNATPorts(cfg)
		if e != nil {
			return nil, e
		}
		defer release()
		c.SSHPort = port
		c.PortMappingLimit = portCount
		c.PortMappings = lxc.SetupDefaultPortMappings(port)
		if manifest.BootProfile == "windows" {
			c.PortMappings[0].ContainerPort = 3389
			c.PortMappings[0].Description = "RDP"
		}
	} else {
		c.PortMappingLimit = 0
	}
	if err = m.ensureDomainDefinition(&c); err != nil {
		return nil, err
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
