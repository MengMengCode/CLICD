package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"clicd/internal/config"
)

type panelAccessPolicyResponse struct {
	Enabled        bool     `json:"enabled"`
	AllowedSources []string `json:"allowed_sources"`
	TrustedProxies []string `json:"trusted_proxies"`
	CurrentSource  string   `json:"current_source"`
	DirectSource   string   `json:"direct_source"`
	UsingForwarded bool     `json:"using_forwarded"`
}

func HandlePanelAccessPolicy(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: panelAccessPolicyStatus(r, config.AppConfig.PanelAccessPolicy)})
	case http.MethodPut:
		updatePanelAccessPolicy(w, r)
	default:
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
	}
}

func updatePanelAccessPolicy(w http.ResponseWriter, r *http.Request) {
	var requested config.PanelAccessPolicy
	if err := json.NewDecoder(r.Body).Decode(&requested); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
		return
	}
	normalized, err := config.NormalizePanelAccessPolicy(requested)
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	}
	decision := evaluatePanelRequest(r, normalized)
	if normalized.Enabled && !decision.Allowed {
		jsonResponse(w, http.StatusBadRequest, APIResponse{
			Success: false,
			Message: "The new access policy does not allow your current source address " + decision.CurrentSource,
		})
		return
	}

	previous := config.AppConfig.PanelAccessPolicy
	config.AppConfig.PanelAccessPolicy = normalized
	if err := config.SaveConfig(); err != nil {
		config.AppConfig.PanelAccessPolicy = previous
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Failed to save panel access policy"})
		return
	}
	detail := "enabled=" + strings.ToLower(strings.TrimSpace(boolText(normalized.Enabled))) +
		",allowed=" + strings.Join(normalized.AllowedSources, ",") +
		",trusted_proxies=" + strings.Join(normalized.TrustedProxies, ",")
	auditRequest(r, "settings.panel_access", "Panel access policy", detail, true, "")
	jsonResponse(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "Panel access policy saved",
		Data:    panelAccessPolicyStatus(r, normalized),
	})
}

func panelAccessPolicyStatus(r *http.Request, policy config.PanelAccessPolicy) panelAccessPolicyResponse {
	decision := evaluatePanelRequest(r, policy)
	return panelAccessPolicyResponse{
		Enabled:        policy.Enabled,
		AllowedSources: append([]string(nil), policy.AllowedSources...),
		TrustedProxies: append([]string(nil), policy.TrustedProxies...),
		CurrentSource:  decision.CurrentSource,
		DirectSource:   decision.DirectSource,
		UsingForwarded: decision.UsedForwarded,
	}
}

func evaluatePanelRequest(r *http.Request, policy config.PanelAccessPolicy) config.PanelAccessDecision {
	return config.EvaluatePanelAccess(policy, r.RemoteAddr, config.ForwardedClientHeaders{
		ForwardedFor:   r.Header.Get("X-Forwarded-For"),
		RealIP:         r.Header.Get("X-Real-IP"),
		CFConnectingIP: r.Header.Get("CF-Connecting-IP"),
	})
}

func boolText(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
