package kvm

import (
	"os"
	"path/filepath"
	"runtime"

	"clicd/internal/config"
)

type Image struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Distro      string `json:"distro"`
	Release     string `json:"release"`
	Arch        string `json:"arch"`
	Description string `json:"description"`
	URL         string `json:"url"`
	Desktop     string `json:"desktop,omitempty"`
	Provisioner string `json:"provisioner,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
	Custom      bool   `json:"custom,omitempty"`
}

func GetImages() []Image {
	var images []Image
	switch runtime.GOARCH {
	case "arm64":
		images = arm64Images()
	default:
		images = amd64Images()
	}
	for _, custom := range config.ListCustomKVMImages() {
		if custom.Arch != runtime.GOARCH {
			continue
		}
		images = append(images, Image{
			ID:          custom.ID,
			Name:        custom.Name,
			Distro:      custom.Distro,
			Release:     custom.Release,
			Arch:        custom.Arch,
			Description: custom.Description,
			URL:         custom.URL,
			Provisioner: custom.Provisioner,
			SHA256:      custom.SHA256,
			Custom:      true,
		})
	}
	return images
}

func amd64Images() []Image {
	return []Image{
		{
			ID: "kvm-ubuntu-noble", Name: "Ubuntu 24.04 KVM",
			Distro: "ubuntu", Release: "noble", Arch: "amd64",
			Description: "Ubuntu 24.04 LTS cloud image for KVM",
			URL:         "https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img",
		},
		{
			ID: "kvm-ubuntu-noble-xfce", Name: "Ubuntu 24.04 XFCE KVM",
			Distro: "ubuntu", Release: "noble", Arch: "amd64",
			Description: "Ubuntu 24.04 LTS cloud image with XFCE desktop provisioned via cloud-init",
			URL:         "https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img",
			Desktop:     "xfce",
		},
		{
			ID: "kvm-ubuntu-jammy", Name: "Ubuntu 22.04 KVM",
			Distro: "ubuntu", Release: "jammy", Arch: "amd64",
			Description: "Ubuntu 22.04 LTS cloud image for KVM",
			URL:         "https://cloud-images.ubuntu.com/jammy/current/jammy-server-cloudimg-amd64.img",
		},
		{
			ID: "kvm-debian-trixie", Name: "Debian 13 KVM",
			Distro: "debian", Release: "trixie", Arch: "amd64",
			Description: "Debian 13 generic cloud image for KVM",
			URL:         "https://cloud.debian.org/images/cloud/trixie/latest/debian-13-genericcloud-amd64.qcow2",
		},
		{
			ID: "kvm-debian-bookworm", Name: "Debian 12 KVM",
			Distro: "debian", Release: "bookworm", Arch: "amd64",
			Description: "Debian 12 generic cloud image for KVM",
			URL:         "https://cloud.debian.org/images/cloud/bookworm/latest/debian-12-genericcloud-amd64.qcow2",
		},
		{
			ID: "kvm-debian-trixie-xfce", Name: "Debian 13 XFCE KVM",
			Distro: "debian", Release: "trixie", Arch: "amd64",
			Description: "Debian 13 generic cloud image with XFCE desktop provisioned via cloud-init",
			URL:         "https://cloud.debian.org/images/cloud/trixie/latest/debian-13-genericcloud-amd64.qcow2",
			Desktop:     "xfce",
		},
		{
			ID: "kvm-debian-bookworm-xfce", Name: "Debian 12 XFCE KVM",
			Distro: "debian", Release: "bookworm", Arch: "amd64",
			Description: "Debian 12 generic cloud image with XFCE desktop provisioned via cloud-init",
			URL:         "https://cloud.debian.org/images/cloud/bookworm/latest/debian-12-genericcloud-amd64.qcow2",
			Desktop:     "xfce",
		},
		{
			ID: "kvm-debian-bullseye", Name: "Debian 11 KVM",
			Distro: "debian", Release: "bullseye", Arch: "amd64",
			Description: "Debian 11 generic cloud image for KVM",
			URL:         "https://cloud.debian.org/images/cloud/bullseye/latest/debian-11-genericcloud-amd64.qcow2",
		},
		{
			ID: "kvm-alpine-3.23", Name: "Alpine 3.23 KVM",
			Distro: "alpine", Release: "3.23", Arch: "amd64",
			Description: "Alpine Linux 3.23 NoCloud cloud-init image for KVM",
			URL:         "https://dev.alpinelinux.org/~tomalok/alpine-cloud-images/v3.23/nocloud/x86_64/nocloud_alpine-3.23.4-x86_64-bios-cloudinit-r0.qcow2",
		},
		{
			ID: "kvm-centos-9-stream", Name: "CentOS Stream 9 KVM",
			Distro: "centos", Release: "9-stream", Arch: "amd64",
			Description: "CentOS Stream 9 GenericCloud image for KVM",
			URL:         "https://cloud.centos.org/centos/9-stream/x86_64/images/CentOS-Stream-GenericCloud-9-latest.x86_64.qcow2",
		},
		{
			ID: "kvm-archlinux-current", Name: "Arch Linux KVM",
			Distro: "archlinux", Release: "current", Arch: "amd64",
			Description: "Arch Linux (Rolling) cloud image for KVM",
			URL:         "https://geo.mirror.pkgbuild.com/images/latest/Arch-Linux-x86_64-cloudimg.qcow2",
		},
		{
			ID: "kvm-fedora-44", Name: "Fedora 44 KVM",
			Distro: "fedora", Release: "44", Arch: "amd64",
			Description: "Fedora 44 GenericCloud image for KVM",
			URL:         "https://download.fedoraproject.org/pub/fedora/linux/releases/44/Cloud/x86_64/images/Fedora-Cloud-Base-Generic-44-1.7.x86_64.qcow2",
		},
		{
			ID: "kvm-rockylinux-9", Name: "Rocky Linux 9 KVM",
			Distro: "rockylinux", Release: "9", Arch: "amd64",
			Description: "Rocky Linux 9 GenericCloud image for KVM",
			URL:         "https://dl.rockylinux.org/pub/rocky/9/images/x86_64/Rocky-9-GenericCloud-Base.latest.x86_64.qcow2",
		},
		{
			ID: "kvm-windows-11", Name: "Windows 11 KVM",
			Distro: "windows", Release: "11", Arch: "amd64",
			Description: "Windows 11 Enterprise LTSC 2024 Evaluation",
			URL:         "https://go.microsoft.com/fwlink/?clcid=0x409&country=us&culture=en-us&linkid=2289029",
		},
		{
			ID: "kvm-windows-10", Name: "Windows 10 KVM",
			Distro: "windows", Release: "10", Arch: "amd64",
			Description: "Windows 10 Enterprise LTSC Evaluation",
			URL:         "https://go.microsoft.com/fwlink/?LinkID=2195404",
		},
	}
}

func arm64Images() []Image {
	return []Image{
		{
			ID: "kvm-ubuntu-noble", Name: "Ubuntu 24.04 KVM",
			Distro: "ubuntu", Release: "noble", Arch: "arm64",
			Description: "Ubuntu 24.04 LTS cloud image for ARM64 KVM",
			URL:         "https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-arm64.img",
		},
		{
			ID: "kvm-ubuntu-jammy", Name: "Ubuntu 22.04 KVM",
			Distro: "ubuntu", Release: "jammy", Arch: "arm64",
			Description: "Ubuntu 22.04 LTS cloud image for ARM64 KVM",
			URL:         "https://cloud-images.ubuntu.com/jammy/current/jammy-server-cloudimg-arm64.img",
		},
		{
			ID: "kvm-debian-trixie", Name: "Debian 13 KVM",
			Distro: "debian", Release: "trixie", Arch: "arm64",
			Description: "Debian 13 generic cloud image for ARM64 KVM",
			URL:         "https://cloud.debian.org/images/cloud/trixie/latest/debian-13-genericcloud-arm64.qcow2",
		},
		{
			ID: "kvm-debian-bookworm", Name: "Debian 12 KVM",
			Distro: "debian", Release: "bookworm", Arch: "arm64",
			Description: "Debian 12 generic cloud image for ARM64 KVM",
			URL:         "https://cloud.debian.org/images/cloud/bookworm/latest/debian-12-genericcloud-arm64.qcow2",
		},
		{
			ID: "kvm-debian-bullseye", Name: "Debian 11 KVM",
			Distro: "debian", Release: "bullseye", Arch: "arm64",
			Description: "Debian 11 generic cloud image for ARM64 KVM",
			URL:         "https://cloud.debian.org/images/cloud/bullseye/latest/debian-11-genericcloud-arm64.qcow2",
		},
		{
			ID: "kvm-centos-9-stream", Name: "CentOS Stream 9 KVM",
			Distro: "centos", Release: "9-stream", Arch: "arm64",
			Description: "CentOS Stream 9 GenericCloud image for ARM64 KVM",
			URL:         "https://cloud.centos.org/centos/9-stream/aarch64/images/CentOS-Stream-GenericCloud-9-latest.aarch64.qcow2",
		},
		{
			ID: "kvm-fedora-44", Name: "Fedora 44 KVM",
			Distro: "fedora", Release: "44", Arch: "arm64",
			Description: "Fedora 44 GenericCloud image for ARM64 KVM",
			URL:         "https://download.fedoraproject.org/pub/fedora/linux/releases/44/Cloud/aarch64/images/Fedora-Cloud-Base-Generic-44-1.7.aarch64.qcow2",
		},
		{
			ID: "kvm-rockylinux-9", Name: "Rocky Linux 9 KVM",
			Distro: "rockylinux", Release: "9", Arch: "arm64",
			Description: "Rocky Linux 9 GenericCloud image for ARM64 KVM",
			URL:         "https://dl.rockylinux.org/pub/rocky/9/images/aarch64/Rocky-9-GenericCloud-Base.latest.aarch64.qcow2",
		},
	}
}

func FindImage(id string) *Image {
	for _, image := range GetImages() {
		if image.ID == id {
			return &image
		}
	}
	return nil
}

func CacheDir() string {
	if pool := config.PreferredStoragePoolForContent(config.StorageContentImages); pool != nil {
		return filepath.Join(pool.Path, "images", "kvm")
	}
	return filepath.Join(BaseDir(), "images")
}

func ImagePath(id string) string {
	img := FindImage(id)
	ext := ".qcow2"
	safeID := "__invalid_image_id__"
	if img != nil {
		safeID = img.ID
	}
	if img != nil && img.IsWindows() {
		ext = ".iso"
	}
	fileName := safeID + ext
	for _, pool := range config.StoragePoolsForContent(config.StorageContentImages) {
		candidate := filepath.Join(pool.Path, "images", "kvm", fileName)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	legacy := filepath.Join("/var/lib/clicd/kvm/images", fileName)
	if info, err := os.Stat(legacy); err == nil && !info.IsDir() {
		return legacy
	}
	return filepath.Join(CacheDir(), fileName)
}

func (image Image) IsWindows() bool {
	return image.Provisioner == config.KVMProvisionerWindows10 ||
		image.Provisioner == config.KVMProvisionerWindows11 ||
		(image.Provisioner == "" && image.Distro == "windows")
}

func (image Image) IsWindows11() bool {
	return image.Provisioner == config.KVMProvisionerWindows11 ||
		(image.Provisioner == "" && image.Distro == "windows" && image.Release == "11")
}

// IsWindowsImage returns true if the image uses Windows unattended installation.
func IsWindowsImage(id string) bool {
	img := FindImage(id)
	return img != nil && img.IsWindows()
}

func IsWindows11Image(id string) bool {
	img := FindImage(id)
	return img != nil && img.IsWindows11()
}

func virtioWinISOPath() string {
	return filepath.Join(CacheDir(), "virtio-win.iso")
}
