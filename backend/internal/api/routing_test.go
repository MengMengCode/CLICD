package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"clicd/internal/config"
)

func TestHandleRoutingGetAllowsRoutingWriteScope(t *testing.T) {
	config.AppConfig = &config.ClicdConfig{}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/routing", nil)
	req = withAuthContext(req, AuthContext{
		Type:   authTypeAPIKey,
		Scopes: []string{"routing:write"},
	})
	rec := httptest.NewRecorder()

	handleRoutingGet(rec, req)

	if rec.Code == http.StatusForbidden {
		t.Fatal("routing:write scope should be able to receive the routing response after updates")
	}
}

func TestHandleRoutingGetReturnsConfiguredNextNATPort(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig = &config.ClicdConfig{
		NATPortStart: 30000,
		NATPortEnd:   35000,
		NextSSHPort:  30000,
		Containers: []config.Container{{
			PortMappings: []config.PortMapping{{HostPort: 30000}},
		}},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/routing", nil)
	req = withAuthContext(req, AuthContext{
		Type:   authTypeAPIKey,
		Scopes: []string{"routing:read"},
	})
	rec := httptest.NewRecorder()
	handleRoutingGet(rec, req)

	var response struct {
		Success bool `json:"success"`
		Data    struct {
			NAT4PortRange nat4PortRange `json:"nat4_port_range"`
			NAT4NextPort  int           `json:"nat4_next_port"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Success {
		t.Fatalf("routing response was unsuccessful: %s", rec.Body.String())
	}
	if response.Data.NAT4PortRange.Start != 30000 || response.Data.NAT4PortRange.End != 35000 {
		t.Fatalf("NAT range = %+v", response.Data.NAT4PortRange)
	}
	if response.Data.NAT4NextPort != 30001 {
		t.Fatalf("next NAT port = %d, want 30001", response.Data.NAT4NextPort)
	}
}
