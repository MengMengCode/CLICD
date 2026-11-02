package kvm

import (
	"context"
	"encoding/xml"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"clicd/internal/config"
	"clicd/internal/lxc"

	"golang.org/x/crypto/ssh"
)

func customWindowsDiskConfig(t *testing.T, ext string, content []byte) string {
	t.Helper()
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	pool := t.TempDir()
	config.AppConfig = &config.ClicdConfig{
		StoragePools: []config.StoragePool{{ID: "test", Path: pool, Enabled: true,
			ContentTypes:    []string{config.StorageContentImages, config.StorageContentKVM},
			DefaultContents: []string{config.StorageContentImages, config.StorageContentKVM}}},
		CustomKVMImages: []config.CustomKVMImage{{ID: "custom-windows-disk", Arch: runtime.GOARCH,
			Distro: "win11", Provisioner: config.KVMProvisionerLinuxCloudInit,
			URL: "https://example.test/windows" + ext}},
	}
	path := filepath.Join(pool, "images", "kvm", "custom-windows-disk"+ext)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestWindowsMediaFormatControlsBootDevices(t *testing.T) {
	for _, tc := range []struct {
		name, ext string
		content   []byte
		cdroms    int
	}{
		{"golden disk", ".qcow2", []byte{'Q', 'F', 'I', 0xfb}, 0},
		{"QCOW2 header despite ISO filename", ".iso", []byte{'Q', 'F', 'I', 0xfb}, 0},
		{"installation media", ".iso", []byte("installation media"), 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := customWindowsDiskConfig(t, tc.ext, tc.content)
			if got := ImagePath("custom-windows-disk"); got != path {
				t.Fatalf("ImagePath() = %q, want %q", got, path)
			}
			installISO := windowsInstallISOPath("custom-windows-disk")
			xmlText := windowsDomainXML("test-windows", 2, 4096, "/instance/disk.qcow2", installISO, "/instance/unattend.iso", "52:54:00:11:22:33", 0, 0, 0, 0)
			var domain struct {
				Disks []struct {
					Device string `xml:"device,attr"`
					Driver struct {
						Type string `xml:"type,attr"`
					} `xml:"driver"`
					Source struct {
						File string `xml:"file,attr"`
					} `xml:"source"`
					Boot struct {
						Order int `xml:"order,attr"`
					} `xml:"boot"`
				} `xml:"devices>disk"`
			}
			if err := xml.Unmarshal([]byte(xmlText), &domain); err != nil {
				t.Fatal(err)
			}
			if len(domain.Disks) != tc.cdroms+1 {
				t.Fatalf("got %d disks, want system disk + %d CD-ROMs: %s", len(domain.Disks), tc.cdroms, xmlText)
			}
			if domain.Disks[0].Driver.Type != "qcow2" || domain.Disks[0].Source.File != "/instance/disk.qcow2" {
				t.Fatalf("invalid system disk: %+v", domain.Disks[0])
			}
			if tc.cdroms == 0 && domain.Disks[0].Boot.Order != 1 {
				t.Fatal("golden system disk is not first in boot order")
			}
			if tc.cdroms != 0 && (domain.Disks[1].Source.File != path || domain.Disks[1].Device != "cdrom" || domain.Disks[1].Boot.Order != 1) {
				t.Fatalf("installation ISO is not the boot CD-ROM: %+v", domain.Disks[1])
			}
		})
	}
}

func TestDefineWindowsQCOW2CreatesOverlayWithoutInventedPassword(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake host commands use a POSIX shell")
	}
	base := customWindowsDiskConfig(t, ".qcow2", []byte{'Q', 'F', 'I', 0xfb})
	commands := t.TempDir()
	log := filepath.Join(commands, "qemu.log")
	writeKVMTestCommand(t, commands, "qemu-img", "printf '%s\\n' \"$*\" >> \"$KVM_COMMAND_LOG\"\n")
	writeKVMTestCommand(t, commands, "virsh", "exit 0\n")
	t.Setenv("PATH", commands+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("KVM_COMMAND_LOG", log)
	m := &Manager{BasePath: filepath.Join(t.TempDir(), "kvm")}
	c, err := m.defineContainer(13, "vm-13", lxc.ContainerConfig{Name: "golden", TemplateID: "custom-windows-disk", VCPU: 2, RAMMB: 4096, DiskGB: 64, StoragePoolID: "test"}, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if c.SSHPassword != "" {
		t.Fatalf("golden disk advertised a password that was never installed: %q", c.SSHPassword)
	}
	commandsRun, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(commandsRun), "-b "+base+" ") {
		t.Fatalf("disk was not created with the golden image as backing file: %s", commandsRun)
	}
	xmlText, err := os.ReadFile(filepath.Join(m.instanceDir("vm-13"), "domain.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(xmlText), "device='cdrom'") || strings.Contains(string(xmlText), base) {
		t.Fatalf("golden image was attached as installation media: %s", xmlText)
	}
}

func TestDestroyContainerPreservesDiskOnUnconfirmedShutdownOrUndefine(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake host commands use a POSIX shell")
	}
	for _, status := range []string{"paused", "shut off"} {
		t.Run(status, func(t *testing.T) {
			customWindowsDiskConfig(t, ".qcow2", []byte{'Q', 'F', 'I', 0xfb})
			commands := t.TempDir()
			writeKVMTestCommand(t, commands, "virsh", "case \"$1\" in\nlist) echo vm-13 ;;\ndomstate) printf '%s\\n' \"$KVM_TEST_STATE\" ;;\nundefine|destroy) exit 1 ;;\n*) exit 0 ;;\nesac\n")
			for _, name := range []string{"iptables", "ip6tables", "nft", "ip"} {
				writeKVMTestCommand(t, commands, name, "exit 0\n")
			}
			t.Setenv("PATH", commands+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("KVM_TEST_STATE", status)
			disk := filepath.Join(t.TempDir(), "vm-13", "disk.qcow2")
			if err := os.MkdirAll(filepath.Dir(disk), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(disk, []byte("guest data"), 0644); err != nil {
				t.Fatal(err)
			}
			config.AppConfig.Containers = []config.Container{{ID: 13, Name: "golden", KVMName: "vm-13", Virtualization: config.VirtualizationKVM, DiskImage: disk}}
			if err := NewManager().DestroyContainer(13); err == nil {
				t.Fatal("deletion succeeded despite an active domain or failed undefine")
			}
			if _, err := os.Stat(disk); err != nil {
				t.Fatalf("guest disk was removed: %v", err)
			}
			if config.FindContainer(13) == nil {
				t.Fatal("container config was removed")
			}
		})
	}
}

func TestCancelledGuestReadinessWaitsReturnPromptly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m := NewManager()
	start := time.Now()
	if _, err := m.waitForContainerIP(ctx, "vm-13", 90); !errors.Is(err, context.Canceled) {
		t.Fatalf("IP wait returned %v", err)
	}
	if err := m.waitForCloudInitReady(ctx, "vm-13", "192.0.2.1", "password"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cloud-init wait returned %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("cancelled readiness waits did not return promptly")
	}
}

func TestContainerStatusUsesCLocale(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake host commands use a POSIX shell")
	}
	commands := t.TempDir()
	writeKVMTestCommand(t, commands, "virsh", "if [ \"$LC_ALL:$LC_MESSAGES:$LANG:$LANGUAGE\" = C:C:C:C ]; then\n  echo 'shut off'\nelse\n  echo 'localized domain state'\nfi\n")
	t.Setenv("PATH", commands+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("LC_ALL", "zh_CN.UTF-8")
	t.Setenv("LANGUAGE", "zh_CN")
	status, err := NewManager().GetContainerStatus("vm-13")
	if err != nil || status != "stopped" {
		t.Fatalf("GetContainerStatus() = %q, %v; want stopped under a non-English host locale", status, err)
	}
}

func TestGuestReadinessSSHHandshakeHonorsCancellation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			accepted <- conn
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		client, err := dialReadinessSSH(ctx, listener.Addr().String(), &ssh.ClientConfig{User: "root", HostKeyCallback: tofuHostKeyCallback})
		if client != nil {
			client.Close()
		}
		done <- err
	}()
	select {
	case conn := <-accepted:
		defer conn.Close()
	case <-time.After(time.Second):
		t.Fatal("SSH client did not connect")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("SSH handshake unexpectedly succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("SSH handshake did not stop after cancellation")
	}
}

func writeKVMTestCommand(t *testing.T, dir, name, script string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script), 0755); err != nil {
		t.Fatal(err)
	}
}
