package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSQLiteConfigMigratesLegacyJSONAndPersists(t *testing.T) {
	resetConfigStoreForTest(t)

	dir := t.TempDir()
	t.Cleanup(func() {
		resetConfigStoreForTest(t)
	})
	legacyPath := filepath.Join(dir, "config.json")
	SetConfigPath(legacyPath)

	legacy := ClicdConfig{
		AdminUser:       "admin",
		AdminPassHash:   "hash",
		JWTSecret:       "secret",
		Port:            8999,
		DataDir:         dir,
		NextContainerID: 2,
		NextVNCPort:     5900,
		NextSSHPort:     22000,
		Containers: []Container{{
			ID:               1,
			UUID:             "uuid-1",
			Name:             "ct1",
			Virtualization:   "lxc",
			Template:         "debian-12",
			Status:           "running",
			PortMappingLimit: 2,
			SnapshotLimit:    3,
			PortMappings: []PortMapping{{
				ContainerPort: 22,
				HostPort:      22001,
				Protocol:      "tcp",
				Description:   "SSH",
			}},
		}},
		AuditLogs: []AuditLog{{
			Time:   "2026-06-07 17:29:00",
			Action: "security_horizontal_scan",
			Target: "ct1",
			Detail: "[medium] 可疑横向探测",
			User:   "system",
		}},
		LoginLogs: []SavedLoginLog{{
			Time:      "2026-06-07 17:29:01 CST",
			Username:  "admin",
			IP:        "127.0.0.1",
			UserAgent: "test",
			Success:   true,
		}},
		Tasks: []SavedTask{{
			ID:            "task-1",
			Type:          "create",
			ContainerName: "ct2",
			Status:        "pending",
			CreatedAt:     "2026-06-07 17:29:02",
			Config:        `{"name":"ct2","template_id":"debian-12","vcpu":1,"ram_mb":512,"disk_gb":5,"extra_ports":[80,443],"nat_port_mappings":[{"host_port":30080,"container_port":80,"protocol":"tcp","description":"HTTP"}],"management_port":30022,"assign_ipv6":true}`,
		}},
		EnabledImages: []string{"debian-12"},
		CustomKVMImages: []CustomKVMImage{{
			ID:          "custom-kvm-test",
			Name:        "Test Cloud Image",
			Description: "third-party image",
			Distro:      "ubuntu",
			Release:     "noble",
			Arch:        "amd64",
			URL:         "https://images.example.test/ubuntu.qcow2",
			Provisioner: KVMProvisionerLinuxCloudInit,
			SHA256:      strings.Repeat("a", 64),
			CreatedAt:   "2026-07-26 10:00:00",
		}},
		CustomLXCImages: []CustomLXCImage{{
			ID:          "custom-lxc-test",
			Name:        "Test Rootfs",
			Description: "third-party LXC image",
			Distro:      "alpine",
			Release:     "3.21",
			Arch:        "amd64",
			URL:         "https://images.example.test/alpine-rootfs.tar.xz",
			SHA256:      strings.Repeat("b", 64),
			CreatedAt:   "2026-07-26 10:00:00",
		}},
		PanelAccessPolicy: PanelAccessPolicy{
			Enabled:        true,
			AllowedSources: []string{"192.0.2.0/24"},
			TrustedProxies: []string{"127.0.0.1"},
		},
		Snapshots: []Snapshot{{
			ID:            "snap-1",
			ContainerID:   1,
			ContainerName: "ct1",
			LXCName:       "ct-1",
			CreatedAt:     "2026-06-07 17:30:00",
			Path:          filepath.Join(dir, "snap-1"),
		}},
	}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyPath, data, 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := InitConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Containers) != 1 || len(cfg.Containers[0].PortMappings) != 1 {
		t.Fatalf("legacy config was not migrated: %+v", cfg.Containers)
	}
	if len(cfg.Tasks) != 1 || !strings.Contains(cfg.Tasks[0].Config, `"extra_ports":[80,443]`) {
		t.Fatalf("task config was not restored from sqlite columns: %+v", cfg.Tasks)
	}
	if !strings.Contains(cfg.Tasks[0].Config, `"nat_port_mappings":[{"host_port":30080,"container_port":80`) {
		t.Fatalf("task NAT mappings were not restored from sqlite: %+v", cfg.Tasks)
	}
	if !strings.Contains(cfg.Tasks[0].Config, `"management_port":30022`) {
		t.Fatalf("task management port was not restored from sqlite: %+v", cfg.Tasks)
	}
	if cfg.TaskConcurrency != DefaultTaskConcurrency {
		t.Fatalf("legacy task concurrency = %d, want default %d", cfg.TaskConcurrency, DefaultTaskConcurrency)
	}
	if !cfg.PanelAccessPolicy.Enabled || len(cfg.PanelAccessPolicy.AllowedSources) != 1 {
		t.Fatalf("legacy panel access policy was not migrated: %+v", cfg.PanelAccessPolicy)
	}
	if len(cfg.CustomKVMImages) != 1 || cfg.CustomKVMImages[0].ID != "custom-kvm-test" {
		t.Fatalf("legacy custom KVM images were not migrated: %+v", cfg.CustomKVMImages)
	}
	if len(cfg.CustomLXCImages) != 1 || cfg.CustomLXCImages[0].ID != "custom-lxc-test" {
		t.Fatalf("legacy custom LXC images were not migrated: %+v", cfg.CustomLXCImages)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.db")); err != nil {
		t.Fatalf("sqlite database was not created: %v", err)
	}

	cfg.Containers[0].Status = "stopped"
	cfg.TaskConcurrency = 6
	if err := SaveConfig(); err != nil {
		t.Fatal(err)
	}

	resetConfigStoreForTest(t)
	SetConfigPath(legacyPath)
	cfg, err = InitConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Containers[0].Status; got != "stopped" {
		t.Fatalf("expected sqlite value to win after migration, got %q", got)
	}
	if got := cfg.TaskConcurrency; got != 6 {
		t.Fatalf("persisted task concurrency = %d, want 6", got)
	}
	if !cfg.PanelAccessPolicy.Enabled || cfg.PanelAccessPolicy.AllowedSources[0] != "192.0.2.0/24" {
		t.Fatalf("persisted panel access policy = %+v", cfg.PanelAccessPolicy)
	}
	if len(cfg.CustomKVMImages) != 1 || cfg.CustomKVMImages[0].SHA256 != strings.Repeat("a", 64) {
		t.Fatalf("persisted custom KVM images = %+v", cfg.CustomKVMImages)
	}
	if len(cfg.CustomLXCImages) != 1 || cfg.CustomLXCImages[0].SHA256 != strings.Repeat("b", 64) {
		t.Fatalf("persisted custom LXC images = %+v", cfg.CustomLXCImages)
	}
}

func resetConfigStoreForTest(t *testing.T) {
	t.Helper()
	if db != nil {
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		db = nil
	}
	AppConfig = nil
	configPath = ""
}
