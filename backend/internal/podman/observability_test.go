package podman

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestImageListGroupsValidTagsByID(t *testing.T) {
	busybox := strings.Repeat("a", 64)
	alpine := strings.Repeat("b", 64)
	data := `[
		{"Id":"sha256:BUSYBOX","Names":["docker.io/library/busybox:1.37","docker.io/library/busybox:stable"],"RepoTags":null,"Size":1428577,"CreatedAt":"2026-11-14T02:00:00Z"},
		{"Id":"BUSYBOX","Names":["docker.io/library/busybox:stable","<none>:<none>"],"RepoTags":["localhost/my-busybox:1.37"],"Size":1428577},
		{"Id":"ALPINE","Names":[],"Size":2100000,"Created":1794621500}
	]`
	images, err := parseImageList([]byte(strings.NewReplacer("BUSYBOX", busybox, "ALPINE", alpine).Replace(data)))
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 2 || images[0].ID != busybox || images[1].ID != alpine {
		t.Fatalf("unexpected images: %+v", images)
	}
	want := []string{"docker.io/library/busybox:1.37", "docker.io/library/busybox:stable", "localhost/my-busybox:1.37"}
	if !reflect.DeepEqual(images[0].Tags, want) || images[0].SizeBytes != 1428577 {
		t.Fatalf("unexpected BusyBox metadata: %+v", images[0])
	}
	if images[1].Tags == nil || len(images[1].Tags) != 0 || images[1].CreatedAt.Unix() != 1794621500 {
		t.Fatalf("unexpected untagged Alpine metadata: %+v", images[1])
	}
}

func TestPodmanStatsUsesCounterDeltasAndCorrectNetworkDirections(t *testing.T) {
	var legacy, current statsSample
	if err := json.Unmarshal([]byte(`{"CPUNano":125000000,"SystemNano":1500000000,"MemUsage":339968,"MemLimit":67108864,"NetInput":132,"NetOutput":442,"BlockInput":4096,"BlockOutput":512,"PIDs":1}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"CPUNano":100,"SystemNano":2500000000,"Network":{"eth0":{"RxBytes":712,"TxBytes":134},"eth1":{"RxBytes":22,"TxBytes":31}}}`), &current); err != nil {
		t.Fatal(err)
	}
	previous := statsSample{CPUNano: 0, SystemNano: uint64(time.Second)}
	oldStats := legacy.since(previous)
	if oldStats.CPUPercent == nil || *oldStats.CPUPercent != 25 || oldStats.NetworkRxBytes != 442 || oldStats.NetworkTxBytes != 132 ||
		oldStats.MemoryUsedBytes != 339968 || oldStats.MemoryLimitBytes != 67108864 || oldStats.BlockReadBytes != 4096 || oldStats.BlockWriteBytes != 512 || oldStats.PIDs != 1 {
		t.Fatalf("incorrect legacy stats: %+v", oldStats)
	}
	newStats := current.since(legacy)
	if newStats.CPUPercent != nil || newStats.NetworkRxBytes != 734 || newStats.NetworkTxBytes != 165 {
		t.Fatalf("incorrect stats after restart: %+v", newStats)
	}
}

func TestInspectExitStatePreservesZeroExitCode(t *testing.T) {
	var container inspectContainer
	if err := json.Unmarshal([]byte(`{"State":{"Status":"exited","ExitCode":0,"StartedAt":"2026-11-14T02:00:00Z","FinishedAt":"2026-11-14T02:00:04Z"}}`), &container); err != nil {
		t.Fatal(err)
	}
	app := container.app()
	if app.Status != "stopped" || app.ExitCode == nil || *app.ExitCode != 0 || app.StartedAt == nil || app.FinishedAt == nil {
		t.Fatalf("missing state of app that exited successfully: %+v", app)
	}
}
