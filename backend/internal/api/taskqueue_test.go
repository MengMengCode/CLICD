package api

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"clicd/internal/config"
	"clicd/internal/lxc"
)

func TestRunnableTaskIndexSkipsActiveContainer(t *testing.T) {
	queue := []*Task{
		{ID: "task-1", Type: TaskStop, ContainerID: 1, ContainerName: "alpha"},
		{ID: "task-2", Type: TaskStart, ContainerID: 1, ContainerName: "alpha"},
		{ID: "task-3", Type: TaskStart, ContainerID: 2, ContainerName: "beta"},
	}
	active := map[string]bool{taskConcurrencyKey(queue[0]): true}

	if got := runnableTaskIndex(queue[1:], active); got != 1 {
		t.Fatalf("runnableTaskIndex() = %d, want 1 for the other container", got)
	}
}

func TestTaskConcurrencyKeyUsesContainerName(t *testing.T) {
	create := &Task{ID: "task-1", Type: TaskCreate, Config: lxcConfigWithName("Example")}
	for _, operationType := range []TaskType{TaskStart, TaskDelete, TaskRestart, TaskReinstall} {
		operation := &Task{ID: "task-2", Type: operationType, ContainerID: 9, ContainerName: "example"}
		if taskConcurrencyKey(create) != taskConcurrencyKey(operation) {
			t.Fatalf("same container received different concurrency keys: %q and %q", taskConcurrencyKey(create), taskConcurrencyKey(operation))
		}
	}
}

func TestTaskConcurrencyKeyDeleteWaitsForActiveCreate(t *testing.T) {
	create := &Task{ID: "task-1", Type: TaskCreate, Config: lxcConfigWithName("example")}
	del := &Task{ID: "task-2", Type: TaskDelete, ContainerID: 9, ContainerName: "example"}

	active := map[string]bool{taskConcurrencyKey(create): true}
	queue := []*Task{del}
	if got := runnableTaskIndex(queue, active); got != -1 {
		t.Fatalf("delete can race the active create (got index %d)", got)
	}
}

func TestTaskQueueDeleteCancelsStartupAtCapacityAndWaitsForExit(t *testing.T) {
	for _, activeType := range []TaskType{TaskCreate, TaskStart} {
		t.Run(string(activeType), func(t *testing.T) {
			setupTaskQueueConfig(t)
			if activeType == TaskStart {
				config.AppConfig.Containers = []config.Container{{ID: 9, Name: "example"}}
			}
			q := newTaskQueue(1)
			started := make(chan struct{})
			cancelled := make(chan struct{})
			release := make(chan struct{})
			exited := make(chan struct{})
			destroyed := make(chan struct{})
			var releaseOnce sync.Once
			var destroyCount atomic.Int32
			var overlap atomic.Bool
			q.runtime.create = func(cfg lxc.ContainerConfig) error {
				id := 9
				if cfg.Name == "other" {
					select {
					case <-destroyed:
					default:
						return errors.New("new create stole the delete's released dispatch slot")
					}
					id = 10
				}
				config.AddContainer(config.Container{ID: id, Name: cfg.Name})
				return nil
			}
			q.runtime.start = func(ctx context.Context, id int) error {
				if id != 9 {
					return nil
				}
				close(started)
				<-ctx.Done()
				close(cancelled)
				<-release
				close(exited)
				return ctx.Err()
			}
			q.runtime.destroy = func(id int) error {
				select {
				case <-exited:
				default:
					overlap.Store(true)
				}
				if id != 9 {
					return fmt.Errorf("destroying unexpected container %d", id)
				}
				config.RemoveContainer(id)
				if destroyCount.Add(1) == 1 {
					close(destroyed)
				}
				return nil
			}
			startTaskQueueDispatchers(t, q)
			t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
			var cfg *lxc.ContainerConfig
			id := 9
			if activeType == TaskCreate {
				cfg = &lxc.ContainerConfig{Name: "example"}
				id = 0
			}
			activeID := q.Enqueue(id, "example", activeType, "", cfg)[0]
			waitTaskQueueSignal(t, started, "startup")
			otherID := q.Enqueue(0, "other", TaskCreate, "", &lxc.ContainerConfig{Name: "other"})[0]
			deleteID := q.Enqueue(9, "example", TaskDelete, "", nil)[0]
			waitTaskQueueSignal(t, cancelled, "startup cancellation at full capacity")
			select {
			case <-destroyed:
				t.Fatal("delete ran before the cancelled worker exited")
			default:
			}
			if settings := q.Settings(); settings.Active != 1 || settings.Pending != 2 {
				t.Fatalf("worker released its slot before exiting: %+v", settings)
			}
			releaseOnce.Do(func() { close(release) })
			waitTaskQueueStatus(t, q, activeID, "failed")
			waitTaskQueueStatus(t, q, deleteID, "done")
			waitTaskQueueStatus(t, q, otherID, "done")
			if overlap.Load() || destroyCount.Load() != 1 {
				t.Fatalf("unsafe or duplicate cleanup: overlap=%v destroys=%d", overlap.Load(), destroyCount.Load())
			}
		})
	}
}

func TestTaskQueueDeleteCancelsPendingTasksAndPreventsRecreation(t *testing.T) {
	setupTaskQueueConfig(t)
	q := newTaskQueue(1)
	var cancelledIDs []string
	for _, taskType := range []TaskType{TaskCreate, TaskStart, TaskRestart, TaskReinstall} {
		cancelledIDs = append(cancelledIDs, q.Enqueue(9, "example", taskType, "", &lxc.ContainerConfig{Name: "example"})[0])
	}
	otherID := q.Enqueue(10, "other", TaskStart, "", nil)[0]
	deleteID := q.Enqueue(9, "example", TaskDelete, "", nil)[0]
	// Persisted tasks can be restored in either order. A create restored after
	// its delete must not resurrect the deleted container.
	cancelledIDs = append(cancelledIDs, q.Enqueue(9, "example", TaskCreate, "", &lxc.ContainerConfig{Name: "example"})[0])
	for _, id := range cancelledIDs {
		if task := q.tasks[id]; task.Status != "failed" || task.Error == "" {
			t.Fatalf("task %s was not cancelled: %+v", id, task)
		}
	}
	if len(q.createQueue) != 0 || len(q.opQueue) != 2 || q.tasks[otherID].Status != "pending" {
		t.Fatalf("delete affected unrelated work or left conflicting work queued: %+v", q.Settings())
	}
	if got := q.takeNextTask(false); got.ID != deleteID {
		t.Fatalf("dispatched %s before delete %s", got.ID, deleteID)
	} else {
		q.finishTask(got, "done", nil)
	}
}

func TestTaskQueueDeleteWaitsForRestartAndReinstall(t *testing.T) {
	for _, activeType := range []TaskType{TaskRestart, TaskReinstall} {
		t.Run(string(activeType), func(t *testing.T) {
			setupTaskQueueConfig(t)
			q := newTaskQueue(2)
			q.Enqueue(9, "example", activeType, "", nil)
			active := q.takeNextTask(false)
			deleteID := q.Enqueue(9, "example", TaskDelete, "", nil)[0]
			if active.ctx.Err() != nil {
				t.Fatal("operation without cooperative cancellation was abandoned")
			}
			if got := runnableTaskIndex(q.opQueue, q.activeTargets); got != -1 {
				t.Fatal("delete became runnable while operation still owns the container")
			}
			q.finishTask(active, "done", nil)
			if got := q.takeNextTask(false); got.ID != deleteID {
				t.Fatalf("next task = %s, want delete %s", got.ID, deleteID)
			} else {
				q.finishTask(got, "done", nil)
			}
		})
	}
}

func TestTaskQueueDeleteDoesNotCancelCaseVariantContainer(t *testing.T) {
	setupTaskQueueConfig(t)
	config.AppConfig.Containers = []config.Container{{ID: 1, Name: "Alpha"}, {ID: 2, Name: "alpha"}}
	q := newTaskQueue(2)
	q.Enqueue(2, "alpha", TaskStart, "", nil)
	active := q.takeNextTask(false)
	pendingID := q.Enqueue(2, "alpha", TaskRestart, "", nil)[0]
	createID := q.Enqueue(0, "alpha", TaskCreate, "", &lxc.ContainerConfig{Name: "alpha"})[0]
	q.Enqueue(1, "Alpha", TaskDelete, "", nil)
	if active.ctx.Err() != nil || active.deleteRequested {
		t.Fatal("delete cancelled another container's active startup")
	}
	for _, id := range []string{pendingID, createID} {
		if task := q.tasks[id]; task.Status != "pending" {
			t.Fatalf("delete cancelled another container's pending task: %+v", task)
		}
	}
	if sameTaskTarget(&Task{ContainerID: 1, ContainerName: "Alpha"}, &Task{ContainerID: 2, ContainerName: "Alpha"}) {
		t.Fatal("different stable IDs were treated as the same target")
	}
	q.finishTask(active, "done", nil)
}

func TestTaskQueueCreateFailureCleanupUsesStableID(t *testing.T) {
	setupTaskQueueConfig(t)
	config.AppConfig.Containers = make([]config.Container, 0, 4)
	q := newTaskQueue(1)
	q.runtime.create = func(cfg lxc.ContainerConfig) error {
		config.AddContainer(config.Container{ID: 9, Name: cfg.Name})
		return nil
	}
	q.runtime.start = func(_ context.Context, id int) error {
		config.AddContainer(config.Container{ID: 10, Name: "unrelated"})
		// Exercise the exact slice compaction that used to change c.ID while
		// runCreateTask was blocked in startup.
		config.RemoveContainer(id)
		return errors.New("startup failed after config changed")
	}
	var cleanupID int
	q.runtime.destroy = func(id int) error { cleanupID = id; return nil }
	q.Enqueue(0, "example", TaskCreate, "", &lxc.ContainerConfig{Name: "example"})
	q.runCreateTask(q.takeNextTask(true))
	if cleanupID != 9 || config.FindContainer(10) == nil {
		t.Fatalf("cleanup targeted the wrong container: id=%d containers=%+v", cleanupID, config.AppConfig.Containers)
	}
}

func setupTaskQueueConfig(t *testing.T) {
	t.Helper()
	previous := config.AppConfig
	config.AppConfig = &config.ClicdConfig{DataDir: t.TempDir()}
	t.Cleanup(func() { config.AppConfig = previous })
}

func startTaskQueueDispatchers(t *testing.T, q *TaskQueue) {
	t.Helper()
	done := make(chan struct{}, 2)
	go func() { q.createDispatcher(); done <- struct{}{} }()
	go func() { q.opDispatcher(); done <- struct{}{} }()
	t.Cleanup(func() {
		q.mu.Lock()
		close(q.stop)
		for task := range q.runningTasks {
			if task.cancel != nil {
				task.cancel()
			}
		}
		q.signalDispatchers()
		q.mu.Unlock()
		waitTaskQueueSignal(t, done, "create dispatcher exit")
		waitTaskQueueSignal(t, done, "operation dispatcher exit")
		deadline := time.Now().Add(3 * time.Second)
		for q.Settings().Active != 0 && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if q.Settings().Active != 0 {
			t.Error("task workers did not exit")
		}
	})
}

func waitTaskQueueSignal(t *testing.T, signal <-chan struct{}, detail string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s", detail)
	}
}

func waitTaskQueueStatus(t *testing.T, q *TaskQueue, id, status string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		q.mu.Lock()
		task := *q.tasks[id]
		q.mu.Unlock()
		if task.Status == status {
			return
		}
		if task.Status == "failed" || task.Status == "done" {
			t.Fatalf("task %s status=%s, want %s: %s", id, task.Status, status, task.Error)
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("task %s did not reach %s", id, status)
}

func TestTaskQueueSetConcurrencyNormalizesAndReports(t *testing.T) {
	q := newTaskQueue(config.DefaultTaskConcurrency)
	q.SetConcurrency(config.MaxTaskConcurrency + 10)
	if got := q.Settings().Concurrency; got != config.MaxTaskConcurrency {
		t.Fatalf("concurrency = %d, want %d", got, config.MaxTaskConcurrency)
	}
	q.SetConcurrency(0)
	if got := q.Settings().Concurrency; got != config.DefaultTaskConcurrency {
		t.Fatalf("concurrency = %d, want default %d", got, config.DefaultTaskConcurrency)
	}
}

func TestTaskQueueUpdateTaskStage(t *testing.T) {
	q := newTaskQueue(config.DefaultTaskConcurrency)
	task := &Task{ID: "task-1", Type: TaskCreate, Status: "running"}

	q.updateTaskStage(task, "rootfs", "下载模板并创建基础文件系统")

	if task.Stage != "rootfs" || task.StageDetail != "下载模板并创建基础文件系统" {
		t.Fatalf("unexpected task stage: %q %q", task.Stage, task.StageDetail)
	}
}

func lxcConfigWithName(name string) lxc.ContainerConfig {
	return lxc.ContainerConfig{Name: name}
}
