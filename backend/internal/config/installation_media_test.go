package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallationMediaSurvivesSaveAndReload(t *testing.T) {
	path := setupInstallationMediaStore(t)
	cfg, err := InitConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Containers = []Container{
		{ID: 1, UUID: "iso-mounted", Name: "installer", Virtualization: VirtualizationKVM, Template: "custom-iso", ManualInstall: true, InstallationMediaImageID: "custom-iso", InstallationMediaBoot: "cdrom", Status: "stopped", VCPU: 1, RAMMB: 512, DiskGB: 4},
		{ID: 2, UUID: "iso-ejected", Name: "installed", Virtualization: VirtualizationKVM, Template: "custom-iso", ManualInstall: true, InstallationMediaBoot: "disk", Status: "running", RestoreOnHostBoot: true, VCPU: 1, RAMMB: 512, DiskGB: 4},
		{ID: 3, UUID: "legacy-cloud", Name: "cloud", Virtualization: VirtualizationKVM, Template: "ubuntu-noble", Status: "stopped", VCPU: 1, RAMMB: 512, DiskGB: 8},
		{ID: 4, UUID: "recovery-media", Name: "recovery", Virtualization: VirtualizationKVM, Template: "ubuntu-noble", InstallationMediaImageID: "rescue-iso", InstallationMediaBoot: "disk", Status: "stopped", VCPU: 1, RAMMB: 512, DiskGB: 8},
	}
	want := append([]Container(nil), cfg.Containers...)
	if err := SaveConfig(); err != nil {
		t.Fatal(err)
	}
	resetConfigStoreForTest(t)
	SetConfigPath(path)
	cfg, err = InitConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Containers) != len(want) {
		t.Fatalf("lost containers: %d", len(cfg.Containers))
	}
	for i, expected := range want {
		actual := cfg.Containers[i]
		if actual.ManualInstall != expected.ManualInstall || actual.InstallationMediaImageID != expected.InstallationMediaImageID || actual.InstallationMediaBoot != expected.InstallationMediaBoot {
			t.Fatalf("media state changed for %s: %+v", expected.Name, actual)
		}
		if actual.Template != expected.Template || actual.Status != expected.Status || actual.RestoreOnHostBoot != expected.RestoreOnHostBoot {
			t.Fatalf("existing workload state changed for %s", expected.Name)
		}
	}
}

func TestInstallationMediaMigrationKeepsLegacyDefaults(t *testing.T) {
	path := setupInstallationMediaStore(t)
	cfg, err := InitConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Containers = []Container{{ID: 1, UUID: "legacy-guest", Name: "existing", Virtualization: VirtualizationKVM, Template: "kvm-windows-11", Status: "running", RestoreOnHostBoot: true, VCPU: 2, RAMMB: 4096, DiskGB: 64}}
	if err := SaveConfig(); err != nil {
		t.Fatal(err)
	}
	// Reproduce a database written before installation-media columns existed.
	for _, column := range []string{"manual_install", "installation_media_image_id", "installation_media_boot"} {
		if _, err := db.Exec("ALTER TABLE containers DROP COLUMN " + column); err != nil {
			t.Fatal(err)
		}
	}
	resetConfigStoreForTest(t)
	SetConfigPath(path)
	cfg, err = InitConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Containers) != 1 {
		t.Fatalf("legacy guest disappeared: %+v", cfg.Containers)
	}
	guest := cfg.Containers[0]
	if guest.ManualInstall || guest.InstallationMediaImageID != "" || guest.InstallationMediaBoot != "" {
		t.Fatalf("migration opted legacy guest into ISO management: %+v", guest)
	}
	if guest.Template != "kvm-windows-11" || guest.Status != "running" || !guest.RestoreOnHostBoot {
		t.Fatalf("migration changed existing boot behavior: %+v", guest)
	}
	if err := SaveConfig(); err != nil {
		t.Fatal(err)
	}
}

func setupInstallationMediaStore(t *testing.T) string {
	t.Helper()
	resetConfigStoreForTest(t)
	dir := t.TempDir()
	t.Cleanup(func() { resetConfigStoreForTest(t) })
	path := filepath.Join(dir, "config.json")
	data, err := json.Marshal(ClicdConfig{AdminUser: "test-admin", AdminPassHash: "test-hash", JWTSecret: "test-secret", Port: 8999, DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	SetConfigPath(path)
	return path
}
