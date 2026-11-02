package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"clicd/internal/config"

	"golang.org/x/crypto/bcrypt"
)

func TestAccessCodeSubUserCanManageAssignedFirewall(t *testing.T) {
	token := loginFirewallSubUser(t)
	handler := AuthMiddleware(SubUserMiddleware(HandleSingleContainer))
	for _, prefix := range []string{"/api/containers/", "/api/v1/containers/"} {
		for _, identifier := range []string{"11", "owned", "owned-uuid"} {
			t.Run(prefix+identifier, func(t *testing.T) {
				path := prefix + identifier + "/firewall"
				body := `{"enabled":true,"default_action":"ACCEPT","rules":[{"id":"tmp-new","network":"ipv4","direction":"in","protocol":"tcp","port":"443","source_ip":"192.0.2.0/24","action":"ACCEPT","enabled":true}]}`
				updated := requestFirewallAs(handler, token, http.MethodPut, path, body)
				if updated.Code != http.StatusOK {
					t.Fatalf("PUT firewall = %d: %s", updated.Code, updated.Body.String())
				}
				read := requestFirewallAs(handler, token, http.MethodGet, path, "")
				if read.Code != http.StatusOK {
					t.Fatalf("GET firewall = %d: %s", read.Code, read.Body.String())
				}
				var result struct {
					Success bool `json:"success"`
					Data    struct {
						Enabled       bool                  `json:"enabled"`
						DefaultAction string                `json:"default_action"`
						Rules         []config.FirewallRule `json:"rules"`
					} `json:"data"`
				}
				if err := json.Unmarshal(read.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if !result.Success || !result.Data.Enabled || result.Data.DefaultAction != "ACCEPT" || len(result.Data.Rules) != 1 {
					t.Fatalf("firewall settings were not saved: %s", read.Body.String())
				}
				rule := result.Data.Rules[0]
				if rule.ID == "" || strings.HasPrefix(rule.ID, "tmp-") || rule.Port != "443" || rule.SourceIP != "192.0.2.0/24" {
					t.Fatalf("new firewall rule was not saved: %+v", rule)
				}
				removed := requestFirewallAs(handler, token, http.MethodPut, path, `{"enabled":true,"default_action":"DROP","rules":[]}`)
				if removed.Code != http.StatusOK || len(config.FindContainer(11).FirewallRules) != 0 || config.FindContainer(11).FirewallDefaultAction != "DROP" {
					t.Fatalf("could not remove firewall rule: %d %s", removed.Code, removed.Body.String())
				}
			})
		}
	}
}

func TestAccessCodeSubUserCannotManageAnotherFirewall(t *testing.T) {
	token := loginFirewallSubUser(t)
	handler := AuthMiddleware(SubUserMiddleware(HandleSingleContainer))
	before := *config.FindContainer(12)
	for _, prefix := range []string{"/api/containers/", "/api/v1/containers/"} {
		for _, identifier := range []string{"12", "other", "other-uuid"} {
			for _, method := range []string{http.MethodGet, http.MethodPut} {
				path := prefix + identifier + "/firewall"
				response := requestFirewallAs(handler, token, method, path, `{"enabled":true,"default_action":"ACCEPT","rules":[]}`)
				if response.Code != http.StatusForbidden {
					t.Fatalf("%s %s = %d, want forbidden: %s", method, path, response.Code, response.Body.String())
				}
			}
		}
	}
	if !reflect.DeepEqual(before, *config.FindContainer(12)) {
		t.Fatal("another container's firewall was changed")
	}
}

func TestAccessCodeFirewallAccessPreservesRestrictions(t *testing.T) {
	token := loginFirewallSubUser(t)
	handler := AuthMiddleware(SubUserMiddleware(HandleSingleContainer))
	for _, request := range []struct{ method, path string }{
		{http.MethodPost, "/api/containers/11/firewall"},
		{http.MethodDelete, "/api/containers/11/firewall"},
		{http.MethodDelete, "/api/containers/11/delete"},
		{http.MethodPut, "/api/containers/11/resource-limit"},
		{http.MethodPut, "/api/containers/11/expiry"},
	} {
		response := requestFirewallAs(handler, token, request.method, request.path, `{}`)
		if response.Code != http.StatusForbidden {
			t.Fatalf("%s %s = %d, want forbidden", request.method, request.path, response.Code)
		}
	}
	for _, path := range []string{"/api/routing", "/api/v1/routing"} {
		response := requestFirewallAs(AdminMiddleware(HandleRouting), token, http.MethodPut, path, `{}`)
		if response.Code != http.StatusForbidden {
			t.Fatalf("host routing write = %d, want forbidden", response.Code)
		}
	}
	unauthenticated := requestFirewallAs(handler, "", http.MethodPut, "/api/containers/11/firewall", `{}`)
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated firewall write = %d, want unauthorized", unauthenticated.Code)
	}
}

func TestFirewallRequiresNetworkScope(t *testing.T) {
	loginFirewallSubUser(t)
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		req := httptest.NewRequest(method, "/api/v1/containers/11/firewall", strings.NewReader(`{"enabled":true}`))
		req = withAuthContext(req, AuthContext{Type: authTypeAPIKey, Scopes: []string{"container:read"}, ContainerUUIDs: []string{"owned-uuid"}})
		response := httptest.NewRecorder()
		HandleSingleContainer(response, req)
		if response.Code != http.StatusForbidden {
			t.Fatalf("firewall with read-only scope = %d, want forbidden", response.Code)
		}
	}
}

func loginFirewallSubUser(t *testing.T) string {
	t.Helper()
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	hash, err := bcrypt.GenerateFromPassword([]byte("firewall-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	config.AppConfig = &config.ClicdConfig{
		DataDir:   t.TempDir(),
		JWTSecret: "firewall-access-test-secret",
		Containers: []config.Container{
			{ID: 11, UUID: "owned-uuid", Name: "owned", Status: "stopped", FirewallDefaultAction: "DROP"},
			{ID: 12, UUID: "other-uuid", Name: "other", Status: "stopped", FirewallDefaultAction: "DROP"},
		},
		SubUsers: []config.SubUser{{Username: "firewall-user", AccessCode: "access-code", PassHash: string(hash), ContainerUUIDs: []string{"owned-uuid"}}},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/sub-user/access", strings.NewReader(`{"code":"access-code","password":"firewall-password"}`))
	response := httptest.NewRecorder()
	HandleSubUserAccessCode(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("access-code login = %d: %s", response.Code, response.Body.String())
	}
	var result struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Data.Token == "" {
		t.Fatal("access-code login did not return a token")
	}
	return result.Data.Token
}

func requestFirewallAs(handler http.HandlerFunc, token, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	handler(response, request)
	return response
}
