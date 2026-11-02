package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"clicd/internal/config"
	"clicd/internal/lxc"
)

func handleIPv6PortMappings(w http.ResponseWriter, r *http.Request, id int, action string) {
	if !requireScope(w, r, "container:network") {
		return
	}
	if action == "ipv6-port-mappings" && r.Method == http.MethodGet {
		state, err := lxc.GetIPv6PortMappings(id)
		if err != nil {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
			return
		}
		jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: state})
		return
	}
	index := -1
	if strings.HasPrefix(action, "ipv6-port-mappings/") {
		var err error
		index, err = strconv.Atoi(strings.TrimPrefix(action, "ipv6-port-mappings/"))
		if err != nil || index < 0 {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid IPv6 port mapping index"})
			return
		}
	}
	if isSubUserRequest(r) && (r.Method != http.MethodPut || index < 0) {
		jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "Administrator permission required"})
		return
	}
	var mappings []config.PortMapping
	var err error
	if r.Method == http.MethodDelete && index >= 0 {
		mappings, err = lxcManager.DeleteIPv6PortMapping(id, index)
	} else if (r.Method == http.MethodPost && index < 0) || (r.Method == http.MethodPut && index >= 0) {
		var mapping config.PortMapping
		if err := json.NewDecoder(r.Body).Decode(&mapping); err != nil {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
			return
		}
		if isSubUserRequest(r) {
			container := config.FindContainer(id)
			if container == nil || index >= len(container.IPv6PortMappings) {
				jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid IPv6 port mapping index"})
				return
			}
			previous := container.IPv6PortMappings[index]
			mapping = config.PortMapping{ContainerPort: mapping.ContainerPort, HostPort: previous.HostPort,
				HostIP: previous.HostIP, Protocol: previous.Protocol, Description: previous.Description}
		}
		if index < 0 {
			mappings, err = lxcManager.AddIPv6PortMapping(id, mapping)
		} else {
			mappings, err = lxcManager.UpdateIPv6PortMapping(id, index, mapping)
		}
	} else {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	}
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: mappings})
}

func setIPv6NATSync(w http.ResponseWriter, r *http.Request, id int) {
	if !requireScope(w, r, "container:network") {
		return
	}
	if isSubUserRequest(r) {
		jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "Administrator permission required"})
		return
	}
	var request struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Enabled == nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "enabled must be a boolean"})
		return
	}
	state, err := lxcManager.SetIPv6NATSync(id, *request.Enabled)
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	}
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: state})
}
