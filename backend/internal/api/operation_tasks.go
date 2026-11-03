package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"clicd/internal/config"
)

type OperationTaskHandler func(context.Context, int, json.RawMessage, func(stage, detail string)) error

var operationTasks = struct {
	sync.RWMutex
	handlers map[TaskType]OperationTaskHandler
}{handlers: make(map[TaskType]OperationTaskHandler)}

func builtInTask(kind TaskType) bool {
	switch kind {
	case TaskCreate, TaskStart, TaskStop, TaskRestart, TaskDelete, TaskReinstall:
		return true
	}
	return false
}

// RegisterOperationTask is intended for package initialization. Operations share
// the same concurrency budget and instance locks as built-in lifecycle tasks.
func RegisterOperationTask(kind TaskType, handler OperationTaskHandler) {
	if strings.TrimSpace(string(kind)) == "" || builtInTask(kind) || handler == nil {
		panic("invalid operation task registration: " + string(kind))
	}
	operationTasks.Lock()
	defer operationTasks.Unlock()
	if operationTasks.handlers[kind] != nil {
		panic("duplicate operation task registration: " + string(kind))
	}
	operationTasks.handlers[kind] = handler
}

func operationHandler(kind TaskType) OperationTaskHandler {
	operationTasks.RLock()
	defer operationTasks.RUnlock()
	return operationTasks.handlers[kind]
}

func EnqueueOperationWithAudit(id int, name string, kind TaskType, payload json.RawMessage, user, ip, ua string, additionalTargets ...string) (string, error) {
	return globalQueue.EnqueueOperationWithAudit(id, name, kind, payload, user, ip, ua, additionalTargets...)
}

func (q *TaskQueue) EnqueueOperationWithAudit(id int, name string, kind TaskType, payload json.RawMessage, user, ip, ua string, additionalTargets ...string) (string, error) {
	if operationHandler(kind) == nil || builtInTask(kind) {
		return "", fmt.Errorf("unsupported operation task: %s", kind)
	}
	if !json.Valid(payload) || len(payload) > 64*1024 {
		return "", fmt.Errorf("operation payload must be valid JSON of at most 64 KiB")
	}
	if id <= 0 && strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("operation target is required")
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	sequence, err := config.ReserveTaskID(q.nextID)
	if err != nil && err != config.ErrHistoryStoreUnavailable {
		return "", fmt.Errorf("reserve task ID: %w", err)
	}
	if err == config.ErrHistoryStoreUnavailable {
		sequence = q.nextID
	}
	q.nextID = sequence + 1
	task := &Task{ID: fmt.Sprintf("task-%d", sequence), Type: kind, ContainerID: id, ContainerName: name,
		Status: "pending", Stage: "queued", StageDetail: "排队等待", CreatedAt: time.Now().Format(time.RFC3339Nano),
		User: user, IP: ip, UserAgent: ua, Payload: append(json.RawMessage(nil), payload...), AdditionalTargets: append([]string(nil), additionalTargets...)}
	q.enqueueTask(task)
	if err := q.persistTasks(); err != nil && err != config.ErrHistoryStoreUnavailable {
		delete(q.tasks, task.ID)
		for i, queued := range q.opQueue {
			if queued == task {
				q.opQueue = append(q.opQueue[:i], q.opQueue[i+1:]...)
				break
			}
		}
		return "", fmt.Errorf("persist operation task: %w", err)
	}
	return task.ID, nil
}

type operationResultKey struct{}

// SetOperationTaskResult publishes safe result metadata, such as a new artifact
// or instance ID. Payloads and internal execution contexts are not API results.
func SetOperationTaskResult(ctx context.Context, result interface{}) error {
	set, ok := ctx.Value(operationResultKey{}).(func(json.RawMessage) error)
	if !ok {
		return fmt.Errorf("not running in an operation task")
	}
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	if len(data) > 64*1024 {
		return fmt.Errorf("task result exceeds 64 KiB")
	}
	return set(data)
}
