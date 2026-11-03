package api

import (
	"encoding/json"
	"fmt"
	"time"

	"clicd/internal/config"
)

func persistMetricPoint(kind, series string, sampledAt int64, point interface{}) {
	data, err := json.Marshal(point)
	if err == nil {
		err = config.SaveMetricHistory(kind, series, sampledAt, data)
	}
	if err != nil && err != config.ErrHistoryStoreUnavailable {
		fmt.Printf("Metric history persistence failed: %v\n", err)
	}
}

func restoreHostMetricHistory() {
	records, err := config.LoadMetricHistory("host", "host")
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-hostMetricRetention).UnixMilli()
	var history []HostMetricPoint
	for _, record := range records {
		var point HostMetricPoint
		if json.Unmarshal(record.Data, &point) == nil && point.TS >= cutoff {
			history = append(history, point)
		}
	}
	hostMetricMu.Lock()
	hostMetricHistory = history
	hostMetricMu.Unlock()
}

func restoreContainerMetricHistory() {
	records, err := config.LoadMetricHistory("container", "")
	if err != nil {
		return
	}
	valid := make(map[string]bool)
	if config.AppConfig != nil {
		for _, c := range config.AppConfig.Containers {
			valid[containerMetricKey(c)] = true
		}
	}
	cutoff := time.Now().Add(-hostMetricRetention).UnixMilli()
	history := make(map[string][]ContainerMetricPoint)
	for _, record := range records {
		var point ContainerMetricPoint
		if valid[record.Series] && json.Unmarshal(record.Data, &point) == nil && point.TS >= cutoff {
			history[record.Series] = append(history[record.Series], point)
		}
	}
	containerMetricMu.Lock()
	containerMetricHistory = history
	containerMetricMu.Unlock()
}
