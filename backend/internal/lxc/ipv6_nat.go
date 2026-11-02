package lxc

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"clicd/internal/config"
)

const NAT6BridgeName = "lxcbr0"

type IPv6NATStatus struct {
	Settings    config.IPv6NATSettings `json:"settings"`
	Available   bool                   `json:"available"`
	Reason      string                 `json:"reason"`
	Bridge      string                 `json:"bridge"`
	Gateway     string                 `json:"gateway"`
	HostAddress string                 `json:"host_address"`
	Interface   string                 `json:"interface"`
}

type ipv6NATHost struct {
	Address     string
	Interface   string
	Addresses   map[string]bool
	SubnetInUse bool
}

var ipv6NATMu sync.Mutex
var saveIPv6NATConfig = config.SaveConfig
var applyIPv6NATPorts = (*Manager).ApplyIPv6PortMappings
var cleanIPv6NATPorts = (*Manager).CleanIPv6PortMappings
var applyIPv6NATFirewall = ApplyFirewallRules
var ipv6NATRootfsMounted = diskImageMounted
var ipv6NATGuestRunning = func(m *Manager, c *config.Container) bool {
	status, _ := m.GetContainerStatus(c.LxcName())
	return status == "running"
}
var runIPv6NATCommand = func(name string, args ...string) ([]byte, error) {
	output, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func currentIPv6NATSettings() config.IPv6NATSettings {
	settings := config.IPv6NATSettings{}
	if config.AppConfig != nil {
		settings = config.AppConfig.IPv6NAT
	}
	normalized, err := config.NormalizeIPv6NATSettings(settings)
	if err != nil {
		return settings
	}
	return normalized
}

func GetIPv6NATStatus() IPv6NATStatus {
	settings := currentIPv6NATSettings()
	status := IPv6NATStatus{Settings: settings, Bridge: NAT6BridgeName, Gateway: ipv6GatewayLinkLocal}
	host, err := inspectIPv6NATHost(settings)
	if err != nil {
		status.Reason = err.Error()
		return status
	}
	status.Available = true
	status.Interface, status.HostAddress = host.Interface, host.Address
	status.Reason = "host IPv6 address is usable for NAT66; a delegated prefix is not required"
	return status
}

// NAT6Endpoint returns only separately assigned ULA addresses. A host /128 is
// an egress/bind address, never a prefix from which guest addresses are allocated.
func NAT6Endpoint(c *config.Container) (guestIPv6, hostIPv6, uplink string, enabled bool) {
	settings := currentIPv6NATSettings()
	if !settings.Enabled || !ipv6NATEligible(c) || c.IPv6NATAddress == "" {
		return "", "", "", false
	}
	prefix, err := netip.ParsePrefix(settings.Subnet)
	address, addressErr := netip.ParseAddr(c.IPv6NATAddress)
	if err != nil || addressErr != nil || !address.Is6() || !prefix.Contains(address) {
		return "", "", "", false
	}
	host, err := inspectIPv6NATHost(settings)
	if err != nil {
		return "", "", "", false
	}
	return address.String(), host.Address, host.Interface, true
}

func ipv6NATEligible(c *config.Container) bool {
	return c != nil && !c.IsKVM() && !c.UsesLANIPv4()
}

func lxcIPv6AddressStrings(c *config.Container) []string {
	addresses := c.IPv6AddressStrings()
	if c.IPv6NATAddress != "" {
		addresses = append(addresses, c.IPv6NATAddress)
	}
	return normalizeIPv6List(addresses)
}

func hasLXCIPv6(c *config.Container) bool {
	return c != nil && (c.IPv6 != "" || len(c.IPv6Addresses) > 0 || c.IPv6NATAddress != "")
}

func (m *Manager) ipv6RootfsAvailable(c *config.Container) bool {
	dir := filepath.Join(m.LxcPath, c.LxcName())
	rootfs := filepath.Join(dir, "rootfs")
	if info, err := os.Stat(rootfs); err != nil || !info.IsDir() {
		return false
	}
	if _, err := os.Stat(filepath.Join(dir, "rootfs.img")); err == nil {
		return ipv6NATRootfsMounted(c.LxcName(), rootfs)
	}
	return true
}

func (m *Manager) syncLXCIPv6Config(c *config.Container) error {
	snapshot := *c
	c = &snapshot
	if err := m.applyIPv6Config(c.LxcName(), lxcIPv6AddressStrings(c)...); err != nil {
		return err
	}
	if !m.ipv6RootfsAvailable(c) {
		return nil
	}
	rootfs := filepath.Join(m.LxcPath, c.LxcName(), "rootfs")
	if hasLXCIPv6(c) {
		return installContainerIPv6Init(rootfs, lxcIPv6AddressStrings(c)...)
	}
	return removeContainerIPv6Init(rootfs)
}

func ipv6NATAddress(settings config.IPv6NATSettings, id int) (string, error) {
	if id <= 0 {
		return "", fmt.Errorf("IPv6 NAT requires a stable positive container ID")
	}
	prefix, err := netip.ParsePrefix(settings.Subnet)
	if err != nil {
		return "", err
	}
	address, err := ipv6Add(prefix.Masked().Addr(), uint64(id)+0x1000)
	if err != nil || !prefix.Contains(address) {
		return "", fmt.Errorf("IPv6 NAT address space is exhausted")
	}
	return address.String(), nil
}

func prepareIPv6NATAddress(c *config.Container) error {
	ipv6NATMu.Lock()
	defer ipv6NATMu.Unlock()
	settings := currentIPv6NATSettings()
	if !settings.Enabled || !ipv6NATEligible(c) {
		return nil
	}
	address, err := ipv6NATAddress(settings, c.ID)
	if err != nil {
		return err
	}
	if c.IPv6NATAddress != "" && c.IPv6NATAddress != address {
		return fmt.Errorf("existing IPv6 NAT address differs from configured subnet; disable NAT6 before changing its subnet")
	}
	c.IPv6NATAddress = address
	return nil
}

func inspectIPv6NATHost(settings config.IPv6NATSettings) (ipv6NATHost, error) {
	settings, err := config.NormalizeIPv6NATSettings(settings)
	if err != nil {
		return ipv6NATHost{}, err
	}
	prefix, _ := netip.ParsePrefix(settings.Subnet)
	output, err := runIPv6NATCommand("ip", "-j", "-6", "address", "show")
	if err != nil {
		return ipv6NATHost{}, err
	}
	var interfaces []struct {
		Name      string `json:"ifname"`
		Addresses []struct {
			Local     string   `json:"local"`
			PrefixLen int      `json:"prefixlen"`
			Flags     []string `json:"flags"`
		} `json:"addr_info"`
	}
	if err := json.Unmarshal(output, &interfaces); err != nil {
		return ipv6NATHost{}, fmt.Errorf("failed to inspect host IPv6 addresses: %w", err)
	}
	output, err = runIPv6NATCommand("ip", "-j", "-6", "route", "show")
	if err != nil {
		return ipv6NATHost{}, err
	}
	var routes []struct {
		Destination string `json:"dst"`
		Interface   string `json:"dev"`
		Source      string `json:"prefsrc"`
		Metric      int    `json:"metric"`
	}
	if err := json.Unmarshal(output, &routes); err != nil {
		return ipv6NATHost{}, fmt.Errorf("failed to inspect host IPv6 routes: %w", err)
	}
	bridgeSubnetInUse := false
	for _, route := range routes {
		if other, err := netip.ParsePrefix(route.Destination); err == nil && other.Bits() > 0 && prefix.Overlaps(other) {
			if route.Interface != NAT6BridgeName {
				return ipv6NATHost{}, fmt.Errorf("IPv6 NAT subnet conflicts with route %s on %s", route.Destination, route.Interface)
			}
			bridgeSubnetInUse = true
		}
	}
	uplink := settings.Interface
	if uplink == "" {
		sort.SliceStable(routes, func(i, j int) bool { return routes[i].Metric < routes[j].Metric })
		for _, route := range routes {
			if route.Destination == "default" && route.Interface != "" && route.Interface != NAT6BridgeName && route.Interface != "lo" {
				uplink = route.Interface
				break
			}
		}
	}
	if uplink == "" {
		return ipv6NATHost{}, fmt.Errorf("no IPv6 uplink detected; choose an interface that owns a public IPv6 address")
	}
	host := ipv6NATHost{Interface: uplink, Addresses: make(map[string]bool), SubnetInUse: bridgeSubnetInUse}
	bridgeFound := false
	for _, iface := range interfaces {
		if iface.Name == NAT6BridgeName {
			bridgeFound = true
		}
		for _, item := range iface.Addresses {
			address, err := netip.ParseAddr(item.Local)
			if err != nil || !address.Is6() {
				continue
			}
			if iface.Name == NAT6BridgeName {
				host.Addresses[address.String()] = true
				if prefix.Contains(address) {
					host.SubnetInUse = true
				}
			} else if prefix.Overlaps(netip.PrefixFrom(address, item.PrefixLen)) {
				return ipv6NATHost{}, fmt.Errorf("IPv6 NAT subnet conflicts with address %s on %s", item.Local, iface.Name)
			}
			flags := " " + strings.Join(item.Flags, " ") + " "
			if iface.Name == uplink && isPublicIPv6(address) && !strings.Contains(flags, " tentative ") && !strings.Contains(flags, " deprecated ") {
				if host.Address == "" || !strings.Contains(flags, " temporary ") {
					host.Address = address.String()
				}
			}
		}
	}
	if !bridgeFound {
		return ipv6NATHost{}, fmt.Errorf("managed LXC bridge %s is not available", NAT6BridgeName)
	}
	if host.Address == "" {
		return ipv6NATHost{}, fmt.Errorf("interface %s has no usable public IPv6 address (a single /128 is supported)", uplink)
	}
	return host, nil
}

type ipv6NATChange struct{ undos []func() error }

func (change *ipv6NATChange) perform(name string, args []string, undo func() error) error {
	if _, err := runIPv6NATCommand(name, args...); err != nil {
		return err
	}
	if undo != nil {
		change.undos = append(change.undos, undo)
	}
	return nil
}

func (change *ipv6NATChange) rollback() error {
	var failures []error
	for i := len(change.undos) - 1; i >= 0; i-- {
		if err := change.undos[i](); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (change *ipv6NATChange) sysctl(key, value string) error {
	output, err := runIPv6NATCommand("sysctl", "-n", key)
	if err != nil {
		return err
	}
	previous := strings.TrimSpace(string(output))
	if previous == value {
		return nil
	}
	return change.perform("sysctl", []string{"-w", key + "=" + value}, func() error {
		_, err := runIPv6NATCommand("sysctl", "-w", key+"="+previous)
		return err
	})
}

func (change *ipv6NATChange) rule(table, chain string, args []string) error {
	base := []string{"-w", "5", "-t", table}
	if _, err := runIPv6NATCommand("ip6tables", append(append([]string{}, base...), append([]string{"-C", chain}, args...)...)...); err == nil {
		return nil
	}
	return change.perform("ip6tables", append(append([]string{}, base...), append([]string{"-I", chain, "1"}, args...)...), func() error {
		_, err := runIPv6NATCommand("ip6tables", append(append([]string{}, base...), append([]string{"-D", chain}, args...)...)...)
		return err
	})
}

func (change *ipv6NATChange) removeRule(table, chain string, args []string) error {
	base := []string{"-w", "5", "-t", table}
	if _, err := runIPv6NATCommand("ip6tables", append(append([]string{}, base...), append([]string{"-C", chain}, args...)...)...); err != nil {
		return nil
	}
	return change.perform("ip6tables", append(append([]string{}, base...), append([]string{"-D", chain}, args...)...), func() error {
		_, err := runIPv6NATCommand("ip6tables", append(append([]string{}, base...), append([]string{"-I", chain, "1"}, args...)...)...)
		return err
	})
}

func setupIPv6NATHost(settings config.IPv6NATSettings, host ipv6NATHost, change *ipv6NATChange) error {
	// Enabling forwarding changes RA handling. Preserve the uplink's router
	// advertisements before enabling it, including on single-address VPS hosts.
	for _, setting := range [][2]string{
		{"net/ipv6/conf/" + host.Interface + "/accept_ra", "2"},
		{"net/ipv6/conf/all/forwarding", "1"},
		{"net/ipv6/conf/" + NAT6BridgeName + "/disable_ipv6", "0"},
		{"net/ipv6/conf/" + NAT6BridgeName + "/accept_ra", "0"},
	} {
		if err := change.sysctl(setting[0], setting[1]); err != nil {
			return err
		}
	}
	if err := change.perform("ip", []string{"link", "set", NAT6BridgeName, "up"}, nil); err != nil {
		return err
	}
	prefix, _ := netip.ParsePrefix(settings.Subnet)
	gateway, _ := ipv6Add(prefix.Addr(), 1)
	for _, address := range []string{ipv6GatewayLinkLocal + "/64", gateway.String() + "/64"} {
		plain := strings.Split(address, "/")[0]
		if host.Addresses[plain] {
			continue
		}
		address := address
		if err := change.perform("ip", []string{"-6", "address", "add", address, "dev", NAT6BridgeName}, func() error {
			_, err := runIPv6NATCommand("ip", "-6", "address", "del", address, "dev", NAT6BridgeName)
			return err
		}); err != nil {
			return err
		}
	}
	for _, kind := range []string{"135", "136"} {
		if err := change.rule("filter", "INPUT", []string{"-i", NAT6BridgeName, "-p", "ipv6-icmp", "--icmpv6-type", kind, "-m", "comment", "--comment", "clicd-nat6-ndp-" + kind, "-j", "ACCEPT"}); err != nil {
			return err
		}
	}
	return nil
}

func ipv6NATTag(id int) string { return "clicd-ct-" + strconv.Itoa(id) + "-nat6-core-" }

func setupIPv6NATContainer(c *config.Container, host ipv6NATHost, change *ipv6NATChange) error {
	address := c.IPv6NATAddress + "/128"
	if output, err := runIPv6NATCommand("ip", "-6", "route", "show", "exact", address); err != nil || !strings.Contains(string(output), "dev "+NAT6BridgeName) {
		if err := change.perform("ip", []string{"-6", "route", "replace", address, "dev", NAT6BridgeName}, func() error {
			_, err := runIPv6NATCommand("ip", "-6", "route", "del", address, "dev", NAT6BridgeName)
			return err
		}); err != nil {
			return err
		}
	}
	tag := ipv6NATTag(c.ID)
	if err := change.rule("nat", "POSTROUTING", []string{"-s", address, "-o", host.Interface, "-m", "comment", "--comment", tag + "egress", "-j", "SNAT", "--to-source", host.Address}); err != nil {
		return err
	}
	for _, rule := range [][]string{
		{"-i", NAT6BridgeName, "-o", host.Interface, "-s", address, "-m", "comment", "--comment", tag + "out", "-j", "ACCEPT"},
		{"-i", host.Interface, "-o", NAT6BridgeName, "-d", address, "-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED", "-m", "comment", "--comment", tag + "return", "-j", "ACCEPT"},
	} {
		if err := change.rule("filter", "FORWARD", rule); err != nil {
			return err
		}
	}
	return nil
}

func removeIPv6NATCore(c *config.Container) error {
	if c == nil || c.IPv6NATAddress == "" {
		return nil
	}
	var failures []error
	for _, table := range []string{"nat", "filter"} {
		output, err := runIPv6NATCommand("ip6tables", "-w", "5", "-t", table, "-S")
		if err != nil {
			failures = append(failures, err)
			continue
		}
		for _, line := range strings.Split(string(output), "\n") {
			args := strings.Fields(line)
			owned := false
			for i, value := range args {
				args[i] = strings.Trim(value, "\"")
				if strings.HasPrefix(args[i], ipv6NATTag(c.ID)) && i > 0 && args[i-1] == "--comment" {
					owned = true
				}
			}
			if !owned || len(args) < 2 || args[0] != "-A" {
				continue
			}
			args[0] = "-D"
			if _, err := runIPv6NATCommand("ip6tables", append([]string{"-w", "5", "-t", table}, args...)...); err != nil {
				failures = append(failures, err)
			}
		}
	}
	if output, err := runIPv6NATCommand("ip", "-6", "route", "show", "exact", c.IPv6NATAddress+"/128"); err == nil && strings.Contains(string(output), "dev "+NAT6BridgeName) {
		if _, err := runIPv6NATCommand("ip", "-6", "route", "del", c.IPv6NATAddress+"/128", "dev", NAT6BridgeName); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (m *Manager) applyIPv6NAT(c *config.Container) error {
	snapshot := *c
	c = &snapshot
	if c.IPv6NATAddress == "" || !ipv6NATEligible(c) || !currentIPv6NATSettings().Enabled {
		return nil
	}
	ipv6NATMu.Lock()
	defer ipv6NATMu.Unlock()
	return m.applyIPv6NATLocked(c)
}

func (m *Manager) applyIPv6NATLocked(c *config.Container) error {
	host, err := inspectIPv6NATHost(currentIPv6NATSettings())
	if err != nil {
		return err
	}
	change := &ipv6NATChange{}
	iptablesMu.Lock()
	err = setupIPv6NATHost(currentIPv6NATSettings(), host, change)
	if err == nil {
		err = setupIPv6NATContainer(c, host, change)
	}
	iptablesMu.Unlock()
	if err != nil {
		return errors.Join(err, change.rollback())
	}
	if err := m.syncLXCIPv6Config(c); err != nil {
		return errors.Join(err, change.rollback())
	}
	if err := updateIPv6NATGuest(m, c, c.IPv6NATAddress, true); err != nil {
		return errors.Join(err, change.rollback())
	}
	if err := applyIPv6NATPorts(m, c.ID); err != nil {
		return errors.Join(err, cleanIPv6NATPorts(m, c.ID), change.rollback())
	}
	return nil
}

func updateIPv6NATGuest(m *Manager, c *config.Container, address string, add bool) error {
	if !ipv6NATGuestRunning(m, c) {
		return nil
	}
	if add {
		_, err := runIPv6NATCommand("lxc-attach", "-n", c.LxcName(), "--", "sh", "-c", fmt.Sprintf("ip -6 addr replace %s/128 dev eth0 && ip -6 route replace default via %s dev eth0 metric 100", shellQuote(address), shellQuote(ipv6GatewayLinkLocal)))
		return err
	}
	_, err := runIPv6NATCommand("lxc-attach", "-n", c.LxcName(), "--", "ip", "-6", "addr", "del", address+"/128", "dev", "eth0")
	if err != nil && !strings.Contains(err.Error(), "Cannot assign requested address") {
		return err
	}
	if c.IPv6 == "" && len(c.IPv6Addresses) == 0 {
		_, err = runIPv6NATCommand("lxc-attach", "-n", c.LxcName(), "--", "ip", "-6", "route", "del", "default", "via", ipv6GatewayLinkLocal, "dev", "eth0", "metric", "100")
		if err != nil && !strings.Contains(err.Error(), "No such process") {
			return err
		}
	}
	return nil
}

func (m *Manager) cleanupIPv6NAT(c *config.Container) error {
	if c == nil || c.IPv6NATAddress == "" {
		return nil
	}
	snapshot := *c
	c = &snapshot
	ipv6NATMu.Lock()
	defer ipv6NATMu.Unlock()
	if err := cleanIPv6NATPorts(m, c.ID); err != nil {
		return err
	}
	iptablesMu.Lock()
	defer iptablesMu.Unlock()
	return removeIPv6NATCore(c)
}

type ipv6NATFile struct {
	path   string
	data   []byte
	mode   os.FileMode
	link   string
	exists bool
}

func captureIPv6NATFiles(m *Manager, c *config.Container) ([]ipv6NATFile, error) {
	rootfs := filepath.Join(m.LxcPath, c.LxcName(), "rootfs")
	paths := []string{filepath.Join(m.LxcPath, c.LxcName(), "config")}
	for _, relative := range []string{
		"usr/local/sbin/clicd-ipv6-init", "etc/systemd/system/clicd-ipv6.service",
		"etc/systemd/system/multi-user.target.wants/clicd-ipv6.service", "etc/init.d/clicd-ipv6",
		"etc/runlevels/default/clicd-ipv6", "etc/rc2.d/S99clicd-ipv6", "etc/rc3.d/S99clicd-ipv6",
		"etc/rc4.d/S99clicd-ipv6", "etc/rc5.d/S99clicd-ipv6",
	} {
		if m.ipv6RootfsAvailable(c) {
			paths = append(paths, filepath.Join(rootfs, filepath.FromSlash(relative)))
		}
	}
	files := make([]ipv6NATFile, 0, len(paths))
	for _, path := range paths {
		file := ipv6NATFile{path: path}
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			files = append(files, file)
			continue
		}
		if err != nil {
			return nil, err
		}
		file.exists, file.mode = true, info.Mode()
		if info.Mode()&os.ModeSymlink != 0 {
			file.link, err = os.Readlink(path)
		} else {
			file.data, err = os.ReadFile(path)
		}
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	return files, nil
}

func restoreIPv6NATFiles(files []ipv6NATFile) error {
	var failures []error
	for _, file := range files {
		var err error
		if !file.exists {
			err = os.Remove(file.path)
			if os.IsNotExist(err) {
				err = nil
			}
		} else if file.link != "" {
			_ = os.Remove(file.path)
			err = os.Symlink(file.link, file.path)
		} else {
			err = os.WriteFile(file.path, file.data, file.mode.Perm())
		}
		if err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// ConfigureIPv6NAT validates the complete candidate inventory before touching
// host networking. Subnet/uplink changes require an explicit disable first.
func (m *Manager) ConfigureIPv6NAT(settings config.IPv6NATSettings) (IPv6NATStatus, error) {
	ipv6NATMu.Lock()
	defer ipv6NATMu.Unlock()
	settings, err := config.NormalizeIPv6NATSettings(settings)
	if err != nil {
		return IPv6NATStatus{}, err
	}
	if config.AppConfig == nil {
		return IPv6NATStatus{}, fmt.Errorf("configuration is not initialized")
	}
	finish, err := BeginIPv6NATConfiguration()
	if err != nil {
		return IPv6NATStatus{}, err
	}
	defer finish()
	previousSettings := config.AppConfig.IPv6NAT
	previousNormalized := currentIPv6NATSettings()
	if previousSettings.Enabled && settings.Enabled && (previousNormalized.Subnet != settings.Subnet || previousNormalized.Interface != settings.Interface) {
		return IPv6NATStatus{}, fmt.Errorf("disable IPv6 NAT before changing its subnet or uplink")
	}
	type candidate struct {
		id                     int
		oldAddress, newAddress string
		files                  []ipv6NATFile
	}
	var candidates []candidate
	for _, c := range config.AppConfig.Containers {
		if !ipv6NATEligible(&c) {
			continue
		}
		entry := candidate{id: c.ID, oldAddress: c.IPv6NATAddress}
		if settings.Enabled {
			entry.newAddress, err = ipv6NATAddress(settings, c.ID)
			if err != nil {
				return IPv6NATStatus{}, err
			}
		}
		entry.files, err = captureIPv6NATFiles(m, &c)
		if err != nil {
			return IPv6NATStatus{}, err
		}
		if _, err := os.Stat(filepath.Join(m.LxcPath, c.LxcName(), "config")); err != nil {
			return IPv6NATStatus{}, fmt.Errorf("cannot configure IPv6 NAT for %s: %w", c.Name, err)
		}
		candidates = append(candidates, entry)
	}
	restoreMemory := func() {
		config.AppConfig.IPv6NAT = previousSettings
		for _, entry := range candidates {
			if c := config.FindContainer(entry.id); c != nil {
				c.IPv6NATAddress = entry.oldAddress
			}
		}
	}
	config.AppConfig.IPv6NAT = settings
	for _, entry := range candidates {
		if c := config.FindContainer(entry.id); c != nil {
			c.IPv6NATAddress = entry.newAddress
		}
	}
	err = ValidateIPv6NATPortMappingsUnlocked()
	restoreMemory()
	if err != nil {
		return IPv6NATStatus{}, err
	}
	var host ipv6NATHost
	if settings.Enabled {
		host, err = inspectIPv6NATHost(settings)
		if err != nil {
			return IPv6NATStatus{}, err
		}
		if !previousSettings.Enabled && host.SubnetInUse {
			return IPv6NATStatus{}, fmt.Errorf("IPv6 NAT subnet is already in use on %s; choose an unused ULA prefix", NAT6BridgeName)
		}
		if _, err := runIPv6NATCommand("ip6tables", "-w", "5", "-t", "nat", "-S"); err != nil {
			return IPv6NATStatus{}, fmt.Errorf("IPv6 NAT table is unavailable: %w", err)
		}
	}
	change := &ipv6NATChange{}
	var touched []int
	rollback := func(cause error) (IPv6NATStatus, error) {
		var failures []error
		for _, id := range touched {
			if c := config.FindContainer(id); c != nil && c.IPv6NATAddress != "" {
				snapshot := *c
				c = &snapshot
				if err := cleanIPv6NATPorts(m, id); err != nil {
					failures = append(failures, err)
				}
				if !previousSettings.Enabled {
					iptablesMu.Lock()
					if err := removeIPv6NATCore(c); err != nil {
						failures = append(failures, err)
					}
					iptablesMu.Unlock()
					if err := updateIPv6NATGuest(m, c, c.IPv6NATAddress, false); err != nil {
						failures = append(failures, err)
					}
				}
			}
		}
		restoreMemory()
		if err := change.rollback(); err != nil {
			failures = append(failures, err)
		}
		for _, entry := range candidates {
			if err := restoreIPv6NATFiles(entry.files); err != nil {
				failures = append(failures, err)
			}
		}
		if previousSettings.Enabled {
			for _, id := range touched {
				if c := config.FindContainer(id); c != nil {
					snapshot := *c
					if err := m.applyIPv6NATLocked(&snapshot); err != nil {
						failures = append(failures, err)
					}
				}
			}
		}
		if len(failures) > 0 {
			cause = errors.Join(cause, fmt.Errorf("IPv6 NAT rollback was incomplete: %w", errors.Join(failures...)))
		}
		return IPv6NATStatus{}, cause
	}
	if settings.Enabled {
		iptablesMu.Lock()
		err = setupIPv6NATHost(settings, host, change)
		iptablesMu.Unlock()
		if err != nil {
			return rollback(err)
		}
	}
	// On disable, clean mappings while the old endpoint is still available for
	// precise conntrack cleanup. Only then clear the address/configuration.
	if !settings.Enabled {
		for _, entry := range candidates {
			c := config.FindContainer(entry.id)
			if c == nil || entry.oldAddress == "" {
				continue
			}
			snapshot := *c
			c = &snapshot
			touched = append(touched, c.ID)
			if err := cleanIPv6NATPorts(m, c.ID); err != nil {
				return rollback(err)
			}
			iptablesMu.Lock()
			err = removeIPv6NATCore(c)
			iptablesMu.Unlock()
			if err != nil {
				return rollback(err)
			}
			if err := updateIPv6NATGuest(m, c, entry.oldAddress, false); err != nil {
				return rollback(err)
			}
		}
	}
	config.AppConfig.IPv6NAT = settings
	for _, entry := range candidates {
		c := config.FindContainer(entry.id)
		if c == nil {
			continue
		}
		c.IPv6NATAddress = entry.newAddress
		snapshot := *c
		c = &snapshot
		if settings.Enabled {
			touched = append(touched, c.ID)
		}
		if err := m.syncLXCIPv6Config(c); err != nil {
			return rollback(err)
		}
		if settings.Enabled {
			if err := m.applyIPv6NATLocked(c); err != nil {
				return rollback(err)
			}
		} else if err := applyIPv6NATFirewall(c.ID); err != nil {
			return rollback(err)
		}
	}
	if !settings.Enabled && previousSettings.Enabled {
		for _, kind := range []string{"135", "136"} {
			if err := change.removeRule("filter", "INPUT", []string{"-i", NAT6BridgeName, "-p", "ipv6-icmp", "--icmpv6-type", kind, "-m", "comment", "--comment", "clicd-nat6-ndp-" + kind, "-j", "ACCEPT"}); err != nil {
				return rollback(err)
			}
		}
		prefix, _ := netip.ParsePrefix(previousNormalized.Subnet)
		gateway, _ := ipv6Add(prefix.Addr(), 1)
		if output, err := runIPv6NATCommand("ip", "-6", "address", "show", "dev", NAT6BridgeName); err == nil && strings.Contains(string(output), gateway.String()+"/64") {
			address := gateway.String() + "/64"
			if err := change.perform("ip", []string{"-6", "address", "del", address, "dev", NAT6BridgeName}, func() error {
				_, err := runIPv6NATCommand("ip", "-6", "address", "add", address, "dev", NAT6BridgeName)
				return err
			}); err != nil {
				return rollback(err)
			}
		}
		// The link-local gateway and forwarding sysctls may also serve routed
		// IPv6 guests. Do not disable those shared host facilities here.
	}
	if err := saveIPv6NATConfig(); err != nil {
		return rollback(err)
	}
	return GetIPv6NATStatus(), nil
}
