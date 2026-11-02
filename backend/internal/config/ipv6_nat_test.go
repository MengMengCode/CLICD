package config

import (
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

func TestNormalizeIPv6NATSettings(t *testing.T) {
	for _, subnet := range []string{"2606:4700::/64", "2606:4700::1/128", "fd00::/48", "::1/128", "10.0.0.0/24"} {
		if _, err := NormalizeIPv6NATSettings(IPv6NATSettings{Subnet: subnet}); err == nil {
			t.Errorf("accepted invalid private IPv6 subnet %q", subnet)
		}
	}
	for _, iface := range []string{"lo", "lxcbr0", "eth0;id", "../../eth0", "an-interface-name-that-is-too-long"} {
		if _, err := NormalizeIPv6NATSettings(IPv6NATSettings{Interface: iface}); err == nil {
			t.Errorf("accepted interface %q", iface)
		}
	}
	settings, err := NormalizeIPv6NATSettings(IPv6NATSettings{Subnet: " fd42:636c:6963::1234/64 ", Interface: " eth0.100 "})
	if err != nil || settings.Subnet != DefaultIPv6NATSubnet || settings.Interface != "eth0.100" {
		t.Fatalf("normalized settings=%+v err=%v", settings, err)
	}
	defaults, err := NormalizeIPv6NATSettings(IPv6NATSettings{})
	if err != nil || defaults.Enabled || defaults.Subnet != DefaultIPv6NATSubnet {
		t.Fatalf("defaults=%+v err=%v", defaults, err)
	}
}

func TestSQLiteIPv6NATAndPortMappingsRoundTrip(t *testing.T) {
	resetConfigStoreForTest(t)
	dir := t.TempDir()
	t.Cleanup(func() { resetConfigStoreForTest(t) })
	SetConfigPath(filepath.Join(dir, "config.json"))
	if err := openConfigDB(); err != nil {
		t.Fatal(err)
	}
	yes, no := true, false
	AppConfig = &ClicdConfig{AdminUser: "admin", IPv6NAT: IPv6NATSettings{Enabled: true, Subnet: DefaultIPv6NATSubnet, Interface: "eth0"}}
	for i, sync := range []*bool{nil, &yes, &no} {
		AppConfig.Containers = append(AppConfig.Containers, Container{ID: i + 1, UUID: "guest-" + strconv.Itoa(i), Name: "guest-" + strconv.Itoa(i), IPv6NATAddress: "fd42:636c:6963::1001",
			IPv6NATSyncIPv4: sync, IPv6PortMappingLimit: 4, IPv6PortMappings: []PortMapping{{HostPort: 30080 + i, ContainerPort: 80, Protocol: "tcp"}}})
		AppConfig.Tasks = append(AppConfig.Tasks, SavedTask{ID: "task-" + strconv.Itoa(i+1), Type: "create", Status: "pending",
			Config: encodeSavedTaskConfig(savedTaskConfig{Name: "guest", IPv6NATSyncIPv4: sync, IPv6PortMappingCount: 4})})
	}
	if err := SaveConfig(); err != nil {
		t.Fatal(err)
	}
	loaded, ok, err := loadConfigFromDB()
	if err != nil || !ok {
		t.Fatalf("load=%v err=%v", ok, err)
	}
	if loaded.IPv6NAT != AppConfig.IPv6NAT {
		t.Fatalf("host settings changed: %+v", loaded.IPv6NAT)
	}
	for i, actual := range loaded.Containers {
		expected := AppConfig.Containers[i]
		if actual.IPv6NATAddress != expected.IPv6NATAddress || actual.IPv6PortMappingLimit != 4 || !reflect.DeepEqual(actual.IPv6NATSyncIPv4, expected.IPv6NATSyncIPv4) || !reflect.DeepEqual(actual.IPv6PortMappings, expected.IPv6PortMappings) {
			t.Fatalf("container fields did not survive SQLite: got=%+v want=%+v", actual, expected)
		}
		cfg := parseSavedTaskConfig(loaded.Tasks[i].Config)
		if cfg.IPv6PortMappingCount != 4 || !reflect.DeepEqual(cfg.IPv6NATSyncIPv4, expected.IPv6NATSyncIPv4) {
			t.Fatalf("pending create lost NAT6 settings: %+v", cfg)
		}
	}
}

func TestAutomaticHostPortAllocationReservesIndependentNAT6Ports(t *testing.T) {
	previous := AppConfig
	t.Cleanup(func() { AppConfig = previous })
	AppConfig = &ClicdConfig{Containers: []Container{{ID: 1, IPv6NATAddress: "fd42::1", IPv6PortMappings: []PortMapping{{HostPort: 30080}}}}}
	if !collectAllHostPorts()[30080] {
		t.Fatal("independent NAT6 port was not reserved for automatic allocation")
	}
}
