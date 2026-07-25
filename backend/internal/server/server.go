package server

import (
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"strings"

	"clicd/internal/api"
	"clicd/internal/config"
)

// webFS holds embedded frontend files
var webFS http.FileSystem

// corsMiddleware adds CORS headers
func corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" && config.IsOriginAllowed(origin, r.Host) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key")

		if r.Method == http.MethodOptions {
			if origin := r.Header.Get("Origin"); origin != "" && !config.IsOriginAllowed(origin, r.Host) {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			w.WriteHeader(http.StatusOK)
			return
		}

		next(w, r)
	}
}

// setupRoutes configures API and static routes
func setupRoutes(mux *http.ServeMux) {
	// API routes
	mux.HandleFunc("/api/login", corsMiddleware(api.HandleLogin))
	mux.HandleFunc("/api/language", corsMiddleware(api.HandleLanguage))
	mux.HandleFunc("/api/check-auth", corsMiddleware(api.AuthMiddleware(api.HandleCheckAuth)))
	mux.HandleFunc("/api/change-password", corsMiddleware(api.AdminMiddleware(api.HandleAdminPasswordChange)))
	mux.HandleFunc("/api/change-username", corsMiddleware(api.AdminMiddleware(api.HandleAdminUsernameChange)))
	mux.HandleFunc("/api/login-logs", corsMiddleware(api.AdminMiddleware(api.HandleLoginLogs)))
	mux.HandleFunc("/api/ssl", corsMiddleware(api.AdminMiddleware(api.HandleSSLSettings)))
	mux.HandleFunc("/api/webssh-origins", corsMiddleware(api.AdminMiddleware(api.HandleWebSSHOriginSettings)))
	mux.HandleFunc("/api/access-policy", corsMiddleware(api.AdminMiddleware(api.HandlePanelAccessPolicy)))
	mux.HandleFunc("/api/containers", corsMiddleware(api.AuthMiddleware(api.SubUserMiddleware(api.HandleContainers))))
	mux.HandleFunc("/api/containers/list", corsMiddleware(api.AuthMiddleware(api.SubUserMiddleware(api.HandleContainerListAlias))))
	mux.HandleFunc("/api/containers/", corsMiddleware(api.AuthMiddleware(api.SubUserMiddleware(api.HandleSingleContainer))))
	mux.HandleFunc("/api/templates", corsMiddleware(api.AuthMiddleware(api.HandleTemplates)))
	mux.HandleFunc("/api/images", corsMiddleware(api.AdminMiddleware(api.HandleImages)))
	mux.HandleFunc("/api/images/custom", corsMiddleware(api.AdminMiddleware(api.HandleCustomKVMImages)))
	mux.HandleFunc("/api/images/download", corsMiddleware(api.AdminMiddleware(api.HandleImageDownload)))
	mux.HandleFunc("/api/images/cancel", corsMiddleware(api.AdminMiddleware(api.HandleImageCancel)))
	mux.HandleFunc("/api/images/delete", corsMiddleware(api.AdminMiddleware(api.HandleImageDelete)))
	mux.HandleFunc("/api/images/toggle", corsMiddleware(api.AdminMiddleware(api.HandleImageToggle)))
	mux.HandleFunc("/api/images/enabled", corsMiddleware(api.AuthMiddleware(api.SubUserMiddleware(api.HandleEnabledImages))))
	mux.HandleFunc("/api/dashboard", corsMiddleware(api.AdminMiddleware(api.HandleDashboard)))
	mux.HandleFunc("/api/host-info", corsMiddleware(api.AdminMiddleware(api.HandleHostInfo)))
	mux.HandleFunc("/api/host-history", corsMiddleware(api.AdminMiddleware(api.HandleHostHistory)))
	mux.HandleFunc("/api/host-report", corsMiddleware(api.AdminMiddleware(api.HandleHostReport)))
	mux.HandleFunc("/api/snapshots", corsMiddleware(api.AdminMiddleware(api.HandleSnapshots)))
	mux.HandleFunc("/api/routing/ipv4-scan", corsMiddleware(api.AdminMiddleware(api.HandleRoutingIPv4Scan)))
	mux.HandleFunc("/api/routing", corsMiddleware(api.AdminMiddleware(api.HandleRouting)))
	mux.HandleFunc("/api/storage", corsMiddleware(api.AdminMiddleware(api.HandleStorage)))
	mux.HandleFunc("/api/ipv6/status", corsMiddleware(api.AdminMiddleware(api.HandleIPv6Status)))
	mux.HandleFunc("/api/tasks", corsMiddleware(api.AuthMiddleware(api.SubUserMiddleware(api.HandleTasks))))
	mux.HandleFunc("/api/tasks/", corsMiddleware(api.AuthMiddleware(api.AdminMiddleware(api.HandleTaskDelete))))
	mux.HandleFunc("/api/task-queue/settings", corsMiddleware(api.AdminMiddleware(api.HandleTaskQueueSettings)))
	mux.HandleFunc("/api/batch-create", corsMiddleware(api.AdminMiddleware(api.HandleBatchCreate)))
	mux.HandleFunc("/api/batch-action", corsMiddleware(api.AdminMiddleware(api.HandleBatchAction)))
	mux.HandleFunc("/api/sub-user/create", corsMiddleware(api.AdminMiddleware(api.HandleSubUserCreate)))
	mux.HandleFunc("/api/sub-user/login", corsMiddleware(api.HandleSubUserLogin))
	mux.HandleFunc("/api/sub-user/access", corsMiddleware(api.HandleSubUserAccessCode))
	mux.HandleFunc("/api/sub-users", corsMiddleware(api.AdminMiddleware(api.HandleSubUserList)))
	mux.HandleFunc("/api/sub-users/", corsMiddleware(api.AdminMiddleware(api.HandleSubUserAction)))
	mux.HandleFunc("/api/audit-logs", corsMiddleware(api.AdminMiddleware(api.HandleAuditLogs)))
	mux.HandleFunc("/api/security/alerts", corsMiddleware(api.AdminMiddleware(api.HandleSecurityAlerts)))
	mux.HandleFunc("/api/security/check", corsMiddleware(api.AdminMiddleware(api.HandleSecurityCheck)))
	mux.HandleFunc("/api/security/logs", corsMiddleware(api.AdminMiddleware(api.HandleSecurityLogs)))
	mux.HandleFunc("/api/security/summary", corsMiddleware(api.AdminMiddleware(api.HandleContainerSecuritySummary)))
	mux.HandleFunc("/api/security/settings", corsMiddleware(api.AdminMiddleware(api.HandleSecuritySettings)))
	mux.HandleFunc("/api/ssh-ticket", corsMiddleware(api.AuthMiddleware(api.HandleWebSSHTicket)))
	mux.HandleFunc("/api/ssh", api.HandleWebSSH) // WebSocket
	mux.HandleFunc("/api/vnc-ticket", corsMiddleware(api.AuthMiddleware(api.HandleVNCTicket)))
	mux.HandleFunc("/api/vnc", api.HandleVNCProxy) // WebSocket

	// API Key management
	mux.HandleFunc("/api/api-keys", corsMiddleware(api.AdminMiddleware(api.HandleApiKeys)))
	mux.HandleFunc("/api/api-keys/", corsMiddleware(api.AdminMiddleware(api.HandleApiKeyDelete)))

	// Versioned external API routes
	mux.HandleFunc("/api/v1/dashboard", corsMiddleware(api.AuthMiddleware(api.HandleDashboard)))
	mux.HandleFunc("/api/v1/language", corsMiddleware(api.HandleLanguage))
	mux.HandleFunc("/api/v1/containers", corsMiddleware(api.AuthMiddleware(api.SubUserMiddleware(api.HandleContainers))))
	mux.HandleFunc("/api/v1/containers/list", corsMiddleware(api.AuthMiddleware(api.SubUserMiddleware(api.HandleContainerListAlias))))
	mux.HandleFunc("/api/v1/containers/", corsMiddleware(api.AuthMiddleware(api.SubUserMiddleware(api.HandleSingleContainer))))
	mux.HandleFunc("/api/v1/templates", corsMiddleware(api.AuthMiddleware(api.HandleTemplates)))
	mux.HandleFunc("/api/v1/images", corsMiddleware(api.AuthMiddleware(api.HandleImages)))
	mux.HandleFunc("/api/v1/images/custom", corsMiddleware(api.AuthMiddleware(api.HandleCustomKVMImages)))
	mux.HandleFunc("/api/v1/images/download", corsMiddleware(api.AuthMiddleware(api.HandleImageDownload)))
	mux.HandleFunc("/api/v1/images/cancel", corsMiddleware(api.AuthMiddleware(api.HandleImageCancel)))
	mux.HandleFunc("/api/v1/images/delete", corsMiddleware(api.AuthMiddleware(api.HandleImageDelete)))
	mux.HandleFunc("/api/v1/images/toggle", corsMiddleware(api.AuthMiddleware(api.HandleImageToggle)))
	mux.HandleFunc("/api/v1/images/enabled", corsMiddleware(api.AuthMiddleware(api.SubUserMiddleware(api.HandleEnabledImages))))
	mux.HandleFunc("/api/v1/host-info", corsMiddleware(api.AuthMiddleware(api.HandleHostInfo)))
	mux.HandleFunc("/api/v1/host-history", corsMiddleware(api.AuthMiddleware(api.HandleHostHistory)))
	mux.HandleFunc("/api/v1/host-report", corsMiddleware(api.AuthMiddleware(api.HandleHostReport)))
	mux.HandleFunc("/api/v1/snapshots", corsMiddleware(api.AuthMiddleware(api.ScopeMiddleware("snapshot:read", api.HandleSnapshots))))
	mux.HandleFunc("/api/v1/routing/ipv4-scan", corsMiddleware(api.AuthMiddleware(api.HandleRoutingIPv4Scan)))
	mux.HandleFunc("/api/v1/routing", corsMiddleware(api.AuthMiddleware(api.HandleRouting)))
	mux.HandleFunc("/api/v1/storage", corsMiddleware(api.AdminMiddleware(api.HandleStorage)))
	mux.HandleFunc("/api/v1/ipv6/status", corsMiddleware(api.AuthMiddleware(api.HandleIPv6Status)))
	mux.HandleFunc("/api/v1/tasks", corsMiddleware(api.AuthMiddleware(api.SubUserMiddleware(api.HandleTasks))))
	mux.HandleFunc("/api/v1/tasks/", corsMiddleware(api.AuthMiddleware(api.HandleTaskDelete)))
	mux.HandleFunc("/api/v1/task-queue/settings", corsMiddleware(api.AdminMiddleware(api.HandleTaskQueueSettings)))
	mux.HandleFunc("/api/v1/batch-create", corsMiddleware(api.AuthMiddleware(api.HandleBatchCreate)))
	mux.HandleFunc("/api/v1/batch-action", corsMiddleware(api.AuthMiddleware(api.HandleBatchAction)))
	mux.HandleFunc("/api/v1/sub-user/create", corsMiddleware(api.AuthMiddleware(api.HandleSubUserCreate)))
	mux.HandleFunc("/api/v1/sub-users", corsMiddleware(api.AuthMiddleware(api.HandleSubUserList)))
	mux.HandleFunc("/api/v1/sub-users/", corsMiddleware(api.AuthMiddleware(api.HandleSubUserAction)))
	mux.HandleFunc("/api/v1/audit-logs", corsMiddleware(api.AuthMiddleware(api.HandleAuditLogs)))
	mux.HandleFunc("/api/v1/login-logs", corsMiddleware(api.AuthMiddleware(api.HandleLoginLogs)))
	mux.HandleFunc("/api/v1/ssl", corsMiddleware(api.AdminMiddleware(api.HandleSSLSettings)))
	mux.HandleFunc("/api/v1/webssh-origins", corsMiddleware(api.AdminMiddleware(api.HandleWebSSHOriginSettings)))
	mux.HandleFunc("/api/v1/access-policy", corsMiddleware(api.AdminMiddleware(api.HandlePanelAccessPolicy)))
	mux.HandleFunc("/api/v1/security/alerts", corsMiddleware(api.AuthMiddleware(api.ScopeMiddleware("security:read", api.HandleSecurityAlerts))))
	mux.HandleFunc("/api/v1/security/check", corsMiddleware(api.AuthMiddleware(api.ScopeMiddleware("security:check", api.HandleSecurityCheck))))
	mux.HandleFunc("/api/v1/security/logs", corsMiddleware(api.AuthMiddleware(api.ScopeMiddleware("security:read", api.HandleSecurityLogs))))
	mux.HandleFunc("/api/v1/security/summary", corsMiddleware(api.AuthMiddleware(api.ScopeMiddleware("security:read", api.HandleContainerSecuritySummary))))
	mux.HandleFunc("/api/v1/security/settings", corsMiddleware(api.AuthMiddleware(api.HandleSecuritySettings)))
	mux.HandleFunc("/api/v1/ssh-ticket", corsMiddleware(api.AuthMiddleware(api.HandleWebSSHTicket)))
	mux.HandleFunc("/api/v1/vnc-ticket", corsMiddleware(api.AuthMiddleware(api.HandleVNCTicket)))
	mux.HandleFunc("/api/v1/api-keys", corsMiddleware(api.AuthMiddleware(api.HandleApiKeys)))
	mux.HandleFunc("/api/v1/api-keys/", corsMiddleware(api.AuthMiddleware(api.HandleApiKeyDelete)))
	mux.HandleFunc("/api/v1/swap", corsMiddleware(api.AuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			api.HandleSwapInfo(w, r)
			return
		}
		api.HandleSwapManage(w, r)
	})))

	// Version (public)
	mux.HandleFunc("/api/version", corsMiddleware(api.HandleVersion))

	// Static files
	if webFS != nil {
		fs := http.FileServer(webFS)
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			// API routes already handled above
			if strings.HasPrefix(r.URL.Path, "/api/") {
				http.NotFound(w, r)
				return
			}
			// Try to serve file
			path := r.URL.Path
			f, err := webFS.Open(path)
			if err != nil {
				// SPA fallback: serve index.html
				indexFile, err := webFS.Open("index.html")
				if err != nil {
					http.Error(w, "Not found", http.StatusNotFound)
					return
				}
				defer indexFile.Close()
				stat, _ := indexFile.Stat()
				http.ServeContent(w, r, "index.html", stat.ModTime(), indexFile)
				return
			}
			defer f.Close()
			fs.ServeHTTP(w, r)
		})
	}
}

// Run starts the HTTP server
func Run() error {
	// Use embedded frontend files
	webFS = GetEmbeddedFS()
	api.StartHostMetricSampler()
	api.StartContainerMetricSampler()

	mux := http.NewServeMux()
	setupRoutes(mux)

	addr := fmt.Sprintf("0.0.0.0:%d", config.AppConfig.Port)
	log.Printf("CLICD Web Server starting on http://0.0.0.0:%d", config.AppConfig.Port)
	log.Printf("Admin user: %s", config.AppConfig.AdminUser)

	server := &http.Server{
		Addr:    addr,
		Handler: panelAccessMiddleware(mux),
	}

	if sslEnabled() {
		certPath, keyPath, err := config.ResolveSSLConfigPaths(config.AppConfig.SSL)
		if err != nil {
			return err
		}
		server.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
			GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
				safeCertPath, err := config.ResolveSSLPath(certPath)
				if err != nil {
					return nil, err
				}
				safeKeyPath, err := config.ResolveSSLPath(keyPath)
				if err != nil {
					return nil, err
				}
				cert, err := tls.LoadX509KeyPair(safeCertPath, safeKeyPath)
				if err != nil {
					return nil, err
				}
				return &cert, nil
			},
		}
		log.Printf("CLICD Web Server SSL enabled on https://0.0.0.0:%d", config.AppConfig.Port)
		return server.ListenAndServeTLS("", "")
	}

	return server.ListenAndServe()
}

func sslEnabled() bool {
	ssl := config.AppConfig.SSL
	if !ssl.Enabled {
		return false
	}
	certPath, keyPath, err := config.ResolveSSLConfigPaths(ssl)
	if err != nil {
		log.Printf("SSL paths are invalid, falling back to HTTP: %v", err)
		return false
	}
	safeCertPath, err := config.ResolveSSLPath(certPath)
	if err != nil {
		log.Printf("SSL certificate path is not allowed, falling back to HTTP: %v", err)
		return false
	}
	safeKeyPath, err := config.ResolveSSLPath(keyPath)
	if err != nil {
		log.Printf("SSL private key path is not allowed, falling back to HTTP: %v", err)
		return false
	}
	if _, err := config.ReadableFileStat(safeCertPath); err != nil {
		log.Printf("SSL certificate is not readable, falling back to HTTP: %v", err)
		return false
	}
	if _, err := config.ReadableFileStat(safeKeyPath); err != nil {
		log.Printf("SSL private key is not readable, falling back to HTTP: %v", err)
		return false
	}
	return true
}
