package api

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"sync"
	"time"

	"clicd/internal/config"
)

type ContainerMetricPoint struct {
	TS        int64   `json:"ts"`
	CPU       float64 `json:"cpu"`
	Memory    float64 `json:"memory"`
	Network   float64 `json:"network"`
	NetworkRx float64 `json:"network_rx"`
	NetworkTx float64 `json:"network_tx"`
	DiskIO    float64 `json:"disk_io"`
	DiskRead  float64 `json:"disk_read"`
	DiskWrite float64 `json:"disk_write"`
}

var containerMetricSamplerOnce sync.Once
var containerMetricMu sync.RWMutex
var containerMetricHistory = map[string][]ContainerMetricPoint{}
var containerMetricInFlight sync.Map

const (
	containerMetricSampleInterval = 30 * time.Second
	containerMetricSampleTimeout  = 20 * time.Second
	containerMetricConcurrency    = 4
)

func StartContainerMetricSampler() {
	containerMetricSamplerOnce.Do(func() {
		restoreContainerMetricHistory()
		go func() {
			sampleAllContainerMetrics()
			ticker := time.NewTicker(containerMetricSampleInterval)
			defer ticker.Stop()
			for range ticker.C {
				sampleAllContainerMetrics()
			}
		}()
	})
}

func sampleAllContainerMetrics() {
	containers, _ := listByRuntime()
	sem := make(chan struct{}, containerMetricConcurrency)
	var wg sync.WaitGroup

	for _, c := range containers {
		c := c
		if c.Status != "running" {
			continue
		}
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			sampleContainerMetricWithTimeout(c)
		}()
	}
	wg.Wait()
	pruneContainerMetricHistory()
}

func sampleContainerMetricWithTimeout(c config.Container) {
	key := containerMetricKey(c)
	if key == "" {
		return
	}
	if _, loaded := containerMetricInFlight.LoadOrStore(key, struct{}{}); loaded {
		return
	}
	done := make(chan struct{}, 1)
	go func() {
		defer containerMetricInFlight.Delete(key)
		if usage, err := usageByRuntime(c.ID); err == nil {
			appendContainerMetricPoint(c, usage)
		}
		done <- struct{}{}
	}()

	select {
	case <-done:
	case <-time.After(containerMetricSampleTimeout):
	}
}

func appendContainerMetricPoint(c config.Container, usage map[string]interface{}) {
	key := containerMetricKey(c)
	if key == "" {
		return
	}
	memoryTotal := numberFromUsage(usage, "memory_total_bytes")
	if memoryTotal <= 0 {
		memoryTotal = float64(c.RAMMB) * 1024 * 1024
	}
	memoryPct := 0.0
	if memoryTotal > 0 {
		memoryPct = clampPercent(numberFromUsage(usage, "memory_usage_bytes") / memoryTotal * 100)
	}
	vcpu := c.VCPU
	if vcpu <= 0 {
		vcpu = 1
	}
	cpuPct := clampPercent(numberFromUsage(usage, "cpu_usage_pct") / vcpu)
	networkRx := positiveNumberFromUsage(usage, "network_rx_bps")
	networkTx := positiveNumberFromUsage(usage, "network_tx_bps")
	diskRead := positiveNumberFromUsage(usage, "disk_read_bps")
	diskWrite := positiveNumberFromUsage(usage, "disk_write_bps")
	point := ContainerMetricPoint{
		TS:        time.Now().UnixMilli(),
		CPU:       cpuPct,
		Memory:    memoryPct,
		NetworkRx: networkRx,
		NetworkTx: networkTx,
		Network:   networkRx + networkTx,
		DiskRead:  diskRead,
		DiskWrite: diskWrite,
		DiskIO:    diskRead + diskWrite,
	}
	cutoff := time.Now().Add(-hostMetricRetention).UnixMilli()
	persistMetricPoint("container", key, point.TS, point)

	containerMetricMu.Lock()
	defer containerMetricMu.Unlock()

	history := containerMetricHistory[key]
	keepFrom := 0
	for keepFrom < len(history) && history[keepFrom].TS < cutoff {
		keepFrom++
	}
	if keepFrom > 0 {
		copy(history, history[keepFrom:])
		history = history[:len(history)-keepFrom]
	}
	if len(history) > 0 && history[len(history)-1].TS/30_000 == point.TS/30_000 {
		history[len(history)-1] = point
		containerMetricHistory[key] = history
	} else {
		containerMetricHistory[key] = append(history, point)
	}
}

func getContainerMetricHistory(c *config.Container) []ContainerMetricPoint {
	if c == nil {
		return nil
	}
	key := containerMetricKey(*c)
	containerMetricMu.RLock()
	defer containerMetricMu.RUnlock()

	history := containerMetricHistory[key]
	result := make([]ContainerMetricPoint, len(history))
	copy(result, history)
	return result
}

func pruneContainerMetricHistory() {
	cutoff := time.Now().Add(-hostMetricRetention).UnixMilli()
	valid := map[string]bool{}
	if config.AppConfig != nil {
		for _, c := range config.AppConfig.Containers {
			valid[containerMetricKey(c)] = true
		}
	}

	containerMetricMu.Lock()
	defer containerMetricMu.Unlock()

	for key, history := range containerMetricHistory {
		if !valid[key] {
			delete(containerMetricHistory, key)
			continue
		}
		keepFrom := 0
		for keepFrom < len(history) && history[keepFrom].TS < cutoff {
			keepFrom++
		}
		if keepFrom > 0 {
			copy(history, history[keepFrom:])
			containerMetricHistory[key] = history[:len(history)-keepFrom]
		}
	}
}

func containerMetricKey(c config.Container) string {
	if c.UUID != "" {
		return "uuid:" + c.UUID
	}
	if c.ID > 0 {
		return fmt.Sprintf("id:%d", c.ID)
	}
	if c.Name != "" {
		return "name:" + c.Name
	}
	return ""
}

func numberFromUsage(usage map[string]interface{}, key string) float64 {
	value, ok := usage[key]
	if !ok || value == nil {
		return 0
	}
	switch v := value.(type) {
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return 0
		}
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case int32:
		return float64(v)
	case uint:
		return float64(v)
	case uint64:
		return float64(v)
	case uint32:
		return float64(v)
	case json.Number:
		n, _ := v.Float64()
		return n
	case string:
		n, _ := strconv.ParseFloat(v, 64)
		return n
	default:
		return 0
	}
}

func positiveNumberFromUsage(usage map[string]interface{}, key string) float64 {
	value := numberFromUsage(usage, key)
	if value < 0 {
		return 0
	}
	return value
}
