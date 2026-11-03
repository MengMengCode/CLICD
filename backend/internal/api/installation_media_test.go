package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"clicd/internal/config"
	"clicd/internal/lxc"
)

func apiMediaFixture(t *testing.T) string {
	token := loginFirewallSubUser(t)
	cfg := config.AppConfig
	cfg.Containers[0].Virtualization = config.VirtualizationKVM
	cfg.Containers[0].KVMName = "vm-11"
	cfg.Containers[0].Template = "iso-allowed"
	cfg.Containers[0].ManualInstall = true
	cfg.Containers[0].InstallationMediaImageID = "iso-allowed"
	cfg.Containers[0].InstallationMediaBoot = "cdrom"
	cfg.SubUsers[0].AllowedImageIDs = []string{"iso-allowed"}
	cfg.SubUsers[0].ImageLimitConfigured = true
	pool := t.TempDir()
	cfg.StoragePools = []config.StoragePool{{ID: "media", Path: pool, Enabled: true, ContentTypes: []string{config.StorageContentImages}, DefaultContents: []string{config.StorageContentImages}}}
	for _, id := range []string{"iso-allowed", "iso-secret"} {
		cfg.CustomKVMImages = append(cfg.CustomKVMImages, config.CustomKVMImage{ID: id, Name: id, Arch: runtime.GOARCH, Provisioner: config.KVMProvisionerManualISO})
		path := filepath.Join(pool, "images", "kvm", id+".iso")
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		data := make([]byte, 17*2048)
		copy(data[16*2048+1:], "CD001")
		data[16*2048+6] = 1
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	return token
}

func TestInstallationMediaSubUserOwnershipAndImageFiltering(t *testing.T) {
	token := apiMediaFixture(t)
	handler := AuthMiddleware(SubUserMiddleware(HandleSingleContainer))
	response := requestFirewallAs(handler, token, http.MethodGet, "/api/containers/11/installation-media", "")
	if response.Code != 200 {
		t.Fatalf("GET: %d %s", response.Code, response.Body.String())
	}
	var result struct {
		Data installationMediaResponse `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Data.ManualInstall || result.Data.ImageID != "iso-allowed" || len(result.Data.Available) != 1 || result.Data.Available[0].ID != "iso-allowed" {
		t.Fatalf("incorrect scoped media: %+v", result.Data)
	}
	if strings.Contains(response.Body.String(), "iso-secret") || strings.Contains(response.Body.String(), config.AppConfig.StoragePools[0].Path) {
		t.Fatal("GET leaked another ISO or local paths")
	}
	response = requestFirewallAs(handler, token, http.MethodGet, "/api/containers/12/installation-media", "")
	if response.Code != 403 {
		t.Fatalf("other owner GET: %d", response.Code)
	}
	response = requestFirewallAs(handler, token, http.MethodPut, "/api/containers/11/installation-media", `{"image_id":"iso-secret","boot":"cdrom"}`)
	if response.Code != 403 {
		t.Fatalf("unallowed ISO PUT: %d %s", response.Code, response.Body.String())
	}
}

func TestInstallationMediaRejectsPathsUnknownFieldsAndBusyContainer(t *testing.T) {
	token := apiMediaFixture(t)
	handler := AuthMiddleware(SubUserMiddleware(HandleSingleContainer))
	for _, body := range []string{`{"image_id":"","boot":"cdrom"}`, `{"image_id":"","boot":"disk","path":"/tmp/evil.iso"}`, `{"image_id":"","boot":"disk"} {}`} {
		response := requestFirewallAs(handler, token, http.MethodPut, "/api/containers/11/installation-media", body)
		if response.Code != 400 {
			t.Fatalf("body %s returned %d %s", body, response.Code, response.Body.String())
		}
	}
	previous := globalQueue
	t.Cleanup(func() { globalQueue = previous })
	globalQueue = newTaskQueue(1)
	globalQueue.tasks["pending"] = &Task{ID: "pending", Type: TaskStart, ContainerID: 11, ContainerName: "owned", Status: "pending"}
	response := requestFirewallAs(handler, token, http.MethodPut, "/api/containers/11/installation-media", `{"image_id":"","boot":"disk"}`)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "active or queued") {
		t.Fatalf("busy PUT: %d %s", response.Code, response.Body.String())
	}
}

func TestInstallationMediaAPIKeyNeedsReinstallScope(t *testing.T) {
	apiMediaFixture(t)
	request := httptest.NewRequest(http.MethodPut, "/api/containers/11/installation-media", strings.NewReader(`{"image_id":"","boot":"disk"}`))
	request = withAuthContext(request, AuthContext{Type: authTypeAPIKey, Scopes: []string{"container:read"}})
	response := httptest.NewRecorder()
	HandleSingleContainer(response, request)
	if response.Code != 403 {
		t.Fatalf("read-only key PUT: %d %s", response.Code, response.Body.String())
	}
}

func TestManualISOCreateAndReinstallSkipSSHValidation(t *testing.T) {
	apiMediaFixture(t)
	cfg := lxc.ContainerConfig{Virtualization: config.VirtualizationKVM, TemplateID: "iso-allowed", SSHAuthMode: lxc.SSHAuthKey, SSHPublicKey: "not a valid key"}
	if err := validateCreateSSHAuth(cfg); err != nil {
		t.Fatal(err)
	}
	if err := validateReinstallSSHAuth(&config.AppConfig.Containers[0], "iso-allowed", cfg); err != nil {
		t.Fatal(err)
	}
	cfg.TemplateID = "kvm-ubuntu-24.04"
	if err := validateCreateSSHAuth(cfg); err == nil {
		t.Fatal("cloud-init SSH validation was bypassed")
	}
}
