package api

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
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
	ID                string              `json:"id"`
	Type              TaskType            `json:"type"`
	ContainerID       int                 `json:"container_id"`
	ContainerName     string              `json:"container_name"`
	ContainerUUID     string              `json:"container_uuid,omitempty"`
	Status            string              `json:"status"`
	Error             string              `json:"error,omitempty"`
	Stage             string              `json:"stage,omitempty"`
	StageDetail       string              `json:"stage_detail,omitempty"`
	CreatedAt         string              `json:"created_at"`
	StartedAt         string              `json:"started_at,omitempty"`
	FinishedAt        string              `json:"finished_at,omitempty"`
	DurationMS        int64               `json:"duration_ms,omitempty"`
	Logs              []TaskEvent         `json:"logs,omitempty"`
	Result            json.RawMessage     `json:"result,omitempty"`
	Payload           json.RawMessage     `json:"-"`
	AdditionalTargets []string            `json:"-"`
	TemplateID        string              `json:"template_id,omitempty"`
	Config            lxc.ContainerConfig `json:"config,omitempty"`
	Name              string              `json:"name,omitempty"`
	User              string              `json:"user,omitempty"` // who created this task
	IP                string              `json:"ip,omitempty"`
	UserAgent         string              `json:"user_agent,omitempty"`
	activeKey         string
	activeKeys        []string
	revision          uint64
	ctx               context.Context
	cancel            context.CancelFunc
	deleteRequested   bool
}

type taskRuntime struct {
	create  func(lxc.ContainerConfig) error
	start   func(context.Context, int) error
	destroy func(int) error
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
	runningTasks   map[*Task]bool
	stop           chan struct{}
	runtime        taskRuntime
	historySaved   map[string]uint64
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
		historySaved:   make(map[string]uint64),
		maxConcurrency: config.NormalizeTaskConcurrency(concurrency),
		activeTargets:  make(map[string]bool),
		runningTasks:   make(map[*Task]bool),
		stop:           make(chan struct{}),
		runtime: taskRuntime{
			create:  createByRuntime,
			start:   startByRuntimeContext,
			destroy: destroyByRuntime,
		},
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
	if task.ContainerID > 0 && config.AppConfig != nil {
		if c := config.FindContainer(task.ContainerID); c != nil {
			if task.ContainerUUID != "" && task.ContainerUUID != c.UUID {
				task.Status = "failed"
				task.Error = "operation target identity changed"
				task.FinishedAt = time.Now().Format(time.RFC3339Nano)
				addTaskEvent(task, "failed", task.Error)
				q.tasks[task.ID] = task
				return
			}
			task.ContainerName = c.Name
			task.ContainerUUID = c.UUID
		}
	}
	q.tasks[task.ID] = task
	addTaskEvent(task, task.Stage, task.StageDetail)
	if task.Type == TaskDelete {
		q.cancelTasksForDelete(task)
	} else {
		for _, existing := range q.tasks {
			if existing.Type == TaskDelete && (existing.Status == "pending" || existing.Status == "running") && sameTaskTarget(existing, task) {
				cancelPendingTaskForDelete(task)
				return
			}
		}
	}
	if task.Type == TaskCreate {
		q.createQueue = append(q.createQueue, task)
		q.createCond.Signal()
	} else {
		q.opQueue = append(q.opQueue, task)
		q.opCond.Signal()
	}
}

// Cancellation is requested while enqueueing, even when all dispatch slots are
// occupied. The active worker retains its target key until it has fully exited.
func (q *TaskQueue) cancelTasksForDelete(del *Task) {
	for task := range q.runningTasks {
		if !sameTaskTarget(task, del) {
			continue
		}
		if task.Type == TaskCreate || task.Type == TaskStart {
			task.deleteRequested = true
			if task.cancel != nil {
				task.cancel()
			}
		}
	}
	for _, task := range q.tasks {
		if task.Type != TaskDelete && task.Status == "pending" && sameTaskTarget(task, del) {
			cancelPendingTaskForDelete(task)
		}
	}
	q.createQueue = pendingTasks(q.createQueue)
	q.opQueue = pendingTasks(q.opQueue)
}

func cancelPendingTaskForDelete(task *Task) {
	task.Status = "failed"
	task.Error = "cancelled because container deletion was requested"
	task.Stage = "failed"
	task.StageDetail = "已取消，等待删除"
	task.FinishedAt = time.Now().Format(time.RFC3339Nano)
	addTaskEvent(task, task.Stage, task.Error)
	if task.Type == TaskCreate {
		name := task.Config.Name
		if name == "" {
			name = task.ContainerName
		}
		lxc.ReleaseQueuedCreateNATPorts(name)
	}
}

func sameTaskTarget(a, b *Task) bool {
	if a.ContainerID > 0 && b.ContainerID > 0 {
		return a.ContainerID == b.ContainerID
	}
	name := func(task *Task) string {
		if value := strings.TrimSpace(task.ContainerName); value != "" {
			return value
		}
		return strings.TrimSpace(task.Config.Name)
	}
	aName, bName := name(a), name(b)
	return aName != "" && aName == bName
}

func pendingTasks(tasks []*Task) []*Task {
	pending := tasks[:0]
	for _, task := range tasks {
		if task.Status == "pending" {
			pending = append(pending, task)
		}
	}
	return pending
}

func (q *TaskQueue) Enqueue(containerID int, containerName string, taskType TaskType, templateID string, cfg *lxc.ContainerConfig) []string {
	return q.EnqueueWithAudit(containerID, containerName, taskType, templateID, cfg, "admin", "", "")
}

func (q *TaskQueue) EnqueueWithAudit(containerID int, containerName string, taskType TaskType, templateID string, cfg *lxc.ContainerConfig, user string, ip string, userAgent string) []string {
	q.mu.Lock()
	defer q.mu.Unlock()

	id := q.allocateTaskID()
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
		id := q.allocateTaskID()
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
	id := q.allocateTaskID()
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

// The two dispatchers keep long-running creates from blocking power operations,
// while sharing one global concurrency budget.
func (q *TaskQueue) createDispatcher() {
	for {
		task := q.takeNextTask(true)
		if task == nil {
			return
		}
		go q.runCreateTask(task)
	}
}

func (q *TaskQueue) opDispatcher() {
	for {
		task := q.takeNextTask(false)
		if task == nil {
			return
		}
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
		select {
		case <-q.stop:
			return nil
		default:
		}
		queue := q.opQueue
		if create {
			queue = q.createQueue
		}
		if q.activeTasks < q.maxConcurrency {
			index := runnableTaskIndex(queue, q.activeTargets)
			deleteIndex := runnableDeleteTaskIndex(q.opQueue, q.activeTargets)
			if deleteIndex >= 0 {
				if create {
					index = -1
					q.opCond.Signal()
				} else {
					index = deleteIndex
				}
			}
			if index >= 0 {
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
				task.StartedAt = time.Now().Format(time.RFC3339Nano)
				addTaskEvent(task, task.Stage, task.StageDetail)
				task.activeKey = taskConcurrencyKey(task)
				task.activeKeys = taskConcurrencyKeys(task)
				task.ctx, task.cancel = context.WithCancel(context.Background())
				q.runningTasks[task] = true
				for _, key := range task.activeKeys {
					q.activeTargets[key] = true
				}
				q.activeTasks++
				q.persistTasks()
				return task
			}
		}
		cond.Wait()
	}
}

func runnableDeleteTaskIndex(queue []*Task, activeTargets map[string]bool) int {
	for index, task := range queue {
		if task.Type == TaskDelete && !taskTargetsActive(task, activeTargets) {
			return index
		}
	}
	return -1
}

func runnableTaskIndex(queue []*Task, activeTargets map[string]bool) int {
	for index, task := range queue {
		if !taskTargetsActive(task, activeTargets) {
			return index
		}
	}
	return -1
}

func taskConcurrencyKey(task *Task) string {
	if task == nil {
		return "task:nil"
	}
	// A delete cancels active startup work, then waits on the same key until
	// that worker has exited; resource removal must never race initialization.
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
	if task.cancel != nil {
		task.cancel()
		task.cancel = nil
	}
	task.Status = status
	task.FinishedAt = time.Now().Format(time.RFC3339Nano)
	if started := taskTime(task.StartedAt); !started.IsZero() {
		task.DurationMS = max(0, time.Since(started).Milliseconds())
	}
	if taskErr != nil {
		task.Error = taskErr.Error()
		if len(task.Error) > 8192 {
			task.Error = task.Error[:8192]
		}
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
	if status == "failed" {
		task.Stage = "failed"
		if taskErr != nil {
			task.StageDetail = taskErr.Error()
		}
	} else {
		task.Stage = "completed"
		task.StageDetail = "操作完成"
	}
	addTaskEvent(task, task.Stage, task.StageDetail)
	for _, key := range task.activeKeys {
		delete(q.activeTargets, key)
	}
	task.activeKeys = nil
	if task.activeKey != "" {
		delete(q.activeTargets, task.activeKey)
		task.activeKey = ""
	}
	delete(q.runningTasks, task)
	if q.activeTasks > 0 {
		q.activeTasks--
	}
	q.persistTasks()
	q.signalDispatchers()
	q.mu.Unlock()
}

func (q *TaskQueue) updateTaskStage(task *Task, stage, detail string) {
	q.mu.Lock()
	addTaskEvent(task, stage, detail)
	q.persistTaskHistoryOnly()
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
	ctx := task.ctx
	if ctx == nil {
		ctx = context.Background()
	}
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
	if err := ctx.Err(); err != nil {
		lxc.ReleaseQueuedCreateNATPorts(cfg.Name)
		q.finishTask(task, "failed", err)
		return
	}
	c := config.FindContainerByName(cfg.Name)
	if c == nil {
		if err := q.runtime.create(cfg); err != nil {
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

	// FindContainer returns a slice-backed pointer. Another container's removal
	// can shift that slice while startup waits, so never retain it for cleanup.
	containerID, containerName, containerUUID := c.ID, c.Name, c.UUID
	q.mu.Lock()
	task.ContainerID = containerID
	task.ContainerName = containerName
	task.ContainerUUID = containerUUID
	q.mu.Unlock()
	startDetail := "启动容器并等待网络就绪"
	if strings.EqualFold(cfg.Virtualization, config.VirtualizationKVM) {
		startDetail = "启动虚拟机并等待网络就绪"
	}
	q.updateTaskStage(task, "starting", startDetail)
	if err := q.runtime.start(ctx, containerID); err != nil {
		q.mu.Lock()
		deleting := task.deleteRequested
		q.mu.Unlock()
		if createdByTask && !deleting {
			_ = q.runtime.destroy(containerID)
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
	var err error
	if builtInTask(task.Type) || task.ContainerID > 0 {
		err = resolveTaskContainer(task)
	}
	ctx := task.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	q.mu.Unlock()
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
	if err == nil {
		switch task.Type {
		case TaskStart:
			err = q.runtime.start(ctx, task.ContainerID)
		case TaskStop:
			err = stopByRuntime(task.ContainerID)
		case TaskRestart:
			err = restartByRuntime(task.ContainerID)
		case TaskDelete:
			err = q.runtime.destroy(task.ContainerID)
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
		default:
			if handler := operationHandler(task.Type); handler != nil {
				ctx = context.WithValue(ctx, operationResultKey{}, func(result json.RawMessage) error {
					q.mu.Lock()
					defer q.mu.Unlock()
					task.Result = append(json.RawMessage(nil), result...)
					task.revision++
					return q.persistTaskHistoryOnly()
				})
				err = handler(ctx, task.ContainerID, task.Payload, func(stage, detail string) { q.updateTaskStage(task, stage, detail) })
			} else {
				err = fmt.Errorf("operation handler unavailable: %s", task.Type)
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
	config.AddAuditLogFull(string(task.Type), task.ContainerName, "成功", auditUser, task.IP, task.UserAgent, true, "")
	switch task.Type {
	case TaskStart:
		config.UpdateContainerStatus(task.ContainerID, "running")
	case TaskStop:
		config.UpdateContainerStatus(task.ContainerID, "stopped")
	case TaskRestart:
		config.UpdateContainerStatus(task.ContainerID, "running")
	}
	q.finishTask(task, "done", nil)
}

func resolveTaskContainer(task *Task) error {
	if task.Type == TaskCreate {
		return nil
	}
	if task.ContainerID > 0 {
		if c := config.FindContainer(task.ContainerID); c != nil {
			if task.ContainerUUID != "" && c.UUID != task.ContainerUUID {
				return fmt.Errorf("container identity changed: %d", task.ContainerID)
			}
			if task.ContainerName == "" {
				task.ContainerName = c.Name
			}
			return nil
		}
	}
	if task.ContainerUUID != "" {
		return fmt.Errorf("container no longer exists: %s", task.ContainerUUID)
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

func (q *TaskQueue) persistTasks() error {
	return q.persistTaskState()
}

func (q *TaskQueue) GetTasks() []*Task {
	q.mu.Lock()
	defer q.mu.Unlock()
	result := make([]*Task, 0, len(q.tasks))
	// Collect all task IDs, sort by creation time (extracted from ID number)
	for _, t := range q.tasks {
		result = append(result, snapshotTask(t))
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

// snapshotTask copies a task while the queue lock is held.
func snapshotTask(task *Task) *Task {
	snapshot := *task
	snapshot.Logs = append([]TaskEvent(nil), task.Logs...)
	snapshot.Result = append(json.RawMessage(nil), task.Result...)
	if start := taskTime(task.StartedAt); task.Status == "running" && !start.IsZero() {
		snapshot.DurationMS = max(0, time.Since(start).Milliseconds())
	}
	return &snapshot
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
		if strings.EqualFold(strings.TrimSpace(req.Containers[i].Virtualization), "podman") {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Message: "Use the Podman container endpoint for OCI applications"})
			return
		}
		req.Containers[i].Virtualization = runtimeFromRequest(req.Containers[i].Virtualization)
		if req.Containers[i].Virtualization != config.VirtualizationLXC && (req.Containers[i].Privileged || req.Containers[i].Nesting) {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: name + ": privileged and nesting options are only supported for LXC containers"})
			return
		}
		if req.Containers[i].Privileged && !isAdministratorRequest(r) {
			jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: name + ": privileged LXC containers require administrator permission"})
			return
		}
		if req.Containers[i].WantsLANIPv4() && req.Containers[i].Virtualization != config.VirtualizationLXC {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: name + ": LAN IPv4 is only supported for LXC containers"})
			return
		}
		if req.Containers[i].RAMMB < 128 {
			req.Containers[i].RAMMB = 512
		}
		if req.Containers[i].DiskGB <= 0 {
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
		if err := validateRuntimeResourceRequest(req.Containers[i].Virtualization, req.Containers[i].TemplateID, req.Containers[i].VCPU, req.Containers[i].RAMMB, req.Containers[i].DiskGB); err != nil {
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

// HandleTask reads or removes a task by its ID.
func HandleTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		HandleTaskDelete(w, r)
		return
	}
	if !requireScope(w, r, "task:read") {
		return
	}
	id := taskIDFromPath(r.URL.Path)
	globalQueue.mu.Lock()
	var snapshot *Task
	if task := globalQueue.tasks[id]; task != nil {
		snapshot = snapshotTask(task)
	}
	globalQueue.mu.Unlock()
	if snapshot == nil || !isHistoricalTaskAllowed(r, snapshot) {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Task not found"})
		return
	}
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: snapshot})
}

func taskIDFromPath(path string) string {
	path = strings.TrimPrefix(path, "/api/v1/tasks/")
	return strings.TrimPrefix(path, "/api/tasks/")
}

// HandleTaskDelete deletes a specific task by ID.
func HandleTaskDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	if !requireScope(w, r, "task:delete") {
		return
	}
	// URL: /api/tasks/{id} or /api/v1/tasks/{id}
	taskID := taskIDFromPath(r.URL.Path)
	if taskID == "" {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Task ID required"})
		return
	}
	globalQueue.mu.Lock()
	task := globalQueue.tasks[taskID]
	if task != nil && !isHistoricalTaskAllowed(r, task) {
		globalQueue.mu.Unlock()
		jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "Access denied to this task"})
		return
	}
	if task != nil && task.Status == "running" {
		globalQueue.mu.Unlock()
		jsonResponse(w, http.StatusConflict, APIResponse{Success: false, Message: "A running task cannot be removed"})
		return
	}
	if err := config.DeleteTaskHistory(taskID); err != nil && err != config.ErrHistoryStoreUnavailable {
		globalQueue.mu.Unlock()
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Failed to remove task history"})
		return
	}
	delete(globalQueue.tasks, taskID)
	delete(globalQueue.historySaved, taskID)
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
	filtered := make([]*Task, 0, len(tasks))
	for _, task := range tasks {
		if isHistoricalTaskAllowed(r, task) {
			filtered = append(filtered, task)
		}
	}
	tasks = filtered
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: tasks})
}

// RestoreTasks restores task queue from config
func RestoreTasks() {
	globalQueue.restoreTaskState()
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
	requiredBytes := int64(math.Round(cfg.DiskGB*1024)) * 1024 * 1024
	pool, err := config.SelectStoragePoolForContent(required, cfg.StoragePoolID, requiredBytes)
	if err != nil {
		return err
	}
	cfg.StoragePoolID = pool.ID
	return nil
}
