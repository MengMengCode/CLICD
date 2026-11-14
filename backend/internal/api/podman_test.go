package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"clicd/internal/config"
)

func TestPodmanRequiresAdministratorSession(t *testing.T) {
	for _, endpoint := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/podman/status"},
		{http.MethodGet, "/api/podman/images"},
		{http.MethodPost, "/api/podman/images"},
		{http.MethodGet, "/api/podman/containers/" + strings.Repeat("a", 64) + "/stats"},
	} {
		for _, auth := range []AuthContext{
			{Type: authTypeSubUser},
			{Type: authTypeAPIKey, Scopes: []string{"*"}},
		} {
			req := httptest.NewRequest(endpoint.method, endpoint.path, nil)
			rec := httptest.NewRecorder()
			HandlePodman(rec, withAuthContext(req, auth))
			if rec.Code != http.StatusForbidden {
				t.Fatalf("%s %s, auth type %s: status %d, want 403: %s", endpoint.method, endpoint.path, auth.Type, rec.Code, rec.Body.String())
			}
		}
	}
}

func TestPodmanRejectsPortsReservedForOtherRuntimes(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig = &config.ClicdConfig{
		NATPortStart: 20000, NATPortEnd: 65535,
		Containers: []config.Container{{ID: 1, PortMappings: []config.PortMapping{
			{HostPort: 8080, Protocol: "tcp"},
		}}},
	}
	for _, input := range []string{
		`{"name":"app","image":"docker.io/library/nginx:alpine","ports":[{"host_port":22000,"container_port":80,"protocol":"tcp"}]}`,
		`{"name":"app","image":"docker.io/library/nginx:alpine","ports":[{"host_port":8080,"container_port":80,"protocol":"tcp"}]}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/podman/containers", strings.NewReader(input))
		rec := httptest.NewRecorder()
		HandlePodman(rec, withAuthContext(req, AuthContext{Type: authTypeAdmin}))
		if rec.Code != http.StatusConflict {
			t.Fatalf("status %d, want 409: %s", rec.Code, rec.Body.String())
		}
	}
}
