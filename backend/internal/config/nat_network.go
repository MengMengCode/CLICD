package config

import (
	"encoding/binary"
	"fmt"
	"net/netip"
	"os"
	"strings"
)

const (
	DefaultLXCNATSubnet = "10.0.3.0/24"
	DefaultKVMNATSubnet = "192.168.122.0/24"
)

type NATNetwork struct {
	Subnet     string `json:"subnet"`
	Gateway    string `json:"gateway"`
	Netmask    string `json:"netmask"`
	DHCPStart  string `json:"dhcp_start"`
	DHCPEnd    string `json:"dhcp_end"`
	DHCPMax    int    `json:"dhcp_max"`
	PrefixBits int    `json:"prefix_bits"`
}

func ParseNATNetwork(raw string) (NATNetwork, error) {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
	if err != nil || !prefix.Addr().Is4() {
		return NATNetwork{}, fmt.Errorf("NAT subnet must be a valid IPv4 CIDR")
	}
	prefix = prefix.Masked()
	if prefix.Bits() < 16 || prefix.Bits() > 28 {
		return NATNetwork{}, fmt.Errorf("NAT subnet prefix must be between /16 and /28")
	}
	if !isRFC1918Prefix(prefix) {
		return NATNetwork{}, fmt.Errorf("NAT subnet must use an RFC1918 private IPv4 range")
	}

	network := ipv4Uint32(prefix.Addr())
	hostBits := 32 - prefix.Bits()
	broadcast := network | uint32((uint64(1)<<hostBits)-1)
	gateway := uint32IPv4(network + 1)
	dhcpStart := uint32IPv4(network + 2)
	dhcpEnd := uint32IPv4(broadcast - 1)
	return NATNetwork{
		Subnet:     prefix.String(),
		Gateway:    gateway.String(),
		Netmask:    netmaskString(prefix.Bits()),
		DHCPStart:  dhcpStart.String(),
		DHCPEnd:    dhcpEnd.String(),
		DHCPMax:    int(broadcast - network - 2),
		PrefixBits: prefix.Bits(),
	}, nil
}

func LXCNATNetwork() NATNetwork {
	return configuredNATNetwork(false)
}

func KVMNATNetwork() NATNetwork {
	return configuredNATNetwork(true)
}

func normalizeNATNetworkDefaults() bool {
	changed := false
	lxcSubnet := configuredSubnetValue(AppConfig.LXCNATSubnet, "CLICD_LXC_SUBNET", DefaultLXCNATSubnet)
	kvmSubnet := configuredSubnetValue(AppConfig.KVMNATSubnet, "CLICD_KVM_SUBNET", DefaultKVMNATSubnet)
	if AppConfig.LXCNATSubnet != lxcSubnet {
		AppConfig.LXCNATSubnet = lxcSubnet
		changed = true
	}
	if AppConfig.KVMNATSubnet != kvmSubnet {
		AppConfig.KVMNATSubnet = kvmSubnet
		changed = true
	}
	return changed
}

func configuredNATNetwork(kvm bool) NATNetwork {
	raw := DefaultLXCNATSubnet
	if kvm {
		raw = DefaultKVMNATSubnet
	}
	if AppConfig != nil {
		if kvm && AppConfig.KVMNATSubnet != "" {
			raw = AppConfig.KVMNATSubnet
		}
		if !kvm && AppConfig.LXCNATSubnet != "" {
			raw = AppConfig.LXCNATSubnet
		}
	}
	network, err := ParseNATNetwork(raw)
	if err == nil {
		return network
	}
	network, _ = ParseNATNetwork(map[bool]string{false: DefaultLXCNATSubnet, true: DefaultKVMNATSubnet}[kvm])
	return network
}

func configuredSubnetValue(current, envName, fallback string) string {
	raw := strings.TrimSpace(current)
	if envValue := strings.TrimSpace(os.Getenv(envName)); envValue != "" {
		raw = envValue
	}
	if network, err := ParseNATNetwork(raw); err == nil {
		return network.Subnet
	}
	network, _ := ParseNATNetwork(fallback)
	return network.Subnet
}

func isRFC1918Prefix(prefix netip.Prefix) bool {
	privateRanges := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("172.16.0.0/12"),
		netip.MustParsePrefix("192.168.0.0/16"),
	}
	for _, privateRange := range privateRanges {
		if privateRange.Contains(prefix.Addr()) {
			last := uint32IPv4(ipv4Uint32(prefix.Addr()) | uint32((uint64(1)<<(32-prefix.Bits()))-1))
			return privateRange.Contains(last)
		}
	}
	return false
}

func ipv4Uint32(addr netip.Addr) uint32 {
	bytes := addr.As4()
	return binary.BigEndian.Uint32(bytes[:])
}

func uint32IPv4(value uint32) netip.Addr {
	var bytes [4]byte
	binary.BigEndian.PutUint32(bytes[:], value)
	return netip.AddrFrom4(bytes)
}

func netmaskString(bits int) string {
	mask := uint32(0)
	if bits > 0 {
		mask = ^uint32(0) << (32 - bits)
	}
	return uint32IPv4(mask).String()
}
