package kvm

import (
	"testing"

	"clicd/internal/config"
)

// Custom images let users type the distro themselves, and a stale
// "linux-cloud-init" provisioner used to mask a Windows distro entirely: the
// VM then went down the Linux start path, which waits for a guest IPv4
// address that a Windows guest never reports, and the create task failed
// after ~3 minutes.
func TestImageIsWindowsDetectsCustomDistroDespiteProvisioner(t *testing.T) {
	cases := []struct {
		name        string
		distro      string
		provisioner string
		want        bool
	}{
		{"windows with stale linux provisioner", "windows", "linux-cloud-init", true},
		{"win10 short distro", "win10", "linux-cloud-init", true},
		{"win11 short distro", "win11", "", true},
		{"windows no provisioner", "windows", "", true},
		{"mixed case", "Windows", "Linux-Cloud-Init", true},
		{"explicit windows-10", "windows", config.KVMProvisionerWindows10, true},
		{"explicit windows-11", "windows", config.KVMProvisionerWindows11, true},
		{"normalized windows provisioner", "ubuntu", " Windows-11 ", true},
		{"linux distro", "ubuntu", "linux-cloud-init", false},
		{"linux distro no provisioner", "debian", "", false},
		{"empty", "", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			img := Image{Distro: tc.distro, Provisioner: tc.provisioner}
			if got := img.IsWindows(); got != tc.want {
				t.Fatalf("IsWindows() = %v, want %v (distro=%q provisioner=%q)", got, tc.want, tc.distro, tc.provisioner)
			}
		})
	}
}

func TestImageIsWindows11UsesSameDetectionAsWindows(t *testing.T) {
	for _, tc := range []struct {
		name, distro, release, provisioner string
		want                               bool
	}{
		{"short distro", "win11", "", "linux-cloud-init", true},
		{"mixed case distro", " Windows ", " 11 ", "linux-cloud-init", true},
		{"normalized provisioner", "", "", " Windows-11 ", true},
		{"explicit Windows 10 wins", "win11", "11", " Windows-10 ", false},
		{"Windows 10", "win10", "10", "linux-cloud-init", false},
		{"Linux release 11", "debian", "11", "linux-cloud-init", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			image := Image{Distro: tc.distro, Release: tc.release, Provisioner: tc.provisioner}
			if got := image.IsWindows11(); got != tc.want {
				t.Fatalf("IsWindows11(%+v) = %v, want %v", image, got, tc.want)
			}
		})
	}
}
