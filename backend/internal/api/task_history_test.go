package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"clicd/internal/config"
)

func testOperationHandler(t *testing.T, kind TaskType, handler OperationTaskHandler) {
	t.Helper()
	RegisterOperationTask(kind, handler)
	t.Cleanup(func() { operationTasks.Lock(); delete(operationTasks.handlers, kind); operationTasks.Unlock() })
}

func TestOperationTasksShareConcurrencyAndAllTargetLocks(t *testing.T) {
	setupTaskQueueConfig(t)
	config.AppConfig.Containers = []config.Container{{ID: 1, UUID: "source", Name: "source"}, {ID: 2, UUID: "destination", Name: "destination"}, {ID: 3, UUID: "other", Name: "other"}}
	started := make(chan int, 3)
	release := make(chan struct{})
	testOperationHandler(t, "qa-multiple-locks", func(ctx context.Context, id int, _ json.RawMessage, progress func(string, string)) error {
		progress("copying", "copying a full disk")
		started <- id
		if id == 1 {
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	})
	q := newTaskQueue(2)
	startTaskQueueDispatchers(t, q)
	first, err := q.EnqueueOperationWithAudit(1, "source", "qa-multiple-locks", json.RawMessage(`{}`), "admin", "", "", "destination")
	if err != nil {
		t.Fatal(err)
	}
	if id := <-started; id != 1 {
		t.Fatalf("first operation = %d", id)
	}
	second, err := q.EnqueueOperationWithAudit(2, "destination", "qa-multiple-locks", json.RawMessage(`{}`), "admin", "", "")
	if err != nil {
		t.Fatal(err)
	}
	third, err := q.EnqueueOperationWithAudit(3, "other", "qa-multiple-locks", json.RawMessage(`{}`), "admin", "", "")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-started:
		if id != 3 {
			t.Fatalf("destination ran concurrently with its clone: %d", id)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("unrelated operation did not use available concurrency")
	}
	waitTaskQueueStatus(t, q, third, "done")
	q.mu.Lock()
	locked := q.tasks[second].Status == "pending" && q.activeTargets["name:source"] && q.activeTargets["name:destination"]
	q.mu.Unlock()
	if !locked {
		t.Fatal("operation did not retain both target locks")
	}
	close(release)
	select {
	case id := <-started:
		if id != 2 {
			t.Fatalf("unexpected subsequent target: %d", id)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("destination lock was not released")
	}
	waitTaskQueueStatus(t, q, first, "done")
	waitTaskQueueStatus(t, q, second, "done")
}

func TestTaskHistoryAndMetricsAcrossProcessRestart(t *testing.T) {
	const dirEnv, phaseEnv = "CLICD_TASK_HISTORY_TEST_DIR", "CLICD_TASK_HISTORY_TEST_PHASE"
	dir, phase := os.Getenv(dirEnv), os.Getenv(phaseEnv)
	if dir == "" {
		dir = t.TempDir()
		cfg := config.ClicdConfig{AdminUser: "test", AdminPassHash: "hash", JWTSecret: "secret", Port: 8999, DataDir: dir,
			Containers: []config.Container{{ID: 1, UUID: "original-owner", Name: "guest", Status: "stopped", VCPU: 1, RAMMB: 512, DiskGB: 4}, {ID: 2, UUID: "other-owner", Name: "other", Status: "stopped", VCPU: 1, RAMMB: 512, DiskGB: 4}}}
		data, _ := json.Marshal(cfg)
		if err := os.WriteFile(filepath.Join(dir, "config.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		for _, phase := range []string{"write", "read"} {
			cmd := exec.Command(executable, "-test.run=^TestTaskHistoryAndMetricsAcrossProcessRestart$", "-test.v")
			cmd.Env = append(os.Environ(), dirEnv+"="+dir, phaseEnv+"="+phase)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%s process: %v\n%s", phase, err, output)
			} else {
				t.Log(string(output))
			}
		}
		return
	}
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv("PATH", "")
	config.SetConfigPath(filepath.Join(dir, "config.json"))
	if _, err := config.InitConfig(); err != nil {
		t.Fatal(err)
	}
	var calls int
	testOperationHandler(t, "qa-history", func(ctx context.Context, _ int, payload json.RawMessage, progress func(string, string)) error {
		calls++
		progress("copying", "copying disk contents")
		if strings.Contains(string(payload), "failure") {
			return errors.New("deliberate operation failure")
		}
		return SetOperationTaskResult(ctx, map[string]string{"artifact_id": "backup-example"})
	})
	q := newTaskQueue(4)
	ids := map[string]string{}
	enqueue := func(payload string) string {
		t.Helper()
		id, err := q.EnqueueOperationWithAudit(1, "guest", "qa-history", json.RawMessage(payload), "admin", "192.0.2.1", "test-agent")
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	if phase == "write" {
		ids["done"] = enqueue(`{"note":"success"}`)
		q.runOperationTask(q.takeNextTask(false))
		ids["failed"] = enqueue(`{"note":"failure"}`)
		q.runOperationTask(q.takeNextTask(false))
		ids["builtin"] = q.Enqueue(2, "other", TaskStart, "", nil)[0]
		q.takeNextTask(false)
		ids["interrupted"] = enqueue(`{"note":"interrupted"}`)
		q.takeNextTask(false)
		ids["pending"] = enqueue(`{"note":"resume pending"}`)
		unsupported := &Task{ID: "task-" + fmt.Sprint(q.allocateTaskID()), Type: "removed-operation", ContainerID: 1, ContainerUUID: "original-owner", ContainerName: "guest", Status: "pending", CreatedAt: time.Now().Format(time.RFC3339Nano), Payload: json.RawMessage(`{}`)}
		ids["unsupported"] = unsupported.ID
		q.tasks[unsupported.ID] = unsupported
		legacy := &Task{ID: "task-" + fmt.Sprint(q.allocateTaskID()), Type: TaskStop, ContainerID: 1, ContainerUUID: "original-owner", ContainerName: "guest", Status: "done", CreatedAt: time.Now().Format(time.RFC3339Nano)}
		ids["legacy-terminal"] = legacy.ID
		q.tasks[legacy.ID] = legacy
		if err := q.persistTaskHistoryOnly(); err != nil {
			t.Fatal(err)
		}
		appendHostMetricPoint(HostInfo{})
		appendContainerMetricPoint(config.AppConfig.Containers[0], map[string]interface{}{"memory_total_bytes": 512, "memory_usage_bytes": 128, "network_rx_bps": 7, "network_tx_bps": 9})
		data, _ := json.Marshal(ids)
		if err := os.WriteFile(filepath.Join(dir, "expected.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
		return
	}
	data, err := os.ReadFile(filepath.Join(dir, "expected.json"))
	if err != nil || json.Unmarshal(data, &ids) != nil {
		t.Fatal("missing writer expectations")
	}
	q.restoreTaskState()
	for _, state := range []string{"done", "failed"} {
		task := q.tasks[ids[state]]
		if task == nil || task.Status != state || task.StartedAt == "" || task.FinishedAt == "" || len(task.Logs) < 3 || task.ContainerUUID != "original-owner" {
			t.Fatalf("terminal task details did not survive restart: %+v", task)
		}
	}
	if task := q.tasks[ids["done"]]; !strings.Contains(string(task.Result), "backup-example") {
		t.Fatalf("operation result lost: %+v", task)
	}
	if task := q.tasks[ids["interrupted"]]; task.Status != "failed" || task.Stage != "interrupted" || !strings.Contains(task.Error, "restart") {
		t.Fatalf("running destructive operation was replayed: %+v", task)
	}
	if q.tasks[ids["pending"]].Status != "pending" || q.tasks[ids["builtin"]].Status != "pending" {
		t.Fatal("pending operation or built-in restart compatibility lost")
	}
	if task := q.tasks[ids["unsupported"]]; task.Status != "failed" || task.Stage != "interrupted" {
		t.Fatalf("unregistered pending operation was replayed: %+v", task)
	}
	if task := q.tasks[ids["legacy-terminal"]]; task.Status != "done" || task.FinishedAt == "" {
		t.Fatal("legacy terminal task lacks a durable retention timestamp")
	}
	q.runtime.start = func(context.Context, int) error { return nil }
	q.runOperationTask(q.takeNextTask(false))
	q.runOperationTask(q.takeNextTask(false))
	if calls != 1 || q.tasks[ids["pending"]].Status != "done" || q.tasks[ids["builtin"]].Status != "done" {
		t.Fatal("recovery did not execute precisely the safe pending operations")
	}
	newID := enqueue(`{}`)
	for _, previous := range ids {
		if parseIDNum(newID) <= parseIDNum(previous) {
			t.Fatal("task ID was reused after restart")
		}
	}
	restoreHostMetricHistory()
	restoreContainerMetricHistory()
	if len(getHostMetricHistory()) != 1 {
		t.Fatal("host metrics did not survive process restart")
	}
	if history := getContainerMetricHistory(&config.AppConfig.Containers[0]); len(history) != 1 || history[0].NetworkRx != 7 || history[0].NetworkTx != 9 {
		t.Fatalf("instance metrics did not survive process restart: %+v", history)
	}
	previousQueue := globalQueue
	globalQueue = q
	t.Cleanup(func() { globalQueue = previousQueue })
	// Reusing an ID and name must not grant a new owner the old task history.
	config.AppConfig.Containers[0].UUID = "replacement-owner"
	request := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil)
	request = withAuthContext(request, AuthContext{Type: authTypeAPIKey, Scopes: []string{"task:read"}, ContainerUUIDs: []string{"replacement-owner"}})
	response := httptest.NewRecorder()
	HandleTasks(response, request)
	var scoped struct {
		Data []Task `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &scoped); err != nil || response.Code != 200 || len(scoped.Data) != 0 {
		t.Fatalf("old history leaked to a reused instance identity: %d %s", response.Code, response.Body.String())
	}
	request = withAuthContext(httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil), AuthContext{Type: authTypeAdmin})
	response = httptest.NewRecorder()
	HandleTasks(response, request)
	if err := json.Unmarshal(response.Body.Bytes(), &scoped); err != nil || len(scoped.Data) < 6 || strings.Contains(response.Body.String(), `"payload"`) {
		t.Fatalf("administrator history is incomplete or exposed operation payload: %s", response.Body.String())
	}
	const taskKey = "clicd_sk_task_detail_test"
	config.AppConfig.ApiKeys = []config.ApiKeyConfig{{ID: "task-detail-test", KeyHash: legacyHashKey(taskKey)}}
	for _, tc := range []struct {
		scopes []string
		uuids  []string
		want   int
	}{
		{[]string{"task:read"}, []string{"replacement-owner"}, http.StatusNotFound},
		{[]string{"container:read"}, nil, http.StatusForbidden},
		{[]string{"task:read"}, []string{"original-owner"}, http.StatusOK},
	} {
		key := &config.AppConfig.ApiKeys[0]
		key.Scopes, key.ContainerUUIDs = tc.scopes, tc.uuids
		request = httptest.NewRequest(http.MethodGet, "/api/v1/tasks/"+ids["done"], nil)
		request.Header.Set("X-API-Key", taskKey)
		response = httptest.NewRecorder()
		AuthMiddleware(HandleTask)(response, request)
		if response.Code != tc.want {
			t.Fatalf("task detail status=%d want=%d: %s", response.Code, tc.want, response.Body.String())
		}
		if tc.want == http.StatusOK {
			var body struct{ Data Task }
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Data.ID != ids["done"] || body.Data.Status != "done" || len(body.Data.Logs) == 0 || !strings.Contains(string(body.Data.Result), "backup-example") || strings.Contains(response.Body.String(), `"payload"`) {
				t.Fatalf("task detail is incomplete or exposed private payload: %s", response.Body.String())
			}
		}
	}
	beforeCalls := calls
	q.runOperationTask(q.takeNextTask(false))
	if calls != beforeCalls || q.tasks[newID].Status != "failed" || !strings.Contains(q.tasks[newID].Error, "identity changed") {
		t.Fatal("queued operation executed against a reused instance identity")
	}
	// Completing an old running task at the retention limit must never leave
	// its pre-completion "running" record behind to replay on the next boot.
	oldID := q.Enqueue(2, "other", TaskStart, "", nil)[0]
	oldTask := q.takeNextTask(false)
	for i := 0; i < config.TaskHistoryLimit; i++ {
		id := fmt.Sprintf("task-%d", 1000+i)
		q.tasks[id] = &Task{ID: id, Type: TaskStop, Status: "done", CreatedAt: time.Now().Format(time.RFC3339Nano), FinishedAt: time.Now().Format(time.RFC3339Nano)}
	}
	q.nextID = 3000
	q.finishTask(oldTask, "done", nil)
	records, _, err := config.LoadTaskHistory()
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		if record.ID == oldID {
			var stored storedTask
			if json.Unmarshal(record.Data, &stored) != nil || stored.Task.Status != "done" {
				t.Fatal("retention discarded a completion and left a running record")
			}
		}
	}
}
