package config

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewSQLiteSchemaOmitsRetiredAlertStorage(t *testing.T) {
	resetConfigStoreForTest(t)
	dir := t.TempDir()
	t.Cleanup(func() { resetConfigStoreForTest(t) })
	SetConfigPath(filepath.Join(dir, "config.json"))
	if err := openConfigDB(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name IN ('security_conntrack_snapshots', 'idx_conntrack_snapshots_ip_time')`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("new database created retired alert snapshot storage")
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('containers') WHERE name LIKE 'policy_blocked%'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("new database created retired alert block columns")
	}
}

func TestLegacySQLiteAlertStateIsIgnoredWithoutDeletingOldRecords(t *testing.T) {
	resetConfigStoreForTest(t)
	dir := t.TempDir()
	t.Cleanup(func() { resetConfigStoreForTest(t) })
	SetConfigPath(filepath.Join(dir, "config.json"))
	if err := openConfigDB(); err != nil {
		t.Fatal(err)
	}
	AppConfig = &ClicdConfig{
		AdminUser: "admin", AdminPassHash: "test-hash", JWTSecret: "test-secret", DataDir: dir,
		IPv6NAT: IPv6NATSettings{Subnet: DefaultIPv6NATSubnet},
		Containers: []Container{{
			ID: 1, UUID: "legacy-guest", Name: "guest", Virtualization: VirtualizationLXC, Status: "running",
			ExpiresAt: "2099-01-01 00:00:00", MonthlyTrafficGB: 50, TrafficUsedRX: 123,
			PortMappingLimit: 4, IPv6PortMappingLimit: 2, IPv6NATAddress: "fd42:636c:6963::1001",
			AllowedImageIDs: []string{"debian-trixie"}, ImageLimitConfigured: true,
			FirewallEnabled: true, FirewallDefaultAction: "DROP",
		}},
		AuditLogs: []AuditLog{{Action: "security_auto_shutdown", Target: "guest", Detail: "historical audit record"}},
	}
	if err := SaveConfig(); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`ALTER TABLE containers ADD COLUMN policy_blocked INTEGER`,
		`ALTER TABLE containers ADD COLUMN policy_blocked_reason TEXT`,
		`ALTER TABLE containers ADD COLUMN policy_blocked_at TEXT`,
		`UPDATE containers SET policy_blocked = 1, policy_blocked_reason = 'legacy scanner block', policy_blocked_at = '2026-01-01'`,
		`INSERT INTO app_meta(key, value) VALUES ('security_auto_shutdown', '1')`,
		`CREATE TABLE security_conntrack_snapshots (id INTEGER PRIMARY KEY, container_ip TEXT, line TEXT, captured_at TEXT)`,
		`CREATE INDEX idx_conntrack_snapshots_ip_time ON security_conntrack_snapshots(container_ip, captured_at)`,
		`INSERT INTO security_conntrack_snapshots VALUES (1, '10.0.3.2', 'historical snapshot', '2026-01-01')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = nil
	if err := openConfigDB(); err != nil {
		t.Fatal(err)
	}
	loaded, ok, err := loadConfigFromDB()
	if err != nil || !ok {
		t.Fatalf("legacy SQLite load: ok=%v err=%v", ok, err)
	}
	container := loaded.Containers[0]
	if container.Status != "running" || container.ExpiresAt != "2099-01-01 00:00:00" || container.MonthlyTrafficGB != 50 || container.TrafficUsedRX != 123 ||
		container.PortMappingLimit != 4 || container.IPv6PortMappingLimit != 2 || container.IPv6NATAddress != "fd42:636c:6963::1001" ||
		!container.ImageLimitConfigured || len(container.AllowedImageIDs) != 1 || !container.FirewallEnabled || container.FirewallDefaultAction != "DROP" {
		t.Fatalf("unrelated instance settings changed during legacy load: %+v", container)
	}
	encoded, err := json.Marshal(loaded)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"policy_blocked`) || strings.Contains(string(encoded), `"security_auto_shutdown":`) {
		t.Fatal("retired alert state is still exposed in configuration JSON")
	}
	AppConfig = loaded
	if err := SaveConfig(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM app_meta WHERE key = 'security_auto_shutdown'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("retired alert setting was written back: count=%d err=%v", count, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM security_conntrack_snapshots`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("legacy snapshot records were deleted: count=%d err=%v", count, err)
	}
	if len(loaded.AuditLogs) != 1 || loaded.AuditLogs[0].Detail != "historical audit record" {
		t.Fatal("historical audit records were deleted")
	}
}
