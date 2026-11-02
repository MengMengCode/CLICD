package api

import (
	"encoding/json"
	"net/http"

	"clicd/internal/config"
	"clicd/internal/lxc"
)

// HandleIPv6NATSettings manages the host's optional LXC NAT66 network.
func HandleIPv6NATSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !requireScope(w, r, "ipv6:read") {
			return
		}
		jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: lxc.GetIPv6NATStatus()})
	case http.MethodPut:
		if !requireScope(w, r, "routing:write") {
			return
		}
		var req struct {
			Enabled   *bool   `json:"enabled"`
			Subnet    *string `json:"subnet"`
			Interface *string `json:"interface"`
		}
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil || req.Enabled == nil && req.Subnet == nil && req.Interface == nil {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Provide enabled, subnet or interface"})
			return
		}
		settings := config.AppConfig.IPv6NAT
		if req.Enabled != nil {
			settings.Enabled = *req.Enabled
		}
		if req.Subnet != nil {
			settings.Subnet = *req.Subnet
		}
		if req.Interface != nil {
			settings.Interface = *req.Interface
		}
		status, err := lxcManager.ConfigureIPv6NAT(settings)
		if err != nil {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
			return
		}
		auditRequest(r, "network.ipv6-nat", "lxcbr0", "IPv6 NAT settings updated", true, "")
		jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: status})
	default:
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
	}
}
