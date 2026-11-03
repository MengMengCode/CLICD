package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"clicd/internal/config"
)

func TestLocalImageUploadRoutesRequireAdministratorAndBothScopes(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig = &config.ClicdConfig{JWTSecret: "upload-permission-test", SubUsers: []config.SubUser{{Username: "tenant"}}, ApiKeys: []config.ApiKeyConfig{{ID: "upload-key", KeyHash: "636c6963645f736b5f73656375726974795f626f756e646172795f7465737400"}}}
	mux := http.NewServeMux()
	setupRoutes(mux)
	for _, path := range []string{"/api/images/upload", "/api/v1/images/upload"} {
		for _, tc := range []struct {
			name, token string
			scopes      []string
			containers  []string
			key         bool
			want        int
		}{
			{name: "anonymous", want: 401},
			{name: "tenant", token: imageAccessToken(t, "tenant"), want: 403},
			{name: "image-scopes-without-admin", key: true, scopes: []string{"image:download", "image:toggle"}, want: 403},
			{name: "admin-missing-download", key: true, scopes: []string{"admin:access", "image:toggle"}, want: 403},
			{name: "admin-missing-toggle", key: true, scopes: []string{"admin:access", "image:download"}, want: 403},
			{name: "container-bound-wildcard-key", key: true, scopes: []string{"*"}, containers: []string{"only-this-container"}, want: 403},
			{name: "admin-both-scopes", key: true, scopes: []string{"admin:access", "image:download", "image:toggle"}, want: 400},
			{name: "admin-session", token: imageAccessToken(t, ""), want: 400},
		} {
			t.Run(path+"/"+tc.name, func(t *testing.T) {
				config.AppConfig.ApiKeys[0].Scopes = tc.scopes
				config.AppConfig.ApiKeys[0].ContainerUUIDs = tc.containers
				request := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
				if tc.key {
					request.Header.Set("X-API-Key", "clicd_sk_security_boundary_test")
				}
				if tc.token != "" {
					request.Header.Set("Authorization", "Bearer "+tc.token)
				}
				response := httptest.NewRecorder()
				mux.ServeHTTP(response, request)
				if response.Code != tc.want {
					t.Fatalf("status=%d want=%d: %s", response.Code, tc.want, response.Body.String())
				}
			})
		}
	}
}
