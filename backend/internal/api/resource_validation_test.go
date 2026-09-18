package api

import (
	"encoding/json"
	"testing"

	"clicd/internal/config"
	"clicd/internal/lxc"
)

func TestValidateRuntimeResourceRequest_DiskLimits(t *testing.T) {
	tests := []struct {
		name        string
		runtime     string
		templateID  string
		diskGB      float64
		expectError bool
	}{
		{
			name:        "Alpine 0.5G succeeds",
			runtime:     config.VirtualizationLXC,
			templateID:  "alpine-3.21",
			diskGB:      0.5,
			expectError: false,
		},
		{
			name:        "Alpine 0.2G fails",
			runtime:     config.VirtualizationLXC,
			templateID:  "alpine-3.21",
			diskGB:      0.2,
			expectError: true,
		},
		{
			name:        "Debian 0.5G fails",
			runtime:     config.VirtualizationLXC,
			templateID:  "debian-bookworm",
			diskGB:      0.5,
			expectError: true,
		},
		{
			name:        "Debian 1.0G succeeds",
			runtime:     config.VirtualizationLXC,
			templateID:  "debian-bookworm",
			diskGB:      1.0,
			expectError: false,
		},
		{
			name:        "Ubuntu 0.5G fails",
			runtime:     config.VirtualizationLXC,
			templateID:  "ubuntu-noble",
			diskGB:      0.5,
			expectError: true,
		},
		{
			name:        "Ubuntu 1.5G succeeds",
			runtime:     config.VirtualizationLXC,
			templateID:  "ubuntu-noble",
			diskGB:      1.5,
			expectError: false,
		},
		{
			name:        "KVM Windows 10G fails",
			runtime:     config.VirtualizationKVM,
			templateID:  "kvm-windows-10",
			diskGB:      10,
			expectError: true,
		},
		{
			name:        "KVM Windows 30G succeeds",
			runtime:     config.VirtualizationKVM,
			templateID:  "kvm-windows-10",
			diskGB:      30,
			expectError: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateRuntimeResourceRequest(tc.runtime, tc.templateID, 1.0, 512, tc.diskGB)
			if tc.expectError && err == nil {
				t.Errorf("expected error for %s (disk=%.2f), but got nil", tc.name, tc.diskGB)
			}
			if !tc.expectError && err != nil {
				t.Errorf("unexpected error for %s (disk=%.2f): %v", tc.name, tc.diskGB, err)
			}
		})
	}
}

func TestNormalizeCreateResourceLimits_DiskParsing(t *testing.T) {
	tests := []struct {
		name       string
		jsonInput  string
		expectedGB float64
	}{
		{
			name:       "Numeric float 0.5",
			jsonInput:  `{"disk_gb": 0.5}`,
			expectedGB: 0.5,
		},
		{
			name:       "String float 0.5",
			jsonInput:  `{"disk_gb": "0.5"}`,
			expectedGB: 0.5,
		},
		{
			name:       "String MB 512M",
			jsonInput:  `{"disk_gb": "512M"}`,
			expectedGB: 0.5,
		},
		{
			name:       "String MB 768MB",
			jsonInput:  `{"disk_gb": "768MB"}`,
			expectedGB: 0.75,
		},
		{
			name:       "String GB 1G",
			jsonInput:  `{"disk_gb": "1G"}`,
			expectedGB: 1.0,
		},
		{
			name:       "Numeric disk_mb field",
			jsonInput:  `{"disk_mb": 512}`,
			expectedGB: 0.5,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var cfg lxc.ContainerConfig
			var fields map[string]json.RawMessage
			_ = json.Unmarshal([]byte(tc.jsonInput), &cfg)
			_ = json.Unmarshal([]byte(tc.jsonInput), &fields)

			err := normalizeCreateResourceLimits(&cfg, fields)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.DiskGB != tc.expectedGB {
				t.Errorf("expected DiskGB %.2f, got %.2f", tc.expectedGB, cfg.DiskGB)
			}
		})
	}
}
