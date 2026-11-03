package kvm

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"clicd/internal/config"
	"clicd/internal/lxc"
)

func manualISOFixture(t *testing.T) (string, *config.Container) {
	t.Helper()
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	pool := t.TempDir()
	config.AppConfig = &config.ClicdConfig{StoragePools: []config.StoragePool{{ID: "test", Path: pool, Enabled: true, ContentTypes: []string{config.StorageContentImages, config.StorageContentKVM}, DefaultContents: []string{config.StorageContentImages, config.StorageContentKVM}}}, CustomKVMImages: []config.CustomKVMImage{{ID: "manual-test", Name: "Rescue", Distro: "windows", Arch: runtime.GOARCH, Provisioner: config.KVMProvisionerManualISO}}}
	path := ImagePath("manual-test")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 17*2048)
	copy(data[16*2048+1:], "CD001")
	data[16*2048+6] = 1
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	c := &config.Container{ID: 13, Name: "manual", KVMName: "vm-13", Virtualization: config.VirtualizationKVM, Template: "manual-test", ManualInstall: true, InstallationMediaImageID: "manual-test", InstallationMediaBoot: "cdrom", DiskImage: filepath.Join(pool, "vm-13", "disk.qcow2"), MACAddress: "52:54:00:11:22:33", VCPU: 1, RAMMB: 512, DiskGB: 4, Status: "stopped"}
	return path, c
}

func TestManualISOValidationRejectsDisguisedDownloads(t *testing.T) {
	path, _ := manualISOFixture(t)
	if err := ValidateISOFile(path); err != nil {
		t.Fatal(err)
	}
	for _, content := range [][]byte{[]byte("<html>login page</html>"), []byte("QFI\xfb qcow2"), make([]byte, 18*2048)} {
		if err := os.WriteFile(path, content, 0644); err != nil {
			t.Fatal(err)
		}
		if err := ValidateISOFile(path); err == nil {
			t.Fatal("non-ISO accepted")
		}
		if ok, _ := ImageDownloadedInfo("manual-test"); ok {
			t.Fatal("invalid ISO exposed as downloaded")
		}
	}
	// A disk with a forged volume descriptor must still fail its QCOW2 header.
	data := make([]byte, 17*2048)
	copy(data, "QFI\xfb")
	copy(data[16*2048+1:], "CD001")
	data[16*2048+6] = 1
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	if ValidateISOFile(path) == nil {
		t.Fatal("QCOW2 with forged descriptor accepted")
	}
}

func TestManualISOUsesDeviceBootOrdersAndEjectSurvivesRegistryChange(t *testing.T) {
	_, c := manualISOFixture(t)
	image := FindImage(c.Template)
	if image.IsWindows() || image.IsWindows11() || !image.IsManualISO() || image.downloadExtension() != ".iso" {
		t.Fatal("manual Windows ISO followed unattended path")
	}
	if GetKVMImageMinDiskGB(c.Template) != 1 {
		t.Fatal("manual ISO inherited cloud disk minimum")
	}
	m := NewManager()
	for _, boot := range []string{"cdrom", "disk"} {
		c.InstallationMediaBoot = boot
		text, err := m.containerDomainXML(c)
		if err != nil {
			t.Fatal(err)
		}
		var domain struct {
			OSBoot []struct{} `xml:"os>boot"`
			Disks  []struct {
				Device string `xml:"device,attr"`
				Source struct {
					File string `xml:"file,attr"`
				} `xml:"source"`
				Boot struct {
					Order int `xml:"order,attr"`
				} `xml:"boot"`
			} `xml:"devices>disk"`
			Memory   int        `xml:"memory"`
			Serial   []struct{} `xml:"devices>serial"`
			Graphics []struct{} `xml:"devices>graphics"`
		}
		if err := xml.Unmarshal([]byte(text), &domain); err != nil {
			t.Fatal(err)
		}
		if len(domain.OSBoot) != 0 || len(domain.Disks) != 2 || domain.Memory != 512 || len(domain.Serial) != 1 || len(domain.Graphics) != 1 {
			t.Fatalf("unexpected manual domain: %s", text)
		}
		diskOrder, cdOrder := 1, 2
		if boot == "cdrom" {
			diskOrder, cdOrder = 2, 1
		}
		if domain.Disks[0].Boot.Order != diskOrder || domain.Disks[1].Boot.Order != cdOrder {
			t.Fatal("incorrect boot ordering")
		}
		if strings.Contains(text, "seed.iso") || strings.Contains(text, "unattend") || strings.Contains(text, "virtio-win") || strings.Contains(text, "file=''") {
			t.Fatal("automatic installation artifacts attached")
		}
	}
	c.InstallationMediaImageID = ""
	c.InstallationMediaBoot = "disk"
	config.AppConfig.CustomKVMImages = nil
	text, err := m.containerDomainXML(c)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "device='cdrom'") || strings.Contains(text, "manual-test") {
		t.Fatal("ejected ISO remounted after registry change")
	}
}

func TestMountedISODeletionBlockedButEjectedManualSourceCanBeDeleted(t *testing.T) {
	path, c := manualISOFixture(t)
	config.AppConfig.Containers = []config.Container{*c}
	if DeleteImage(c.Template) == nil {
		t.Fatal("mounted installation media deleted")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	config.AppConfig.Containers[0].InstallationMediaImageID = ""
	config.AppConfig.Containers[0].InstallationMediaBoot = "disk"
	if err := DeleteImage(c.Template); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("ejected source was not removed")
	}
}

func TestDefineManualISOSkipsSSHAndAutomaticMedia(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake host commands use POSIX shell")
	}
	path, _ := manualISOFixture(t)
	commands := t.TempDir()
	log := filepath.Join(commands, "commands.log")
	writeKVMTestCommand(t, commands, "qemu-img", "printf '%s\\n' \"$*\" >> \"$KVM_COMMAND_LOG\"\n")
	writeKVMTestCommand(t, commands, "virsh", "exit 0\n")
	t.Setenv("PATH", commands+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("KVM_COMMAND_LOG", log)
	m := &Manager{BasePath: filepath.Join(t.TempDir(), "kvm")}
	c, err := m.defineContainer(13, "vm-13", lxc.ContainerConfig{Name: "manual", TemplateID: "manual-test", VCPU: 1, RAMMB: 512, DiskGB: 4, SSHAuthMode: "key", SSHPublicKey: "invalid-key"}, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if c.SSHPassword != "" || !c.ManualInstall || c.InstallationMediaImageID != "manual-test" || c.InstallationMediaBoot != "cdrom" {
		t.Fatalf("invalid persisted state: %+v", c)
	}
	content, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), " -b ") || strings.Contains(string(content), path) {
		t.Fatal("ISO used as a backing disk")
	}
	text, err := os.ReadFile(filepath.Join(m.instanceDir("vm-13"), "domain.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(text), path) || strings.Contains(string(text), "unattend") || strings.Contains(string(text), "seed.iso") {
		t.Fatal("manual ISO definition incorrect")
	}
}

func TestMediaChangeFailsClosedForPausedOrShuttingDownDomain(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake host commands use POSIX shell")
	}
	_, c := manualISOFixture(t)
	config.AppConfig.Containers = []config.Container{*c}
	commands := t.TempDir()
	writeKVMTestCommand(t, commands, "virsh", "case \"$1\" in\ndomstate) echo \"$KVM_TEST_STATE\";;\n*) exit 21;;\nesac\n")
	t.Setenv("PATH", commands+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, state := range []string{"running", "paused", "in shutdown", "pmsuspended", "unknown"} {
		t.Setenv("KVM_TEST_STATE", state)
		if err := NewManager().SetInstallationMedia(13, "", "disk"); err == nil || !strings.Contains(err.Error(), "must be stopped") {
			t.Fatalf("state %q: %v", state, err)
		}
		if config.AppConfig.Containers[0].InstallationMediaImageID != "manual-test" {
			t.Fatal("rejected request changed state")
		}
	}
}

func TestExplicitReinstallUsesNewTemplateMediaAfterEjectOrRescueMount(t *testing.T) {
	path, c := manualISOFixture(t)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	config.AppConfig.CustomKVMImages = append(config.AppConfig.CustomKVMImages, config.CustomKVMImage{ID: "new-manual", Name: "New ISO", Arch: runtime.GOARCH, Provisioner: config.KVMProvisionerManualISO})
	if err := os.WriteFile(ImagePath("new-manual"), content, 0644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ oldMedia, target string }{{"", "manual-test"}, {"", "new-manual"}, {"new-manual", "manual-test"}} {
		current := *c
		current.InstallationMediaImageID = tc.oldMedia
		current.InstallationMediaBoot = "disk"
		next := *c
		next.Template = tc.target
		next.InstallationMediaImageID = tc.target
		next.InstallationMediaBoot = "cdrom"
		resetReinstalledMedia(&current, &next)
		text, err := NewManager().containerDomainXML(&current)
		if err != nil {
			t.Fatal(err)
		}
		if current.Template != tc.target || current.InstallationMediaImageID != tc.target || current.InstallationMediaBoot != "cdrom" || !strings.Contains(text, "file='"+xmlEscape(ImagePath(tc.target))+"'") {
			t.Fatalf("reinstall retained old media: %+v", current)
		}
		if strings.Count(text, "device='cdrom'") != 1 {
			t.Fatal("reinstall did not attach exactly its target ISO")
		}
	}
}

func TestPersistedManualModeDoesNotRequireSourceRegistryOrCloudTools(t *testing.T) {
	_, c := manualISOFixture(t)
	c.InstallationMediaImageID = ""
	c.InstallationMediaBoot = "disk"
	config.AppConfig.CustomKVMImages = nil
	t.Setenv("PATH", t.TempDir())
	if err := validateProvisioningTools(c.Template, c.ManualInstall); err != nil {
		t.Fatalf("ejected manual VM required provisioning tools: %v", err)
	}
	if _, err := NewManager().containerDomainXML(c); err != nil {
		t.Fatal(err)
	}
	if err := validateProvisioningTools(c.Template, false); err == nil || !strings.Contains(err.Error(), "cloud-localds") {
		t.Fatalf("cloud provisioning dependency was accidentally skipped: %v", err)
	}
}
