package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"clicd/internal/config"
)

func TestFirewallUpdateValidationIsAtomic(t *testing.T) {
	// Config's SQLite connection is process-wide. Keep real persistence checks
	// isolated so other API tests cannot save their fixtures into this database.
	const testDirEnv = "CLICD_FIREWALL_ATOMICITY_TEST_DIR"
	dir := os.Getenv(testDirEnv)
	if dir == "" {
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		command := exec.Command(executable, "-test.run=^TestFirewallUpdateValidationIsAtomic$", "-test.v")
		command.Env = append(os.Environ(), testDirEnv+"="+t.TempDir())
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("isolated firewall API test: %v\n%s", err, output)
		}
		t.Log(string(output))
		return
	}
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	// No runtime network commands are needed for a stopped, enabled firewall.
	t.Setenv("PATH", "")
	path := filepath.Join(dir, "config.json")
	cfg := config.ClicdConfig{
		AdminUser: "test-admin", AdminPassHash: "test-hash", JWTSecret: "test-secret", DataDir: dir, Port: 8999,
		Containers: []config.Container{{
			ID: 11, UUID: "firewall-atomic", Name: "firewall-atomic", Status: "stopped",
			VCPU: 1, RAMMB: 512, DiskGB: 4, FirewallEnabled: true, FirewallDefaultAction: "",
			FirewallRules: []config.FirewallRule{{ID: "original", Network: "ipv4", Direction: "in", Protocol: "tcp", Port: "22", Action: "ACCEPT", Enabled: true}},
		}},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	config.SetConfigPath(path)
	if _, err := config.InitConfig(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	type savedFirewall struct {
		enabled       bool
		defaultAction string
		rules         string
	}
	readSaved := func() savedFirewall {
		t.Helper()
		var saved savedFirewall
		if err := db.QueryRow("SELECT firewall_enabled, firewall_default_action, firewall_rules FROM containers WHERE id = 11").Scan(&saved.enabled, &saved.defaultAction, &saved.rules); err != nil {
			t.Fatal(err)
		}
		return saved
	}
	beforeMemory, err := json.Marshal(config.AppConfig)
	if err != nil {
		t.Fatal(err)
	}
	beforeSaved := readSaved()
	request := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPut, "/api/containers/11/firewall", strings.NewReader(body))
		req = withAuthContext(req, AuthContext{Type: authTypeAdmin, Username: "test-admin"})
		response := httptest.NewRecorder()
		HandleSingleContainer(response, req)
		return response
	}
	invalidRule := func(rule config.FirewallRule) string {
		t.Helper()
		// The first valid rule must not replace existing rules if a later one is invalid.
		body := map[string]interface{}{"enabled": false, "default_action": "ACCEPT", "rules": []config.FirewallRule{
			{ID: "tmp-valid", Network: "ipv4", Direction: "in", Protocol: "tcp", Port: "443", Action: "ACCEPT", Enabled: true}, rule,
		}}
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	validRule := config.FirewallRule{ID: "tmp-invalid", Network: "ipv4", Direction: "in", Protocol: "tcp", Port: "80", Action: "ACCEPT", Enabled: true}
	cases := []struct{ name, body string }{
		{"empty-default-action", `{"enabled":false,"default_action":"","rules":[]}`},
		{"invalid-default-action", `{"enabled":false,"default_action":"REJECT","rules":[]}`},
	}
	for _, field := range []string{"network", "direction", "protocol", "action", "source-ip", "port", "icmp-port"} {
		rule := validRule
		switch field {
		case "network":
			rule.Network = "invalid"
		case "direction":
			rule.Direction = "forward"
		case "protocol":
			rule.Protocol = "invalid"
		case "action":
			rule.Action = "REJECT"
		case "source-ip":
			rule.SourceIP = "2001:db8::1"
		case "port":
			rule.Port = "65536"
		case "icmp-port":
			rule.Protocol = "icmp"
		}
		cases = append(cases, struct{ name, body string }{field, invalidRule(rule)})
	}
	// Omitted default_action must not normalize the legacy blank value on rejection.
	cases = append(cases, struct{ name, body string }{"omitted-default-action", `{"enabled":false,"rules":[{"direction":"invalid"}]}`})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			response := request(tc.body)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("PUT = %d: %s", response.Code, response.Body.String())
			}
			afterMemory, err := json.Marshal(config.AppConfig)
			if err != nil {
				t.Fatal(err)
			}
			if string(afterMemory) != string(beforeMemory) {
				t.Fatalf("rejected request changed in-memory config: %+v", config.FindContainer(11))
			}
			if got := readSaved(); got != beforeSaved {
				t.Fatalf("rejected request changed persisted firewall: %+v", got)
			}
			// A later unrelated save must not persist any partial rejected update.
			if err := config.SaveConfig(); err != nil {
				t.Fatal(err)
			}
			if got := readSaved(); got != beforeSaved {
				t.Fatalf("partial update escaped through a later save: %+v", got)
			}
		})
	}
	response := request(`{"enabled":true,"default_action":" accept ","rules":[{"id":"tmp-new","network":" nat4 ","direction":" IN ","protocol":" TCP ","port":" 443 ","source_ip":" 192.0.2.0/24 ","action":" accept ","enabled":true}]}`)
	if response.Code != http.StatusOK {
		t.Fatalf("valid PUT = %d: %s", response.Code, response.Body.String())
	}
	c := config.FindContainer(11)
	if !c.FirewallEnabled || c.FirewallDefaultAction != "ACCEPT" || len(c.FirewallRules) != 1 {
		t.Fatalf("valid update was not committed: %+v", c)
	}
	rule := c.FirewallRules[0]
	if rule.ID == "" || strings.HasPrefix(rule.ID, "tmp-") || rule.Network != "ipv4" || rule.Direction != "in" || rule.Protocol != "tcp" || rule.Action != "ACCEPT" || rule.Port != "443" || rule.SourceIP != "192.0.2.0/24" {
		t.Fatalf("valid rule was not normalized: %+v", rule)
	}
	saved := readSaved()
	var savedRules []config.FirewallRule
	if err := json.Unmarshal([]byte(saved.rules), &savedRules); err != nil {
		t.Fatal(err)
	}
	if !saved.enabled || saved.defaultAction != "ACCEPT" || !reflect.DeepEqual(savedRules, c.FirewallRules) {
		t.Fatalf("valid update was not persisted: %+v", saved)
	}
	// Partial updates continue to preserve omitted settings and rules.
	if response := request(`{"enabled":true}`); response.Code != http.StatusOK || !reflect.DeepEqual(c.FirewallRules, savedRules) || readSaved() != saved {
		t.Fatalf("partial update did not preserve settings: %d %s", response.Code, response.Body.String())
	}
}
