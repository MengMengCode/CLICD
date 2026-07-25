package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"clicd/internal/config"
)

func panelAccessMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		decision := config.EvaluatePanelAccess(
			config.AppConfig.PanelAccessPolicy,
			r.RemoteAddr,
			config.ForwardedClientHeaders{
				ForwardedFor:   r.Header.Get("X-Forwarded-For"),
				RealIP:         r.Header.Get("X-Real-IP"),
				CFConnectingIP: r.Header.Get("CF-Connecting-IP"),
			},
		)
		if decision.Allowed {
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("Cache-Control", "no-store")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": false,
				"message": "Access denied by panel source policy",
			})
			return
		}
		http.Error(w, "Access denied by panel source policy", http.StatusForbidden)
	})
}
