package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"clicd/internal/config"
	"github.com/golang-jwt/jwt/v5"
)

func TestImageManagementRoutesRequireAdministratorAndOperationScope(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig = &config.ClicdConfig{
		JWTSecret: "image-permission-test",
		SubUsers:  []config.SubUser{{Username: "tenant"}},
		ApiKeys: []config.ApiKeyConfig{{
			ID: "limited-images",
			// Legacy fixture is migrated by authentication on its first use.
			KeyHash: "636c6963645f736b5f73656375726974795f626f756e646172795f7465737400",
		}},
	}
	mux := http.NewServeMux()
	setupRoutes(mux)
	for _, route := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/images/custom"},
		{http.MethodPost, "/api/v1/images/download"},
		{http.MethodPost, "/api/v1/images/cancel"},
		{http.MethodDelete, "/api/v1/images/delete"},
		{http.MethodPut, "/api/v1/images/toggle"},
	} {
		for _, access := range []struct {
			name, token string
			scopes      []string
			want        int
		}{
			{"image scope without administrator", "", []string{"image:*"}, http.StatusForbidden},
			{"administrator without image scope", "", []string{"admin:access"}, http.StatusForbidden},
			{"administrator with image scope", "", []string{"admin:access", "image:*"}, http.StatusBadRequest},
			{"administrator session", imageAccessToken(t, ""), nil, http.StatusBadRequest},
			{"tenant session", imageAccessToken(t, "tenant"), nil, http.StatusForbidden},
		} {
			t.Run(route.path+"/"+access.name, func(t *testing.T) {
				config.AppConfig.ApiKeys[0].Scopes = access.scopes
				req := httptest.NewRequest(route.method, route.path, strings.NewReader("{"))
				if access.token != "" {
					req.Header.Set("Authorization", "Bearer "+access.token)
				} else {
					req.Header.Set("X-API-Key", "clicd_sk_security_boundary_test")
				}
				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, req)
				if rec.Code != access.want {
					t.Fatalf("status=%d, want %d: %s", rec.Code, access.want, rec.Body.String())
				}
			})
		}
	}
	config.AppConfig.ApiKeys[0].Scopes = []string{"image:read"}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/images/enabled", nil)
	req.Header.Set("X-API-Key", "clicd_sk_security_boundary_test")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("read-only image access status=%d: %s", rec.Code, rec.Body.String())
	}
}

func imageAccessToken(t *testing.T, tenant string) string {
	t.Helper()
	claims := jwt.MapClaims{"username": "admin", "exp": time.Now().Add(time.Minute).Unix()}
	if tenant != "" {
		claims["sub_user"] = tenant
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(config.AppConfig.JWTSecret))
	if err != nil {
		t.Fatal(err)
	}
	return token
}
