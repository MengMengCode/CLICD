package config

import (
	"fmt"
	"net/netip"
	"regexp"
	"strings"
)

const DefaultIPv6NATSubnet = "fd42:636c:6963::/64"

type IPv6NATSettings struct {
	Enabled   bool   `json:"enabled"`
	Subnet    string `json:"subnet"`
	Interface string `json:"interface,omitempty"`
}

var ipv6NATInterfacePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,14}$`)

func NormalizeIPv6NATSettings(settings IPv6NATSettings) (IPv6NATSettings, error) {
	settings.Subnet = strings.TrimSpace(settings.Subnet)
	if settings.Subnet == "" {
		settings.Subnet = DefaultIPv6NATSubnet
	}
	prefix, err := netip.ParsePrefix(settings.Subnet)
	if err != nil || !prefix.Addr().Is6() || prefix.Addr().Is4In6() || prefix.Bits() != 64 || !prefix.Addr().IsPrivate() {
		return IPv6NATSettings{}, fmt.Errorf("IPv6 NAT subnet must be a ULA /64 prefix")
	}
	settings.Subnet = prefix.Masked().String()
	settings.Interface = strings.TrimSpace(settings.Interface)
	if settings.Interface != "" && (!ipv6NATInterfacePattern.MatchString(settings.Interface) || settings.Interface == "lo" || settings.Interface == "lxcbr0") {
		return IPv6NATSettings{}, fmt.Errorf("IPv6 NAT uplink must be a valid external interface name")
	}
	return settings, nil
}
