package api

import (
	"errors"
	"fmt"
	"strings"

	"clicd/internal/config"
)

var ErrContainerBusy = errors.New("container has an active or queued operation")

// WithIdleContainer reserves the same target key used by the dispatchers.
// The callback runs without q.mu: KVM operations can report task progress while
// holding their media lock, so holding both locks here would invert that order.
// New operations can be queued, but cannot start until the callback finishes.
func (q *TaskQueue) WithIdleContainer(id int, callback func() error) error {
	if callback == nil {
		return fmt.Errorf("operation callback is required")
	}
	if q == nil {
		return callback()
	}
	q.mu.Lock()
	target := &Task{ContainerID: id}
	if config.AppConfig != nil {
		if c := config.FindContainer(id); c != nil {
			target.ContainerName = c.Name
		}
	}
	key := taskConcurrencyKey(target)
	matches := func(task *Task) bool {
		return task != nil && (task.ContainerID == id ||
			(target.ContainerName != "" && (strings.EqualFold(strings.TrimSpace(task.ContainerName), target.ContainerName) ||
				strings.EqualFold(strings.TrimSpace(task.Config.Name), target.ContainerName))))
	}
	busy := q.activeTargets[key]
	for _, task := range q.tasks {
		if matches(task) && (task.Status == "pending" || task.Status == "running") {
			busy = true
		}
	}
	for task := range q.runningTasks {
		if matches(task) {
			busy = true
		}
	}
	if busy {
		q.mu.Unlock()
		return ErrContainerBusy
	}
	if q.activeTargets == nil {
		q.activeTargets = make(map[string]bool)
	}
	q.activeTargets[key] = true
	q.mu.Unlock()
	defer func() {
		q.mu.Lock()
		delete(q.activeTargets, key)
		q.signalDispatchers()
		q.mu.Unlock()
	}()
	return callback()
}
