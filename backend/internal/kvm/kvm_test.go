package kvm

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"clicd/internal/config"

	"golang.org/x/crypto/ssh"
)

func TestImagePathUsesAllowlistedImageID(t *testing.T) {
	for _, id := range []string{"", ".", "..", "../../etc/passwd", `..\\..\\windows`, "/absolute", "unknown-image"} {
		if got := filepath.Base(ImagePath(id)); got != "__invalid_image_id__.qcow2" {
			t.Fatalf("ImagePath(%q) basename = %q", id, got)
		}
	}
	validID := GetImages()[0].ID
	if got := filepath.Base(ImagePath(validID)); got != validID+".qcow2" {
		t.Fatalf("ImagePath(%q) basename = %q", validID, got)
	}
}

func TestWindows11ImageDefinition(t *testing.T) {
	image := FindImage("kvm-windows-11")
	if image == nil {
		t.Fatal("Windows 11 image is missing from the amd64 image list")
	}
	if image.Distro != "windows" || image.Release != "11" || image.Arch != "amd64" {
		t.Fatalf("Windows 11 image metadata = %+v", image)
	}
	if !strings.Contains(image.URL, "microsoft.com/fwlink/") {
		t.Fatalf("Windows 11 image does not use an official Microsoft URL: %s", image.URL)
	}
	if got := filepath.Base(ImagePath(image.ID)); got != "kvm-windows-11.iso" {
		t.Fatalf("Windows 11 image basename = %q", got)
	}
}

func TestWindows11UnattendAddsCompatibilityChecksOnlyForWindows11(t *testing.T) {
	windows11 := windowsAutounattendXML("win11-test", "Password123!", true)
	windows10 := windowsAutounattendXML("win10-test", "Password123!", false)

	for _, key := range []string{"BypassTPMCheck", "BypassSecureBootCheck", "BypassCPUCheck"} {
		if !strings.Contains(windows11, key) {
			t.Fatalf("Windows 11 unattend is missing %s", key)
		}
		if strings.Contains(windows10, key) {
			t.Fatalf("Windows 10 unattend unexpectedly contains %s", key)
		}
	}
	var document struct {
		XMLName xml.Name
	}
	if err := xml.Unmarshal([]byte(windows11), &document); err != nil {
		t.Fatalf("Windows 11 unattend XML is invalid: %v", err)
	}
}

func TestWindowsMinimumResources(t *testing.T) {
	if cpu, ram, disk := windowsMinimumResources("kvm-windows-11"); cpu != 2 || ram != 4096 || disk != 64 {
		t.Fatalf("Windows 11 minimums = %v vCPU, %d MB, %d GB", cpu, ram, disk)
	}
	if cpu, ram, disk := windowsMinimumResources("kvm-windows-10"); cpu != 1 || ram != 2048 || disk != 30 {
		t.Fatalf("Windows 10 minimums = %v vCPU, %d MB, %d GB", cpu, ram, disk)
	}
}

func TestLibvirtNetworkActiveParsesCLocaleOutput(t *testing.T) {
	tests := []struct {
		name string
		info string
		want bool
	}{
		{name: "active", info: "Name: default\nActive: yes\n", want: true},
		{name: "spacing and case", info: "  Active  :  YES  \r\n", want: true},
		{name: "inactive", info: "Name: default\nActive: no\n", want: false},
		{name: "missing field", info: "Name: default\nAutostart: yes\n", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := libvirtNetworkActive(tc.info); got != tc.want {
				t.Fatalf("libvirtNetworkActive(%q) = %v, want %v", tc.info, got, tc.want)
			}
		})
	}
}

func TestChpasswdStdinPreservesShellMetacharacters(t *testing.T) {
	password := `pa'";$(touch /tmp/pwned); echo #\\word`
	got, err := chpasswdStdin("root", password)
	if err != nil {
		t.Fatalf("chpasswdStdin returned error: %v", err)
	}

	want := []byte("root:" + password + "\n")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("chpasswdStdin = %#v, want %#v", got, want)
	}
}

func TestChpasswdStdinRejectsNewlines(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password string
	}{
		{name: "username newline", username: "root\nadmin", password: "safe"},
		{name: "username colon", username: "root:admin", password: "safe"},
		{name: "password newline", username: "root", password: "safe\nroot:evil"},
		{name: "password carriage return", username: "root", password: "safe\rroot:evil"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := chpasswdStdin(tc.username, tc.password); err == nil {
				t.Fatal("chpasswdStdin returned nil error")
			}
		})
	}
}

func TestVerifyKVMHostKeyCapturesAndRejectsMismatch(t *testing.T) {
	key1 := testSSHPublicKey(t)
	key2 := testSSHPublicKey(t)

	saves := 0
	c := &config.Container{}
	save := func() error {
		saves++
		return nil
	}

	if err := verifyKVMHostKey(c, key1, save); err != nil {
		t.Fatalf("first host key verification returned error: %v", err)
	}
	if c.SSHHostKey == "" {
		t.Fatal("first host key verification did not capture fingerprint")
	}
	if c.SSHHostKey != sshHostKeyFingerprint(key1) {
		t.Fatalf("captured fingerprint = %q, want %q", c.SSHHostKey, sshHostKeyFingerprint(key1))
	}
	if saves != 1 {
		t.Fatalf("save count = %d, want 1", saves)
	}

	if err := verifyKVMHostKey(c, key1, save); err != nil {
		t.Fatalf("same host key verification returned error: %v", err)
	}
	if saves != 1 {
		t.Fatalf("save count after same key = %d, want 1", saves)
	}

	if err := verifyKVMHostKey(c, key2, save); err == nil {
		t.Fatal("mismatched host key verification returned nil error")
	}
}

func TestGetImagesIncludesHostArchitectureCustomImage(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig = &config.ClicdConfig{
		CustomKVMImages: []config.CustomKVMImage{
			{
				ID:          "custom-kvm-linux",
				Name:        "Custom Linux",
				Distro:      "ubuntu",
				Release:     "noble",
				Arch:        runtime.GOARCH,
				URL:         "https://example.test/linux.qcow2",
				Provisioner: config.KVMProvisionerLinuxCloudInit,
			},
			{
				ID:          "custom-kvm-other-arch",
				Name:        "Other Architecture",
				Distro:      "ubuntu",
				Release:     "noble",
				Arch:        "not-" + runtime.GOARCH,
				URL:         "https://example.test/other.qcow2",
				Provisioner: config.KVMProvisionerLinuxCloudInit,
			},
		},
	}

	image := FindImage("custom-kvm-linux")
	if image == nil || !image.Custom || image.Provisioner != config.KVMProvisionerLinuxCloudInit {
		t.Fatalf("custom image was not exposed correctly: %+v", image)
	}
	if FindImage("custom-kvm-other-arch") != nil {
		t.Fatal("custom image for another architecture was exposed")
	}
}

func TestCustomWindowsProvisionerControlsImageType(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig = &config.ClicdConfig{CustomKVMImages: []config.CustomKVMImage{{
		ID:          "custom-kvm-windows",
		Name:        "Custom Windows",
		Distro:      "windows",
		Release:     "11",
		Arch:        runtime.GOARCH,
		URL:         "https://example.test/windows.iso",
		Provisioner: config.KVMProvisionerWindows11,
	}}}

	if !IsWindowsImage("custom-kvm-windows") || !IsWindows11Image("custom-kvm-windows") {
		t.Fatal("custom Windows 11 provisioner was not recognized")
	}
	if ext := filepath.Ext(ImagePath("custom-kvm-windows")); ext != ".iso" {
		t.Fatalf("custom Windows image extension = %q, want .iso", ext)
	}
}

func TestVerifyFileSHA256(t *testing.T) {
	path := filepath.Join(t.TempDir(), "image")
	content := []byte("clicd custom image")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	if err := verifyFileSHA256(path, hex.EncodeToString(sum[:])); err != nil {
		t.Fatalf("valid checksum failed: %v", err)
	}
	if err := verifyFileSHA256(path, strings.Repeat("0", 64)); err == nil {
		t.Fatal("invalid checksum unexpectedly passed")
	}
}

func testSSHPublicKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return signer.PublicKey()
}
