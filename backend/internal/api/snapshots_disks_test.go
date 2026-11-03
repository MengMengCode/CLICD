package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"clicd/internal/config"
)

func TestSnapshotAPIReturnsConflictForInstancesWithDataDisks(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	t.Setenv("PATH", "")
	for _, runtime := range []string{"lxc", "kvm"} {
		for _, attached := range []bool{true, false} {
			config.AppConfig = &config.ClicdConfig{Containers: []config.Container{{ID: 14, UUID: "with-data-disk", Name: "with-data-disk", Virtualization: runtime, Status: "stopped", DataDisks: []config.DataDisk{{ID: "disk-one", Attached: attached, SizeGB: 2}}}}, Snapshots: []config.Snapshot{{ID: "existing", ContainerID: 14, ContainerName: "with-data-disk"}}}
			before, _ := json.Marshal(config.AppConfig)
			for _, path := range []string{"/api/v1/containers/14/snapshots", "/api/v1/containers/14/snapshots/existing/restore"} {
				request := withAuthContext(httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`)), AuthContext{Type: authTypeAdmin})
				response := httptest.NewRecorder()
				HandleSingleContainer(response, request)
				if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "full backup") {
					t.Fatalf("runtime=%s attached=%v %s: %d %s", runtime, attached, path, response.Code, response.Body.String())
				}
				request = withAuthContext(httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`)), AuthContext{Type: authTypeAPIKey, Scopes: []string{"container:read"}})
				response = httptest.NewRecorder()
				HandleSingleContainer(response, request)
				if response.Code != http.StatusForbidden {
					t.Fatalf("data-disk check bypassed scope enforcement: %d", response.Code)
				}
			}
			after, _ := json.Marshal(config.AppConfig)
			if string(before) != string(after) {
				t.Fatal("rejected snapshot operation modified instance or snapshot metadata")
			}
		}
	}
}
