package api

import (
	"context"
	"fmt"
	"math"
	"os"
	"strings"

	"clicd/internal/config"
	"clicd/internal/kvm"
	"clicd/internal/lxc"
)

var kvmManager = kvm.NewManager()

const noNetworkSelectedMessage = "请勾选任意一个可用网络"

func runtimeFromRequest(value string) string {
	return config.NormalizeVirtualization(value)
}

func hasRequestedNetwork(cfg lxc.ContainerConfig) bool {
	nat6 := config.AppConfig != nil && config.AppConfig.IPv6NAT.Enabled &&
		runtimeFromRequest(cfg.Virtualization) == config.VirtualizationLXC && !cfg.WantsLANIPv4()
	return cfg.WantsNAT() || cfg.WantsLANIPv4() || cfg.AssignIPv4 || len(cfg.PublicIPv4s) > 0 || cfg.AssignIPv6 || len(cfg.IPv6Addresses) > 0 || nat6
}

func runtimeFromTemplateID(templateID string) string {
	if kvm.FindImage(templateID) != nil {
		return config.VirtualizationKVM
	}
	return config.VirtualizationLXC
}

func createByRuntime(cfg lxc.ContainerConfig) error {
	cfg.Virtualization = runtimeFromRequest(cfg.Virtualization)
	cfg.NormalizeResourceAliases()
	if cfg.Virtualization == config.VirtualizationKVM {
		return kvmManager.CreateContainer(cfg)
	}
	return lxcManager.CreateContainer(cfg)
}

func validateCreateSSHAuth(cfg lxc.ContainerConfig) error {
	if cfg.Virtualization == config.VirtualizationKVM && (kvm.IsWindowsImage(cfg.TemplateID) || kvm.IsManualISOImage(cfg.TemplateID) || kvm.IsManualDiskImage(cfg.TemplateID)) {
		return nil
	}
	_, err := lxc.ResolveCreateSSHAccess(cfg)
	return err
}

func validateReinstallSSHAuth(c *config.Container, templateID string, cfg lxc.ContainerConfig) error {
	if c != nil && c.IsKVM() && (kvm.IsWindowsImage(templateID) || kvm.IsManualISOImage(templateID) || kvm.IsManualDiskImage(templateID)) {
		return nil
	}
	currentPassword := ""
	if c != nil {
		currentPassword = c.SSHPassword
	}
	_, err := lxc.ResolveReinstallSSHAccess(currentPassword, cfg)
	return err
}

func startByRuntime(id int) error {
	return startByRuntimeContext(context.Background(), id)
}

func startByRuntimeContext(ctx context.Context, id int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c := config.FindContainer(id)
	if c != nil && c.IsKVM() {
		return kvmManager.StartContainerContext(ctx, id)
	}
	return lxcManager.StartContainer(id)
}

func stopByRuntime(id int) error {
	c := config.FindContainer(id)
	if c != nil && c.IsKVM() {
		return kvmManager.StopContainer(id)
	}
	return lxcManager.StopContainer(id)
}

func restartByRuntime(id int) error {
	c := config.FindContainer(id)
	if c != nil && c.IsKVM() {
		return kvmManager.RestartContainer(id)
	}
	return lxcManager.RestartContainer(id)
}

func destroyByRuntime(id int) error {
	c := config.FindContainer(id)
	if c != nil && c.IsKVM() {
		return kvmManager.DestroyContainer(id)
	}
	return lxcManager.DestroyContainer(id)
}

func reinstallByRuntime(id int, templateID string, authConfig ...lxc.ContainerConfig) error {
	c := config.FindContainer(id)
	if c != nil && c.IsKVM() {
		return kvmManager.ReinstallContainer(id, templateID, authConfig...)
	}
	return lxcManager.ReinstallContainer(id, templateID, authConfig...)
}

func resetPasswordByRuntime(id int, password string) (string, error) {
	c := config.FindContainer(id)
	if c != nil && c.IsKVM() {
		return kvmManager.ResetSSHPassword(id, password)
	}
	return lxcManager.ResetSSHPassword(id, password)
}

func assignIPv6ByRuntime(id int) (*config.Container, error) {
	c := config.FindContainer(id)
	if c != nil && c.IsKVM() {
		return kvmManager.AssignIPv6(id)
	}
	return lxcManager.AssignIPv6(id)
}

func updatePublicIPv4ByRuntime(id int, requested []string, count int, auto bool) (*config.Container, error) {
	c := config.FindContainer(id)
	if c != nil && c.IsKVM() {
		return kvmManager.UpdatePublicIPv4Assignments(id, requested, count, auto)
	}
	return lxcManager.UpdatePublicIPv4Assignments(id, requested, count, auto)
}

func updateIPv6ByRuntime(id int, requested []string, count int, auto bool) (*config.Container, error) {
	c := config.FindContainer(id)
	if c != nil && c.IsKVM() {
		return kvmManager.UpdateIPv6Assignments(id, requested, count, auto)
	}
	return lxcManager.UpdateIPv6Assignments(id, requested, count, auto)
}

func usageByRuntime(id int) (map[string]interface{}, error) {
	c := config.FindContainer(id)
	if c != nil && c.IsKVM() {
		return kvmManager.GetResourceUsage(id)
	}
	return lxcManager.GetResourceUsage(id)
}

func trafficByRuntime(id int) map[string]interface{} {
	c := config.FindContainer(id)
	if c != nil && c.IsKVM() {
		return kvmManager.GetTrafficInfo(id)
	}
	return lxcManager.GetTrafficInfo(id)
}

func createSnapshotByRuntime(id int, createdBy string, scheduled bool, rotateLimit int, storagePoolID ...string) (config.Snapshot, error) {
	c := config.FindContainer(id)
	if c != nil && c.IsKVM() {
		return kvmManager.CreateSnapshot(id, createdBy, scheduled, rotateLimit, storagePoolID...)
	}
	return lxcManager.CreateSnapshot(id, createdBy, scheduled, rotateLimit, storagePoolID...)
}

func deleteSnapshotByRuntime(snapshotID string) error {
	snapshot := config.FindSnapshot(snapshotID)
	if snapshot != nil {
		if c := config.FindContainer(snapshot.ContainerID); c != nil && c.IsKVM() {
			return kvmManager.DeleteSnapshot(snapshotID)
		}
		if strings.Contains(snapshot.Path, string(os.PathSeparator)+"kvm"+string(os.PathSeparator)) {
			return kvmManager.DeleteSnapshot(snapshotID)
		}
	}
	return lxcManager.DeleteSnapshot(snapshotID)
}

func restoreSnapshotByRuntime(snapshotID string) error {
	snapshot := config.FindSnapshot(snapshotID)
	if snapshot != nil {
		if c := config.FindContainer(snapshot.ContainerID); c != nil && c.IsKVM() {
			return kvmManager.RestoreSnapshot(snapshotID)
		}
		if strings.Contains(snapshot.Path, string(os.PathSeparator)+"kvm"+string(os.PathSeparator)) {
			return kvmManager.RestoreSnapshot(snapshotID)
		}
	}
	return lxcManager.RestoreSnapshot(snapshotID)
}

func setSnapshotScheduleByRuntime(id int, enabled bool, intervalHours int, scheduleTime string, createdBy string) (*config.Container, error) {
	c := config.FindContainer(id)
	if c != nil && c.IsKVM() {
		return kvmManager.SetSnapshotSchedule(id, enabled, intervalHours, scheduleTime, createdBy)
	}
	return lxcManager.SetSnapshotSchedule(id, enabled, intervalHours, scheduleTime, createdBy)
}

func applyLimitsByRuntime(c *config.Container) error {
	if c != nil && c.IsKVM() {
		return kvmManager.ApplyContainerLimits(c)
	}
	return lxcManager.ApplyContainerLimits(c)
}

func listByRuntime() ([]config.Container, error) {
	containers, err := lxcManager.ListContainers()
	if err != nil {
		containers = config.AppConfig.Containers
	}
	containers = kvmManager.ListContainers(containers)
	return containers, err
}

func validateRuntimeResourceRequest(runtime string, templateID string, vcpu float64, ramMB int, diskGB float64) error {
	if runtime == config.VirtualizationKVM {
		if vcpu < 1 || math.Abs(vcpu-math.Round(vcpu)) > 0.000001 {
			return fmt.Errorf("KVM vCPU must be a whole number and at least 1")
		}
		if ramMB < 1024 {
			return fmt.Errorf("KVM memory must be at least 1024 MB")
		}
		minDisk := float64(10)
		if templateID != "" {
			imageMinDisk := kvm.GetKVMImageMinDiskGB(templateID)
			if imageMinDisk > minDisk {
				minDisk = imageMinDisk
			}
			if diskGB < minDisk {
				return fmt.Errorf("template %s requires at least %.2f GB disk, but requested %.2f GB", templateID, minDisk, diskGB)
			}
		} else if diskGB < minDisk {
			return fmt.Errorf("KVM disk must be at least %.0f GB", minDisk)
		}
	} else {
		if templateID != "" {
			minDisk := lxc.GetTemplateMinDiskGB(templateID)
			if diskGB < minDisk {
				return fmt.Errorf("template %s requires at least %.2f GB disk, but requested %.2f GB", templateID, minDisk, diskGB)
			}
		} else if diskGB < 0.25 {
			return fmt.Errorf("disk must be at least 0.25 GB")
		}
	}
	return validateContainerResourceRequest(vcpu, ramMB, diskGB)
}
