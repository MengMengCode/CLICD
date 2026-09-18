package api

import (
	"fmt"
	"math"
)

const minVCPU = 0.25

func validateContainerResourceRequest(vcpu float64, ramMB int, diskGB float64) error {
	host := getHostInfo()

	if vcpu <= 0 {
		return fmt.Errorf("vCPU must be greater than 0")
	}
	if vcpu < minVCPU {
		return fmt.Errorf("vCPU must be at least %.2f", minVCPU)
	}
	if math.Abs(vcpu*4-math.Round(vcpu*4)) > 0.000001 {
		return fmt.Errorf("vCPU must use 0.25 increments")
	}
	if host.CPU.Cores > 0 && vcpu > float64(host.CPU.Cores) {
		return fmt.Errorf("vCPU cannot exceed host CPU cores (%d)", host.CPU.Cores)
	}
	if host.RAM.TotalMB > 0 && ramMB > int(host.RAM.TotalMB) {
		return fmt.Errorf("memory cannot exceed host memory (%d MB)", host.RAM.TotalMB)
	}
	if diskGB <= 0 {
		return fmt.Errorf("disk must be greater than 0")
	}
	if host.Disk.TotalGB > 0 {
		maxDiskGB := host.Disk.TotalGB
		if maxDiskGB < 1 {
			maxDiskGB = 1
		}
		if diskGB > maxDiskGB {
			return fmt.Errorf("disk cannot exceed host disk (%.0f GB)", maxDiskGB)
		}
	}
	return nil
}
