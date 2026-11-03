package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

func setupHistoryStore(t *testing.T) string {
	t.Helper()
	resetConfigStoreForTest(t)
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	t.Cleanup(func() { resetConfigStoreForTest(t) })
	path := filepath.Join(dir, "config.json")
	data, err := json.Marshal(ClicdConfig{AdminUser: "test", AdminPassHash: "hash", JWTSecret: "secret", Port: 8999, DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	SetConfigPath(path)
	if _, err := InitConfig(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestHistoryTablesSurviveConfigSaveAndReopen(t *testing.T) {
	path := setupHistoryStore(t)
	id, err := ReserveTaskID(71)
	if err != nil || id != 71 {
		t.Fatalf("reserve = %d %v", id, err)
	}
	task := TaskHistoryRecord{ID: "task-71", Sequence: 71, FinishedAt: time.Now().UnixMilli(), Data: json.RawMessage(`{"status":"done","logs":[{"stage":"completed"}]}`)}
	if err := SaveTaskHistory([]TaskHistoryRecord{task}, 72); err != nil {
		t.Fatal(err)
	}
	metric := json.RawMessage(`{"ts":123,"cpu":12.5}`)
	if err := SaveMetricHistory("host", "host", time.Now().UnixMilli(), metric); err != nil {
		t.Fatal(err)
	}
	if err := SaveConfig(); err != nil {
		t.Fatal(err)
	}
	resetConfigStoreForTest(t)
	SetConfigPath(path)
	if _, err := InitConfig(); err != nil {
		t.Fatal(err)
	}
	records, next, err := LoadTaskHistory()
	if err != nil || len(records) != 1 || next != 72 || string(records[0].Data) != string(task.Data) {
		t.Fatalf("reopened task history = %+v next=%d err=%v", records, next, err)
	}
	metrics, err := LoadMetricHistory("host", "host")
	if err != nil || len(metrics) != 1 || string(metrics[0].Data) != string(metric) {
		t.Fatalf("reopened metrics = %+v %v", metrics, err)
	}
	if err := DeleteTaskHistory(task.ID); err != nil {
		t.Fatal(err)
	}
	if id, err := ReserveTaskID(0); err != nil || id != 72 {
		t.Fatalf("deleted history reused its ID: %d %v", id, err)
	}
}

func TestHistoryRetentionAndMetricBucketsAreBounded(t *testing.T) {
	setupHistoryStore(t)
	now := time.Now()
	records := []TaskHistoryRecord{{ID: "task-old", Sequence: 1, FinishedAt: now.Add(-TaskHistoryRetention - time.Hour).UnixMilli(), Data: json.RawMessage(`{}`)}, {ID: "task-active", Sequence: 2, Data: json.RawMessage(`{}`)}}
	for i := 0; i < TaskHistoryLimit+3; i++ {
		records = append(records, TaskHistoryRecord{ID: fmt.Sprintf("task-%d", i+3), Sequence: i + 3, FinishedAt: now.UnixMilli(), Data: json.RawMessage(`{}`)})
	}
	if err := SaveTaskHistory(records, 5000); err != nil {
		t.Fatal(err)
	}
	got, next, err := LoadTaskHistory()
	if err != nil || len(got) != TaskHistoryLimit+1 || next != 5000 {
		t.Fatalf("bounded history = %d next=%d err=%v", len(got), next, err)
	}
	for _, record := range got {
		if record.ID == "task-old" || record.ID == "task-3" || record.ID == "task-4" || record.ID == "task-5" {
			t.Fatalf("expired/excess task retained: %s", record.ID)
		}
	}
	if id, err := ReserveTaskID(0); err != nil || id != 5000 {
		t.Fatalf("purge reset task sequence: %d %v", id, err)
	}
	for _, timestamp := range []int64{now.Add(-8 * 24 * time.Hour).UnixMilli(), now.UnixMilli() / metricBucketMillis * metricBucketMillis, now.UnixMilli()/metricBucketMillis*metricBucketMillis + 1} {
		if err := SaveMetricHistory("container", "uuid:guest", timestamp, json.RawMessage(fmt.Sprintf(`{"ts":%d}`, timestamp))); err != nil {
			t.Fatal(err)
		}
	}
	metrics, err := LoadMetricHistory("container", "uuid:guest")
	if err != nil || len(metrics) != 1 {
		t.Fatalf("expired or duplicate metric retained: %+v %v", metrics, err)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM metric_history").Scan(&count); err != nil || count != 1 {
		t.Fatalf("expired metric not physically purged: %d %v", count, err)
	}
}

func TestHistoryWriteRollbackAndConfigWritesDoNotInterfere(t *testing.T) {
	setupHistoryStore(t)
	if err := SaveTaskHistory([]TaskHistoryRecord{{ID: "good", Sequence: 1, Data: json.RawMessage(`{}`)}, {ID: "bad", Sequence: 2, Data: json.RawMessage(`{`)}}, 10); err == nil {
		t.Fatal("invalid JSON was accepted")
	}
	if records, next, err := LoadTaskHistory(); err != nil || len(records) != 0 || next != 0 {
		t.Fatalf("partially committed history: %+v %d %v", records, next, err)
	}
	var wg sync.WaitGroup
	errors := make(chan error, 30)
	for i := 0; i < 10; i++ {
		wg.Add(3)
		go func() { defer wg.Done(); errors <- SaveConfig() }()
		go func(i int) {
			defer wg.Done()
			errors <- SaveTaskHistory([]TaskHistoryRecord{{ID: fmt.Sprint(i), Sequence: i, Data: json.RawMessage(`{}`)}}, 20)
		}(i)
		go func(i int) {
			defer wg.Done()
			errors <- SaveMetricHistory("container", fmt.Sprint(i), time.Now().UnixMilli(), json.RawMessage(`{}`))
		}(i)
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	if records, _, err := LoadTaskHistory(); err != nil || len(records) != 10 {
		t.Fatalf("config replacement lost task history: %d %v", len(records), err)
	}
	if records, err := LoadMetricHistory("container", ""); err != nil || len(records) != 10 {
		t.Fatalf("config replacement lost metric history: %d %v", len(records), err)
	}
}

func TestDataDiskAndStandaloneMigrationRoundTrip(t *testing.T) {
	path := setupHistoryStore(t)
	AppConfig.Containers = []Container{{ID: 1, UUID: "legacy", Name: "guest", Status: "stopped", VCPU: 1, RAMMB: 512, DiskGB: 4}}
	if err := SaveConfig(); err != nil {
		t.Fatal(err)
	}
	for _, column := range []string{"data_disks", "standalone"} {
		if _, err := db.Exec("ALTER TABLE containers DROP COLUMN " + column); err != nil {
			t.Fatal(err)
		}
	}
	resetConfigStoreForTest(t)
	SetConfigPath(path)
	if _, err := InitConfig(); err != nil {
		t.Fatal(err)
	}
	c := &AppConfig.Containers[0]
	if c.Standalone || len(c.DataDisks) != 0 || c.Status != "stopped" || c.UUID != "legacy" {
		t.Fatalf("migration changed legacy instance behavior: %+v", c)
	}
	c.Standalone = true
	c.DataDisks = []DataDisk{{ID: "disk-example", Name: "data", SizeGB: 12.5, StoragePoolID: "pool", Path: "/pool/disks/data.qcow2", Format: "qcow2", Target: "vdb", Bus: "virtio", MountPath: "/data", Attached: true, CreatedAt: "2026-11-03T10:00:00Z"}}
	want := append([]DataDisk(nil), c.DataDisks...)
	if err := SaveConfig(); err != nil {
		t.Fatal(err)
	}
	resetConfigStoreForTest(t)
	SetConfigPath(path)
	if _, err := InitConfig(); err != nil {
		t.Fatal(err)
	}
	if c := AppConfig.Containers[0]; !c.Standalone || !reflect.DeepEqual(c.DataDisks, want) {
		t.Fatalf("disk metadata did not survive restart: %+v", c)
	}
}
