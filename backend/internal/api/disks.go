package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"clicd/internal/config"
)

const (
	TaskDiskAdd    TaskType = "disk-add"
	TaskDiskResize TaskType = "disk-resize"
	TaskDiskRemove TaskType = "disk-remove"
	TaskDiskAttach TaskType = "disk-attach"
)

type diskOperationPayload struct {
	UUID          string  `json:"uuid"`
	DiskID        string  `json:"disk_id,omitempty"`
	Name          string  `json:"name,omitempty"`
	SizeGB        float64 `json:"size_gb,omitempty"`
	StoragePoolID string  `json:"storage_pool_id,omitempty"`
	MountPath     string  `json:"mount_path,omitempty"`
	Attached      bool    `json:"attached,omitempty"`
}

func init() {
	for _, kind := range []TaskType{TaskDiskAdd, TaskDiskResize, TaskDiskRemove, TaskDiskAttach} {
		operation := kind
		RegisterOperationTask(operation, func(ctx context.Context, id int, raw json.RawMessage, progress func(string, string)) error {
			return runDiskOperation(ctx, id, operation, raw, progress)
		})
	}
}
func diskInventory(ctx context.Context, c *config.Container) (config.DiskInventory, error) {
	if c.IsKVM() {
		return kvmManager.GetDisks(ctx, c.ID)
	}
	return lxcManager.GetDisks(ctx, c.ID)
}
func runDiskOperation(ctx context.Context, id int, kind TaskType, raw json.RawMessage, progress func(string, string)) error {
	var payload diskOperationPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return err
	}
	c := config.FindContainer(id)
	if c == nil || c.UUID != payload.UUID {
		return fmt.Errorf("operation target no longer exists")
	}
	isKVM := c.IsKVM()
	progress("checking", "检查实例停止状态与受管磁盘")
	switch kind {
	case TaskDiskAdd:
		progress("creating_disk", "创建数据盘并保存挂载配置")
		var disk config.DataDisk
		var err error
		if isKVM {
			if payload.MountPath != "" {
				return fmt.Errorf("mount_path is only supported for LXC")
			}
			disk, err = kvmManager.AddDataDisk(ctx, id, payload.Name, payload.SizeGB, payload.StoragePoolID)
		} else {
			disk, err = lxcManager.AddDataDisk(ctx, id, payload.Name, payload.SizeGB, payload.StoragePoolID, payload.MountPath)
		}
		if err != nil {
			return err
		}
		_ = SetOperationTaskResult(ctx, config.PublicDataDisk(disk))
		return nil
	case TaskDiskResize:
		progress("resizing_disk", "扩容受管磁盘；不会缩小磁盘或修改 KVM 客户机分区")
		if isKVM {
			return kvmManager.ResizeDisk(ctx, id, payload.DiskID, payload.SizeGB)
		}
		return lxcManager.ResizeDisk(ctx, id, payload.DiskID, payload.SizeGB)
	case TaskDiskRemove:
		progress("removing_disk", "移除数据盘配置并删除受管数据文件")
		if isKVM {
			return kvmManager.DeleteDataDisk(ctx, id, payload.DiskID)
		}
		return lxcManager.DeleteDataDisk(ctx, id, payload.DiskID)
	case TaskDiskAttach:
		progress("updating_disk", "保存数据盘连接状态")
		if isKVM {
			return kvmManager.SetDataDiskAttached(ctx, id, payload.DiskID, payload.Attached)
		}
		return lxcManager.SetDataDiskAttached(ctx, id, payload.DiskID, payload.Attached)
	}
	return fmt.Errorf("unsupported disk operation")
}

// HandleContainerDisks is dispatched by HandleSingleContainer after ownership
// validation. Mutations require administrator resize privileges.
func HandleContainerDisks(w http.ResponseWriter, r *http.Request, id int, action string) {
	c := config.FindContainer(id)
	if c == nil {
		jsonResponse(w, 404, APIResponse{Success: false, Message: "Container not found"})
		return
	}
	if !isContainerAllowedForRequest(r, c.UUID) {
		jsonResponse(w, 403, APIResponse{Success: false, Message: "Access denied to this container"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if action == "disks" && r.Method == http.MethodGet {
		if !requireScope(w, r, "container:read") {
			return
		}
		inventory, err := diskInventory(ctx, c)
		if err != nil {
			jsonResponse(w, 400, APIResponse{Success: false, Message: err.Error()})
			return
		}
		jsonResponse(w, 200, APIResponse{Success: true, Data: inventory})
		return
	}
	if isSubUserRequest(r) || isAccessRestrictedRequest(r) {
		jsonResponse(w, 403, APIResponse{Success: false, Message: "Administrator permission required"})
		return
	}
	if !requireScope(w, r, "container:resize") {
		return
	}
	var request struct {
		Name          string   `json:"name"`
		SizeGB        *float64 `json:"size_gb"`
		StoragePoolID string   `json:"storage_pool_id"`
		MountPath     string   `json:"mount_path"`
		Attached      *bool    `json:"attached"`
		DeleteData    *bool    `json:"delete_data"`
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 8192))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		jsonResponse(w, 400, APIResponse{Success: false, Message: "Invalid disk request"})
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		jsonResponse(w, 400, APIResponse{Success: false, Message: "Invalid disk request body"})
		return
	}
	payload := diskOperationPayload{UUID: c.UUID, Name: strings.TrimSpace(request.Name), StoragePoolID: request.StoragePoolID, MountPath: request.MountPath}
	kind := TaskType("")
	suffix := strings.TrimPrefix(action, "disks/")
	switch {
	case action == "disks" && r.Method == http.MethodPost:
		kind = TaskDiskAdd
		if c.SnapshotScheduleEnabled {
			jsonResponse(w, 409, APIResponse{Success: false, Message: "Disable scheduled snapshots before adding data disks; use full backups to include all disks"})
			return
		}
		if payload.Name == "" || utf8.RuneCountInString(payload.Name) > 80 {
			jsonResponse(w, 400, APIResponse{Success: false, Message: "Disk name must contain 1 to 80 characters"})
			return
		}
		if !c.IsKVM() {
			if err := config.ValidateGuestMountPath(payload.MountPath); err != nil {
				jsonResponse(w, 400, APIResponse{Success: false, Message: err.Error()})
				return
			}
		} else if payload.MountPath != "" {
			jsonResponse(w, 400, APIResponse{Success: false, Message: "mount_path is only supported for LXC"})
			return
		}
	case strings.HasSuffix(suffix, "/attachment") && r.Method == http.MethodPut:
		kind = TaskDiskAttach
		payload.DiskID = strings.TrimSuffix(suffix, "/attachment")
		if request.Attached == nil {
			jsonResponse(w, 400, APIResponse{Success: false, Message: "attached is required"})
			return
		}
		payload.Attached = *request.Attached
	case action != "disks" && !strings.Contains(suffix, "/") && r.Method == http.MethodPut:
		kind = TaskDiskResize
		payload.DiskID = suffix
	case action != "disks" && !strings.Contains(suffix, "/") && r.Method == http.MethodDelete:
		kind = TaskDiskRemove
		payload.DiskID = suffix
		if request.DeleteData == nil || !*request.DeleteData {
			jsonResponse(w, 400, APIResponse{Success: false, Message: "Deleting a disk requires explicit delete_data: true"})
			return
		}
	default:
		jsonResponse(w, 405, APIResponse{Success: false, Message: "Unsupported disk operation"})
		return
	}
	if kind == TaskDiskAdd || kind == TaskDiskResize {
		if request.SizeGB == nil {
			jsonResponse(w, 400, APIResponse{Success: false, Message: "size_gb is required"})
			return
		}
		if _, err := config.DiskSizeBytes(*request.SizeGB); err != nil {
			jsonResponse(w, 400, APIResponse{Success: false, Message: err.Error()})
			return
		}
		payload.SizeGB = *request.SizeGB
	}
	if kind != TaskDiskAdd && payload.DiskID != "system" {
		found := false
		for _, disk := range c.DataDisks {
			if disk.ID == payload.DiskID {
				found = true
				break
			}
		}
		if !found {
			jsonResponse(w, 404, APIResponse{Success: false, Message: "Data disk not found"})
			return
		}
	}
	if payload.DiskID == "system" && kind != TaskDiskResize {
		jsonResponse(w, 400, APIResponse{Success: false, Message: "System disks cannot be detached or deleted"})
		return
	}
	inventory, err := diskInventory(ctx, c)
	if err != nil {
		jsonResponse(w, 400, APIResponse{Success: false, Message: err.Error()})
		return
	}
	if !inventory.CanEdit {
		jsonResponse(w, 409, APIResponse{Success: false, Message: inventory.Reason})
		return
	}
	if kind == TaskDiskAdd && len(c.DataDisks) >= config.MaxDataDisks {
		jsonResponse(w, 400, APIResponse{Success: false, Message: "Data disk limit reached"})
		return
	}
	if kind == TaskDiskResize {
		info := inventory.System
		if payload.DiskID != "system" {
			for _, disk := range inventory.DataDisks {
				if disk.ID == payload.DiskID {
					info = disk
					break
				}
			}
		}
		if !info.CanResize || info.Error != "" {
			jsonResponse(w, 400, APIResponse{Success: false, Message: "Disk cannot be resized: " + info.Error})
			return
		}
		if payload.SizeGB < info.SizeGB {
			jsonResponse(w, 400, APIResponse{Success: false, Message: "Disk shrinking is not supported"})
			return
		}
	}
	raw, _ := json.Marshal(payload)
	taskID, err := EnqueueOperationWithAudit(id, c.Name, kind, raw, requestActor(r), clientIP(r), r.UserAgent())
	if err != nil {
		jsonResponse(w, 409, APIResponse{Success: false, Message: err.Error()})
		return
	}
	jsonResponse(w, 202, APIResponse{Success: true, Message: "Disk operation queued", Data: map[string]interface{}{"task_id": taskID, "status": "pending"}})
}
