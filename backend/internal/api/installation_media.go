package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"

	"clicd/internal/config"
	"clicd/internal/kvm"
)

type installationMediaChoice struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Distro      string `json:"distro"`
	Release     string `json:"release"`
	Arch        string `json:"arch"`
	Provisioner string `json:"provisioner"`
	Format      string `json:"format"`
}
type installationMediaResponse struct {
	Supported     bool                      `json:"supported"`
	ImageID       string                    `json:"image_id"`
	ImageName     string                    `json:"image_name"`
	Boot          string                    `json:"boot"`
	ManualInstall bool                      `json:"manual_install"`
	Running       bool                      `json:"running"`
	Available     []installationMediaChoice `json:"available"`
}

func installationMediaChoices(r *http.Request, c *config.Container) []installationMediaChoice {
	result := []installationMediaChoice{}
	enabled := getEnabledImageSet()
	for _, image := range kvm.GetImages() {
		if !enabled[image.ID] || image.Arch != runtime.GOARCH || !isTemplateAllowedForRequest(r, c, image.ID) {
			continue
		}
		if ok, _ := kvm.ImageDownloadedInfo(image.ID); !ok {
			continue
		}
		if kvm.ValidateISOFile(kvm.ImagePath(image.ID)) != nil {
			continue
		}
		result = append(result, installationMediaChoice{image.ID, image.Name, image.Distro, image.Release, image.Arch, image.EffectiveProvisioner(), "iso"})
	}
	return result
}

func installationMediaData(r *http.Request, c *config.Container) installationMediaResponse {
	imageID, boot := kvm.InstallationMedia(c)
	data := installationMediaResponse{Supported: true, ImageID: imageID, Boot: boot, ManualInstall: c.ManualInstall || kvm.IsManualISOImage(c.Template), Running: c.Status != "stopped", Available: installationMediaChoices(r, c)}
	if image := kvm.FindImage(imageID); image != nil && isTemplateAllowedForRequest(r, c, imageID) {
		data.ImageName = image.Name
	}
	return data
}

func handleInstallationMedia(w http.ResponseWriter, r *http.Request, id int) {
	c := config.FindContainer(id)
	if c == nil {
		jsonResponse(w, 404, APIResponse{Success: false, Message: "Container not found"})
		return
	}
	if !c.IsKVM() {
		jsonResponse(w, 400, APIResponse{Success: false, Message: "Installation media is only supported for KVM"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		if !requireScope(w, r, "container:read") {
			return
		}
		jsonResponse(w, 200, APIResponse{Success: true, Data: installationMediaData(r, c)})
	case http.MethodPut:
		if !requireScope(w, r, "container:reinstall") {
			return
		}
		var req struct {
			ImageID string `json:"image_id"`
			Boot    string `json:"boot"`
		}
		decoder := json.NewDecoder(io.LimitReader(r.Body, 4096))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil {
			jsonResponse(w, 400, APIResponse{Success: false, Message: "Invalid installation media request"})
			return
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			jsonResponse(w, 400, APIResponse{Success: false, Message: "Invalid request body"})
			return
		}
		if req.ImageID != strings.TrimSpace(req.ImageID) || (req.Boot != "disk" && req.Boot != "cdrom") || (req.ImageID == "" && req.Boot != "disk") {
			jsonResponse(w, 400, APIResponse{Success: false, Message: "Use image_id and boot disk or cdrom; CD-ROM boot requires an image"})
			return
		}
		if req.ImageID != "" {
			allowed := false
			for _, image := range installationMediaChoices(r, c) {
				if image.ID == req.ImageID {
					allowed = true
					break
				}
			}
			if !allowed {
				jsonResponse(w, 403, APIResponse{Success: false, Message: "ISO image is not enabled, downloaded, or allowed for this container"})
				return
			}
		}
		err := globalQueue.WithIdleContainer(id, func() error { return kvmManager.SetInstallationMedia(id, req.ImageID, req.Boot) })
		if err != nil {
			jsonResponse(w, http.StatusConflict, APIResponse{Success: false, Message: fmt.Sprintf("Cannot change installation media: %v", err)})
			return
		}
		c = config.FindContainer(id)
		jsonResponse(w, 200, APIResponse{Success: true, Message: "Installation media updated", Data: installationMediaData(r, c)})
	default:
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
	}
}
