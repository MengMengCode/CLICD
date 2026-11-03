package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"clicd/internal/config"
	"clicd/internal/transfer"
)

func TestContainerResponsesHideDataDiskPathsWithoutChangingPersistence(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	t.Setenv("PATH", "") // No runtime tools or real instances participate.
	const path = "/owned/data-disks/secret-owner/disk-one/disk.qcow2"
	config.AppConfig = &config.ClicdConfig{Containers: []config.Container{{ID: 1, UUID: "disk-owner", Name: "guest", Virtualization: "kvm", Status: "stopped", DataDisks: []config.DataDisk{{ID: "disk-one", Name: "Data", Path: path, SizeGB: 1, Attached: true}}}}}
	for _, test := range []struct {
		name string
		run  func(http.ResponseWriter, *http.Request)
	}{{"list", listContainers}, {"get", func(w http.ResponseWriter, r *http.Request) { getContainer(w, r, 1) }}} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			test.run(response, httptest.NewRequest(http.MethodGet, "/api/containers", nil))
			body := response.Body.String()
			if response.Code != 200 || strings.Contains(body, path) || strings.Contains(body, `"path"`) || !strings.Contains(body, `"disk-one"`) {
				t.Fatalf("unsafe or incomplete container response: %d %s", response.Code, body)
			}
			if config.AppConfig.Containers[0].DataDisks[0].Path != path {
				t.Fatal("response redaction mutated the persistent configuration")
			}
		})
	}
	data, err := json.Marshal(config.AppConfig.Containers[0].DataDisks)
	if err != nil || !strings.Contains(string(data), path) {
		t.Fatal("SQLite data_disks JSON lost its internal path", err)
	}
	manifest, err := json.Marshal(transfer.Manifest{Container: config.AppConfig.Containers[0]})
	if err != nil || !strings.Contains(string(manifest), path) {
		t.Fatal("portable manifest lost its internal path", err)
	}
}
