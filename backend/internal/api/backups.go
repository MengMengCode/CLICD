package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"clicd/internal/config"
	"clicd/internal/kvm"
	"clicd/internal/lxc"
	"clicd/internal/transfer"
)

type transferRequest struct {
	Name             string `json:"name"`
	StoragePoolID    string `json:"storage_pool_id,omitempty"`
	AssignNAT        *bool  `json:"assign_nat,omitempty"`
	PortMappingCount int    `json:"port_mapping_count,omitempty"`
	ArtifactID       string `json:"artifact_id,omitempty"`
	Kind             string `json:"kind,omitempty"`
}

func init() {
	RegisterOperationTask(TaskType("backup"), runCreateBackup)
	RegisterOperationTask(TaskType("save-template"), runCreateTemplate)
	RegisterOperationTask(TaskType("clone"), runFullClone)
	RegisterOperationTask(TaskType("restore-backup"), runRestoreBackup)
	RegisterOperationTask(TaskType("instantiate-template"), runInstantiateTemplate)
}

func requireTransferAdmin(w http.ResponseWriter, r *http.Request) bool {
	auth, ok := authContextFromRequest(r)
	if !ok {
		jsonResponse(w, 401, APIResponse{Success: false, Message: "Authentication required"})
		return false
	}
	if (auth.Type != authTypeAdmin && auth.Type != authTypeAPIKey) || auth.Type == authTypeAPIKey && (!scopeAllowed(auth.Scopes, "admin:access") || isAccessRestrictedRequest(r)) {
		jsonResponse(w, 403, APIResponse{Success: false, Message: "Administrator permission required"})
		return false
	}
	return true
}

func transferPath(r *http.Request, prefix string) []string {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1")
	path = strings.TrimPrefix(path, "/api")
	path = strings.TrimPrefix(path, prefix)
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}

func HandleBackups(w http.ResponseWriter, r *http.Request) {
	if !requireTransferAdmin(w, r) {
		return
	}
	parts := transferPath(r, "/backups")
	if len(parts) == 0 {
		switch r.Method {
		case http.MethodGet:
			jsonResponse(w, 200, APIResponse{Success: true, Data: transfer.List("backup")})
		case http.MethodPost:
			uploadPortableBackup(w, r)
		default:
			w.WriteHeader(405)
		}
		return
	}
	info, path, err := transfer.Resolve(parts[0])
	if err != nil || info.Kind != "backup" {
		jsonResponse(w, 404, APIResponse{Success: false, Message: "Backup not found"})
		return
	}
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			servePortableArtifact(w, r, info, path)
		case http.MethodDelete:
			if err = transfer.Delete(info.ID, "backup"); err != nil {
				jsonResponse(w, 500, APIResponse{Success: false, Message: err.Error()})
			} else {
				config.AddAuditLog("backup.delete", info.SourceName, info.ID, requestActor(r))
				jsonResponse(w, 200, APIResponse{Success: true})
			}
		default:
			w.WriteHeader(405)
		}
		return
	}
	if len(parts) == 2 && r.Method == http.MethodPost {
		switch parts[1] {
		case "restore":
			enqueueArtifactRestore(w, r, info.ID, "backup")
			return
		case "download-ticket":
			issueBackupTicket(w, r, info.ID)
			return
		}
	}
	http.NotFound(w, r)
}

func HandleInstanceTemplates(w http.ResponseWriter, r *http.Request) {
	if !requireTransferAdmin(w, r) {
		return
	}
	parts := transferPath(r, "/instance-templates")
	if len(parts) == 0 && r.Method == http.MethodGet {
		jsonResponse(w, 200, APIResponse{Success: true, Data: transfer.List("template")})
		return
	}
	if len(parts) == 0 {
		w.WriteHeader(405)
		return
	}
	info, _, err := transfer.Resolve(parts[0])
	if err != nil || info.Kind != "template" {
		jsonResponse(w, 404, APIResponse{Success: false, Message: "Template not found"})
		return
	}
	if len(parts) == 1 && r.Method == http.MethodDelete {
		if err = transfer.Delete(info.ID, "template"); err != nil {
			jsonResponse(w, 500, APIResponse{Success: false, Message: err.Error()})
		} else {
			config.AddAuditLog("template.delete", info.SourceName, info.ID, requestActor(r))
			jsonResponse(w, 200, APIResponse{Success: true})
		}
		return
	}
	if len(parts) == 2 && parts[1] == "instantiate" && r.Method == http.MethodPost {
		enqueueArtifactRestore(w, r, info.ID, "template")
		return
	}
	http.NotFound(w, r)
}

func decodeTransferRequest(w http.ResponseWriter, r *http.Request) (transferRequest, bool) {
	var request transferRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil && err != io.EOF {
		jsonResponse(w, 400, APIResponse{Success: false, Message: "Invalid transfer request"})
		return request, false
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		jsonResponse(w, 400, APIResponse{Success: false, Message: "Request must contain one JSON object"})
		return request, false
	}
	request.Name = strings.TrimSpace(request.Name)
	request.StoragePoolID = strings.TrimSpace(request.StoragePoolID)
	if request.PortMappingCount == 0 {
		request.PortMappingCount = 2
	}
	if request.PortMappingCount < 1 || request.PortMappingCount > 64 {
		jsonResponse(w, 400, APIResponse{Success: false, Message: "port_mapping_count must be between 1 and 64"})
		return request, false
	}
	return request, true
}

func HandleContainerTransfer(w http.ResponseWriter, r *http.Request, id int, action string) {
	if !requireTransferAdmin(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	c := config.FindContainer(id)
	if c == nil {
		jsonResponse(w, 404, APIResponse{Success: false, Message: "Instance not found"})
		return
	}
	request, ok := decodeTransferRequest(w, r)
	if !ok {
		return
	}
	kind := TaskType("backup")
	additional := []string{}
	switch action {
	case "backup":
		if request.Name == "" {
			request.Name = c.Name + " " + time.Now().Format("2006-01-02 15:04")
		}
	case "template":
		kind = TaskType("save-template")
	case "clone":
		kind = TaskType("clone")
		if !validRestoreName(w, request.Name) {
			return
		}
		additional = append(additional, request.Name)
	default:
		http.NotFound(w, r)
		return
	}
	if request.Name == "" || utf8.RuneCountInString(request.Name) > 128 {
		jsonResponse(w, 400, APIResponse{Success: false, Message: "A name of 1-128 characters is required"})
		return
	}
	payload, _ := json.Marshal(request)
	task, err := EnqueueOperationWithAudit(id, c.Name, kind, payload, requestActor(r), clientIP(r), r.UserAgent(), additional...)
	writeTransferTask(w, task, err)
}

func validRestoreName(w http.ResponseWriter, name string) bool {
	if !config.IsValidContainerName(name) || config.FindContainerByName(name) != nil {
		jsonResponse(w, 409, APIResponse{Success: false, Message: "Invalid or already used instance name"})
		return false
	}
	return true
}

func enqueueArtifactRestore(w http.ResponseWriter, r *http.Request, id, kind string) {
	request, ok := decodeTransferRequest(w, r)
	if !ok || !validRestoreName(w, request.Name) {
		return
	}
	request.ArtifactID = id
	request.Kind = kind
	payload, _ := json.Marshal(request)
	operation := TaskType("restore-backup")
	if kind == "template" {
		operation = TaskType("instantiate-template")
	}
	task, err := EnqueueOperationWithAudit(0, request.Name, operation, payload, requestActor(r), clientIP(r), r.UserAgent())
	writeTransferTask(w, task, err)
}

func writeTransferTask(w http.ResponseWriter, task string, err error) {
	if err != nil {
		jsonResponse(w, 409, APIResponse{Success: false, Message: err.Error()})
		return
	}
	jsonResponse(w, 202, APIResponse{Success: true, Data: map[string]string{"task_id": task}})
}

func validatePortable(ctx context.Context, dir string, m *transfer.Manifest) error {
	if m.Runtime == "kvm" {
		return kvm.ValidateTransfer(ctx, dir, m)
	}
	return lxc.ValidateTransfer(ctx, dir, m)
}

func exportPortable(ctx context.Context, sourceID int, request transferRequest, kind string, progress func(string, string)) (transfer.Info, error) {
	c := config.FindContainer(sourceID)
	if c == nil {
		return transfer.Info{}, fmt.Errorf("source instance no longer exists")
	}
	source := *c
	progress("cold_copy", "Stopping source, copying every disk, and restoring its original power state")
	exporter := func(ctx context.Context, dir string, m *transfer.Manifest) error {
		if source.IsKVM() {
			return kvm.NewManagerForStoragePool(source.StoragePoolID).ExportTransfer(ctx, sourceID, dir, m)
		}
		return lxc.NewManager().ExportTransfer(ctx, sourceID, dir, m)
	}
	info, err := transfer.Create(ctx, request.StoragePoolID, request.Name, kind, source, exporter)
	if err == nil {
		progress("verified", "Self-contained disks and manifest checksums saved")
	}
	return info, err
}

func runCreateBackup(ctx context.Context, id int, payload json.RawMessage, progress func(string, string)) error {
	var request transferRequest
	if err := json.Unmarshal(payload, &request); err != nil {
		return err
	}
	info, err := exportPortable(ctx, id, request, "backup", progress)
	if err != nil {
		return err
	}
	return SetOperationTaskResult(ctx, map[string]interface{}{"artifact_id": info.ID, "artifact": info})
}

func runCreateTemplate(ctx context.Context, id int, payload json.RawMessage, progress func(string, string)) error {
	var request transferRequest
	if err := json.Unmarshal(payload, &request); err != nil {
		return err
	}
	info, err := exportPortable(ctx, id, request, "template", progress)
	if err != nil {
		return err
	}
	return SetOperationTaskResult(ctx, map[string]interface{}{"artifact_id": info.ID, "artifact": info})
}

func restorePortable(ctx context.Context, request transferRequest, kind string, progress func(string, string)) (*config.Container, error) {
	progress("verify", "Verifying manifest, architecture and every disk checksum")
	manifest, dir, cleanup, err := transfer.OpenVerified(ctx, request.ArtifactID, kind, validatePortable)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	progress("restore", "Creating a stopped instance with a new identity and independent storage")
	assignNAT := request.AssignNAT == nil || *request.AssignNAT
	if manifest.Runtime == "kvm" {
		return kvm.NewManager().RestoreTransfer(ctx, manifest, dir, request.Name, request.StoragePoolID, assignNAT, request.PortMappingCount)
	}
	return lxc.NewManager().RestoreTransfer(ctx, manifest, dir, request.Name, request.StoragePoolID, assignNAT, request.PortMappingCount)
}

func containerTransferResult(ctx context.Context, c *config.Container) error {
	return SetOperationTaskResult(ctx, map[string]interface{}{"container_id": c.ID, "container_uuid": c.UUID, "name": c.Name, "status": "stopped", "credentials_preserved": true})
}

func runRestoreBackup(ctx context.Context, _ int, payload json.RawMessage, progress func(string, string)) error {
	var request transferRequest
	if err := json.Unmarshal(payload, &request); err != nil {
		return err
	}
	c, err := restorePortable(ctx, request, "backup", progress)
	if err != nil {
		return err
	}
	return containerTransferResult(ctx, c)
}

func runInstantiateTemplate(ctx context.Context, _ int, payload json.RawMessage, progress func(string, string)) error {
	var request transferRequest
	if err := json.Unmarshal(payload, &request); err != nil {
		return err
	}
	c, err := restorePortable(ctx, request, "template", progress)
	if err != nil {
		return err
	}
	return containerTransferResult(ctx, c)
}

func runFullClone(ctx context.Context, id int, payload json.RawMessage, progress func(string, string)) error {
	var request transferRequest
	if err := json.Unmarshal(payload, &request); err != nil {
		return err
	}
	archiveRequest := request
	archiveRequest.StoragePoolID = ""
	artifact, err := exportPortable(ctx, id, archiveRequest, "backup", progress)
	if err != nil {
		return err
	}
	defer transfer.Delete(artifact.ID, "backup")
	request.ArtifactID = artifact.ID
	c, err := restorePortable(ctx, request, "backup", progress)
	if err != nil {
		return err
	}
	return containerTransferResult(ctx, c)
}

func uploadPortableBackup(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, transfer.MaxArchiveBytes+(4<<20))
	reader := io.Reader(r.Body)
	pool, name := strings.TrimSpace(r.URL.Query().Get("storage_pool_id")), ""
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		multipart, err := r.MultipartReader()
		if err != nil {
			jsonResponse(w, 400, APIResponse{Success: false, Message: "Invalid multipart upload"})
			return
		}
		var upload *os.File
		seen := map[string]bool{}
		for {
			part, e := multipart.NextPart()
			if e == io.EOF {
				break
			}
			if e != nil {
				jsonResponse(w, 400, APIResponse{Success: false, Message: "Incomplete multipart upload"})
				return
			}
			field := part.FormName()
			if seen[field] || (field != "file" && field != "name" && field != "storage_pool_id") {
				jsonResponse(w, 400, APIResponse{Success: false, Message: "Unexpected or duplicate upload field"})
				return
			}
			seen[field] = true
			if field == "file" {
				file, cleanup, e := transfer.SpoolUpload(r.Context(), part, pool)
				if e != nil {
					jsonResponse(w, 400, APIResponse{Success: false, Message: e.Error()})
					return
				}
				part.Close()
				defer cleanup()
				upload = file
			} else {
				data, e := io.ReadAll(io.LimitReader(part, 4097))
				part.Close()
				if e != nil || len(data) > 4096 {
					jsonResponse(w, 400, APIResponse{Success: false, Message: "Upload field too large"})
					return
				}
				if field == "name" {
					name = string(data)
				} else {
					requested := strings.TrimSpace(string(data))
					if pool != "" && requested != "" && pool != requested {
						jsonResponse(w, 400, APIResponse{Success: false, Message: "Conflicting storage_pool_id values"})
						return
					}
					if requested != "" {
						pool = requested
					}
				}
			}
		}
		if upload == nil {
			jsonResponse(w, 400, APIResponse{Success: false, Message: "Backup file field is required"})
			return
		}
		reader = upload
	}
	info, err := transfer.Import(r.Context(), pool, name, reader, validatePortable)
	if err != nil {
		jsonResponse(w, 400, APIResponse{Success: false, Message: err.Error()})
		return
	}
	config.AddAuditLog("backup.import", info.SourceName, info.ID, requestActor(r))
	jsonResponse(w, 201, APIResponse{Success: true, Data: info})
}

func servePortableArtifact(w http.ResponseWriter, r *http.Request, info transfer.Info, path string) {
	file, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": info.ID + ".clicd.tar.gz"}))
	http.ServeContent(w, r, info.ID+".clicd.tar.gz", stat.ModTime(), file)
}

type backupTicket struct {
	ID      string
	Expires time.Time
}

var backupTickets = struct {
	sync.Mutex
	Items map[string]backupTicket
}{Items: map[string]backupTicket{}}

func issueBackupTicket(w http.ResponseWriter, r *http.Request, id string) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		jsonResponse(w, 500, APIResponse{Success: false, Message: "Cannot create download ticket"})
		return
	}
	token := hex.EncodeToString(bytes[:])
	expiry := time.Now().Add(2 * time.Minute)
	backupTickets.Lock()
	for key, ticket := range backupTickets.Items {
		if time.Now().After(ticket.Expires) {
			delete(backupTickets.Items, key)
		}
	}
	if len(backupTickets.Items) >= 1024 {
		backupTickets.Unlock()
		jsonResponse(w, 429, APIResponse{Success: false, Message: "Too many pending download tickets"})
		return
	}
	backupTickets.Items[token] = backupTicket{ID: id, Expires: expiry}
	backupTickets.Unlock()
	prefix := "/api/backup-download/"
	if strings.HasPrefix(r.URL.Path, "/api/v1/") {
		prefix = "/api/v1/backup-download/"
	}
	jsonResponse(w, 200, APIResponse{Success: true, Data: map[string]string{"url": prefix + token, "expires_at": expiry.UTC().Format(time.RFC3339)}})
}

// This route intentionally accepts only an opaque, short-lived scoped ticket.
// It streams through ServeContent (including Range), never exposing JWTs in URLs.
func HandleBackupDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(405)
		return
	}
	parts := transferPath(r, "/backup-download")
	if len(parts) != 1 || len(parts[0]) != 64 {
		http.NotFound(w, r)
		return
	}
	backupTickets.Lock()
	ticket, ok := backupTickets.Items[parts[0]]
	if ok && time.Now().After(ticket.Expires) {
		delete(backupTickets.Items, parts[0])
		ok = false
	}
	backupTickets.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	info, path, err := transfer.Resolve(ticket.ID)
	if err != nil || info.Kind != "backup" {
		http.NotFound(w, r)
		return
	}
	servePortableArtifact(w, r, info, path)
}
