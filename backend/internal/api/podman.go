package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"clicd/internal/config"
	"clicd/internal/lxc"
	"clicd/internal/podman"
)

var (
	podmanPortConfiguration sync.Mutex
	errPodmanPortConflict   = errors.New("Podman port conflict")
)

func HandlePodman(w http.ResponseWriter, r *http.Request) {
	auth, _ := authContextFromRequest(r)
	if auth.Type != authTypeAdmin {
		jsonResponse(w, http.StatusForbidden, APIResponse{Message: "Administrator login required"})
		return
	}

	ctx := r.Context()
	manager := podman.Manager{}
	path := strings.TrimPrefix(r.URL.Path, "/api/podman/")
	switch path {
	case "status":
		if r.Method != http.MethodGet {
			podmanMethodNotAllowed(w, http.MethodGet)
			return
		}
		info, err := manager.Info(ctx)
		if err != nil {
			jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: map[string]any{
				"available": false, "message": err.Error(),
			}})
			return
		}
		jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: map[string]any{"available": true, "version": info.Version}})
	case "containers":
		switch r.Method {
		case http.MethodGet:
			items, err := manager.List(ctx)
			if err != nil {
				podmanError(w, err)
				return
			}
			if items == nil {
				items = []podman.Container{}
			}
			jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: items})
		case http.MethodPost:
			decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
			decoder.DisallowUnknownFields()
			var input podman.CreateRequest
			if err := decoder.Decode(&input); err != nil || decoder.Decode(new(any)) != io.EOF {
				jsonResponse(w, http.StatusBadRequest, APIResponse{Message: "Invalid Podman request body"})
				return
			}
			item, err := createPodmanApplication(r, manager, input)
			if err != nil {
				podmanError(w, err)
				return
			}
			jsonResponse(w, http.StatusCreated, APIResponse{Success: true, Data: item})
		default:
			podmanMethodNotAllowed(w, "GET, POST")
		}
	case "images":
		handlePodmanImages(w, r, manager)
	default:
		handlePodmanContainer(w, r, manager, path)
	}
}

func handlePodmanImages(w http.ResponseWriter, r *http.Request, manager podman.Manager) {
	switch r.Method {
	case http.MethodGet:
		images, err := manager.Images(r.Context())
		if err != nil {
			podmanError(w, err)
			return
		}
		if images == nil {
			images = []podman.Image{}
		}
		jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: images})
	case http.MethodPost:
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
		decoder.DisallowUnknownFields()
		var input struct {
			Image string `json:"image"`
		}
		if err := decoder.Decode(&input); err != nil || decoder.Decode(new(any)) != io.EOF {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Message: "Invalid Podman image request body"})
			return
		}
		id, err := manager.PullImage(r.Context(), input.Image)
		auditPodmanAction(r, "pull", input.Image, err)
		if err != nil {
			podmanError(w, err)
			return
		}
		jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: map[string]string{"id": id}})
	default:
		podmanMethodNotAllowed(w, "GET, POST")
	}
}

func createPodmanApplication(r *http.Request, manager podman.Manager, input podman.CreateRequest) (podman.Container, error) {
	podmanPortConfiguration.Lock()
	defer podmanPortConfiguration.Unlock()
	if err := checkPodmanPorts(input); err != nil {
		return podman.Container{}, err
	}
	if len(input.Ports) > 0 {
		apps, err := manager.List(r.Context())
		if err != nil {
			return podman.Container{}, err
		}
		for _, app := range apps {
			for _, used := range app.Ports {
				for _, wanted := range input.Ports {
					protocol := strings.ToLower(strings.TrimSpace(wanted.Protocol))
					if protocol == "" {
						protocol = "tcp"
					}
					if used.HostPort == wanted.HostPort && used.Protocol == protocol {
						return podman.Container{}, fmt.Errorf("%w: host port %d/%s is assigned to %s", errPodmanPortConflict, used.HostPort, used.Protocol, app.Name)
					}
				}
			}
		}
	}
	item, err := manager.Create(r.Context(), input)
	auditPodmanAction(r, "create", input.Name, err)
	return item, err
}

func checkPodmanPorts(input podman.CreateRequest) error {
	start, end := config.NATPortRange()
	candidate := &config.Container{ID: -1}
	for _, mapping := range input.Ports {
		if mapping.HostPort < 1 || mapping.HostPort > 65535 {
			continue // The Podman runtime reports invalid port numbers.
		}
		if mapping.HostPort >= start && mapping.HostPort <= end {
			return fmt.Errorf("%w: host port %d overlaps the CLICD IPv4 NAT range (%d–%d)", errPodmanPortConflict, mapping.HostPort, start, end)
		}
		if !lxc.HostPortAvailable(candidate, "", mapping.HostPort, strings.ToLower(strings.TrimSpace(mapping.Protocol))) {
			return fmt.Errorf("%w: host port %d is already mapped by a CLICD instance", errPodmanPortConflict, mapping.HostPort)
		}
	}
	return nil
}

func checkNATRangeAgainstPodman(ctx context.Context, start, end int) error {
	apps, err := (podman.Manager{}).List(ctx)
	if errors.Is(err, podman.ErrUnavailable) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, app := range apps {
		for _, mapping := range app.Ports {
			if mapping.HostPort >= start && mapping.HostPort <= end {
				return fmt.Errorf("%w: NAT range includes host port %d published by %s", errPodmanPortConflict, mapping.HostPort, app.Name)
			}
		}
	}
	return nil
}

func handlePodmanContainer(w http.ResponseWriter, r *http.Request, manager podman.Manager, path string) {
	tail, ok := strings.CutPrefix(path, "containers/")
	if !ok || tail == "" {
		jsonResponse(w, http.StatusNotFound, APIResponse{Message: "Podman endpoint not found"})
		return
	}
	ctx := r.Context()
	id, action, subresource := strings.Cut(tail, "/")
	expectedMethod := http.MethodPost
	if !subresource {
		action, expectedMethod = "delete", http.MethodDelete
	} else if action == "logs" || action == "stats" {
		expectedMethod = http.MethodGet
	} else if action != "start" && action != "stop" && action != "restart" {
		jsonResponse(w, http.StatusNotFound, APIResponse{Message: "Podman endpoint not found"})
		return
	}
	if r.Method != expectedMethod {
		podmanMethodNotAllowed(w, expectedMethod)
		return
	}
	if action == "logs" {
		logs, err := manager.Logs(ctx, id)
		if err != nil {
			podmanError(w, err)
			return
		}
		jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: map[string]string{"logs": logs}})
		return
	}
	if action == "stats" {
		stats, err := manager.Stats(ctx, id)
		if err != nil {
			podmanError(w, err)
			return
		}
		jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: stats})
		return
	}

	var err error
	switch action {
	case "start":
		err = manager.Start(ctx, id)
	case "stop":
		err = manager.Stop(ctx, id)
	case "restart":
		err = manager.Restart(ctx, id)
	case "delete":
		err = manager.Delete(ctx, id)
	}
	auditPodmanAction(r, action, id, err)
	if err != nil {
		podmanError(w, err)
		return
	}
	jsonResponse(w, http.StatusOK, APIResponse{Success: true})
}

func podmanMethodNotAllowed(w http.ResponseWriter, allow string) {
	w.Header().Set("Allow", allow)
	jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Message: "Method not allowed"})
}

func podmanError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, podman.ErrInvalid):
		status = http.StatusBadRequest
	case errors.Is(err, podman.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, podman.ErrUnavailable):
		status = http.StatusServiceUnavailable
	case errors.Is(err, errPodmanPortConflict), errors.Is(err, podman.ErrPortConflict), errors.Is(err, podman.ErrNotRunning):
		status = http.StatusConflict
	}
	jsonResponse(w, status, APIResponse{Message: err.Error()})
}

func auditPodmanAction(r *http.Request, action, target string, err error) {
	target = strings.TrimSpace(target)
	if len(target) > 64 {
		target = target[:64]
	}
	detail := "Succeeded"
	if err != nil {
		detail = "Failed"
	}
	auditRequest(r, "podman:"+action, target, detail, err == nil, "")
}
