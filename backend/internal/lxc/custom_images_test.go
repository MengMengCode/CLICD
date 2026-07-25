package lxc

import (
	"path/filepath"
	"runtime"
	"testing"

	"clicd/internal/config"
)

func TestGetTemplatesIncludesHostArchitectureCustomLXCImage(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig = &config.ClicdConfig{CustomLXCImages: []config.CustomLXCImage{
		{
			ID: "custom-lxc-host", Name: "Host Rootfs", Distro: "alpine",
			Release: "3.21", Arch: runtime.GOARCH, URL: "https://example.test/rootfs.tar.xz",
		},
		{
			ID: "custom-lxc-other", Name: "Other Rootfs", Distro: "alpine",
			Release: "3.21", Arch: "not-" + runtime.GOARCH, URL: "https://example.test/other.tar.xz",
		},
	}}

	template := FindTemplate("custom-lxc-host")
	if template == nil || !template.Custom || template.URL == "" {
		t.Fatalf("custom LXC template was not exposed correctly: %+v", template)
	}
	if FindTemplate("custom-lxc-other") != nil {
		t.Fatal("custom LXC template for another architecture was exposed")
	}
}

func TestCustomImagePathUsesAllowlistedID(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig = &config.ClicdConfig{}

	for _, id := range []string{"", ".", "..", "../../etc/passwd", "/absolute", "unknown"} {
		got := filepath.ToSlash(CustomImagePath(id))
		if filepath.Base(filepath.Dir(got)) != "__invalid_image_id__" {
			t.Fatalf("CustomImagePath(%q) = %q", id, got)
		}
	}
}

func TestValidateCustomRootfsEntries(t *testing.T) {
	if err := validateCustomRootfsEntries([]string{"./etc/", "./bin/", "./bin/sh"}); err != nil {
		t.Fatalf("valid rootfs entries failed: %v", err)
	}
	for _, entries := range [][]string{
		{},
		{"etc/passwd"},
		{"/etc/passwd", "bin/sh"},
		{"../../etc/passwd", "bin/sh"},
	} {
		if err := validateCustomRootfsEntries(entries); err == nil {
			t.Fatalf("unsafe rootfs entries unexpectedly passed: %#v", entries)
		}
	}
}
