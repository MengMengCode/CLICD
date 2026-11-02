package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"clicd/internal/config"
)

func TestIPv6NATSettingsRequireAdministratorAndOperationScope(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig = &config.ClicdConfig{
		JWTSecret: "nat6-permission-test", SubUsers: []config.SubUser{{Username: "tenant"}},
		ApiKeys: []config.ApiKeyConfig{{ID: "nat6-limited", KeyHash: "636c6963645f736b5f73656375726974795f626f756e646172795f7465737400"}},
	}
	mux := http.NewServeMux()
	setupRoutes(mux)
	for _, path := range []string{"/api/ipv6/nat", "/api/v1/ipv6/nat"} {
		for _, tc := range []struct {
			name, method, token string
			scopes              []string
			want                int
		}{
			{"tenant read", "GET", imageAccessToken(t, "tenant"), nil, 403},
			{"tenant write", "PUT", imageAccessToken(t, "tenant"), nil, 403},
			{"network scope without administrator", "PUT", "", []string{"routing:write"}, 403},
			{"administrator without write scope", "PUT", "", []string{"admin:access", "ipv6:read"}, 403},
			{"administrator with write scope reaches validation", "PUT", "", []string{"admin:access", "routing:write"}, 400},
			{"administrator session reaches validation", "PUT", imageAccessToken(t, ""), nil, 400},
			{"read scope without administrator", "GET", "", []string{"ipv6:read"}, 403},
		} {
			t.Run(path+"/"+tc.name, func(t *testing.T) {
				config.AppConfig.ApiKeys[0].Scopes = tc.scopes
				req := httptest.NewRequest(tc.method, path, strings.NewReader(`{}`))
				if tc.token != "" {
					req.Header.Set("Authorization", "Bearer "+tc.token)
				} else {
					req.Header.Set("X-API-Key", "clicd_sk_security_boundary_test")
				}
				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, req)
				if rec.Code != tc.want {
					t.Fatalf("status=%d want %d: %s", rec.Code, tc.want, rec.Body.String())
				}
				if config.AppConfig.IPv6NAT.Enabled {
					t.Fatal("rejected request enabled host NAT6")
				}
			})
		}
		request := httptest.NewRequest(http.MethodPut, path, strings.NewReader(`{"enabled":true}`))
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("unauthenticated write=%d", response.Code)
		}
	}
}
