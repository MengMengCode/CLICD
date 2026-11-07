package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"clicd/internal/config"
)

func setupDownloadArtifact(t *testing.T) string {
	t.Helper()
	pool := t.TempDir()
	previous := config.AppConfig
	config.AppConfig = &config.ClicdConfig{StoragePools: []config.StoragePool{{ID: "portable", Path: pool, ContentTypes: []string{config.StorageContentBackups}}}}
	t.Cleanup(func() { config.AppConfig = previous })
	root := filepath.Join(pool, "backups", "instances")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	id := "bk-" + strings.Repeat("a", 32)
	if err := os.WriteFile(filepath.Join(root, id+".clicd.tar.gz"), []byte("streamed-test-backup"), 0600); err != nil {
		t.Fatal(err)
	}
	metadata := map[string]interface{}{"id": id, "name": "portable", "kind": "backup", "format_version": 1, "runtime": "kvm", "arch": "amd64", "size_bytes": 20}
	data, _ := json.Marshal(metadata)
	if err := os.WriteFile(filepath.Join(root, id+".json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestBackupAdminBoundaryAndScopedDownloadTicket(t *testing.T) {
	id := setupDownloadArtifact(t)
	for _, auth := range []AuthContext{{Type: authTypeSubUser}, {Type: authTypeAPIKey}, {Type: authTypeAPIKey, Scopes: []string{"admin:access"}, ContainerUUIDs: []string{"only-one-uuid"}}} {
		request := withAuthContext(httptest.NewRequest(http.MethodGet, "/api/backups", nil), auth)
		response := httptest.NewRecorder()
		HandleBackups(response, request)
		if response.Code != 403 {
			t.Fatalf("nonadmin listed backup archives: %d", response.Code)
		}
	}
	limited := AuthContext{Type: authTypeAPIKey, Scopes: []string{"admin:access"}, ContainerUUIDs: []string{"only-one-uuid"}}
	for _, endpoint := range []struct {
		method, path string
		handler      http.HandlerFunc
	}{
		{http.MethodGet, "/api/backups/" + id, HandleBackups},
		{http.MethodPost, "/api/backups/" + id + "/download-ticket", HandleBackups},
		{http.MethodPost, "/api/backups/" + id + "/restore", HandleBackups},
		{http.MethodGet, "/api/instance-templates", HandleInstanceTemplates},
	} {
		r := withAuthContext(httptest.NewRequest(endpoint.method, endpoint.path, nil), limited)
		w := httptest.NewRecorder()
		endpoint.handler(w, r)
		if w.Code != 403 {
			t.Fatalf("UUID-bound admin key reached %s: %d", endpoint.path, w.Code)
		}
	}
	r := withAuthContext(httptest.NewRequest(http.MethodPost, "/api/containers/1/backup", nil), limited)
	w := httptest.NewRecorder()
	HandleContainerTransfer(w, r, 1, "backup")
	if w.Code != 403 {
		t.Fatal("restricted admin key created source backup")
	}
	unauth := httptest.NewRecorder()
	HandleBackups(unauth, httptest.NewRequest(http.MethodGet, "/api/backups", nil))
	if unauth.Code != 401 {
		t.Fatal("unauthenticated backup request accepted")
	}
	request := withAuthContext(httptest.NewRequest(http.MethodPost, "/api/backups/"+id+"/download-ticket", nil), AuthContext{Type: authTypeAdmin})
	response := httptest.NewRecorder()
	HandleBackups(response, request)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	var data struct {
		Data struct {
			URL     string `json:"url"`
			Expires string `json:"expires_at"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(data.Data.URL, "/api/backup-download/") || strings.Contains(data.Data.URL, "token=") {
		t.Fatal("download leaks authentication token")
	}
	if len(strings.TrimPrefix(data.Data.URL, "/api/backup-download/")) != 64 {
		t.Fatal("download ticket is not high entropy")
	}
	download := httptest.NewRecorder()
	get := httptest.NewRequest(http.MethodGet, data.Data.URL, nil)
	get.Header.Set("Range", "bytes=0-7")
	HandleBackupDownload(download, get)
	if download.Code != http.StatusPartialContent || download.Body.String() != "streamed" || download.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.HasPrefix(download.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("streaming range download failed: code=%d", download.Code)
	}
	key := strings.TrimPrefix(data.Data.URL, "/api/backup-download/")
	backupTickets.Lock()
	ticket := backupTickets.Items[key]
	ticket.Expires = time.Now().Add(-time.Minute)
	backupTickets.Items[key] = ticket
	backupTickets.Unlock()
	expired := httptest.NewRecorder()
	HandleBackupDownload(expired, httptest.NewRequest(http.MethodGet, data.Data.URL, nil))
	if expired.Code != 404 {
		t.Fatal("expired scoped ticket accepted")
	}
	versioned := httptest.NewRecorder()
	HandleBackups(versioned, withAuthContext(httptest.NewRequest(http.MethodPost, "/api/v1/backups/"+id+"/download-ticket", nil), AuthContext{Type: authTypeAdmin}))
	if err := json.Unmarshal(versioned.Body.Bytes(), &data); versioned.Code != http.StatusOK || err != nil || !strings.HasPrefix(data.Data.URL, "/api/v1/backup-download/") {
		t.Fatalf("versioned ticket returned the wrong download URL: %s", versioned.Body.String())
	}
	head := httptest.NewRecorder()
	HandleBackupDownload(head, httptest.NewRequest(http.MethodHead, data.Data.URL, nil))
	if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("Content-Length") != "20" {
		t.Fatalf("versioned ticket HEAD failed: %d %v", head.Code, head.Header())
	}
	for _, path := range []string{"/api/backup-download/../config.db", "/api/backup-download/" + strings.Repeat("b", 64), "/api/backups/../../config.db"} {
		rec := httptest.NewRecorder()
		if strings.Contains(path, "backup-download") {
			HandleBackupDownload(rec, httptest.NewRequest(http.MethodGet, path, nil))
		} else {
			HandleBackups(rec, withAuthContext(httptest.NewRequest(http.MethodGet, path, nil), AuthContext{Type: authTypeAdmin}))
		}
		if rec.Code != 404 {
			t.Fatalf("invalid artifact/ticket path accepted: %d", rec.Code)
		}
	}
}

func TestTransferRequestRejectsHostPathAndInjectedSourceIdentity(t *testing.T) {
	for _, body := range []string{`{"name":"copy","host_ip":"192.0.2.1"}`, `{"name":"copy","disk_image":"/etc/shadow"}`, `{"name":"copy","port_mapping_count":65}`} {
		r := httptest.NewRequest(http.MethodPost, "/api/containers/1/clone", strings.NewReader(body))
		w := httptest.NewRecorder()
		if _, ok := decodeTransferRequest(w, r); ok || w.Code != 400 {
			t.Fatal("untrusted transfer fields accepted")
		}
	}
}

func TestBackupUploadRejectsConflictingStorageSelection(t *testing.T) {
	var body bytes.Buffer
	multipart := multipart.NewWriter(&body)
	multipart.WriteField("storage_pool_id", "body-pool")
	multipart.Close()
	r := withAuthContext(httptest.NewRequest(http.MethodPost, "/api/backups?storage_pool_id=query-pool", &body), AuthContext{Type: authTypeAdmin})
	r.Header.Set("Content-Type", multipart.FormDataContentType())
	w := httptest.NewRecorder()
	HandleBackups(w, r)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "Conflicting storage_pool_id") {
		t.Fatalf("conflicting query/multipart pool accepted: %d", w.Code)
	}
}
