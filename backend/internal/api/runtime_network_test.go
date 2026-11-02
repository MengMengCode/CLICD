package api

import (
	"testing"

	"clicd/internal/config"
	"clicd/internal/lxc"
)

func TestNAT6OnlyNetworkSelectionIsLimitedToManagedLXCBridge(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	natOff := false
	for _, tc := range []struct {
		name    string
		enabled bool
		request lxc.ContainerConfig
		want    bool
	}{
		{"disabled", false, lxc.ContainerConfig{}, false},
		{"enabled LXC", true, lxc.ContainerConfig{}, true},
		{"enabled KVM still needs a network", true, lxc.ContainerConfig{Virtualization: config.VirtualizationKVM}, false},
		{"IPv4 NAT remains available", false, lxc.ContainerConfig{PortMappingCount: 1}, true},
		{"routed IPv6 remains available", false, lxc.ContainerConfig{AssignIPv6: true}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config.AppConfig = &config.ClicdConfig{IPv6NAT: config.IPv6NATSettings{Enabled: tc.enabled}}
			if tc.request.PortMappingCount == 0 {
				tc.request.AssignNAT = &natOff
			}
			if got := hasRequestedNetwork(tc.request); got != tc.want {
				t.Fatalf("hasRequestedNetwork=%v want %v", got, tc.want)
			}
		})
	}
}
