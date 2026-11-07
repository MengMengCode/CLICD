package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"clicd/internal/config"
)

func TestVersionedAPIReadsAndAuthorization(t *testing.T) {
	oldConfig := config.AppConfig
	t.Cleanup(func() { config.AppConfig = oldConfig })
	config.AppConfig = &config.ClicdConfig{
		Language: "zh",
		Containers: []config.Container{
			{ID: 1, UUID: "mine", Name: "owned", PortMappings: []config.PortMapping{{HostPort: 22000, ContainerPort: 22, Protocol: "tcp"}}},
			{ID: 2, UUID: "theirs", Name: "other"},
		},
		ApiKeys: []config.ApiKeyConfig{{
			ID: "test", Scopes: []string{"container:read"},
			KeyHash:        "636c6963645f736b5f73656375726974795f626f756e646172795f7465737400",
			ContainerUUIDs: []string{"mine"},
		}},
	}
	mux := http.NewServeMux()
	setupRoutes(mux)
	for _, tc := range []struct {
		method, path string
		key          bool
		want         int
	}{
		{"GET", "/api/v1/version", false, 200},
		{"GET", "/api/v1/containers/mine/port-mappings", false, 401},
		{"GET", "/api/v1/containers/mine/port-mappings", true, 200},
		{"GET", "/api/v1/containers/theirs/port-mappings", true, 403},
		{"GET", "/api/v1/containers/mine/unknown-action", true, 404},
		{"GET", "/api/v1/tasks/task-missing", true, 403},
		{"GET", "/api/v1/language", false, 200},
		{"POST", "/api/v1/language", false, 401},
		{"PUT", "/api/language", false, 401},
		{"PUT", "/api/v1/language", true, 403},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{"language":"en"}`))
			if tc.key {
				req.Header.Set("X-API-Key", "clicd_sk_security_boundary_test")
			}
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, req)
			if response.Code != tc.want {
				t.Fatalf("status=%d want=%d: %s", response.Code, tc.want, response.Body.String())
			}
			if tc.want == 200 && strings.HasSuffix(tc.path, "/port-mappings") {
				var body struct{ Data []config.PortMapping }
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || len(body.Data) != 1 || body.Data[0].HostPort != 22000 {
					t.Fatalf("invalid port mapping list: %s", response.Body.String())
				}
			}
		})
	}
	if config.AppConfig.Language != "zh" {
		t.Fatal("unauthorized request changed the panel language")
	}
}

func TestVersionedBackupDownloadObeysPanelSourcePolicy(t *testing.T) {
	oldConfig := config.AppConfig
	t.Cleanup(func() { config.AppConfig = oldConfig })
	config.AppConfig = &config.ClicdConfig{
		PanelAccessPolicy: config.PanelAccessPolicy{Enabled: true, AllowedSources: []string{"192.0.2.0/24"}},
	}
	mux := http.NewServeMux()
	setupRoutes(mux)
	handler := panelAccessMiddleware(mux)
	for _, tc := range []struct {
		source, method string
		want           int
	}{
		{"192.0.2.8:50000", http.MethodPost, http.StatusMethodNotAllowed},
		{"192.0.2.8:50000", http.MethodGet, http.StatusNotFound},
		{"198.51.100.8:50000", http.MethodGet, http.StatusForbidden},
	} {
		req := httptest.NewRequest(tc.method, "/api/v1/backup-download/"+strings.Repeat("b", 64), nil)
		req.RemoteAddr = tc.source
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != tc.want {
			t.Fatalf("%s from %s status=%d want=%d", tc.method, tc.source, response.Code, tc.want)
		}
	}
}
