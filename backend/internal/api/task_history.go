package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"clicd/internal/config"
	"clicd/internal/lxc"
)

const maxTaskEvents = 200

type TaskEvent struct {
	Time   string `json:"time"`
	Stage  string `json:"stage"`
	Detail string `json:"detail"`
}

type storedTask struct {
	Task              *Task           `json:"task"`
	Payload           json.RawMessage `json:"payload,omitempty"`
	AdditionalTargets []string        `json:"additional_targets,omitempty"`
}

func taskTime(value string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05"} {
		if parsed, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func addTaskEvent(task *Task, stage, detail string) {
	if len(stage) > 128 {
		stage = stage[:128]
	}
	if len(detail) > 4096 {
		detail = detail[:4096]
	}
	task.Stage, task.StageDetail = stage, detail
	if len(task.Logs) == 0 || task.Logs[len(task.Logs)-1].Stage != stage || task.Logs[len(task.Logs)-1].Detail != detail {
		task.Logs = append(task.Logs, TaskEvent{Time: time.Now().Format(time.RFC3339Nano), Stage: stage, Detail: detail})
		if len(task.Logs) > maxTaskEvents {
			task.Logs = append([]TaskEvent(nil), task.Logs[len(task.Logs)-maxTaskEvents:]...)
		}
	}
	task.revision++
}

func taskConcurrencyKeys(task *Task) []string {
	keys := []string{taskConcurrencyKey(task)}
	for _, name := range task.AdditionalTargets {
		key := "name:" + strings.ToLower(strings.TrimSpace(name))
		if key == "name:" {
			continue
		}
		found := false
		for _, existing := range keys {
			found = found || existing == key
		}
		if !found {
			keys = append(keys, key)
		}
	}
	return keys
}

func taskTargetsActive(task *Task, active map[string]bool) bool {
	for _, key := range taskConcurrencyKeys(task) {
		if active[key] {
			return true
		}
	}
	return false
}

func (q *TaskQueue) allocateTaskID() int {
	id, err := config.ReserveTaskID(q.nextID)
	if err != nil {
		id = q.nextID
		if err != config.ErrHistoryStoreUnavailable {
			fmt.Printf("Task sequence persistence failed: %v\n", err)
		}
	}
	q.nextID = id + 1
	return id
}

func (q *TaskQueue) pruneTaskMemory() {
	var completed []*Task
	cutoff := time.Now().Add(-config.TaskHistoryRetention)
	for id, task := range q.tasks {
		if task.Status != "done" && task.Status != "failed" {
			continue
		}
		finished := taskTime(task.FinishedAt)
		if finished.IsZero() {
			finished = time.Now()
			task.FinishedAt = finished.Format(time.RFC3339Nano)
			task.revision++
		}
		if !finished.IsZero() && finished.Before(cutoff) {
			delete(q.tasks, id)
			delete(q.historySaved, id)
		} else {
			completed = append(completed, task)
		}
	}
	sort.Slice(completed, func(i, j int) bool { return parseIDNum(completed[i].ID) > parseIDNum(completed[j].ID) })
	for _, task := range completed[min(len(completed), config.TaskHistoryLimit):] {
		delete(q.tasks, task.ID)
		delete(q.historySaved, task.ID)
	}
}

func (q *TaskQueue) persistTaskState() error {
	return q.persistTaskStateWithLegacy(true)
}

func (q *TaskQueue) persistTaskHistoryOnly() error {
	return q.persistTaskStateWithLegacy(false)
}

func (q *TaskQueue) persistTaskStateWithLegacy(saveLegacy bool) error {
	if q.historySaved == nil {
		q.historySaved = make(map[string]uint64)
	}
	var records []config.TaskHistoryRecord
	var changed []*Task
	var pending []config.SavedTask
	for _, task := range q.tasks {
		// Legacy terminal rows may lack timestamps. Give them a durable finish
		// marker before serialization so SQL retention cannot treat them as active.
		if (task.Status == "done" || task.Status == "failed") && taskTime(task.FinishedAt).IsZero() {
			task.FinishedAt = time.Now().Format(time.RFC3339Nano)
			task.revision++
		}
		if previous, ok := q.historySaved[task.ID]; !ok || previous != task.revision {
			data, err := json.Marshal(storedTask{Task: task, Payload: task.Payload, AdditionalTargets: task.AdditionalTargets})
			if err != nil {
				return err
			}
			finished := int64(0)
			if parsed := taskTime(task.FinishedAt); !parsed.IsZero() {
				finished = parsed.UnixMilli()
			}
			records = append(records, config.TaskHistoryRecord{ID: task.ID, Sequence: parseIDNum(task.ID), FinishedAt: finished, Data: data})
			changed = append(changed, task)
		}
		if task.Status == "pending" || task.Status == "running" {
			cfgJSON, _ := json.Marshal(task.Config)
			pending = append(pending, config.SavedTask{ID: task.ID, Type: string(task.Type), ContainerID: task.ContainerID, ContainerName: task.ContainerName,
				Status: task.Status, Error: task.Error, CreatedAt: task.CreatedAt, TemplateID: task.TemplateID, Config: string(cfgJSON), User: task.User, IP: task.IP, UserAgent: task.UserAgent})
		}
	}
	err := config.SaveTaskHistory(records, q.nextID)
	if err == nil {
		for _, task := range changed {
			q.historySaved[task.ID] = task.revision
		}
		// Persist terminal transitions before pruning memory. Otherwise a very
		// old operation finishing at the limit could remain "running" on disk.
		q.pruneTaskMemory()
	} else if err != config.ErrHistoryStoreUnavailable {
		fmt.Printf("Task history persistence failed: %v\n", err)
	}
	if saveLegacy && config.AppConfig != nil {
		config.SaveTasks(pending)
	}
	return err
}

func (q *TaskQueue) restoreTaskState() {
	q.mu.Lock()
	defer q.mu.Unlock()
	restored := make(map[string]*Task)
	for _, saved := range config.AppConfig.Tasks {
		var cfg lxc.ContainerConfig
		_ = json.Unmarshal([]byte(saved.Config), &cfg)
		if saved.ContainerName == "" {
			saved.ContainerName = cfg.Name
		}
		if cfg.Name == "" {
			cfg.Name = saved.ContainerName
		}
		cfg.NormalizeResourceAliases()
		restored[saved.ID] = &Task{ID: saved.ID, Type: TaskType(saved.Type), ContainerID: saved.ContainerID, ContainerName: saved.ContainerName,
			Status: saved.Status, Error: saved.Error, CreatedAt: saved.CreatedAt, TemplateID: saved.TemplateID, Config: cfg, User: saved.User, IP: saved.IP, UserAgent: saved.UserAgent}
	}
	records, next, err := config.LoadTaskHistory()
	if err != nil && err != config.ErrHistoryStoreUnavailable {
		fmt.Printf("Load task history failed: %v\n", err)
	}
	if next > q.nextID {
		q.nextID = next
	}
	for _, record := range records {
		var stored storedTask
		if json.Unmarshal(record.Data, &stored) == nil && stored.Task != nil {
			stored.Task.Payload = stored.Payload
			stored.Task.AdditionalTargets = stored.AdditionalTargets
			restored[record.ID] = stored.Task
		}
	}
	var ordered []*Task
	for _, task := range restored {
		if sequence := parseIDNum(task.ID); sequence >= q.nextID {
			q.nextID = sequence + 1
		}
		ordered = append(ordered, task)
	}
	sort.Slice(ordered, func(i, j int) bool { return parseIDNum(ordered[i].ID) < parseIDNum(ordered[j].ID) })
	for _, task := range ordered {
		if task.Type == TaskStop && task.User == "system:security" {
			continue
		}
		q.tasks[task.ID] = task
		if task.Status != "pending" && task.Status != "running" {
			if task.FinishedAt == "" {
				task.FinishedAt = task.CreatedAt
			}
			continue
		}
		if !builtInTask(task.Type) && (task.Status == "running" || operationHandler(task.Type) == nil || !json.Valid(task.Payload)) {
			task.Status = "failed"
			task.Error = "operation interrupted by service restart or its handler is unavailable; inspect state before retrying"
			task.FinishedAt = time.Now().Format(time.RFC3339Nano)
			addTaskEvent(task, "interrupted", task.Error)
			continue
		}
		task.Status = "pending"
		task.StartedAt = ""
		task.FinishedAt = ""
		task.DurationMS = 0
		if task.ContainerID <= 0 && task.Type != TaskCreate && task.ContainerName != "" {
			if c := config.FindContainerByName(task.ContainerName); c != nil {
				task.ContainerID = c.ID
			}
		}
		addTaskEvent(task, "queued", "服务重启后恢复排队")
		q.enqueueTask(task)
	}
	q.persistTasks()
	// The queue now owns recovery. Keep the compatibility field empty until its
	// next transition, matching the old RestoreTasks contract.
	config.SaveTasks([]config.SavedTask{})
}

func isHistoricalTaskAllowed(r *http.Request, task *Task) bool {
	allowed, restricted := requestAllowedContainers(r)
	if !restricted {
		return true
	}
	if task == nil {
		return false
	}
	if task.ContainerUUID != "" {
		return allowed.uuids[task.ContainerUUID]
	}
	// Legacy records have no immutable owner identity. Only live matching
	// containers can be authorized; never grant old history by name alone.
	if task.Status != "pending" && task.Status != "running" {
		return false
	}
	return isTaskAllowedForRequest(r, task)
}
