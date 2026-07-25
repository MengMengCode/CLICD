package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"clicd/internal/config"
	"clicd/internal/lxc"
)

type TaskType string

const (
	TaskCreate    TaskType = "create"
	TaskStart     TaskType = "start"
	TaskStop      TaskType = "stop"
	TaskRestart   TaskType = "restart"
	TaskDelete    TaskType = "delete"
	TaskReinstall TaskType = "reinstall"
)

type Task struct {
	ID            string              `json:"id"`
	Type          TaskType            `json:"type"`
	ContainerID   int                 `json:"container_id"`
	ContainerName string              `json:"container_name"`
	Status        string              `json:"status"`
	Error         string              `json:"error,omitempty"`
	Stage         string              `json:"stage,omitempty"`
	StageDetail   string              `json:"stage_detail,omitempty"`
	CreatedAt     string              `json:"created_at"`
	TemplateID    string              `json:"template_id,omitempty"`
	Config        lxc.ContainerConfig `json:"config,omitempty"`
	Name          string              `json:"name,omitempty"`
	User          string              `json:"user,omitempty"` // who created this task
	IP            string              `json:"ip,omitempty"`
	UserAgent     string              `json:"user_agent,omitempty"`
	activeKey     string
}

type TaskQueue struct {
	mu             sync.Mutex
	createQueue    []*Task
	opQueue        []*Task
	tasks          map[string]*Task
	nextID         int
	createCond     *sync.Cond
	opCond         *sync.Cond
	maxConcurrency int
	activeTasks    int
	activeTargets  map[string]bool
	stop           chan struct{}
}

type TaskQueueSettings struct {
	Concurrency int `json:"concurrency"`
	Active      int `json:"active"`
	Pending     int `json:"pending"`
}

var globalQueue *TaskQueue

func init() {
	globalQueue = newTaskQueue(config.DefaultTaskConcurrency)
	go globalQueue.createDispatcher()
	go globalQueue.opDispatcher()
}

func newTaskQueue(concurrency int) *TaskQueue {
	q := &TaskQueue{
		tasks:          make(map[string]*Task),
		maxConcurrency: config.NormalizeTaskConcurrency(concurrency),
		activeTargets:  make(map[string]bool),
		stop:           make(chan struct{}),
	}
	q.createCond = sync.NewCond(&q.mu)
	q.opCond = sync.NewCond(&q.mu)
	return q
}

func ConfigureTaskQueue(concurrency int) {
	globalQueue.SetConcurrency(concurrency)
}

func (q *TaskQueue) SetConcurrency(concurrency int) {
	q.mu.Lock()
	q.maxConcurrency = config.NormalizeTaskConcurrency(concurrency)
	q.createCond.Broadcast()
	q.opCond.Broadcast()
	q.mu.Unlock()
}

func (q *TaskQueue) Settings() TaskQueueSettings {
	q.mu.Lock()
	defer q.mu.Unlock()
	return TaskQueueSettings{
		Concurrency: q.maxConcurrency,
		Active:      q.activeTasks,
		Pending:     len(q.createQueue) + len(q.opQueue),
	}
}

func (q *TaskQueue) signalDispatchers() {
	q.createCond.Broadcast()
	q.opCond.Broadcast()
}

func (q *TaskQueue) enqueueTask(task *Task) {
	q.tasks[task.ID] = task
	if task.Type == TaskCreate {
		q.createQueue = append(q.createQueue, task)
		q.createCond.Signal()
	} else {
		q.opQueue = append(q.opQueue, task)
		q.opCond.Signal()
	}
}

func (q *TaskQueue) Enqueue(containerID int, containerName string, taskType TaskType, templateID string, cfg *lxc.ContainerConfig) []string {
	return q.EnqueueWithAudit(containerID, containerName, taskType, templateID, cfg, "admin", "", "")
}

func (q *TaskQueue) EnqueueWithAudit(containerID int, containerName string, taskType TaskType, templateID string, cfg *lxc.ContainerConfig, user string, ip string, userAgent string) []string {
	q.mu.Lock()
	defer q.mu.Unlock()

	id := q.nextID
	q.nextID++
	task := &Task{
		ID:            fmt.Sprintf("task-%d", id),
		Type:          taskType,
		ContainerID:   containerID,
		ContainerName: containerName,
		Status:        "pending",
		Stage:         "queued",
		StageDetail:   "排队等待",
		CreatedAt:     time.Now().Format("2006-01-02 15:04:05"),
		TemplateID:    templateID,
		User:          user,
		IP:            ip,
		UserAgent:     userAgent,
	}
	if cfg != nil {
		task.Config = *cfg
		task.Config.NormalizeResourceAliases()
	}
	q.enqueueTask(task)
	q.persistTasks()
	return []string{task.ID}
}

func (q *TaskQueue) EnqueueBatch(taskType TaskType, ids []int, templateID string) []string {
	return q.EnqueueBatchWithUser(taskType, ids, templateID, "admin")
}

func (q *TaskQueue) EnqueueBatchWithUser(taskType TaskType, ids []int, templateID string, user string) []string {
	return q.EnqueueBatchWithAudit(taskType, ids, templateID, user, "", "")
}

func (q *TaskQueue) EnqueueBatchWithAudit(taskType TaskType, ids []int, templateID string, user string, ip string, userAgent string) []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	var result []string
	for _, id := range ids {
		c := config.FindContainer(id)
		name := ""
		if c != nil {
			name = c.Name
		}
		result = append(result, q.enqueueSingleWithAudit(id, name, taskType, templateID, user, ip, userAgent))
	}
	q.persistTasks()
	return result
}

func (q *TaskQueue) EnqueueBatchCreate(configs []lxc.ContainerConfig) []string {
	return q.EnqueueBatchCreateWithAudit(configs, "admin", "", "")
}

func (q *TaskQueue) EnqueueBatchCreateWithAudit(configs []lxc.ContainerConfig, user string, ip string, userAgent string) []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.enqueueBatchCreateList(configs, user, ip, userAgent)
}

func (q *TaskQueue) ActiveCreateNames() map[string]bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	names := make(map[string]bool)
	for _, task := range q.tasks {
		if task.Type != TaskCreate || (task.Status != "pending" && task.Status != "running") {
			continue
		}
		name := task.Config.Name
		if name == "" {
			name = task.ContainerName
		}
		if name != "" {
			names[name] = true
		}
	}
	return names
}

func (q *TaskQueue) enqueueBatchCreateList(configs []lxc.ContainerConfig, user string, ip string, userAgent string) []string {
	var result []string
	for _, cfg := range configs {
		cfgCopy := cfg
		cfgCopy.NormalizeResourceAliases()
		id := q.nextID
		q.nextID++
		task := &Task{
			ID:            fmt.Sprintf("task-%d", id),
			Type:          TaskCreate,
			ContainerID:   0,
			ContainerName: cfgCopy.Name,
			Status:        "pending",
			Stage:         "queued",
			StageDetail:   "排队等待",
			CreatedAt:     time.Now().Format("2006-01-02 15:04:05"),
			Config:        cfgCopy,
			User:          user,
			IP:            ip,
			UserAgent:     userAgent,
		}
		q.enqueueTask(task)
		result = append(result, task.ID)
	}
	q.persistTasks()
	return result
}

func (q *TaskQueue) enqueueSingle(containerID int, containerName string, taskType TaskType, templateID string) string {
	return q.enqueueSingleWithUser(containerID, containerName, taskType, templateID, "admin")
}

func (q *TaskQueue) enqueueSingleWithUser(containerID int, containerName string, taskType TaskType, templateID string, user string) string {
	return q.enqueueSingleWithAudit(containerID, containerName, taskType, templateID, user, "", "")
}

func (q *TaskQueue) enqueueSingleWithAudit(containerID int, containerName string, taskType TaskType, templateID string, user string, ip string, userAgent string) string {
	id := q.nextID
	q.nextID++
	task := &Task{
		ID:            fmt.Sprintf("task-%d", id),
		Type:          taskType,
		ContainerID:   containerID,
		ContainerName: containerName,
		Status:        "pending",
		Stage:         "queued",
		StageDetail:   "排队等待",
		CreatedAt:     time.Now().Format("2006-01-02 15:04:05"),
		TemplateID:    templateID,
		User:          user,
		IP:            ip,
		UserAgent:     userAgent,
	}
	q.enqueueTask(task)
	return task.ID
}

func (q *TaskQueue) EnqueueSecurityStop(containerID int, containerName string) (string, bool) {
	if !config.AppConfig.SecurityAutoShutdown {
		return "", false
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	for _, task := range q.tasks {
		if task.Type != TaskStop || task.ContainerID != containerID {
			continue
		}
		if task.Status == "pending" || task.Status == "running" {
			return task.ID, false
		}
	}

	taskID := q.enqueueSingleWithAudit(containerID, containerName, TaskStop, "", "system:security", "", "")
	q.persistTasks()
	return taskID, true
}

func (q *TaskQueue) CancelPendingSecurityStops() int {
	q.mu.Lock()
	defer q.mu.Unlock()

	cancelled := 0
	newOpQueue := make([]*Task, 0, len(q.opQueue))
	for _, task := range q.opQueue {
		if isSecurityStopTask(task) && task.Status == "pending" {
			delete(q.tasks, task.ID)
			cancelled++
			continue
		}
		newOpQueue = append(newOpQueue, task)
	}
	q.opQueue = newOpQueue

	for id, task := range q.tasks {
		if isSecurityStopTask(task) && task.Status == "pending" {
			delete(q.tasks, id)
			cancelled++
		}
	}
	if cancelled > 0 {
		q.persistTasks()
	}
	return cancelled
}

// The two dispatchers keep long-running creates from blocking power operations,
// while sharing one global concurrency budget.
func (q *TaskQueue) createDispatcher() {
	for {
		task := q.takeNextTask(true)
		go q.runCreateTask(task)
	}
}

func (q *TaskQueue) opDispatcher() {
	for {
		task := q.takeNextTask(false)
		go q.runOperationTask(task)
	}
}

func (q *TaskQueue) takeNextTask(create bool) *Task {
	q.mu.Lock()
	defer q.mu.Unlock()
	cond := q.opCond
	if create {
		cond = q.createCond
	}
	for {
		queue := q.opQueue
		if create {
			queue = q.createQueue
		}
		if q.activeTasks < q.maxConcurrency {
			if index := runnableTaskIndex(queue, q.activeTargets); index >= 0 {
				task := queue[index]
				queue = append(queue[:index], queue[index+1:]...)
				if create {
					q.createQueue = queue
				} else {
					q.opQueue = queue
				}
				task.Status = "running"
				task.Error = ""
				task.Stage = "preparing"
				task.StageDetail = "准备初始化环境"
				task.activeKey = taskConcurrencyKey(task)
				q.activeTargets[task.activeKey] = true
				q.activeTasks++
				q.persistTasks()
				return task
			}
		}
		cond.Wait()
	}
}

func runnableTaskIndex(queue []*Task, activeTargets map[string]bool) int {
	for index, task := range queue {
		if !activeTargets[taskConcurrencyKey(task)] {
			return index
		}
	}
	return -1
}

func taskConcurrencyKey(task *Task) string {
	if task == nil {
		return "task:nil"
	}
	name := strings.TrimSpace(task.ContainerName)
	if name == "" {
		name = strings.TrimSpace(task.Config.Name)
	}
	if name != "" {
		return "name:" + strings.ToLower(name)
	}
	if task.ContainerID > 0 {
		return fmt.Sprintf("id:%d", task.ContainerID)
	}
	return "task:" + task.ID
}

func (q *TaskQueue) finishTask(task *Task, status string, taskErr error) {
	q.mu.Lock()
	task.Status = status
	if taskErr != nil {
		task.Error = taskErr.Error()
		if task.Type == TaskCreate {
			task.Stage = "failed"
			task.StageDetail = "初始化失败"
		}
	} else {
		task.Error = ""
		if task.Type == TaskCreate {
			task.Stage = "completed"
			task.StageDetail = "初始化完成"
		}
	}
	if task.activeKey != "" {
		delete(q.activeTargets, task.activeKey)
		task.activeKey = ""
	}
	if q.activeTasks > 0 {
		q.activeTasks--
	}
	q.persistTasks()
	q.signalDispatchers()
	q.mu.Unlock()
}

func (q *TaskQueue) updateTaskStage(task *Task, stage, detail string) {
	q.mu.Lock()
	task.Stage = stage
	task.StageDetail = detail
	q.mu.Unlock()
}

// runCreateTask handles lxc-create, resource setup, start, and SSH init. A
// restored task resumes initialization when the same-name container exists.
func (q *TaskQueue) runCreateTask(task *Task) {
	q.mu.Lock()
	createdByTask := false
	if task.Config.Name == "" {
		task.Config.Name = task.ContainerName
	}
	task.Config.NormalizeResourceAliases()
	cfg := task.Config
	q.mu.Unlock()
	cfg.Progress = func(stage, detail string) {
		q.updateTaskStage(task, stage, detail)
	}
	if cfg.Name == "" {
		err := fmt.Errorf("container name is required")
		config.AddAuditLog(string(task.Type), task.ContainerName, "failed: "+err.Error(), "admin")
		q.finishTask(task, "failed", err)
		return
	}
	c := config.FindContainerByName(cfg.Name)
	if c == nil {
		if err := createByRuntime(cfg); err != nil {
			config.AddAuditLog(string(task.Type), cfg.Name, "失败: "+err.Error(), "admin")
			q.finishTask(task, "failed", err)
			return
		}
		createdByTask = true
		c = config.FindContainerByName(cfg.Name)
		if c == nil {
			err := fmt.Errorf("created but not found in config")
			config.AddAuditLog(string(task.Type), task.Config.Name, "失败: "+err.Error(), "admin")
			q.finishTask(task, "failed", err)
			return
		}
	} else {
		lxc.ReleaseQueuedCreateNATPorts(cfg.Name)
	}

	q.mu.Lock()
	task.ContainerID = c.ID
	task.ContainerName = c.Name
	q.mu.Unlock()
	startDetail := "启动容器并等待网络就绪"
	if strings.EqualFold(cfg.Virtualization, config.VirtualizationKVM) {
		startDetail = "启动虚拟机并等待网络就绪"
	}
	q.updateTaskStage(task, "starting", startDetail)
	if err := startByRuntime(c.ID); err != nil {
		if createdByTask {
			_ = destroyByRuntime(c.ID)
		}
		config.AddAuditLog(string(task.Type), task.ContainerName, "初始化失败: "+err.Error(), "admin")
		q.finishTask(task, "failed", err)
		return
	}
	config.AddAuditLog(string(task.Type), task.ContainerName, "成功", "admin")
	q.finishTask(task, "done", nil)
}

func (q *TaskQueue) runOperationTask(task *Task) {
	q.mu.Lock()
	err := resolveTaskContainer(task)
	q.mu.Unlock()
	skipped := false
	if err == nil && (task.Type == TaskStart || task.Type == TaskRestart || task.Type == TaskReinstall) {
		c := config.FindContainer(task.ContainerID)
		if c != nil {
			if lxc.IsExpired(*c) {
				err = fmt.Errorf("容器已到期，不允许此操作")
			} else if lxc.IsTrafficExceeded(*c) {
				err = fmt.Errorf("容器流量已超限，不允许此操作")
			}
		}
	}
	if err == nil && isSecurityStopTask(task) && !config.AppConfig.SecurityAutoShutdown {
		skipped = true
	}
	if err == nil && !skipped {
		switch task.Type {
		case TaskStart:
			err = startByRuntime(task.ContainerID)
		case TaskStop:
			err = stopByRuntime(task.ContainerID)
		case TaskRestart:
			err = restartByRuntime(task.ContainerID)
		case TaskDelete:
			err = destroyByRuntime(task.ContainerID)
			if err == nil {
				time.Sleep(time.Second)
				if config.FindContainer(task.ContainerID) != nil {
					err = fmt.Errorf("container still exists after delete: %d", task.ContainerID)
				}
			}
		case TaskReinstall:
			if lxc.HasSSHAuthOptions(task.Config) {
				err = reinstallByRuntime(task.ContainerID, task.TemplateID, task.Config)
			} else {
				err = reinstallByRuntime(task.ContainerID, task.TemplateID)
			}
		}
	}

	auditUser := task.User
	if auditUser == "" {
		auditUser = "admin"
	}
	if err != nil {
		config.AddAuditLogFull(string(task.Type), task.ContainerName, "失败: "+err.Error(), auditUser, task.IP, task.UserAgent, false, err.Error())
		q.finishTask(task, "failed", err)
		return
	}
	if skipped {
		config.AddAuditLogFull(string(task.Type), task.ContainerName, "跳过: 安全告警自动关机已关闭", auditUser, task.IP, task.UserAgent, true, "")
		q.finishTask(task, "done", nil)
		return
	}

	config.AddAuditLogFull(string(task.Type), task.ContainerName, "成功", auditUser, task.IP, task.UserAgent, true, "")
	switch task.Type {
	case TaskStart:
		config.UpdateContainerStatus(task.ContainerID, "running")
		clearPolicyBlockAfterAdminRecovery(task)
	case TaskStop:
		config.UpdateContainerStatus(task.ContainerID, "stopped")
	case TaskRestart:
		config.UpdateContainerStatus(task.ContainerID, "running")
		clearPolicyBlockAfterAdminRecovery(task)
	case TaskReinstall:
		clearPolicyBlockAfterAdminRecovery(task)
	}
	q.finishTask(task, "done", nil)
}

func isSecurityStopTask(task *Task) bool {
	return task != nil && task.Type == TaskStop && task.User == "system:security"
}

func clearPolicyBlockAfterAdminRecovery(task *Task) {
	if task == nil || strings.HasPrefix(task.User, "user:") || task.User == "system:security" {
		return
	}
	c := config.FindContainer(task.ContainerID)
	if c != nil && c.PolicyBlocked {
		config.SetContainerPolicyBlock(c.ID, false, "")
		config.AddAuditLog("security_policy_unblock", c.Name, "管理员操作后解除策略临时封禁", task.User)
	}
}

func resolveTaskContainer(task *Task) error {
	if task.Type == TaskCreate {
		return nil
	}
	if task.ContainerID > 0 {
		if c := config.FindContainer(task.ContainerID); c != nil {
			if task.ContainerName == "" {
				task.ContainerName = c.Name
			}
			return nil
		}
	}
	if task.ContainerName != "" {
		if c := config.FindContainerByName(task.ContainerName); c != nil {
			task.ContainerID = c.ID
			task.ContainerName = c.Name
			return nil
		}
		return fmt.Errorf("container not found: %s", task.ContainerName)
	}
	return fmt.Errorf("container not found: %d", task.ContainerID)
}

func (q *TaskQueue) persistTasks() {
	saved := make([]config.SavedTask, 0)
	for _, t := range q.tasks {
		// Only persist pending and running tasks to avoid
		// re-queuing already completed/failed tasks after restart.
		if t.Status != "pending" && t.Status != "running" {
			continue
		}
		cfgJSON, _ := json.Marshal(t.Config)
		saved = append(saved, config.SavedTask{
			ID:            t.ID,
			Type:          string(t.Type),
			ContainerID:   t.ContainerID,
			ContainerName: t.ContainerName,
			Status:        t.Status,
			Error:         t.Error,
			CreatedAt:     t.CreatedAt,
			TemplateID:    t.TemplateID,
			Config:        string(cfgJSON),
			User:          t.User,
			IP:            t.IP,
			UserAgent:     t.UserAgent,
		})
	}
	config.SaveTasks(saved)
}

func (q *TaskQueue) GetTasks() []*Task {
	q.mu.Lock()
	defer q.mu.Unlock()
	result := make([]*Task, 0, len(q.tasks))
	// Collect all task IDs, sort by creation time (extracted from ID number)
	for _, t := range q.tasks {
		copyTask := *t
		result = append(result, &copyTask)
	}
	// Stable sort by ID number (task-N where N is sequential)
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if parseIDNum(result[i].ID) > parseIDNum(result[j].ID) {
				result[i], result[j] = result[j], result[i]
			}
		}
	}
	return result
}

// HandleSingleTaskAction creates a task for a single container action
func HandleSingleTaskAction(w http.ResponseWriter, r *http.Request, id int, action string) {
	c := config.FindContainer(id)
	name := ""
	if c != nil {
		name = c.Name
	}

	// Determine user from authenticated request context.
	user := requestActor(r)
	ip := clientIP(r)
	userAgent := r.Header.Get("User-Agent")

	var taskType TaskType
	var templateID string
	var taskConfig *lxc.ContainerConfig
	switch action {
	case "start":
		taskType = TaskStart
	case "stop":
		taskType = TaskStop
	case "restart":
		taskType = TaskRestart
	case "delete":
		taskType = TaskDelete
	case "reinstall":
		var req struct {
			TemplateID   string `json:"template_id"`
			SSHAuthMode  string `json:"ssh_auth_mode,omitempty"`
			SSHPassword  string `json:"ssh_password,omitempty"`
			SSHPublicKey string `json:"ssh_public_key,omitempty"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		templateID = req.TemplateID
		if templateID == "" {
			c := config.FindContainer(id)
			if c != nil {
				templateID = c.Template
			}
		}
		runtime := runtimeFromTemplateID(templateID)
		if c := config.FindContainer(id); c != nil {
			runtime = c.Runtime()
		}
		if !isTemplateAllowedForRequest(r, c, templateID) {
			jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "Template is not allowed for this user"})
			return
		}
		if !isTemplateAvailableForRequest(r, c, templateID, runtime) {
			jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "Template is not enabled or downloaded"})
			return
		}
		authCfg := lxc.ContainerConfig{
			TemplateID:   templateID,
			SSHAuthMode:  req.SSHAuthMode,
			SSHPassword:  req.SSHPassword,
			SSHPublicKey: req.SSHPublicKey,
		}
		if lxc.HasSSHAuthOptions(authCfg) {
			if err := validateReinstallSSHAuth(c, templateID, authCfg); err != nil {
				jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
				return
			}
			taskConfig = &authCfg
		}
		taskType = TaskReinstall
	default:
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Unknown action"})
		return
	}

	ids := globalQueue.EnqueueWithAudit(id, name, taskType, templateID, taskConfig, user, ip, userAgent)
	jsonResponse(w, http.StatusAccepted, APIResponse{
		Success: true,
		Message: "Task queued",
		Data:    map[string]interface{}{"task_id": ids[0], "container_name": name, "status": "pending"},
	})
}

// HandleBatchCreate handles batch container creation
func HandleBatchCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	if !requireScope(w, r, "container:create") {
		return
	}
	if isAccessRestrictedRequest(r) {
		jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "Container-bound API keys cannot create containers"})
		return
	}
	var req struct {
		Containers []lxc.ContainerConfig `json:"containers"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
		return
	}
	if len(req.Containers) == 0 {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "No containers requested"})
		return
	}

	activeCreateNames := globalQueue.ActiveCreateNames()
	requestNames := make(map[string]bool)
	requestNATPorts := make(map[string]string)
	for i := range req.Containers {
		name := strings.TrimSpace(req.Containers[i].Name)
		req.Containers[i].Name = name
		if !config.IsValidContainerNameSyntax(name) {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid container name: " + name})
			return
		}
		if requestNames[name] {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Duplicate container name in request: " + name})
			return
		}
		if config.FindContainerByName(name) != nil {
			jsonResponse(w, http.StatusConflict, APIResponse{Success: false, Message: "Container name already exists: " + name})
			return
		}
		if activeCreateNames[name] {
			jsonResponse(w, http.StatusConflict, APIResponse{Success: false, Message: "Container creation already queued: " + name})
			return
		}
		if req.Containers[i].VCPU <= 0 {
			req.Containers[i].VCPU = 1
		}
		if err := rejectNegativeCreateLimits(req.Containers[i]); err != nil {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: name + ": " + err.Error()})
			return
		}
		req.Containers[i].NormalizeResourceAliases()
		req.Containers[i].Virtualization = runtimeFromRequest(req.Containers[i].Virtualization)
		if req.Containers[i].WantsLANIPv4() && req.Containers[i].Virtualization != config.VirtualizationLXC {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: name + ": LAN IPv4 is only supported for LXC containers"})
			return
		}
		if req.Containers[i].RAMMB < 128 {
			req.Containers[i].RAMMB = 512
		}
		if req.Containers[i].DiskGB < 1 {
			req.Containers[i].DiskGB = 5
		}
		if err := validateCreateStoragePool(&req.Containers[i]); err != nil {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: name + ": " + err.Error()})
			return
		}
		if !isImageEnabledAndDownloaded(req.Containers[i].TemplateID, req.Containers[i].Virtualization) {
			jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: name + ": template is not enabled or downloaded"})
			return
		}
		if ids, err := normalizeAllowedImageIDs(req.Containers[i].AllowedImageIDs); err != nil {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: name + ": " + err.Error()})
			return
		} else {
			req.Containers[i].AllowedImageIDs = ids
		}
		if req.Containers[i].PortMappingCount < 0 {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: name + ": port mapping count cannot be negative"})
			return
		}
		if err := req.Containers[i].NormalizeCreateNATMappings(); err != nil {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: name + ": " + err.Error()})
			return
		}
		if err := lxc.ValidateCreateNATPortAvailability(req.Containers[i]); err != nil {
			jsonResponse(w, http.StatusConflict, APIResponse{Success: false, Message: name + ": " + err.Error()})
			return
		}
		if req.Containers[i].ManagementPort > 0 {
			key := fmt.Sprintf("%d/tcp", req.Containers[i].ManagementPort)
			if owner := requestNATPorts[key]; owner != "" {
				jsonResponse(w, http.StatusConflict, APIResponse{Success: false, Message: fmt.Sprintf("%s: NAT management port %s is also requested by %s", name, key, owner)})
				return
			}
			requestNATPorts[key] = name
		}
		for _, mapping := range req.Containers[i].NATPortMappings {
			key := fmt.Sprintf("%d/%s", mapping.HostPort, mapping.Protocol)
			if owner := requestNATPorts[key]; owner != "" {
				jsonResponse(w, http.StatusConflict, APIResponse{Success: false, Message: fmt.Sprintf("%s: NAT host port %s is also requested by %s", name, key, owner)})
				return
			}
			requestNATPorts[key] = name
		}
		if req.Containers[i].PortMappingCount > 64 {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: name + ": port mapping count cannot exceed 64"})
			return
		}
		if req.Containers[i].IPv4Count < 0 || req.Containers[i].IPv6Count < 0 {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: name + ": IP address count cannot be negative"})
			return
		}
		if req.Containers[i].IPv4Count > 64 || req.Containers[i].IPv6Count > 64 {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: name + ": IP address count cannot exceed 64"})
			return
		}
		if !req.Containers[i].AssignIPv4 && len(req.Containers[i].PublicIPv4s) == 0 {
			req.Containers[i].IPv4Count = 0
		}
		if !req.Containers[i].AssignIPv6 && len(req.Containers[i].IPv6Addresses) == 0 {
			req.Containers[i].IPv6Count = 0
		}
		if !hasRequestedNetwork(req.Containers[i]) {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: name + ": " + noNetworkSelectedMessage})
			return
		}
		if req.Containers[i].SnapshotLimit <= 0 {
			req.Containers[i].SnapshotLimit = config.DefaultSnapshotLimit
		}
		if err := validateRuntimeResourceRequest(req.Containers[i].Virtualization, req.Containers[i].VCPU, req.Containers[i].RAMMB, req.Containers[i].DiskGB); err != nil {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: name + ": " + err.Error()})
			return
		}
		if err := validateCreateSSHAuth(req.Containers[i]); err != nil {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: name + ": " + err.Error()})
			return
		}
		requestNames[name] = true
	}
	planned, err := lxc.ReserveBatchCreateNATPorts(req.Containers)
	if err != nil {
		jsonResponse(w, http.StatusConflict, APIResponse{Success: false, Message: err.Error()})
		return
	}
	ids := globalQueue.EnqueueBatchCreateWithAudit(planned, requestActor(r), clientIP(r), r.UserAgent())
	jsonResponse(w, http.StatusAccepted, APIResponse{Success: true, Data: ids})
}

// HandleBatchAction handles batch container actions
func HandleBatchAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	if !hasAnyScope(r, "container:power", "container:delete", "container:reinstall") {
		jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "Insufficient API key scope"})
		return
	}
	var req struct {
		Action       string `json:"action"`
		Containers   []int  `json:"containers"`
		TemplateID   string `json:"template_id,omitempty"`
		SSHAuthMode  string `json:"ssh_auth_mode,omitempty"`
		SSHPassword  string `json:"ssh_password,omitempty"`
		SSHPublicKey string `json:"ssh_public_key,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
		return
	}

	var taskType TaskType
	var requiredScope string
	var taskConfig *lxc.ContainerConfig
	switch req.Action {
	case "start":
		taskType = TaskStart
		requiredScope = "container:power"
	case "stop":
		taskType = TaskStop
		requiredScope = "container:power"
	case "restart":
		taskType = TaskRestart
		requiredScope = "container:power"
	case "delete":
		taskType = TaskDelete
		requiredScope = "container:delete"
	case "reinstall":
		if req.TemplateID == "" {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "template_id required"})
			return
		}
		if !isTemplateEnabledAndDownloaded(req.TemplateID) {
			jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "Template is not enabled or downloaded"})
			return
		}
		authCfg := lxc.ContainerConfig{
			TemplateID:   req.TemplateID,
			SSHAuthMode:  req.SSHAuthMode,
			SSHPassword:  req.SSHPassword,
			SSHPublicKey: req.SSHPublicKey,
		}
		if lxc.HasSSHAuthOptions(authCfg) {
			taskConfig = &authCfg
		}
		taskType = TaskReinstall
		requiredScope = "container:reinstall"
	default:
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Unknown action"})
		return
	}
	if !requireScope(w, r, requiredScope) {
		return
	}
	for _, id := range req.Containers {
		c := config.FindContainer(id)
		if c == nil || !isContainerAllowedForRequest(r, c.UUID) {
			jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "Access denied to one or more containers"})
			return
		}
		if taskType == TaskReinstall && !isTemplateAllowedForRequest(r, c, req.TemplateID) {
			jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: c.Name + ": template is not allowed for this user"})
			return
		}
		if taskConfig != nil {
			if err := validateReinstallSSHAuth(c, req.TemplateID, *taskConfig); err != nil {
				jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: c.Name + ": " + err.Error()})
				return
			}
		}
	}

	var ids []string
	if taskConfig != nil {
		for _, id := range req.Containers {
			c := config.FindContainer(id)
			name := ""
			if c != nil {
				name = c.Name
			}
			queued := globalQueue.EnqueueWithAudit(id, name, taskType, req.TemplateID, taskConfig, requestActor(r), clientIP(r), r.UserAgent())
			ids = append(ids, queued...)
		}
	} else {
		ids = globalQueue.EnqueueBatchWithAudit(taskType, req.Containers, req.TemplateID, requestActor(r), clientIP(r), r.UserAgent())
	}
	jsonResponse(w, http.StatusAccepted, APIResponse{Success: true, Data: ids})
}

// HandleTaskDelete deletes a specific task by ID
func HandleTaskDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	if !requireScope(w, r, "task:delete") {
		return
	}
	// URL: /api/tasks/{id} or /api/v1/tasks/{id}
	taskID := strings.TrimPrefix(r.URL.Path, "/api/v1/tasks/")
	taskID = strings.TrimPrefix(taskID, "/api/tasks/")
	if taskID == "" {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Task ID required"})
		return
	}
	globalQueue.mu.Lock()
	task := globalQueue.tasks[taskID]
	if task != nil && !isTaskAllowedForRequest(r, task) {
		globalQueue.mu.Unlock()
		jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "Access denied to this task"})
		return
	}
	delete(globalQueue.tasks, taskID)
	// Also remove from both queues if pending
	newCreate := make([]*Task, 0, len(globalQueue.createQueue))
	for _, t := range globalQueue.createQueue {
		if t.ID != taskID {
			newCreate = append(newCreate, t)
		}
	}
	globalQueue.createQueue = newCreate
	newOp := make([]*Task, 0, len(globalQueue.opQueue))
	for _, t := range globalQueue.opQueue {
		if t.ID != taskID {
			newOp = append(newOp, t)
		}
	}
	globalQueue.opQueue = newOp
	globalQueue.persistTasks()
	globalQueue.mu.Unlock()
	if task != nil && task.Type == TaskCreate {
		lxc.ReleaseQueuedCreateNATPorts(task.Config.Name)
	}
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "Task deleted"})
}

// HandleTasks returns the current task queue
func HandleTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	if !requireScope(w, r, "task:read") {
		return
	}
	tasks := globalQueue.GetTasks()
	tasks = filterTasksForRequest(r, tasks)
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: tasks})
}

// RestoreTasks restores task queue from config
func RestoreTasks() {
	globalQueue.mu.Lock()
	defer globalQueue.mu.Unlock()
	for _, st := range config.AppConfig.Tasks {
		if st.Type == string(TaskStop) && st.User == "system:security" && !config.AppConfig.SecurityAutoShutdown {
			continue
		}
		var cfg lxc.ContainerConfig
		if st.Config != "" {
			json.Unmarshal([]byte(st.Config), &cfg)
		}
		containerName := st.ContainerName
		if containerName == "" {
			containerName = cfg.Name
		}
		if cfg.Name == "" {
			cfg.Name = containerName
		}
		cfg.NormalizeResourceAliases()
		containerID := st.ContainerID
		if containerID <= 0 && containerName != "" {
			if c := config.FindContainerByName(containerName); c != nil {
				containerID = c.ID
			}
		}
		globalQueue.tasks[st.ID] = &Task{
			ID:            st.ID,
			Type:          TaskType(st.Type),
			ContainerID:   containerID,
			ContainerName: containerName,
			Status:        st.Status,
			Error:         st.Error,
			Stage:         "queued",
			StageDetail:   "排队等待",
			CreatedAt:     st.CreatedAt,
			TemplateID:    st.TemplateID,
			Config:        cfg,
			User:          st.User,
			IP:            st.IP,
			UserAgent:     st.UserAgent,
		}
		if st.Status == "pending" || st.Status == "running" {
			// Reset running tasks back to pending so they get retried
			globalQueue.tasks[st.ID].Status = "pending"
			globalQueue.enqueueTask(globalQueue.tasks[st.ID])
		}
		if num := parseIDNum(st.ID); num >= globalQueue.nextID {
			globalQueue.nextID = num + 1
		}
	}
	// Clear persisted tasks from disk (they're now in memory)
	config.SaveTasks([]config.SavedTask{})
}

func parseIDNum(id string) int {
	var num int
	for _, c := range id {
		if c >= '0' && c <= '9' {
			num = num*10 + int(c-'0')
		}
	}
	return num
}

func validateCreateStoragePool(cfg *lxc.ContainerConfig) error {
	required := config.StorageContentLXC
	if cfg.Virtualization == config.VirtualizationKVM {
		required = config.StorageContentKVM
	}
	requiredBytes := int64(cfg.DiskGB) * 1024 * 1024 * 1024
	pool, err := config.SelectStoragePoolForContent(required, cfg.StoragePoolID, requiredBytes)
	if err != nil {
		return err
	}
	cfg.StoragePoolID = pool.ID
	return nil
}
