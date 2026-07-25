package lxc

import (
	"runtime"

	"clicd/internal/config"
)

// Template represents an LXC image template
type Template struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Distro      string `json:"distro"`
	Release     string `json:"release"`
	Arch        string `json:"arch"`
	Variant     string `json:"variant"`
	Description string `json:"description"`
	URL         string `json:"url,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
	Custom      bool   `json:"custom,omitempty"`
}

// GetTemplates returns available LXC image templates (only verified working ones)
func GetTemplates() []Template {
	arch := defaultTemplateArch()
	templates := []Template{
		{
			ID: "ubuntu-noble", Name: "Ubuntu 24.04",
			Distro: "ubuntu", Release: "noble", Arch: arch,
			Description: "Ubuntu 24.04 LTS",
		},
		{
			ID: "ubuntu-jammy", Name: "Ubuntu 22.04",
			Distro: "ubuntu", Release: "jammy", Arch: arch,
			Description: "Ubuntu 22.04 LTS",
		},
		{
			ID: "debian-trixie", Name: "Debian 13",
			Distro: "debian", Release: "trixie", Arch: arch,
			Description: "Debian 13 (Trixie)",
		},
		{
			ID: "debian-bookworm", Name: "Debian 12",
			Distro: "debian", Release: "bookworm", Arch: arch,
			Description: "Debian 12 (Bookworm)",
		},
		{
			ID: "debian-bullseye", Name: "Debian 11",
			Distro: "debian", Release: "bullseye", Arch: arch,
			Description: "Debian 11 (Bullseye)",
		},
		{
			ID: "alpine-3.21", Name: "Alpine 3.21",
			Distro: "alpine", Release: "3.21", Arch: arch,
			Description: "Alpine Linux 3.21",
		},
		{
			ID: "centos-9-stream", Name: "CentOS 9 Stream",
			Distro: "centos", Release: "9-Stream", Arch: arch,
			Description: "CentOS 9 Stream",
		},
		{
			ID: "archlinux-current", Name: "Arch Linux",
			Distro: "archlinux", Release: "current", Arch: arch,
			Description: "Arch Linux (Rolling)",
		},
		{
			ID: "fedora-44", Name: "Fedora 44",
			Distro: "fedora", Release: "44", Arch: arch,
			Description: "Fedora 44",
		},
		{
			ID: "rockylinux-10", Name: "Rocky Linux 10",
			Distro: "rockylinux", Release: "10", Arch: arch,
			Description: "Rocky Linux 10",
		},
	}
	for _, custom := range config.ListCustomLXCImages() {
		if custom.Arch != arch {
			continue
		}
		templates = append(templates, Template{
			ID:          custom.ID,
			Name:        custom.Name,
			Distro:      custom.Distro,
			Release:     custom.Release,
			Arch:        custom.Arch,
			Description: custom.Description,
			URL:         custom.URL,
			SHA256:      custom.SHA256,
			Custom:      true,
		})
	}
	return templates
}

func defaultTemplateArch() string {
	switch runtime.GOARCH {
	case "arm64":
		return "arm64"
	default:
		return "amd64"
	}
}

// FindTemplate finds a template by ID
func FindTemplate(id string) *Template {
	templates := GetTemplates()
	for _, t := range templates {
		if t.ID == id {
			return &t
		}
	}
	return nil
}
