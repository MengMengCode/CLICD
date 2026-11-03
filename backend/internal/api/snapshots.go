package api

import (
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"clicd/internal/config"
)

func HandleSnapshots(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	if !requireScope(w, r, "snapshot:read") {
		return
	}
	snapshots := append([]config.Snapshot(nil), config.AppConfig.Snapshots...)
	snapshots = filterSnapshotsForRequest(r, snapshots)
	sortSnapshotsNewestFirst(snapshots)
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: snapshots})
}

func handleContainerSnapshots(w http.ResponseWriter, r *http.Request, containerID int, action string) {
	switch {
	case action == "snapshots" && r.Method == http.MethodGet:
		if !requireScope(w, r, "snapshot:read") {
			return
		}
		listContainerSnapshots(w, r, containerID)
	case action == "snapshots" && r.Method == http.MethodPost:
		if !requireScope(w, r, "snapshot:create") {
			return
		}
		createContainerSnapshot(w, r, containerID)
	case action == "snapshots/schedule" && r.Method == http.MethodPost:
		if !requireScope(w, r, "snapshot:schedule") {
			return
		}
		updateSnapshotSchedule(w, r, containerID)
	case action == "snapshots/quota" && r.Method == http.MethodPut:
		if !requireScope(w, r, "snapshot:schedule") {
			return
		}
		updateSnapshotQuota(w, r, containerID)
	case strings.HasPrefix(action, "snapshots/") && strings.HasSuffix(action, "/restore") && r.Method == http.MethodPost:
		if !requireScope(w, r, "snapshot:restore") {
			return
		}
		snapshotID := strings.TrimSuffix(strings.TrimPrefix(action, "snapshots/"), "/restore")
		restoreContainerSnapshot(w, r, containerID, snapshotID)
	case strings.HasPrefix(action, "snapshots/") && r.Method == http.MethodDelete:
		if !requireScope(w, r, "snapshot:delete") {
			return
		}
		snapshotID := strings.TrimPrefix(action, "snapshots/")
		deleteContainerSnapshot(w, r, containerID, snapshotID)
	default:
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Snapshot action not found"})
	}
}

func listContainerSnapshots(w http.ResponseWriter, r *http.Request, containerID int) {
	c := config.FindContainer(containerID)
	if c == nil {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Container not found"})
		return
	}
	snapshots := config.ContainerSnapshots(containerID)
	sortSnapshotsNewestFirst(snapshots)
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: map[string]interface{}{
		"snapshots": snapshots,
		"quota":     config.ContainerSnapshotLimit(c),
		"schedule": map[string]interface{}{
			"enabled":        c.SnapshotScheduleEnabled,
			"interval_hours": c.SnapshotScheduleIntervalHours,
			"time":           c.SnapshotScheduleTime,
			"last_run":       c.SnapshotScheduleLastRun,
			"next_run":       c.SnapshotScheduleNextRun,
			"created_by":     c.SnapshotScheduleCreatedBy,
		},
	}})
}

func createContainerSnapshot(w http.ResponseWriter, r *http.Request, containerID int) {
	c := config.FindContainer(containerID)
	if c == nil {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Container not found"})
		return
	}
	if len(c.DataDisks) > 0 {
		jsonResponse(w, http.StatusConflict, APIResponse{Success: false, Message: "Instances with data disks require full backup and restore; system-disk snapshots are unavailable"})
		return
	}
	user := requestUser(r)
	var req struct {
		StoragePoolID string `json:"storage_pool_id"`
	}
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
			return
		}
	}
	req.StoragePoolID = strings.TrimSpace(req.StoragePoolID)
	if _, err := config.SelectStoragePoolForContent(config.StorageContentSnapshots, req.StoragePoolID, 0); err != nil {
		jsonResponse(w, http.StatusConflict, APIResponse{Success: false, Message: err.Error()})
		return
	}
	if isSubUserRequest(r) {
		limit := config.ContainerSnapshotLimit(c)
		if len(config.ContainerSnapshots(containerID)) >= limit {
			jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "Snapshot quota reached. Delete an old snapshot first."})
			return
		}
	}
	snapshot, err := createSnapshotByRuntime(containerID, user, false, 0, req.StoragePoolID)
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: err.Error()})
		return
	}
	config.AddAuditLog("snapshot.create", snapshot.ContainerName, snapshot.ID, user)
	jsonResponse(w, http.StatusCreated, APIResponse{Success: true, Data: snapshot})
}

func updateSnapshotQuota(w http.ResponseWriter, r *http.Request, containerID int) {
	if isSubUserRequest(r) {
		jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "Sub-users cannot change snapshot quota"})
		return
	}
	var req struct {
		SnapshotLimit int `json:"snapshot_limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
		return
	}
	if req.SnapshotLimit <= 0 {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Snapshot quota must be at least 1"})
		return
	}
	c := config.FindContainer(containerID)
	if c == nil {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Container not found"})
		return
	}
	c.SnapshotLimit = req.SnapshotLimit
	if err := config.SaveConfig(); err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Failed to save config"})
		return
	}
	user := requestUser(r)
	config.AddAuditLog("snapshot.quota", c.Name, "limit="+strconv.Itoa(req.SnapshotLimit), user)
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: map[string]interface{}{
		"container": c,
		"quota":     c.SnapshotLimit,
	}})
}

func updateSnapshotSchedule(w http.ResponseWriter, r *http.Request, containerID int) {
	var req struct {
		Enabled       bool   `json:"enabled"`
		IntervalHours int    `json:"interval_hours"`
		Time          string `json:"time"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
		return
	}
	if req.IntervalHours <= 0 {
		req.IntervalHours = 24
	}
	if req.IntervalHours < 24 {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Snapshot schedule interval cannot be less than 24 hours"})
		return
	}
	if req.Time == "" {
		req.Time = "03:00"
	}
	if req.Enabled {
		if _, err := config.SelectStoragePoolForContent(config.StorageContentSnapshots, "", 0); err != nil {
			jsonResponse(w, http.StatusConflict, APIResponse{Success: false, Message: err.Error()})
			return
		}
	}
	user := requestUser(r)
	c, err := setSnapshotScheduleByRuntime(containerID, req.Enabled, req.IntervalHours, req.Time, user)
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	}

	if req.Enabled {
		config.AddAuditLog("snapshot.schedule", c.Name, "enabled", user)
	} else {
		config.AddAuditLog("snapshot.schedule", c.Name, "disabled", user)
	}

	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: map[string]interface{}{
		"container": c,
	}})
}

func deleteContainerSnapshot(w http.ResponseWriter, r *http.Request, containerID int, snapshotID string) {
	snapshot := config.FindSnapshot(snapshotID)
	if snapshot == nil || snapshot.ContainerID != containerID {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Snapshot not found"})
		return
	}
	user := requestUser(r)
	if err := deleteSnapshotByRuntime(snapshotID); err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: err.Error()})
		return
	}
	config.AddAuditLog("snapshot.delete", snapshot.ContainerName, snapshot.ID, user)
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "Snapshot deleted"})
}

func restoreContainerSnapshot(w http.ResponseWriter, r *http.Request, containerID int, snapshotID string) {
	snapshot := config.FindSnapshot(snapshotID)
	if snapshot == nil || snapshot.ContainerID != containerID {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Snapshot not found"})
		return
	}
	c := config.FindContainer(containerID)
	if c == nil {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Container not found"})
		return
	}
	if len(c.DataDisks) > 0 {
		jsonResponse(w, http.StatusConflict, APIResponse{Success: false, Message: "Instances with data disks require full backup and restore; system-disk snapshots are unavailable"})
		return
	}
	user := requestUser(r)
	if err := restoreSnapshotByRuntime(snapshotID); err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: err.Error()})
		return
	}
	config.AddAuditLog("snapshot.restore", snapshot.ContainerName, snapshot.ID, user)
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "Snapshot restored"})
}

func requestUser(r *http.Request) string {
	return requestActor(r)
}

func sortSnapshotsNewestFirst(snapshots []config.Snapshot) {
	sort.SliceStable(snapshots, func(i, j int) bool {
		ti, _ := time.Parse("2006-01-02 15:04:05", snapshots[i].CreatedAt)
		tj, _ := time.Parse("2006-01-02 15:04:05", snapshots[j].CreatedAt)
		return tj.Before(ti)
	})
}

func filterSnapshotsForRequest(r *http.Request, snapshots []config.Snapshot) []config.Snapshot {
	allowed, restricted := requestAllowedContainers(r)
	if !restricted {
		return snapshots
	}
	filtered := make([]config.Snapshot, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if c := config.FindContainer(snapshot.ContainerID); c != nil && isContainerAllowed(allowed, c) {
			filtered = append(filtered, snapshot)
		}
	}
	return filtered
}
