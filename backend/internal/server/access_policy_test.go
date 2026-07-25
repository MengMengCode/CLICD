package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"clicd/internal/config"
)

func TestPanelAccessMiddleware(t *testing.T) {
	previous := config.AppConfig
	config.AppConfig = &config.ClicdConfig{
		PanelAccessPolicy: config.PanelAccessPolicy{
			Enabled:        true,
			AllowedSources: []string{"192.0.2.0/24"},
			TrustedProxies: []string{"10.0.0.1"},
		},
	}
	t.Cleanup(func() {
		config.AppConfig = previous
	})

	handler := panelAccessMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	allowed := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	allowed.RemoteAddr = "192.0.2.8:50000"
	allowedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(allowedRecorder, allowed)
	if allowedRecorder.Code != http.StatusNoContent {
		t.Fatalf("allowed status = %d", allowedRecorder.Code)
	}

	denied := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	denied.RemoteAddr = "198.51.100.8:50000"
	deniedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(deniedRecorder, denied)
	if deniedRecorder.Code != http.StatusForbidden {
		t.Fatalf("denied status = %d", deniedRecorder.Code)
	}
	if got := deniedRecorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("denied content type = %q", got)
	}
}
