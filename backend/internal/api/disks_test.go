package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"clicd/internal/config"
)

func diskAPIFixture(t *testing.T) *config.Container {
	t.Helper()
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig = &config.ClicdConfig{Containers: []config.Container{{ID: 1, UUID: "disk-owner", Name: "guest", Virtualization: config.VirtualizationKVM, DataDisks: []config.DataDisk{{ID: "disk-one", Name: "Data", SizeGB: 1}}}}}
	return &config.AppConfig.Containers[0]
}
func requestDisk(t *testing.T, method, action, body string, auth AuthContext) *httptest.ResponseRecorder {
	t.Helper()
	r := withAuthContext(httptest.NewRequest(method, "/api/containers/1/"+action, strings.NewReader(body)), auth)
	w := httptest.NewRecorder()
	HandleContainerDisks(w, r, 1, action)
	return w
}
func TestDiskAPIRejectsUnsafeOrAmbiguousRequests(t *testing.T) {
	diskAPIFixture(t)
	admin := AuthContext{Type: authTypeAdmin}
	for _, tc := range []struct {
		method, action, body string
		code                 int
	}{
		{"POST", "disks", `{"name":"data","size_gb":1,"path":"/etc/passwd"}`, 400},
		{"POST", "disks", `{"name":"data","size_gb":1} {}`, 400},
		{"POST", "disks", `{"name":"data","size_gb":-1}`, 400},
		{"POST", "disks", `{"name":"data","size_gb":1,"mount_path":"/data"}`, 400},
		{"DELETE", "disks/disk-one", `{}`, 400},
		{"DELETE", "disks/disk-one", `{"delete_data":false}`, 400},
		{"DELETE", "disks/system", `{"delete_data":true}`, 400},
		{"PUT", "disks/system/attachment", `{"attached":false}`, 400},
		{"PUT", "disks/disk-one/attachment", `{}`, 400},
		{"PUT", "disks/unknown", `{"size_gb":2}`, 404},
	} {
		got := requestDisk(t, tc.method, tc.action, tc.body, admin)
		if got.Code != tc.code {
			t.Fatalf("%s %s %s: %d %s", tc.method, tc.action, tc.body, got.Code, got.Body.String())
		}
	}
}
func TestDiskAPIOwnershipAndAdministratorPrivileges(t *testing.T) {
	diskAPIFixture(t)
	for _, auth := range []AuthContext{
		{Type: authTypeSubUser, ContainerUUIDs: []string{"disk-owner"}},
		{Type: authTypeAPIKey, ContainerUUIDs: []string{"disk-owner"}, Scopes: []string{"*"}},
		{Type: authTypeAPIKey, Scopes: []string{"container:read"}},
	} {
		got := requestDisk(t, http.MethodPost, "disks", `{"name":"data","size_gb":1}`, auth)
		if got.Code != 403 {
			t.Fatalf("mutation allowed to %+v: %d", auth, got.Code)
		}
	}
	got := requestDisk(t, http.MethodGet, "disks", "", AuthContext{Type: authTypeSubUser, ContainerUUIDs: []string{"other-owner"}})
	if got.Code != 403 {
		t.Fatalf("other owner inventory: %d", got.Code)
	}
	got = requestDisk(t, http.MethodGet, "disks", "", AuthContext{Type: authTypeSubUser, ContainerUUIDs: []string{"disk-owner"}})
	if got.Code != 200 {
		t.Fatalf("own inventory: %d %s", got.Code, got.Body.String())
	}
}
func TestDiskAPIScheduledSnapshotAndStaleIdentityGuards(t *testing.T) {
	c := diskAPIFixture(t)
	c.SnapshotScheduleEnabled = true
	got := requestDisk(t, http.MethodPost, "disks", `{"name":"data","size_gb":1}`, AuthContext{Type: authTypeAdmin})
	if got.Code != 409 || !strings.Contains(got.Body.String(), "scheduled snapshots") {
		t.Fatal(got.Body.String())
	}
	if err := runDiskOperation(context.Background(), c.ID, TaskDiskRemove, []byte(`{"uuid":"old-owner","disk_id":"disk-one"}`), func(string, string) {}); err == nil {
		t.Fatal("stale identity operation executed")
	}
}
