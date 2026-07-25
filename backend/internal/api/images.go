package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"clicd/internal/config"
	"clicd/internal/kvm"
	"clicd/internal/lxc"
)

// ImageInfo represents a template image with its download/enable status.
type ImageInfo struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Type            string `json:"type"`
	Distro          string `json:"distro"`
	Release         string `json:"release"`
	Arch            string `json:"arch"`
	Description     string `json:"description"`
	Downloaded      bool   `json:"downloaded"`
	Enabled         bool   `json:"enabled"`
	Downloading     bool   `json:"downloading"`
	Progress        int    `json:"progress"`
	DownloadedBytes int64  `json:"downloaded_bytes"`
	TotalBytes      int64  `json:"total_bytes"`
	Stage           string `json:"stage,omitempty"`
	Error           string `json:"error,omitempty"`
	SizeBytes       int64  `json:"size_bytes"`
	ManualPath      string `json:"manual_path,omitempty"`
	Desktop         string `json:"desktop,omitempty"`
	Provisioner     string `json:"provisioner,omitempty"`
	Custom          bool   `json:"custom,omitempty"`
	SHA256          string `json:"sha256,omitempty"`
}

var customImageFieldPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
var sha256Pattern = regexp.MustCompile(`^[a-fA-F0-9]{64}$`)

var imageDownloadsMu sync.Mutex
var imageDownloads = map[string]*imageDownloadStatus{}
var lxcImageCacheMu sync.Mutex
var lxcImageDownloadMu sync.Mutex
var lxcImageDownloadActive bool

type imageDownloadStatus struct {
	Downloading     bool
	Progress        int
	DownloadedBytes int64
	TotalBytes      int64
	Stage           string
	Error           string
	Cancel          context.CancelFunc
	UpdatedAt       time.Time
}

type imageDownloadSnapshot struct {
	Downloading     bool
	Progress        int
	DownloadedBytes int64
	TotalBytes      int64
	Stage           string
	Error           string
}

func imageDownloadInfo(id string) imageDownloadSnapshot {
	imageDownloadsMu.Lock()
	defer imageDownloadsMu.Unlock()
	st := imageDownloads[id]
	if st == nil {
		return imageDownloadSnapshot{}
	}
	return imageDownloadSnapshot{
		Downloading:     st.Downloading,
		Progress:        st.Progress,
		DownloadedBytes: st.DownloadedBytes,
		TotalBytes:      st.TotalBytes,
		Stage:           st.Stage,
		Error:           st.Error,
	}
}

func startImageDownload(id, stage string) (context.Context, bool) {
	imageDownloadsMu.Lock()
	defer imageDownloadsMu.Unlock()
	if st := imageDownloads[id]; st != nil && st.Downloading {
		return nil, false
	}
	ctx, cancel := context.WithCancel(context.Background())
	imageDownloads[id] = &imageDownloadStatus{
		Downloading: true,
		Stage:       stage,
		Cancel:      cancel,
		UpdatedAt:   time.Now(),
	}
	return ctx, true
}

func updateImageDownload(id string, update func(*imageDownloadStatus)) {
	imageDownloadsMu.Lock()
	defer imageDownloadsMu.Unlock()
	st := imageDownloads[id]
	if st == nil {
		return
	}
	update(st)
	st.UpdatedAt = time.Now()
}

func finishImageDownload(id string, err error) {
	imageDownloadsMu.Lock()
	defer imageDownloadsMu.Unlock()
	st := imageDownloads[id]
	if st == nil {
		return
	}
	st.Downloading = false
	st.Cancel = nil
	st.UpdatedAt = time.Now()
	if err != nil {
		st.Error = err.Error()
		return
	}
	delete(imageDownloads, id)
}

func clearImageDownload(id string) {
	imageDownloadsMu.Lock()
	delete(imageDownloads, id)
	imageDownloadsMu.Unlock()
}

func isImageDownloadActive(id string) bool {
	imageDownloadsMu.Lock()
	defer imageDownloadsMu.Unlock()
	st := imageDownloads[id]
	return st != nil && st.Downloading
}

func beginLXCImageDownload() bool {
	lxcImageDownloadMu.Lock()
	defer lxcImageDownloadMu.Unlock()
	if lxcImageDownloadActive {
		return false
	}
	lxcImageDownloadActive = true
	return true
}

func endLXCImageDownload() {
	lxcImageDownloadMu.Lock()
	lxcImageDownloadActive = false
	lxcImageDownloadMu.Unlock()
}

func lxcImageDownloadTempName(id string) string {
	return fmt.Sprintf("clicd-img-dl-%s", id)
}

func cleanupLXCImageDownloadTemp(id string) {
	tmpName := lxcImageDownloadTempName(id)
	exec.Command("lxc-destroy", "-n", tmpName, "-f").Run()
	os.RemoveAll(filepath.Join("/var/lib/lxc", tmpName))
}

func cleanupOldImageDownloadErrors() {
	imageDownloadsMu.Lock()
	defer imageDownloadsMu.Unlock()
	cutoff := time.Now().Add(-10 * time.Minute)
	for id, st := range imageDownloads {
		if !st.Downloading && st.UpdatedAt.Before(cutoff) {
			delete(imageDownloads, id)
		}
	}
}

// isImageDownloaded checks if the LXC download cache exists for a template.
func isImageDownloaded(distro, release, arch string) bool {
	downloaded, _ := imageDownloadedInfo(distro, release, arch)
	return downloaded
}

// imageDownloadedInfo returns whether the image is downloaded and its total size in bytes.
func imageDownloadedInfo(distro, release, arch string) (bool, int64) {
	cachePath := filepath.Join("/var/cache/lxc/download", distro, release, arch)
	info, err := os.Stat(cachePath)
	if err != nil || !info.IsDir() {
		return false, 0
	}
	// Check directly for rootfs.tar.xz (some LXC versions store it here)
	if fi, err := os.Stat(filepath.Join(cachePath, "rootfs.tar.xz")); err == nil {
		return true, fi.Size()
	}
	if fi, err := os.Stat(filepath.Join(cachePath, "meta.tar.xz")); err == nil {
		return true, fi.Size()
	}
	// Check one level deeper (LXC uses variant subdirectories like "default")
	entries, err := os.ReadDir(cachePath)
	if err != nil {
		return false, 0
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		subPath := filepath.Join(cachePath, entry.Name())
		if fi, err := os.Stat(filepath.Join(subPath, "rootfs.tar.xz")); err == nil {
			return true, fi.Size()
		}
		if fi, err := os.Stat(filepath.Join(subPath, "meta.tar.xz")); err == nil {
			return true, fi.Size()
		}
	}
	return false, 0
}

func lxcTemplateDownloadedInfo(template lxc.Template) (bool, int64) {
	if template.Custom {
		return lxc.CustomImageDownloadedInfo(template.ID)
	}
	return imageDownloadedInfo(template.Distro, template.Release, template.Arch)
}

// getEnabledImageSet returns the set of enabled image IDs.
// If none have been explicitly set, all templates are enabled by default.
func getEnabledImageSet() map[string]bool {
	set := make(map[string]bool)
	if len(config.AppConfig.EnabledImages) == 0 {
		for _, t := range lxc.GetTemplates() {
			set[t.ID] = true
		}
		for _, t := range kvm.GetImages() {
			set[t.ID] = true
		}
	} else {
		for _, id := range config.AppConfig.EnabledImages {
			set[id] = true
		}
	}
	return set
}

// HandleImages returns the list of templates with download/enable status.
func HandleImages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	if !requireScope(w, r, "image:read") {
		return
	}

	enabledSet := getEnabledImageSet()
	cleanupOldImageDownloadErrors()
	kvmAvailable := hostKVMAvailable()

	templates := lxc.GetTemplates()
	kvmImages := []kvm.Image{}
	if kvmAvailable {
		kvmImages = kvm.GetImages()
	}
	images := make([]ImageInfo, 0, len(templates)+len(kvmImages))
	for _, t := range templates {
		dl := imageDownloadInfo(t.ID)
		downloaded, size := lxcTemplateDownloadedInfo(t)
		images = append(images, ImageInfo{
			ID:              t.ID,
			Name:            t.Name,
			Type:            config.VirtualizationLXC,
			Distro:          t.Distro,
			Release:         t.Release,
			Arch:            t.Arch,
			Description:     t.Description,
			Downloaded:      downloaded,
			Enabled:         enabledSet[t.ID],
			Downloading:     dl.Downloading,
			Progress:        dl.Progress,
			DownloadedBytes: dl.DownloadedBytes,
			TotalBytes:      dl.TotalBytes,
			Stage:           dl.Stage,
			Error:           dl.Error,
			SizeBytes:       size,
			Custom:          t.Custom,
			SHA256:          t.SHA256,
		})
	}
	for _, t := range kvmImages {
		dl := imageDownloadInfo(t.ID)
		downloaded, size := kvm.ImageDownloadedInfo(t.ID)
		manualPath := ""
		if t.IsWindows() {
			manualPath = kvm.ImagePath(t.ID)
		}
		images = append(images, ImageInfo{
			ID:              t.ID,
			Name:            t.Name,
			Type:            config.VirtualizationKVM,
			Distro:          t.Distro,
			Release:         t.Release,
			Arch:            t.Arch,
			Description:     t.Description,
			Downloaded:      downloaded,
			Enabled:         enabledSet[t.ID],
			Downloading:     dl.Downloading,
			Progress:        dl.Progress,
			DownloadedBytes: dl.DownloadedBytes,
			TotalBytes:      dl.TotalBytes,
			Stage:           dl.Stage,
			Error:           dl.Error,
			SizeBytes:       size,
			ManualPath:      manualPath,
			Desktop:         t.Desktop,
			Provisioner:     t.Provisioner,
			Custom:          t.Custom,
			SHA256:          t.SHA256,
		})
	}

	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: images})
}

// HandleCustomKVMImages creates or removes administrator-defined LXC/KVM image sources.
func HandleCustomKVMImages(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		if !requireScope(w, r, "image:download") {
			return
		}
		handleCustomKVMImageCreate(w, r)
	case http.MethodDelete:
		if !requireScope(w, r, "image:delete") {
			return
		}
		handleCustomKVMImageDelete(w, r)
	default:
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
	}
}

func handleCustomKVMImageCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Type        string `json:"type"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Distro      string `json:"distro"`
		Release     string `json:"release"`
		Arch        string `json:"arch"`
		URL         string `json:"url"`
		Provisioner string `json:"provisioner"`
		SHA256      string `json:"sha256"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Type = strings.ToLower(strings.TrimSpace(req.Type))
	if req.Type == "" {
		req.Type = config.VirtualizationKVM
	}
	req.Description = strings.TrimSpace(req.Description)
	req.Distro = strings.ToLower(strings.TrimSpace(req.Distro))
	req.Release = strings.ToLower(strings.TrimSpace(req.Release))
	req.Arch = strings.ToLower(strings.TrimSpace(req.Arch))
	req.URL = strings.TrimSpace(req.URL)
	req.Provisioner = strings.ToLower(strings.TrimSpace(req.Provisioner))
	req.SHA256 = strings.ToLower(strings.TrimSpace(req.SHA256))

	if req.Name == "" || len(req.Name) > 100 {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "name must be between 1 and 100 characters"})
		return
	}
	if len(req.Description) > 500 {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "description must not exceed 500 characters"})
		return
	}
	if req.Arch != runtime.GOARCH || (req.Arch != "amd64" && req.Arch != "arm64") {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "image architecture must match the host architecture"})
		return
	}
	if req.Type == config.VirtualizationLXC {
		if !customImageFieldPattern.MatchString(req.Distro) {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "distro contains unsupported characters"})
			return
		}
		if !customImageFieldPattern.MatchString(req.Release) {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "release contains unsupported characters"})
			return
		}
	} else if req.Type == config.VirtualizationKVM {
		switch req.Provisioner {
		case config.KVMProvisionerLinuxCloudInit:
			if !customImageFieldPattern.MatchString(req.Distro) {
				jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "distro contains unsupported characters"})
				return
			}
			if !customImageFieldPattern.MatchString(req.Release) {
				jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "release contains unsupported characters"})
				return
			}
		case config.KVMProvisionerWindows10:
			if req.Arch != "amd64" {
				jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Windows unattended installation currently requires an amd64 host"})
				return
			}
			req.Distro = "windows"
			req.Release = "10"
		case config.KVMProvisionerWindows11:
			if req.Arch != "amd64" {
				jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Windows unattended installation currently requires an amd64 host"})
				return
			}
			req.Distro = "windows"
			req.Release = "11"
		default:
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "unsupported unattended installation template"})
			return
		}
	} else {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "type must be lxc or kvm"})
		return
	}
	if len(req.URL) > 4096 {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "url must not exceed 4096 characters"})
		return
	}
	parsedURL, err := url.ParseRequestURI(req.URL)
	if err != nil || parsedURL.Host == "" || (parsedURL.Scheme != "https" && parsedURL.Scheme != "http") || parsedURL.User != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "url must be a valid HTTP or HTTPS download URL without credentials"})
		return
	}
	if req.SHA256 != "" && !sha256Pattern.MatchString(req.SHA256) {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "sha256 must contain exactly 64 hexadecimal characters"})
		return
	}
	if req.Type == config.VirtualizationLXC {
		for _, existing := range lxc.GetTemplates() {
			if strings.EqualFold(existing.Name, req.Name) {
				jsonResponse(w, http.StatusConflict, APIResponse{Success: false, Message: "an image with this name already exists"})
				return
			}
			if existing.Custom && existing.URL == req.URL {
				jsonResponse(w, http.StatusConflict, APIResponse{Success: false, Message: "this image URL is already registered"})
				return
			}
		}
	} else {
		for _, existing := range kvm.GetImages() {
			if strings.EqualFold(existing.Name, req.Name) {
				jsonResponse(w, http.StatusConflict, APIResponse{Success: false, Message: "an image with this name already exists"})
				return
			}
			if existing.Custom && existing.URL == req.URL {
				jsonResponse(w, http.StatusConflict, APIResponse{Success: false, Message: "this image URL is already registered"})
				return
			}
		}
	}

	random := make([]byte, 5)
	if _, err := rand.Read(random); err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "failed to generate image ID"})
		return
	}
	createdAt := time.Now().Format("2006-01-02 15:04:05")
	if req.Type == config.VirtualizationLXC {
		image := config.CustomLXCImage{
			ID:          "custom-lxc-" + hex.EncodeToString(random),
			Name:        req.Name,
			Description: req.Description,
			Distro:      req.Distro,
			Release:     req.Release,
			Arch:        req.Arch,
			URL:         req.URL,
			SHA256:      req.SHA256,
			CreatedAt:   createdAt,
		}
		if err := config.AddCustomLXCImage(image); err != nil {
			jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "failed to save custom image: " + err.Error()})
			return
		}
		jsonResponse(w, http.StatusCreated, APIResponse{Success: true, Message: "Custom image added", Data: image})
		return
	}
	image := config.CustomKVMImage{
		ID: "custom-kvm-" + hex.EncodeToString(random), Name: req.Name, Description: req.Description,
		Distro: req.Distro, Release: req.Release, Arch: req.Arch, URL: req.URL,
		Provisioner: req.Provisioner, SHA256: req.SHA256, CreatedAt: createdAt,
	}
	if err := config.AddCustomKVMImage(image); err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "failed to save custom image: " + err.Error()})
		return
	}
	jsonResponse(w, http.StatusCreated, APIResponse{Success: true, Message: "Custom image added", Data: image})
}

func handleCustomKVMImageDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.ID) == "" {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "id required"})
		return
	}
	req.ID = strings.TrimSpace(req.ID)
	kvmImage := kvm.FindImage(req.ID)
	lxcImage := lxc.FindTemplate(req.ID)
	isCustomKVM := kvmImage != nil && kvmImage.Custom
	isCustomLXC := lxcImage != nil && lxcImage.Custom
	if !isCustomKVM && !isCustomLXC {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Custom image not found"})
		return
	}
	if isImageDownloadActive(req.ID) {
		jsonResponse(w, http.StatusConflict, APIResponse{Success: false, Message: "Image is downloading; cancel it before removing the source"})
		return
	}
	for i := range config.AppConfig.Containers {
		if config.AppConfig.Containers[i].Template == req.ID {
			jsonResponse(w, http.StatusConflict, APIResponse{Success: false, Message: "This image is still used by a container"})
			return
		}
	}
	for i := range config.AppConfig.Tasks {
		task := &config.AppConfig.Tasks[i]
		if task.Status != "pending" && task.Status != "running" {
			continue
		}
		var taskConfig struct {
			TemplateID string `json:"template_id"`
		}
		_ = json.Unmarshal([]byte(task.Config), &taskConfig)
		if task.TemplateID == req.ID || taskConfig.TemplateID == req.ID {
			jsonResponse(w, http.StatusConflict, APIResponse{Success: false, Message: "This image is still referenced by an active task"})
			return
		}
	}
	var deleteErr error
	if isCustomLXC {
		deleteErr = lxc.DeleteCustomImage(req.ID)
	} else {
		deleteErr = kvm.DeleteImage(req.ID)
	}
	if deleteErr != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Failed to delete image cache: " + deleteErr.Error()})
		return
	}
	removeImageEnabled(req.ID)
	var removed bool
	var err error
	if isCustomLXC {
		removed, err = config.RemoveCustomLXCImage(req.ID)
	} else {
		removed, err = config.RemoveCustomKVMImage(req.ID)
	}
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Failed to remove custom image: " + err.Error()})
		return
	}
	if !removed {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Custom image not found"})
		return
	}
	clearImageDownload(req.ID)
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "Custom image removed"})
}

// HandleImageDownload starts a template image download in the background.
func HandleImageDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	if !requireScope(w, r, "image:download") {
		return
	}

	var req struct {
		TemplateID string `json:"template_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TemplateID == "" {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "template_id required"})
		return
	}
	tmpl := lxc.FindTemplate(req.TemplateID)
	if tmpl == nil {
		image := kvm.FindImage(req.TemplateID)
		if image == nil {
			jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Template not found"})
			return
		}
		if !hostKVMAvailable() {
			jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "KVM is not available on this host"})
			return
		}
		if _, err := config.SelectStoragePoolForContent(config.StorageContentImages, "", 1024*1024*1024); err != nil {
			jsonResponse(w, http.StatusConflict, APIResponse{Success: false, Message: err.Error()})
			return
		}
		if ok, _ := kvm.ImageDownloadedInfo(image.ID); ok {
			ensureImageEnabled(image.ID)
			clearImageDownload(image.ID)
			jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "Already downloaded"})
			return
		}
		ctx, ok := startImageDownload(image.ID, "downloading")
		if !ok {
			jsonResponse(w, http.StatusConflict, APIResponse{Success: false, Message: "Already downloading"})
			return
		}
		go func(image kvm.Image) {
			err := kvm.DownloadImageWithProgress(ctx, image, func(p kvm.DownloadProgress) {
				updateImageDownload(image.ID, func(st *imageDownloadStatus) {
					if p.Stage != "" {
						st.Stage = p.Stage
					}
					if p.DownloadedBytes > 0 || p.TotalBytes > 0 {
						st.DownloadedBytes = p.DownloadedBytes
						st.TotalBytes = p.TotalBytes
					}
					st.Progress = p.Percent
				})
			})
			if err != nil {
				if ctx.Err() != nil {
					os.Remove(kvm.ImagePath(image.ID) + ".tmp")
					os.Remove(kvm.ImagePath(image.ID))
					finishImageDownload(image.ID, nil)
					return
				}
				finishImageDownload(image.ID, err)
				return
			}
			ensureImageEnabled(image.ID)
			finishImageDownload(image.ID, nil)
		}(*image)
		jsonResponse(w, http.StatusAccepted, APIResponse{Success: true, Message: "Download started"})
		return
	}
	if !beginLXCImageDownload() {
		jsonResponse(w, http.StatusConflict, APIResponse{Success: false, Message: "Another LXC image download is active"})
		return
	}
	lxcDownloadHandedOff := false
	defer func() {
		if !lxcDownloadHandedOff {
			endLXCImageDownload()
		}
	}()
	imagePool, err := config.SelectStoragePoolForContent(
		config.StorageContentImages,
		"",
		dirSizeBytes("/var/cache/lxc/download")+1024*1024*1024,
	)
	if err != nil {
		jsonResponse(w, http.StatusConflict, APIResponse{Success: false, Message: err.Error()})
		return
	}
	if err := ensureLXCImageCachePool(*imagePool); err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: err.Error()})
		return
	}

	// Already downloaded? Just enable if needed.
	if downloaded, _ := lxcTemplateDownloadedInfo(*tmpl); downloaded {
		ensureImageEnabled(tmpl.ID)
		clearImageDownload(tmpl.ID)
		jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "Already downloaded"})
		return
	}

	startStage := "lxc-create"
	if tmpl.Custom {
		startStage = "downloading"
	}
	ctx, ok := startImageDownload(tmpl.ID, startStage)
	if !ok {
		jsonResponse(w, http.StatusConflict, APIResponse{Success: false, Message: "Already downloading"})
		return
	}

	if tmpl.Custom {
		go func(tmpl lxc.Template) {
			defer endLXCImageDownload()
			err := lxc.DownloadCustomImageWithProgress(ctx, tmpl, func(progress lxc.CustomImageDownloadProgress) {
				updateImageDownload(tmpl.ID, func(status *imageDownloadStatus) {
					status.Stage = progress.Stage
					status.DownloadedBytes = progress.DownloadedBytes
					status.TotalBytes = progress.TotalBytes
					status.Progress = progress.Percent
				})
			})
			if err != nil {
				if ctx.Err() != nil {
					_ = os.Remove(lxc.CustomImagePath(tmpl.ID) + ".tmp")
					_ = os.Remove(lxc.CustomImagePath(tmpl.ID))
					finishImageDownload(tmpl.ID, nil)
					return
				}
				finishImageDownload(tmpl.ID, err)
				return
			}
			ensureImageEnabled(tmpl.ID)
			finishImageDownload(tmpl.ID, nil)
		}(*tmpl)
		lxcDownloadHandedOff = true
		jsonResponse(w, http.StatusAccepted, APIResponse{Success: true, Message: "Download started"})
		return
	}

	go func(tmpl lxc.Template) {
		defer endLXCImageDownload()
		// Download via lxc-create with a temp container, then destroy it.
		tmpName := lxcImageDownloadTempName(tmpl.ID)
		args := []string{"-n", tmpName, "-t", "download", "--",
			"-d", tmpl.Distro, "-r", tmpl.Release, "-a", tmpl.Arch}
		if tmpl.Variant != "" {
			args = append(args, "--variant", tmpl.Variant)
		}
		updateImageDownload(tmpl.ID, func(st *imageDownloadStatus) {
			st.Stage = "lxc-create"
		})
		cmd := exec.CommandContext(ctx, "lxc-create", args...)
		output, err := runLXCImageDownloadCommand(cmd, tmpl.ID)

		// Clean up the temp container unconditionally.
		cleanupLXCImageDownloadTemp(tmpl.ID)

		if err != nil {
			if ctx.Err() != nil {
				finishImageDownload(tmpl.ID, nil)
				return
			}
			err = fmt.Errorf("Download failed: %v, output: %s", err, string(output))
			finishImageDownload(tmpl.ID, err)
			return
		}
		ensureImageEnabled(tmpl.ID)
		finishImageDownload(tmpl.ID, nil)
	}(*tmpl)
	lxcDownloadHandedOff = true

	jsonResponse(w, http.StatusAccepted, APIResponse{Success: true, Message: "Download started"})
}

type lxcImageDownloadCommandResult struct {
	output []byte
	err    error
}

func runLXCImageDownloadCommand(cmd *exec.Cmd, templateID string) ([]byte, error) {
	startedAt := time.Now()
	done := make(chan lxcImageDownloadCommandResult, 1)
	go func() {
		output, err := cmd.CombinedOutput()
		done <- lxcImageDownloadCommandResult{output: output, err: err}
	}()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var lastBytes int64
	for {
		select {
		case result := <-done:
			return result.output, result.err
		case <-ticker.C:
			downloadedBytes := newestLXCRootfsDownloadSize(startedAt)
			if downloadedBytes <= 0 || downloadedBytes == lastBytes {
				continue
			}
			lastBytes = downloadedBytes
			updateImageDownload(templateID, func(st *imageDownloadStatus) {
				st.Stage = "downloading"
				st.DownloadedBytes = downloadedBytes
			})
		}
	}
}

func newestLXCRootfsDownloadSize(startedAt time.Time) int64 {
	matches, _ := filepath.Glob("/tmp/tmp.*/rootfs.tar.xz")
	var newestTime time.Time
	var newestSize int64
	for _, match := range matches {
		info, err := os.Stat(match)
		if err != nil || info.IsDir() || info.ModTime().Before(startedAt.Add(-5*time.Second)) {
			continue
		}
		if info.ModTime().After(newestTime) {
			newestTime = info.ModTime()
			newestSize = info.Size()
		}
	}
	return newestSize
}

func ensureLXCImageCachePool(pool config.StoragePool) error {
	lxcImageCacheMu.Lock()
	defer lxcImageCacheMu.Unlock()

	cachePath := "/var/cache/lxc/download"
	targetPath := filepath.Join(pool.Path, "images", "lxc")
	targetAbs, err := filepath.Abs(targetPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(targetAbs, 0755); err != nil {
		return fmt.Errorf("failed to create LXC image storage: %v", err)
	}

	info, err := os.Lstat(cachePath)
	if os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
			return err
		}
		return os.Symlink(targetAbs, cachePath)
	}
	if err != nil {
		return err
	}

	sourcePath := cachePath
	linked := info.Mode()&os.ModeSymlink != 0
	if linked {
		sourcePath, err = filepath.EvalSymlinks(cachePath)
		if err != nil {
			return fmt.Errorf("failed to resolve LXC image cache: %v", err)
		}
	}
	sourceAbs, err := filepath.Abs(sourcePath)
	if err != nil {
		return err
	}
	if sourceAbs == targetAbs {
		return nil
	}
	if strings.HasPrefix(targetAbs, sourceAbs+string(os.PathSeparator)) || strings.HasPrefix(sourceAbs, targetAbs+string(os.PathSeparator)) {
		return fmt.Errorf("LXC image cache source and target must not be nested")
	}
	if !info.IsDir() && !linked {
		return fmt.Errorf("LXC image cache is not a directory: %s", cachePath)
	}

	if output, err := exec.Command("cp", "-a", sourceAbs+string(os.PathSeparator)+".", targetAbs+string(os.PathSeparator)).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to migrate LXC image cache: %v, output: %s", err, strings.TrimSpace(string(output)))
	}

	tempLink := fmt.Sprintf("%s.clicd-new-%d", cachePath, time.Now().UnixNano())
	if err := os.Symlink(targetAbs, tempLink); err != nil {
		return err
	}
	if linked {
		if err := os.Rename(tempLink, cachePath); err != nil {
			_ = os.Remove(tempLink)
			return fmt.Errorf("failed to switch LXC image cache: %v", err)
		}
		if isManagedLXCImageCachePath(sourceAbs) {
			_ = os.RemoveAll(sourceAbs)
		}
		return nil
	}

	backupPath := fmt.Sprintf("%s.clicd-backup-%d", cachePath, time.Now().UnixNano())
	if err := os.Rename(cachePath, backupPath); err != nil {
		_ = os.Remove(tempLink)
		return fmt.Errorf("failed to prepare LXC image cache migration: %v", err)
	}
	if err := os.Rename(tempLink, cachePath); err != nil {
		_ = os.Rename(backupPath, cachePath)
		_ = os.Remove(tempLink)
		return fmt.Errorf("failed to activate LXC image storage: %v", err)
	}
	if err := os.RemoveAll(backupPath); err != nil {
		return fmt.Errorf("LXC image cache migrated but old cache cleanup failed: %v", err)
	}
	return nil
}

func isManagedLXCImageCachePath(path string) bool {
	path = filepath.Clean(path)
	for _, pool := range config.StoragePoolsForContent(config.StorageContentImages) {
		if path == filepath.Clean(filepath.Join(pool.Path, "images", "lxc")) {
			return true
		}
	}
	return path == filepath.Clean("/var/lib/clicd/images/lxc")
}

// HandleImageCancel cancels an in-progress image download.
func HandleImageCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	if !requireScope(w, r, "image:download") {
		return
	}
	var req struct {
		TemplateID string `json:"template_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TemplateID == "" {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "template_id required"})
		return
	}

	imageDownloadsMu.Lock()
	st := imageDownloads[req.TemplateID]
	if st == nil || !st.Downloading || st.Cancel == nil {
		imageDownloadsMu.Unlock()
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "No active download"})
		return
	}
	cancel := st.Cancel
	st.Stage = "canceling"
	st.UpdatedAt = time.Now()
	imageDownloadsMu.Unlock()

	cancel()
	if image := kvm.FindImage(req.TemplateID); image != nil {
		os.Remove(kvm.ImagePath(image.ID) + ".tmp")
		os.Remove(kvm.ImagePath(image.ID))
	}
	if tmpl := lxc.FindTemplate(req.TemplateID); tmpl != nil {
		if tmpl.Custom {
			_ = os.Remove(lxc.CustomImagePath(tmpl.ID) + ".tmp")
			_ = os.Remove(lxc.CustomImagePath(tmpl.ID))
		} else {
			go cleanupLXCImageDownloadTemp(tmpl.ID)
		}
	}
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "Cancel requested"})
}

// HandleImageDelete deletes a cached template image from disk.
func HandleImageDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	if !requireScope(w, r, "image:delete") {
		return
	}

	var req struct {
		TemplateID string `json:"template_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TemplateID == "" {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "template_id required"})
		return
	}
	if isImageDownloadActive(req.TemplateID) {
		jsonResponse(w, http.StatusConflict, APIResponse{Success: false, Message: "Image is downloading; cancel it before deleting"})
		return
	}

	tmpl := lxc.FindTemplate(req.TemplateID)
	if tmpl == nil {
		if image := kvm.FindImage(req.TemplateID); image != nil {
			if err := kvm.DeleteImage(image.ID); err != nil {
				jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Failed to delete image cache: " + err.Error()})
				return
			}
			removeImageEnabled(image.ID)
			jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "Deleted"})
			return
		}
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Template not found"})
		return
	}
	if tmpl.Custom {
		if err := lxc.DeleteCustomImage(tmpl.ID); err != nil {
			jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Failed to delete image cache: " + err.Error()})
			return
		}
		removeImageEnabled(tmpl.ID)
		jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "Deleted"})
		return
	}

	// Remove cache directory
	cachePath := filepath.Join("/var/cache/lxc/download", tmpl.Distro, tmpl.Release, tmpl.Arch)
	if err := os.RemoveAll(cachePath); err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to delete image cache: %v", err),
		})
		return
	}

	// Remove from enabled list
	removeImageEnabled(tmpl.ID)

	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "Deleted"})
}

// HandleImageToggle enables or disables a template image.
func HandleImageToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	if !requireScope(w, r, "image:toggle") {
		return
	}

	var req struct {
		TemplateID string `json:"template_id"`
		Enabled    bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TemplateID == "" {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "template_id required"})
		return
	}

	if req.Enabled {
		ensureImageEnabled(req.TemplateID)
	} else {
		removeImageEnabled(req.TemplateID)
	}

	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "OK"})
}

// HandleEnabledImages returns only the enabled AND downloaded templates.
// Used by container create / reinstall to filter available templates.
func HandleEnabledImages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	if !requireScope(w, r, "image:read") {
		return
	}

	runtime := runtimeFromRequest(r.URL.Query().Get("type"))
	enabledSet := getEnabledImageSet()
	var subUser *config.SubUser
	var targetContainer *config.Container
	currentImageIDs := map[string]bool{}
	if isSubUserRequest(r) {
		subUser = subUserFromRequest(r)
		if identifier := r.URL.Query().Get("container"); identifier != "" {
			targetContainer = containerByIdentifier(identifier)
			if targetContainer == nil || !isContainerAllowedForRequest(r, identifier) {
				jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "Access denied to this container"})
				return
			}
			currentImageIDs[targetContainer.Template] = true
		} else {
			for _, id := range subUserCurrentImageIDs(subUser) {
				currentImageIDs[id] = true
			}
		}
	}

	result := make([]map[string]string, 0)
	if runtime == config.VirtualizationKVM {
		if !hostKVMAvailable() {
			jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: result})
			return
		}
		for _, t := range kvm.GetImages() {
			if subUser != nil && !isImageAllowedForSubUser(subUser, targetContainer, t.ID) {
				continue
			}
			if downloaded, _ := kvm.ImageDownloadedInfo(t.ID); downloaded && (enabledSet[t.ID] || currentImageIDs[t.ID]) {
				result = append(result, map[string]string{
					"id": t.ID, "name": t.Name, "distro": t.Distro, "release": t.Release, "arch": t.Arch,
					"description": t.Description, "type": config.VirtualizationKVM, "desktop": t.Desktop,
				})
			}
		}
	} else {
		for _, t := range lxc.GetTemplates() {
			if subUser != nil && !isImageAllowedForSubUser(subUser, targetContainer, t.ID) {
				continue
			}
			if downloaded, _ := lxcTemplateDownloadedInfo(t); downloaded && (enabledSet[t.ID] || currentImageIDs[t.ID]) {
				result = append(result, map[string]string{
					"id": t.ID, "name": t.Name, "distro": t.Distro, "release": t.Release, "arch": t.Arch,
					"variant": t.Variant, "description": t.Description, "type": config.VirtualizationLXC,
				})
			}
		}
	}

	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: result})
}

func isTemplateEnabledAndDownloaded(templateID string) bool {
	return isImageEnabledAndDownloaded(templateID, runtimeFromTemplateID(templateID))
}

func imageTemplateExists(templateID string) bool {
	return lxc.FindTemplate(templateID) != nil || kvm.FindImage(templateID) != nil
}

func isImageDownloadedForRuntime(templateID string, runtime string) bool {
	runtime = runtimeFromRequest(runtime)
	if runtime == config.VirtualizationKVM {
		if !hostKVMAvailable() {
			return false
		}
		image := kvm.FindImage(templateID)
		if image == nil {
			return false
		}
		downloaded, _ := kvm.ImageDownloadedInfo(image.ID)
		return downloaded
	}
	tmpl := lxc.FindTemplate(templateID)
	if tmpl == nil {
		return false
	}
	downloaded, _ := lxcTemplateDownloadedInfo(*tmpl)
	return downloaded
}

func isTemplateAvailableForRequest(r *http.Request, c *config.Container, templateID string, runtime string) bool {
	if isSubUserRequest(r) {
		if !isTemplateAllowedForRequest(r, c, templateID) {
			return false
		}
		if c != nil && c.Template == templateID {
			return isImageDownloadedForRuntime(templateID, runtime)
		}
	}
	return isImageEnabledAndDownloaded(templateID, runtime)
}

func isImageEnabledAndDownloaded(templateID string, runtime string) bool {
	runtime = runtimeFromRequest(runtime)
	if runtime == config.VirtualizationKVM {
		if !hostKVMAvailable() {
			return false
		}
		image := kvm.FindImage(templateID)
		if image == nil {
			return false
		}
		enabledSet := getEnabledImageSet()
		downloaded, _ := kvm.ImageDownloadedInfo(image.ID)
		return enabledSet[image.ID] && downloaded
	}
	tmpl := lxc.FindTemplate(templateID)
	if tmpl == nil {
		return false
	}
	enabledSet := getEnabledImageSet()
	downloaded, _ := lxcTemplateDownloadedInfo(*tmpl)
	return enabledSet[tmpl.ID] && downloaded
}

func hostKVMAvailable() bool {
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		return false
	}
	return fileExists("/dev/kvm") && commandExists("virsh") && commandExists(kvmQEMUCheckKey())
}

func ensureImageEnabled(id string) {
	// If the enabled list is empty, all templates are currently enabled by default.
	// We must populate the list with all template IDs first so that explicit toggles stick.
	if len(config.AppConfig.EnabledImages) == 0 {
		for _, t := range lxc.GetTemplates() {
			config.AppConfig.EnabledImages = append(config.AppConfig.EnabledImages, t.ID)
		}
		for _, t := range kvm.GetImages() {
			config.AppConfig.EnabledImages = append(config.AppConfig.EnabledImages, t.ID)
		}
		config.SaveConfig()
		return // Already contains all IDs including this one
	}
	found := false
	for _, eid := range config.AppConfig.EnabledImages {
		if eid == id {
			found = true
			break
		}
	}
	if !found {
		config.AppConfig.EnabledImages = append(config.AppConfig.EnabledImages, id)
		config.SaveConfig()
	}
}

func removeImageEnabled(id string) {
	// If the enabled list is empty, populate it first with all templates,
	// then remove the one being disabled.
	if len(config.AppConfig.EnabledImages) == 0 {
		for _, t := range lxc.GetTemplates() {
			if t.ID != id {
				config.AppConfig.EnabledImages = append(config.AppConfig.EnabledImages, t.ID)
			}
		}
		for _, t := range kvm.GetImages() {
			if t.ID != id {
				config.AppConfig.EnabledImages = append(config.AppConfig.EnabledImages, t.ID)
			}
		}
		config.SaveConfig()
		return
	}
	filtered := make([]string, 0, len(config.AppConfig.EnabledImages))
	for _, eid := range config.AppConfig.EnabledImages {
		if eid != id {
			filtered = append(filtered, eid)
		}
	}
	if len(filtered) != len(config.AppConfig.EnabledImages) {
		config.AppConfig.EnabledImages = filtered
		config.SaveConfig()
	}
}
