package kvm

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"

	"clicd/internal/config"
)

// Serializes cache deletion with creation and media changes. Queued lifecycle
// operations are additionally serialized by the API task queue.
var installationMediaMu sync.Mutex

// ValidateISOFile recognizes ISO9660 and UDF volume descriptors, rather than
// trusting a filename or response MIME type. It also rejects renamed disk images.
func ValidateISOFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var header [4]byte
	if _, err := f.ReadAt(header[:], 0); err != nil {
		return fmt.Errorf("invalid ISO image: %w", err)
	}
	if string(header[:]) == "QFI\xfb" {
		return fmt.Errorf("QCOW2 disk cannot be mounted as installation media")
	}
	var sector [2048]byte
	for block := int64(16); block < 33; block++ {
		if _, err := f.ReadAt(sector[:], block*2048); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		magic := string(sector[1:6])
		if (magic == "CD001" && sector[6] == 1) || magic == "BEA01" || magic == "NSR02" || magic == "NSR03" {
			return nil
		}
	}
	return fmt.Errorf("download is not an ISO9660/UDF installation image")
}

func ImageFormat(image Image) string {
	if image.IsManualISO() {
		return "iso"
	}
	if ok, _ := ImageDownloadedInfo(image.ID); ok {
		if qcow, err := isQCOW2File(ImagePath(image.ID)); err == nil && qcow {
			return "qcow2"
		}
		if ValidateISOFile(ImagePath(image.ID)) == nil {
			return "iso"
		}
	}
	if image.downloadExtension() == ".iso" {
		return "iso"
	}
	return "qcow2"
}

// InstallationMedia preserves the legacy Windows install definition until the
// first explicit change. Explicit empty media always means ejected.
func InstallationMedia(c *config.Container) (imageID, boot string) {
	if c.InstallationMediaBoot != "" {
		return c.InstallationMediaImageID, c.InstallationMediaBoot
	}
	if c.ManualInstall || IsManualISOImage(c.Template) {
		return c.Template, "cdrom"
	}
	if IsWindowsImage(c.Template) && windowsInstallISOPath(c.Template) != "" {
		return c.Template, "cdrom"
	}
	return "", "disk"
}

func manualDomainXML(name string, vcpu, ram int, disk, iso, mac string, read, write, down, up int, boot string) string {
	xml := domainXML(name, vcpu, ram, disk, "", mac, read, write, down, up, false)
	if runtime.GOARCH == "amd64" {
		xml = strings.Replace(xml, "<target dev='vda' bus='virtio'/>", "<target dev='sda' bus='sata'/>", 1)
		xml = strings.Replace(xml, "<model type='virtio'/>", "<model type='e1000e'/>", 1)
		xml = strings.Replace(xml, "<video><model type='virtio'/></video>", "<video><model type='vga'/></video>", 1)
	}
	return withInstallationMedia(xml, iso, boot)
}

// Only modifies XML produced by our own domain builders. Device boot ordering
// replaces OS boot ordering; libvirt forbids mixing the two forms.
var deviceDiskXML = regexp.MustCompile(`(?s)<disk type='file' device='disk'>.*?</disk>`)
var deviceBootXML = regexp.MustCompile(`\s*<boot order='[0-9]+'/>`)

func withInstallationMedia(xml, iso, boot string) string {
	xml = strings.ReplaceAll(xml, "    <boot dev='hd'/>\n", "")
	xml = deviceBootXML.ReplaceAllString(xml, "")
	order := 1
	if iso != "" && boot == "cdrom" {
		order = 2
	}
	first := true
	xml = deviceDiskXML.ReplaceAllStringFunc(xml, func(disk string) string {
		if !first {
			return disk
		}
		first = false
		return strings.Replace(disk, "</disk>", fmt.Sprintf("<boot order='%d'/></disk>", order), 1)
	})
	if iso != "" {
		bus := "sata"
		controller := ""
		if runtime.GOARCH == "arm64" {
			bus = "scsi"
			controller = "<controller type='scsi' model='virtio-scsi'/>"
		}
		cdOrder := 2
		if boot == "cdrom" {
			cdOrder = 1
		}
		media := fmt.Sprintf("%s<disk type='file' device='cdrom'><driver name='qemu' type='raw'/><source file='%s'/><target dev='sdb' bus='%s'/><readonly/><boot order='%d'/></disk>\n", controller, xmlEscape(iso), bus, cdOrder)
		xml = strings.Replace(xml, "  </devices>", "    "+media+"  </devices>", 1)
	}
	return xml
}

func (m *Manager) baseContainerDomainXML(c *config.Container) (string, error) {
	if c.Standalone {
		switch c.Template {
		case "standalone-windows":
			return withInstallationMedia(windowsDomainXML(c.VirshName(), int(c.VCPU), c.RAMMB, c.DiskImage, "", "", c.MACAddress, c.IOReadMBps, c.IOWriteMBps, c.NetworkDownMbps, c.NetworkUpMbps), "", "disk"), nil
		case "standalone-manual":
			return manualDomainXML(c.VirshName(), int(c.VCPU), c.RAMMB, c.DiskImage, "", c.MACAddress, c.IOReadMBps, c.IOWriteMBps, c.NetworkDownMbps, c.NetworkUpMbps, "disk"), nil
		default:
			return withInstallationMedia(domainXML(c.VirshName(), int(c.VCPU), c.RAMMB, c.DiskImage, "", c.MACAddress, c.IOReadMBps, c.IOWriteMBps, c.NetworkDownMbps, c.NetworkUpMbps, false), "", "disk"), nil
		}
	}
	imageID, boot := InstallationMedia(c)
	iso := ""
	if imageID != "" {
		if FindImage(imageID) == nil {
			return "", fmt.Errorf("installation image not found: %s", imageID)
		}
		iso = ImagePath(imageID)
		if err := ValidateISOFile(iso); err != nil {
			return "", fmt.Errorf("installation media unavailable: %w", err)
		}
	}
	if c.ManualInstall || IsManualISOImage(c.Template) {
		return manualDomainXML(c.VirshName(), int(c.VCPU), c.RAMMB, c.DiskImage, iso, c.MACAddress, c.IOReadMBps, c.IOWriteMBps, c.NetworkDownMbps, c.NetworkUpMbps, boot), nil
	}
	if IsWindowsImage(c.Template) {
		if c.InstallationMediaBoot == "" {
			return windowsDomainXML(c.VirshName(), int(c.VCPU), c.RAMMB, c.DiskImage, windowsInstallISOPath(c.Template), existingWindowsUnattendISO(m.instanceDir(c.VirshName())), c.MACAddress, c.IOReadMBps, c.IOWriteMBps, c.NetworkDownMbps, c.NetworkUpMbps), nil
		}
		xml := windowsDomainXML(c.VirshName(), int(c.VCPU), c.RAMMB, c.DiskImage, "", "", c.MACAddress, c.IOReadMBps, c.IOWriteMBps, c.NetworkDownMbps, c.NetworkUpMbps)
		return withInstallationMedia(xml, iso, boot), nil
	}
	xml := domainXML(c.VirshName(), int(c.VCPU), c.RAMMB, c.DiskImage, filepath.Join(m.instanceDir(c.VirshName()), "seed.iso"), c.MACAddress, c.IOReadMBps, c.IOWriteMBps, c.NetworkDownMbps, c.NetworkUpMbps, isKVMDesktopTemplate(c.Template))
	if c.InstallationMediaBoot != "" {
		xml = withInstallationMedia(xml, iso, boot)
	}
	return xml, nil
}

// SetInstallationMedia requires the caller to exclude concurrent queued power
// operations. Changes only the stopped domain; it never starts or stops a guest.
func (m *Manager) SetInstallationMedia(id int, imageID, boot string) error {
	installationMediaMu.Lock()
	defer installationMediaMu.Unlock()
	stored := config.FindContainer(id)
	if stored == nil || !stored.IsKVM() {
		return fmt.Errorf("KVM container not found")
	}
	previous := *stored
	if boot != "disk" && boot != "cdrom" {
		return fmt.Errorf("boot must be disk or cdrom")
	}
	if boot == "cdrom" && imageID == "" {
		return fmt.Errorf("CD-ROM boot requires installation media")
	}
	if imageID != "" {
		image := FindImage(imageID)
		if image == nil || image.Arch != runtime.GOARCH {
			return fmt.Errorf("installation image not found or architecture mismatch")
		}
		if err := ValidateISOFile(ImagePath(imageID)); err != nil {
			return err
		}
	}
	state, err := m.GetContainerStatus(previous.VirshName())
	if err != nil {
		return fmt.Errorf("cannot inspect VM state: %w", err)
	}
	if state != "stopped" {
		return fmt.Errorf("VM must be stopped before changing installation media")
	}
	next := previous
	next.InstallationMediaImageID, next.InstallationMediaBoot = imageID, boot
	if err := m.ensureDomainDefinition(&next); err != nil {
		return err
	}
	current := config.FindContainer(id)
	if current == nil {
		return fmt.Errorf("container disappeared")
	}
	current.InstallationMediaImageID, current.InstallationMediaBoot = imageID, boot
	if err := config.SaveConfig(); err != nil {
		current.InstallationMediaImageID, current.InstallationMediaBoot = previous.InstallationMediaImageID, previous.InstallationMediaBoot
		if rollbackErr := m.ensureDomainDefinition(&previous); rollbackErr != nil {
			return fmt.Errorf("save failed: %v; definition rollback failed: %w", err, rollbackErr)
		}
		return err
	}
	return nil
}

// An explicit destructive reinstall starts a new installation. Ordinary starts
// and resource updates keep the existing media selection (including eject).
func resetReinstalledMedia(current, next *config.Container) {
	current.Template = next.Template
	current.Standalone = next.Standalone
	current.ManualInstall = next.ManualInstall
	current.InstallationMediaImageID = next.InstallationMediaImageID
	current.InstallationMediaBoot = next.InstallationMediaBoot
}
