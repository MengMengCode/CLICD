package podman

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type Stats struct {
	CPUPercent       *float64 `json:"cpu_percent"`
	MemoryUsedBytes  uint64   `json:"memory_used_bytes"`
	MemoryLimitBytes uint64   `json:"memory_limit_bytes"`
	NetworkRxBytes   uint64   `json:"network_rx_bytes"`
	NetworkTxBytes   uint64   `json:"network_tx_bytes"`
	BlockReadBytes   uint64   `json:"block_read_bytes"`
	BlockWriteBytes  uint64   `json:"block_write_bytes"`
	PIDs             uint64   `json:"pids"`
}

type statsSample struct {
	ContainerID string
	CPUNano     uint64
	SystemNano  uint64
	MemUsage    uint64
	MemLimit    uint64
	NetInput    uint64
	NetOutput   uint64
	Network     map[string]struct {
		RxBytes uint64
		TxBytes uint64
	}
	BlockInput  uint64
	BlockOutput uint64
	PIDs        uint64
}

func (m Manager) Stats(ctx context.Context, id string) (Stats, error) {
	ctx, cancel := context.WithTimeout(ctx, quickTimeout)
	defer cancel()

	container, err := m.inspectOwned(ctx, id)
	if err != nil {
		return Stats{}, err
	}
	if container.State.Status != "running" && container.State.Status != "paused" {
		return Stats{}, ErrNotRunning
	}

	first, err := readStatsSample(ctx, id)
	if err != nil {
		return Stats{}, err
	}
	// Podman 3.4 computes the first sample's CPU percentage against a zero
	// timestamp. Use the deltas between snapshots to report current activity.
	timer := time.NewTimer(250 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return Stats{}, ctx.Err()
	case <-timer.C:
	}
	last, err := readStatsSample(ctx, id)
	if err != nil {
		return Stats{}, err
	}
	return last.since(first), nil
}

func readStatsSample(ctx context.Context, id string) (statsSample, error) {
	out, err := run(ctx, quickTimeout, "stats", "--no-stream", "--no-reset", "--format", "{{json .ContainerStats}}", "--", id)
	if err != nil {
		return statsSample{}, err
	}
	if len(out) == 0 {
		return statsSample{}, ErrNotRunning
	}
	var sample statsSample
	if err := json.Unmarshal(out, &sample); err != nil {
		return statsSample{}, fmt.Errorf("invalid Podman statistics: %w", err)
	}
	if sample.ContainerID != id {
		return statsSample{}, fmt.Errorf("Podman statistics returned an unexpected container ID")
	}
	return sample, nil
}

func (sample statsSample) since(previous statsSample) Stats {
	stats := Stats{
		MemoryUsedBytes: sample.MemUsage, MemoryLimitBytes: sample.MemLimit,
		BlockReadBytes: sample.BlockInput, BlockWriteBytes: sample.BlockOutput, PIDs: sample.PIDs,
	}
	if sample.CPUNano >= previous.CPUNano && sample.SystemNano > previous.SystemNano {
		pct := float64(sample.CPUNano-previous.CPUNano) / float64(sample.SystemNano-previous.SystemNano) * 100
		stats.CPUPercent = &pct
	}
	if sample.Network == nil {
		// Podman 3.4 populates NetOutput with guest receives and NetInput with
		// guest transmits. Newer releases expose guest counters per interface.
		stats.NetworkRxBytes, stats.NetworkTxBytes = sample.NetOutput, sample.NetInput
	} else {
		for _, iface := range sample.Network {
			stats.NetworkRxBytes += iface.RxBytes
			stats.NetworkTxBytes += iface.TxBytes
		}
	}
	return stats
}
