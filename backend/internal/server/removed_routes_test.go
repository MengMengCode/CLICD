package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRemovedAlertEndpointsReturnNotFound(t *testing.T) {
	mux := http.NewServeMux()
	setupRoutes(mux)
	for _, prefix := range []string{"/api", "/api/v1"} {
		for _, endpoint := range []struct{ method, path string }{
			{http.MethodGet, "/security/alerts"},
			{http.MethodPost, "/security/check"},
			{http.MethodGet, "/security/logs?container=guest"},
			{http.MethodGet, "/security/summary"},
			{http.MethodGet, "/security/settings"},
			{http.MethodPut, "/security/settings"},
		} {
			request := httptest.NewRequest(endpoint.method, prefix+endpoint.path, strings.NewReader(`{"auto_shutdown":true}`))
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, request)
			if response.Code != http.StatusNotFound {
				t.Fatalf("%s %s = %d, want 404: %s", endpoint.method, request.URL, response.Code, response.Body.String())
			}
		}
	}
}
