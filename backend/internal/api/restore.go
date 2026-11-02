package api

import (
	"fmt"
	"time"

	"clicd/internal/config"
	"clicd/internal/kvm"
	"clicd/internal/lxc"
)

// CaptureRuntimeRestoreState records which managed workloads are actually
// running before the CLICD service exits. On the next host boot, only those
// workloads are started again.
func CaptureRuntimeRestoreState() {
	if config.AppConfig == nil {
		return
	}
	lxcManager := lxc.NewManager()
	kvmManager := kvm.NewManager()
	changed := false

	for i := range config.AppConfig.Containers {
		c := &config.AppConfig.Containers[i]
		status, err := runtimeStatus(*c, lxcManager, kvmManager)
		if err != nil {
			fmt.Printf("Warning: failed to capture runtime state for %s: %v\n", c.Name, err)
			continue
		}
		restore := status == "running"
		if c.RestoreOnHostBoot != restore {
			c.RestoreOnHostBoot = restore
			changed = true
		}
		if status != "" && c.Status != status {
			c.Status = status
			changed = true
		}
	}
	if changed {
		if err := config.SaveConfig(); err != nil {
			fmt.Printf("Warning: failed to save host boot restore state: %v\n", err)
		}
	}
}

func StartHostBootRestore() {
	go RestoreHostBootState()
}

func RestoreHostBootState() {
	if config.AppConfig == nil {
		return
	}
	time.Sleep(2 * time.Second)

	lxcManager := lxc.NewManager()
	kvmManager := kvm.NewManager()
	containers := append([]config.Container(nil), config.AppConfig.Containers...)

	for _, c := range containers {
		if !c.RestoreOnHostBoot {
			continue
		}
		if lxc.IsExpired(c) {
			fmt.Printf("Skipping host boot restore for %s: expired at %s\n", c.Name, c.ExpiresAt)
			continue
		}

		status, err := runtimeStatus(c, lxcManager, kvmManager)
		if err == nil && status == "running" {
			config.UpdateContainerStatusAndRestore(c.ID, "running", true)
			if !c.IsKVM() {
				_ = lxcManager.ApplyPortMappings(c.ID)
			} else {
				_ = lxc.NewManager().ApplyPortMappings(c.ID)
			}
			continue
		}

		fmt.Printf("Restoring workload after host boot: %s (ID=%d)\n", c.Name, c.ID)
		if c.IsKVM() {
			if err := kvmManager.StartContainer(c.ID); err != nil {
				fmt.Printf("Warning: failed to restore KVM %s: %v\n", c.Name, err)
			}
			continue
		}
		if err := lxcManager.StartContainer(c.ID); err != nil {
			fmt.Printf("Warning: failed to restore LXC %s: %v\n", c.Name, err)
		}
	}
	lxc.EnsureAllRunningPortMappings()
}

func runtimeStatus(c config.Container, lxcManager *lxc.Manager, kvmManager *kvm.Manager) (string, error) {
	if c.IsKVM() {
		return kvmManager.GetContainerStatus(c.VirshName())
	}
	return lxcManager.GetContainerStatus(c.LxcName())
}
