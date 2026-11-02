package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"clicd/internal/config"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

func generateRandomStr(length int) string {
	b := make([]byte, length)
	rand.Read(b)
	return hex.EncodeToString(b)[:length]
}

type subUserResponse struct {
	ID                   string   `json:"id"`
	Username             string   `json:"username"`
	Password             string   `json:"password,omitempty"`
	ContainerNames       []string `json:"container_names"`
	ContainerUUIDs       []string `json:"container_uuids,omitempty"`
	AllowedImageIDs      []string `json:"allowed_image_ids,omitempty"`
	ImageLimitConfigured bool     `json:"image_limit_configured,omitempty"`
	CurrentImageIDs      []string `json:"current_image_ids,omitempty"`
	AccessCode           string   `json:"access_code"`
	CreatedAt            string   `json:"created_at"`
}

func newSubUserResponse(su config.SubUser, password string) subUserResponse {
	return subUserResponse{
		ID:                   su.ID,
		Username:             su.Username,
		Password:             password,
		ContainerNames:       su.ContainerNames,
		ContainerUUIDs:       su.ContainerUUIDs,
		AllowedImageIDs:      effectiveSubUserAllowedImageIDs(&su),
		ImageLimitConfigured: su.ImageLimitConfigured,
		CurrentImageIDs:      subUserCurrentImageIDs(&su),
		AccessCode:           su.AccessCode,
		CreatedAt:            su.CreatedAt,
	}
}

// HandleSubUserCreate creates a sub-user for a specific container
func HandleSubUserCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	if !requireScope(w, r, "subuser:create") {
		return
	}

	var req struct {
		ContainerName string `json:"container_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
		return
	}

	c := containerByIdentifier(req.ContainerName)

	if c == nil {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Container not found"})
		return
	}
	containerName := c.Name

	// Check if sub-user already exists and return the same management password.
	for i := range config.AppConfig.SubUsers {
		su := &config.AppConfig.SubUsers[i]
		for _, uuid := range su.ContainerUUIDs {
			if uuid == c.UUID {
				if su.AccessCode == "" {
					su.AccessCode = generateRandomStr(8)
				}
				password := su.Password
				message := "Sub-user link returned"
				if password == "" {
					password = generateRandomStr(16)
					hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
					if err != nil {
						jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Failed to generate password"})
						return
					}
					su.PassHash = string(hash)
					su.Password = password
					su.Token = ""
					su.TokenVersion++
					message = "Sub-user password generated"
				}
				su.ContainerNames = appendUniqueString(su.ContainerNames, containerName)
				su.ContainerUUIDs = appendUniqueString(su.ContainerUUIDs, c.UUID)
				if !su.ImageLimitConfigured && len(su.AllowedImageIDs) == 0 {
					su.AllowedImageIDs = effectiveContainerAllowedImageIDs(c)
					su.ImageLimitConfigured = true
				}
				config.SaveConfig()
				jsonResponse(w, http.StatusOK, APIResponse{
					Success: true,
					Message: message,
					Data:    newSubUserResponse(*su, password),
				})
				return
			}
		}
	}

	// Create new sub-user
	username := "user-" + generateRandomStr(8)
	password := generateRandomStr(16)
	hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)

	// Generate short access code (8 chars, for URL sharing)
	accessCode := generateRandomStr(8)

	subUser := config.SubUser{
		ID:                   "sub-" + generateRandomStr(8),
		Username:             username,
		Password:             password,
		PassHash:             string(hash),
		ContainerNames:       []string{containerName},
		ContainerUUIDs:       []string{c.UUID},
		AllowedImageIDs:      effectiveContainerAllowedImageIDs(c),
		ImageLimitConfigured: true,
		AccessCode:           accessCode,
		CreatedAt:            time.Now().Format("2006-01-02 15:04:05"),
	}

	config.AppConfig.SubUsers = append(config.AppConfig.SubUsers, subUser)
	config.SaveConfig()
	config.AddAuditLog("创建子用户", containerName, fmt.Sprintf("用户: %s", username), "admin")

	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "Sub-user created", Data: newSubUserResponse(subUser, password)})
}

// HandleSubUserLogin handles sub-user login
func HandleSubUserLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
		return
	}

	clientIP := r.Header.Get("X-Forwarded-For")
	if clientIP == "" {
		clientIP = r.RemoteAddr
	}
	clientUA := r.Header.Get("User-Agent")

	// Find sub-user
	for _, su := range config.AppConfig.SubUsers {
		if su.Username == req.Username {
			if err := bcrypt.CompareHashAndPassword([]byte(su.PassHash), []byte(req.Password)); err == nil {
				containerUUIDs := activeSubUserContainerUUIDs(&su)
				if len(containerUUIDs) == 0 {
					config.AddLoginLog(su.Username, clientIP, clientUA, false)
					jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "No active container is assigned to this user"})
					return
				}
				tokenStr := newSubUserToken(su.Username, containerUUIDs, time.Now().Add(24*time.Hour), su.TokenVersion)
				config.AddLoginLog(su.Username, clientIP, clientUA, true)

				jsonResponse(w, http.StatusOK, APIResponse{
					Success: true,
					Data: map[string]interface{}{
						"token":           tokenStr,
						"username":        su.Username,
						"container_uuids": containerUUIDs,
					},
				})
				return
			} else {
				config.AddLoginLog(su.Username, clientIP, clientUA, false)
			}
		}
	}

	jsonResponse(w, http.StatusUnauthorized, APIResponse{Success: false, Message: "Invalid credentials"})
}

// HandleSubUserAccessCode handles access via short code + password (no token in URL)
func HandleSubUserAccessCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}

	var req struct {
		Code     string `json:"code"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
		return
	}

	// Find sub-user by access code
	clientIP := r.Header.Get("X-Forwarded-For")
	if clientIP == "" {
		clientIP = r.RemoteAddr
	}
	clientUA := r.Header.Get("User-Agent")

	for _, su := range config.AppConfig.SubUsers {
		if su.AccessCode == req.Code {
			if err := bcrypt.CompareHashAndPassword([]byte(su.PassHash), []byte(req.Password)); err != nil {
				config.AddLoginLog(su.Username, clientIP, clientUA, false)
				jsonResponse(w, http.StatusUnauthorized, APIResponse{Success: false, Message: "Invalid password"})
				return
			}

			containerUUIDs := activeSubUserContainerUUIDs(&su)
			if len(containerUUIDs) == 0 {
				config.AddLoginLog(su.Username, clientIP, clientUA, false)
				jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "No active container is assigned to this link"})
				return
			}
			tokenStr := newSubUserToken(su.Username, containerUUIDs, time.Now().Add(24*time.Hour), su.TokenVersion)
			config.AddLoginLog(su.Username, clientIP, clientUA, true)

			jsonResponse(w, http.StatusOK, APIResponse{
				Success: true,
				Data: map[string]interface{}{
					"token":           tokenStr,
					"username":        su.Username,
					"container_uuids": containerUUIDs,
				},
			})
			return
		}
	}

	jsonResponse(w, http.StatusUnauthorized, APIResponse{Success: false, Message: "Invalid access code"})
}

func newSubUserToken(username string, containerUUIDs []string, expiresAt time.Time, tokenVersion int) string {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub_user":        username,
		"container_uuids": containerUUIDs,
		"token_version":   tokenVersion,
		"exp":             expiresAt.Unix(),
		"iat":             time.Now().Unix(),
	})
	tokenStr, _ := token.SignedString([]byte(config.AppConfig.JWTSecret))
	return tokenStr
}

type subUserAccess struct {
	names map[string]bool
	uuids map[string]bool
}

func subUserAllowedContainers(r *http.Request) (subUserAccess, bool) {
	claims, ok := claimsFromRequest(r)
	if !ok {
		return subUserAccess{}, false
	}
	if _, isSubUser := claims["sub_user"]; !isSubUser {
		return subUserAccess{}, false
	}

	allowed := subUserAccess{
		names: make(map[string]bool),
		uuids: make(map[string]bool),
	}
	if containerUUIDs, ok := claims["container_uuids"].([]interface{}); ok {
		for _, item := range containerUUIDs {
			if uuid, ok := item.(string); ok {
				allowed.uuids[uuid] = true
			}
		}
	}
	if containerUUIDs, ok := claims["container_uuids"].([]string); ok {
		for _, uuid := range containerUUIDs {
			allowed.uuids[uuid] = true
		}
	}
	return allowed, true
}

func requestAllowedContainers(r *http.Request) (subUserAccess, bool) {
	if ctx, ok := authContextFromRequest(r); ok {
		if ctx.Type == authTypeAPIKey && len(ctx.ContainerUUIDs) == 0 {
			return subUserAccess{}, false
		}
		if ctx.Type == authTypeSubUser || ctx.Type == authTypeAPIKey {
			allowed := subUserAccess{names: make(map[string]bool), uuids: make(map[string]bool)}
			for _, uuid := range ctx.ContainerUUIDs {
				allowed.uuids[uuid] = true
			}
			if ctx.Type == authTypeSubUser && len(ctx.ContainerUUIDs) == 0 {
				legacy, ok := subUserAllowedContainers(r)
				if ok {
					return legacy, true
				}
			}
			return allowed, true
		}
	}
	return subUserAllowedContainers(r)
}

func subUserFromRequest(r *http.Request) *config.SubUser {
	username := ""
	if ctx, ok := authContextFromRequest(r); ok && ctx.Type == authTypeSubUser {
		username = ctx.Username
	}
	if username == "" {
		if claims, ok := claimsFromRequest(r); ok {
			username, _ = claims["sub_user"].(string)
		}
	}
	if username == "" {
		return nil
	}
	for i := range config.AppConfig.SubUsers {
		if config.AppConfig.SubUsers[i].Username == username {
			return &config.AppConfig.SubUsers[i]
		}
	}
	return nil
}

func normalizeAllowedImageIDs(ids []string) ([]string, error) {
	seen := map[string]bool{}
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		if !imageTemplateExists(id) {
			return nil, fmt.Errorf("unknown image template: %s", id)
		}
		seen[id] = true
		result = append(result, id)
	}
	return result, nil
}

func isTemplateAllowedForRequest(r *http.Request, c *config.Container, templateID string) bool {
	if !isSubUserRequest(r) {
		return true
	}
	return isImageAllowedForSubUser(subUserFromRequest(r), c, templateID)
}

func isImageAllowedForSubUser(su *config.SubUser, c *config.Container, templateID string) bool {
	if su == nil || strings.TrimSpace(templateID) == "" {
		return false
	}
	for _, id := range effectiveSubUserAllowedImageIDs(su) {
		if id == templateID {
			return true
		}
	}
	return false
}

func effectiveContainerAllowedImageIDs(c *config.Container) []string {
	if c == nil {
		return nil
	}
	if c.ImageLimitConfigured || len(c.AllowedImageIDs) > 0 {
		return cleanImageIDList(c.AllowedImageIDs)
	}
	if c.Template != "" {
		return []string{c.Template}
	}
	return nil
}

func effectiveSubUserAllowedImageIDs(su *config.SubUser) []string {
	if su == nil {
		return nil
	}
	if su.ImageLimitConfigured || len(su.AllowedImageIDs) > 0 {
		return cleanImageIDList(su.AllowedImageIDs)
	}
	result := []string{}
	seen := map[string]bool{}
	for _, c := range subUserAssignedContainers(su) {
		for _, id := range effectiveContainerAllowedImageIDs(c) {
			if id != "" && !seen[id] {
				seen[id] = true
				result = append(result, id)
			}
		}
	}
	return result
}

func cleanImageIDList(ids []string) []string {
	result := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		result = append(result, id)
	}
	return result
}

func subUserCurrentImageIDs(su *config.SubUser) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, c := range subUserAssignedContainers(su) {
		if c.Template != "" && !seen[c.Template] {
			seen[c.Template] = true
			result = append(result, c.Template)
		}
	}
	return result
}

func subUserAssignedContainers(su *config.SubUser) []*config.Container {
	if su == nil {
		return nil
	}
	result := []*config.Container{}
	seen := map[string]bool{}
	for _, uuid := range su.ContainerUUIDs {
		if c := config.FindContainerByUUID(uuid); c != nil {
			key := c.UUID
			if key == "" {
				key = c.Name
			}
			if !seen[key] {
				seen[key] = true
				result = append(result, c)
			}
		}
	}
	for _, name := range su.ContainerNames {
		if c := config.FindContainerByName(name); c != nil {
			key := c.UUID
			if key == "" {
				key = c.Name
			}
			if !seen[key] {
				seen[key] = true
				result = append(result, c)
			}
		}
	}
	return result
}

func isAccessRestrictedRequest(r *http.Request) bool {
	_, restricted := requestAllowedContainers(r)
	return restricted
}

func containerByIdentifier(identifier string) *config.Container {
	return config.FindContainerByIdentifier(identifier)
}

func isContainerAllowedForRequest(r *http.Request, identifier string) bool {
	allowed, restricted := requestAllowedContainers(r)
	if !restricted {
		return true
	}
	c := containerByIdentifier(identifier)
	if c == nil {
		return false
	}
	return isContainerAllowed(allowed, c)
}

// HandleAuditLogs returns audit logs
func HandleAuditLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	if !requireScope(w, r, "audit:read") {
		return
	}

	logs := config.AppConfig.AuditLogs
	if logs == nil {
		logs = []config.AuditLog{}
	}
	// Return in reverse order (newest first)
	reversed := make([]config.AuditLog, len(logs))
	for i, l := range logs {
		reversed[len(logs)-1-i] = l
	}

	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: reversed})
}

// SubUserMiddleware checks if a request is from a sub-user and restricts container access
func SubUserMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		allowed, isSubUser := subUserAllowedContainers(r)
		if !isSubUser {
			next(w, r)
			return
		}

		path := r.URL.Path
		containerPrefix := "/api/containers/"
		containerListPath := "/api/containers"
		tasksPath := "/api/tasks"
		if strings.HasPrefix(path, "/api/v1/") {
			containerPrefix = "/api/v1/containers/"
			containerListPath = "/api/v1/containers"
			tasksPath = "/api/v1/tasks"
		}
		if path == tasksPath && r.Method == http.MethodGet {
			next(w, r)
			return
		}

		imagesEnabledPath := "/api/images/enabled"
		if strings.HasPrefix(path, "/api/v1/") {
			imagesEnabledPath = "/api/v1/images/enabled"
		}
		if path == imagesEnabledPath && r.Method == http.MethodGet {
			next(w, r)
			return
		}

		if path == containerListPath {
			if r.Method != http.MethodGet {
				jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "Sub-users cannot create containers"})
				return
			}
			next(w, r)
			return
		}

		if strings.HasPrefix(path, containerPrefix) {
			rest := path[len(containerPrefix):]
			parts := splitPath(rest)
			if len(parts) > 0 && parts[0] != "" {
				c := containerByIdentifier(parts[0])
				if c == nil || !isContainerAllowed(allowed, c) {
					jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "Access denied to this container"})
					return
				}
				action := ""
				if len(parts) > 1 {
					action = strings.Join(parts[1:], "/")
				}
				if !isSubUserContainerActionAllowed(action, r.Method) {
					jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "Action is not allowed for this link"})
					return
				}
			}
			next(w, r)
			return
		}

		jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "Access denied"})
		return
	}
}

func filterContainersForRequest(r *http.Request, containers []config.Container) []config.Container {
	allowed, restricted := requestAllowedContainers(r)
	if !restricted {
		return containers
	}
	filtered := make([]config.Container, 0, len(containers))
	for _, c := range containers {
		if isContainerAllowed(allowed, &c) {
			filtered = append(filtered, c)
		}
	}
	return filtered
}

func filterTasksForRequest(r *http.Request, tasks []*Task) []*Task {
	filtered := make([]*Task, 0, len(tasks))
	for _, task := range tasks {
		if isTaskAllowedForRequest(r, task) {
			filtered = append(filtered, task)
		}
	}
	return filtered
}

func isTaskAllowedForRequest(r *http.Request, task *Task) bool {
	allowed, restricted := requestAllowedContainers(r)
	if !restricted {
		return true
	}
	if task == nil {
		return false
	}
	if c := config.FindContainer(task.ContainerID); c != nil && isContainerAllowed(allowed, c) {
		return true
	}
	if task.ContainerName != "" {
		if c := config.FindContainerByName(task.ContainerName); c != nil && isContainerAllowed(allowed, c) {
			return true
		}
	}
	if task.Config.Name != "" {
		if c := config.FindContainerByName(task.Config.Name); c != nil && isContainerAllowed(allowed, c) {
			return true
		}
	}
	return false
}

func isContainerAllowed(allowed subUserAccess, c *config.Container) bool {
	if c == nil {
		return false
	}
	if c.UUID != "" && allowed.uuids[c.UUID] {
		return true
	}
	return c.Name != "" && allowed.names[c.Name]
}

func isSubUserContainerActionAllowed(action string, method string) bool {
	if action == "" {
		return method == http.MethodGet
	}
	switch {
	case action == "usage" || action == "traffic" || action == "history" || action == "random-port":
		return method == http.MethodGet
	case action == "firewall":
		return method == http.MethodGet || method == http.MethodPut
	case action == "snapshots":
		return method == http.MethodGet || method == http.MethodPost
	case action == "snapshots/schedule":
		return method == http.MethodPost
	case strings.HasPrefix(action, "snapshots/"):
		return method == http.MethodDelete || method == http.MethodPost
	case action == "start" || action == "stop" || action == "restart" || action == "reinstall" || action == "reset-password":
		return method == http.MethodPost
	case strings.HasPrefix(action, "port-mappings/"):
		return method == http.MethodPut
	case action == "ipv6-port-mappings":
		return method == http.MethodGet
	case strings.HasPrefix(action, "ipv6-port-mappings/"):
		return method == http.MethodPut
	default:
		return false
	}
}

func activeSubUserContainerUUIDs(su *config.SubUser) []string {
	uuids := make([]string, 0, len(su.ContainerUUIDs))
	for _, uuid := range su.ContainerUUIDs {
		if c := config.FindContainerByUUID(uuid); c != nil {
			uuids = appendUniqueString(uuids, c.UUID)
		}
	}
	if len(uuids) > 0 {
		return uuids
	}
	return subUserContainerUUIDs(su.ContainerNames)
}

func subUserContainerUUIDs(containerNames []string) []string {
	uuids := make([]string, 0, len(containerNames))
	for _, name := range containerNames {
		if c := config.FindContainerByName(name); c != nil && c.UUID != "" {
			uuids = appendUniqueString(uuids, c.UUID)
		}
	}
	return uuids
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func splitPath(path string) []string {
	parts := make([]string, 0)
	for _, p := range splitBy(path, "/") {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

func splitBy(s, sep string) []string {
	result := make([]string, 0)
	current := ""
	for _, c := range s {
		if string(c) == sep {
			result = append(result, current)
			current = ""
		} else {
			current += string(c)
		}
	}
	result = append(result, current)
	return result
}

// SubUserListItem is the enriched sub-user info returned by the list API
type SubUserListItem struct {
	ID                   string   `json:"id"`
	Username             string   `json:"username"`
	ContainerNames       []string `json:"container_names"`
	ContainerUUIDs       []string `json:"container_uuids"`
	AllowedImageIDs      []string `json:"allowed_image_ids"`
	ImageLimitConfigured bool     `json:"image_limit_configured"`
	CurrentImageIDs      []string `json:"current_image_ids"`
	ContainerName        string   `json:"container_name"`
	ContainerUUID        string   `json:"container_uuid"`
	AccessCode           string   `json:"access_code"`
	Password             string   `json:"password,omitempty"`
	CreatedAt            string   `json:"created_at"`
	LastLogin            string   `json:"last_login"`
	LastLoginIP          string   `json:"last_login_ip"`
	LastLoginUA          string   `json:"last_login_ua"`
}

// HandleSubUserList returns the list of all sub-users with container info
func HandleSubUserList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	if !requireScope(w, r, "subuser:read") {
		return
	}

	result := make([]SubUserListItem, 0, len(config.AppConfig.SubUsers))
	for _, su := range config.AppConfig.SubUsers {
		item := SubUserListItem{
			ID:                   su.ID,
			Username:             su.Username,
			ContainerNames:       su.ContainerNames,
			ContainerUUIDs:       su.ContainerUUIDs,
			AllowedImageIDs:      effectiveSubUserAllowedImageIDs(&su),
			ImageLimitConfigured: su.ImageLimitConfigured,
			CurrentImageIDs:      subUserCurrentImageIDs(&su),
			AccessCode:           su.AccessCode,
			Password:             su.Password,
			CreatedAt:            su.CreatedAt,
		}

		// Resolve container name from first active UUID
		for _, uuid := range su.ContainerUUIDs {
			if c := config.FindContainerByUUID(uuid); c != nil {
				item.ContainerName = c.Name
				item.ContainerUUID = c.UUID
				break
			}
		}
		if item.ContainerName == "" && len(su.ContainerNames) > 0 {
			item.ContainerName = su.ContainerNames[0]
		}

		// Find last login time
		for i := len(config.AppConfig.LoginLogs) - 1; i >= 0; i-- {
			log := config.AppConfig.LoginLogs[i]
			if log.Username == su.Username {
				item.LastLogin = log.Time
				item.LastLoginIP = log.IP
				item.LastLoginUA = log.UserAgent
				break
			}
		}

		// Skip orphaned sub-users with no active containers
		if item.ContainerName == "" && item.ContainerUUID == "" {
			continue
		}

		result = append(result, item)
	}

	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: result})
}

// HandleSubUserAction handles actions on a specific sub-user
func HandleSubUserAction(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/sub-users/")
	path = strings.TrimPrefix(path, "/api/sub-users/")
	parts := strings.SplitN(path, "/", 2)
	subUserID := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	// Find sub-user
	var target *config.SubUser
	for i := range config.AppConfig.SubUsers {
		if config.AppConfig.SubUsers[i].ID == subUserID {
			target = &config.AppConfig.SubUsers[i]
			break
		}
	}
	if target == nil {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Sub-user not found"})
		return
	}

	switch {
	case action == "rotate-password" && r.Method == http.MethodPost:
		if !requireScope(w, r, "subuser:update") {
			return
		}
		password := generateRandomStr(16)
		if hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost); err == nil {
			target.PassHash = string(hash)
			target.Password = password
			target.Token = ""
			target.TokenVersion++ // invalidate all existing tokens
			config.SaveConfig()
			jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: map[string]string{
				"password":    password,
				"access_code": target.AccessCode,
				"username":    target.Username,
			}})
			return
		}
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Failed to generate password"})

	case action == "audit-logs" && r.Method == http.MethodGet:
		if !requireScope(w, r, "audit:read") {
			return
		}
		// Filter audit logs for this sub-user
		logs := filterSubUserAuditLogs(target.Username)
		jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: logs})

	case action == "login-logs" && r.Method == http.MethodGet:
		if !requireScope(w, r, "loginlog:read") {
			return
		}
		// Filter login logs for this sub-user
		logs := filterSubUserLoginLogs(target.Username)
		jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: logs})

	case action == "images" && r.Method == http.MethodPut:
		if !requireScope(w, r, "subuser:update") {
			return
		}
		var req struct {
			AllowedImageIDs []string `json:"allowed_image_ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
			return
		}
		ids, err := normalizeAllowedImageIDs(req.AllowedImageIDs)
		if err != nil {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
			return
		}
		target.AllowedImageIDs = ids
		target.ImageLimitConfigured = true
		target.TokenVersion++
		config.SaveConfig()
		jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: newSubUserResponse(*target, target.Password)})

	default:
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Action not found"})
	}
}

func filterSubUserAuditLogs(username string) []config.AuditLog {
	result := make([]config.AuditLog, 0)
	for i := len(config.AppConfig.AuditLogs) - 1; i >= 0; i-- {
		log := config.AppConfig.AuditLogs[i]
		if log.User == username || strings.HasPrefix(log.User, "user:") && strings.Contains(log.User, username) {
			result = append(result, log)
		}
	}
	if result == nil {
		result = []config.AuditLog{}
	}
	return result
}

func filterSubUserLoginLogs(username string) []config.SavedLoginLog {
	result := make([]config.SavedLoginLog, 0)
	for i := len(config.AppConfig.LoginLogs) - 1; i >= 0; i-- {
		log := config.AppConfig.LoginLogs[i]
		if log.Username == username {
			result = append(result, log)
		}
	}
	if result == nil {
		result = []config.SavedLoginLog{}
	}
	return result
}
