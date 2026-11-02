package lxc

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"clicd/internal/config"
)

const nat6TestHost = `[{"ifname":"lxcbr0","addr_info":[]},{"ifname":"uplink0","addr_info":[{"local":"2606:4700:abcd::10","prefixlen":128}]}]`

func setupIPv6NATTest(t *testing.T) (*Manager, *[]string) {
	t.Helper()
	previous, command, save := config.AppConfig, runIPv6NATCommand, saveIPv6NATConfig
	applyPorts, cleanPorts, firewall, running := applyIPv6NATPorts, cleanIPv6NATPorts, applyIPv6NATFirewall, ipv6NATGuestRunning
	portCommand, endpoint := runIPv6PortMappingCommand, nat6PortEndpoint
	t.Cleanup(func() {
		config.AppConfig, runIPv6NATCommand, saveIPv6NATConfig = previous, command, save
		applyIPv6NATPorts, cleanIPv6NATPorts, applyIPv6NATFirewall, ipv6NATGuestRunning = applyPorts, cleanPorts, firewall, running
		runIPv6PortMappingCommand, nat6PortEndpoint = portCommand, endpoint
	})
	config.AppConfig = &config.ClicdConfig{IPv6NAT: config.IPv6NATSettings{Subnet: config.DefaultIPv6NATSubnet, Interface: "uplink0"}}
	var calls []string
	runIPv6NATCommand = func(name string, args ...string) ([]byte, error) {
		call := name + " " + strings.Join(args, " ")
		calls = append(calls, call)
		switch {
		case call == "ip -j -6 address show":
			return []byte(nat6TestHost), nil
		case call == "ip -j -6 route show":
			return []byte(`[]`), nil
		case strings.HasPrefix(call, "sysctl -n "):
			return []byte("0\n"), nil
		case strings.Contains(call, " -C "):
			return nil, errors.New("rule not found")
		default:
			return nil, nil
		}
	}
	saveIPv6NATConfig = func() error { return nil }
	applyIPv6NATPorts = func(*Manager, int) error { return nil }
	cleanIPv6NATPorts = func(*Manager, int) error { return nil }
	applyIPv6NATFirewall = func(int) error { return nil }
	ipv6NATGuestRunning = func(*Manager, *config.Container) bool { return false }
	runIPv6PortMappingCommand = func(string, ...string) ([]byte, error) { return nil, nil }
	nat6PortEndpoint = NAT6Endpoint
	return &Manager{LxcPath: t.TempDir()}, &calls
}

func TestIPv6NATUsesHost128WithoutAllocatingFromIt(t *testing.T) {
	_, _ = setupIPv6NATTest(t)
	config.AppConfig.IPv6NAT.Enabled = true
	status := GetIPv6NATStatus()
	if !status.Available || status.HostAddress != "2606:4700:abcd::10" || status.Interface != "uplink0" {
		t.Fatalf("/128 uplink rejected: %+v", status)
	}
	c := &config.Container{ID: 1}
	if err := prepareIPv6NATAddress(c); err != nil {
		t.Fatal(err)
	}
	guest, host, _, enabled := NAT6Endpoint(c)
	if !enabled || guest != "fd42:636c:6963::1001" || host != status.HostAddress {
		t.Fatalf("wrong NAT6 endpoint: guest=%s host=%s enabled=%v", guest, host, enabled)
	}
	for _, c := range []*config.Container{{ID: 2, Virtualization: config.VirtualizationKVM}, {ID: 3, LANIPv4Mode: config.LANIPv4ModeDHCP}} {
		if err := prepareIPv6NATAddress(c); err != nil || c.IPv6NATAddress != "" {
			t.Fatalf("unmanaged network was modified: %+v %v", c, err)
		}
	}
}

func TestIPv6NATRejectsOverlappingHostNetworks(t *testing.T) {
	_, _ = setupIPv6NATTest(t)
	previous := runIPv6NATCommand
	for _, route := range []bool{false, true} {
		runIPv6NATCommand = func(name string, args ...string) ([]byte, error) {
			call := name + " " + strings.Join(args, " ")
			if route && call == "ip -j -6 route show" {
				return []byte(`[{"dst":"fd42:636c:6963::/64","dev":"other0"}]`), nil
			}
			if !route && call == "ip -j -6 address show" {
				return []byte(`[{"ifname":"other0","addr_info":[{"local":"fd42:636c:6963::2","prefixlen":64}]}]`), nil
			}
			return previous(name, args...)
		}
		if GetIPv6NATStatus().Available {
			t.Fatalf("overlapping network was accepted (route=%v)", route)
		}
	}
}

func TestIPv6NATCommandsPreserveRAAndRollback(t *testing.T) {
	_, calls := setupIPv6NATTest(t)
	settings := currentIPv6NATSettings()
	host, err := inspectIPv6NATHost(settings)
	if err != nil {
		t.Fatal(err)
	}
	change := &ipv6NATChange{}
	if err := setupIPv6NATHost(settings, host, change); err != nil {
		t.Fatal(err)
	}
	c := &config.Container{ID: 1, IPv6NATAddress: "fd42:636c:6963::1001"}
	if err := setupIPv6NATContainer(c, host, change); err != nil {
		t.Fatal(err)
	}
	all := strings.Join(*calls, "\n")
	ra, forwarding := strings.Index(all, "uplink0/accept_ra=2"), strings.Index(all, "all/forwarding=1")
	if ra < 0 || forwarding < ra || !strings.Contains(all, "--to-source 2606:4700:abcd::10") || strings.Contains(all, "proxy_ndp") || strings.Contains(all, "neigh") {
		t.Fatalf("unsafe NAT66 command plan:\n%s", all)
	}
	if err := change.rollback(); err != nil {
		t.Fatal(err)
	}
	all = strings.Join(*calls, "\n")
	if !strings.Contains(all, "-D POSTROUTING") || !strings.Contains(all, "route del fd42:636c:6963::1001/128") || !strings.Contains(all, "uplink0/accept_ra=0") {
		t.Fatalf("new network state was not reversed:\n%s", all)
	}
}

func addNAT6Fixture(t *testing.T, m *Manager, c config.Container) {
	t.Helper()
	dir := filepath.Join(m.LxcPath, c.LxcName())
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte("lxc.net.0.type = veth\nlxc.net.0.link = lxcbr0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	config.AppConfig.Containers = append(config.AppConfig.Containers, c)
}

func TestConfigureIPv6NATPreflightsPortsBeforeNetworkChanges(t *testing.T) {
	m, calls := setupIPv6NATTest(t)
	for id := 1; id <= 2; id++ {
		addNAT6Fixture(t, m, config.Container{ID: id, Name: "guest", PortMappingLimit: 1, PortMappings: []config.PortMapping{{HostPort: 30080, ContainerPort: 80 + id, Protocol: "tcp"}}})
	}
	settings := currentIPv6NATSettings()
	settings.Enabled = true
	if _, err := m.ConfigureIPv6NAT(settings); err == nil {
		t.Fatal("conflicting IPv4 mappings were mirrored onto one IPv6 port")
	}
	for _, call := range *calls {
		if !strings.HasPrefix(call, "ip -j -6 ") {
			t.Fatalf("preflight failure modified host state: %s", call)
		}
	}
	if config.AppConfig.IPv6NAT.Enabled || config.AppConfig.Containers[0].IPv6NATAddress != "" {
		t.Fatal("preflight failure changed persistent candidate state")
	}
}

func TestConfigureIPv6NATRollsBackPersistenceFailure(t *testing.T) {
	m, calls := setupIPv6NATTest(t)
	addNAT6Fixture(t, m, config.Container{ID: 1, Name: "guest"})
	path := filepath.Join(m.LxcPath, "ct-1", "config")
	before, _ := os.ReadFile(path)
	saveIPv6NATConfig = func() error { return errors.New("disk full") }
	settings := currentIPv6NATSettings()
	settings.Enabled = true
	if _, err := m.ConfigureIPv6NAT(settings); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("persistence failure=%v", err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) || config.AppConfig.IPv6NAT.Enabled || config.AppConfig.Containers[0].IPv6NATAddress != "" {
		t.Fatal("failed enable left changed configuration")
	}
	all := strings.Join(*calls, "\n")
	if !strings.Contains(all, "-D INPUT") || !strings.Contains(all, "address del fd42:636c:6963::1/64") {
		t.Fatalf("host setup was not rolled back:\n%s", all)
	}
}

func TestNAT6OnlyQuotaDoesNotEnableIPv4NAT(t *testing.T) {
	no := false
	cfg := ContainerConfig{AssignNAT: &no, IPv6PortMappingCount: 4, PortMappingCount: 3, ExtraPorts: []int{80}}
	if err := cfg.NormalizeCreateNATMappings(); err != nil {
		t.Fatal(err)
	}
	if cfg.WantsNAT() || cfg.PortMappingCount != 0 || cfg.IPv6PortMappingCount != 4 || len(cfg.NATPortMappings) != 0 {
		t.Fatalf("NAT6-only request enabled IPv4 egress: %+v", cfg)
	}
	for _, invalid := range []int{-1, 65} {
		cfg.IPv6PortMappingCount = invalid
		if err := cfg.NormalizeCreateNATMappings(); err == nil {
			t.Fatalf("accepted quota %d", invalid)
		}
	}
}

func TestConfigureIPv6NATDoesNotAdoptExistingBridgeULA(t *testing.T) {
	m, _ := setupIPv6NATTest(t)
	previous := runIPv6NATCommand
	runIPv6NATCommand = func(name string, args ...string) ([]byte, error) {
		if name+" "+strings.Join(args, " ") == "ip -j -6 address show" {
			return []byte(strings.Replace(nat6TestHost, `"addr_info":[]`, `"addr_info":[{"local":"fd42:636c:6963::1","prefixlen":64}]`, 1)), nil
		}
		return previous(name, args...)
	}
	settings := currentIPv6NATSettings()
	settings.Enabled = true
	if _, err := m.ConfigureIPv6NAT(settings); err == nil || !strings.Contains(err.Error(), "already in use") {
		t.Fatalf("existing bridge gateway was adopted: %v", err)
	}
}

func TestConfigureIPv6NATDisableRemovesOwnedNDPOnly(t *testing.T) {
	m, calls := setupIPv6NATTest(t)
	config.AppConfig.IPv6NAT.Enabled = true
	previous := runIPv6NATCommand
	runIPv6NATCommand = func(name string, args ...string) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), " -C INPUT ") {
			*calls = append(*calls, name+" "+strings.Join(args, " "))
			return nil, nil
		}
		return previous(name, args...)
	}
	settings := currentIPv6NATSettings()
	settings.Enabled = false
	if _, err := m.ConfigureIPv6NAT(settings); err != nil {
		t.Fatal(err)
	}
	all := strings.Join(*calls, "\n")
	for _, kind := range []string{"135", "136"} {
		if !strings.Contains(all, "-D INPUT -i lxcbr0 -p ipv6-icmp --icmpv6-type "+kind) {
			t.Fatalf("owned NDP rule %s remained: %s", kind, all)
		}
	}
	if strings.Contains(all, "sysctl -w") {
		t.Fatal("disable reset shared forwarding/RA settings")
	}
}

func TestStoppedDiskImageDefersInitChangesUntilMountedStartSync(t *testing.T) {
	m, _ := setupIPv6NATTest(t)
	addNAT6Fixture(t, m, config.Container{ID: 1, Name: "guest"})
	c := &config.AppConfig.Containers[0]
	rootfs := filepath.Join(m.LxcPath, c.LxcName(), "rootfs")
	script := filepath.Join(rootfs, "usr", "local", "sbin", "clicd-ipv6-init")
	if err := os.MkdirAll(filepath.Dir(script), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("old disk init"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(rootfs), "rootfs.img"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	previous := ipv6NATRootfsMounted
	t.Cleanup(func() { ipv6NATRootfsMounted = previous })
	mounted := false
	ipv6NATRootfsMounted = func(string, string) bool { return mounted }
	settings := currentIPv6NATSettings()
	settings.Enabled = true
	if _, err := m.ConfigureIPv6NAT(settings); err != nil {
		t.Fatal(err)
	}
	settings.Enabled = false
	if _, err := m.ConfigureIPv6NAT(settings); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(script); err != nil || string(data) != "old disk init" {
		t.Fatalf("unmounted image mountpoint was changed: %q %v", data, err)
	}
	// Start calls this synchronization after ensureDiskImageMounted. Stale init
	// inside the now mounted OS must be removed even when hasLXCIPv6 is false.
	mounted = true
	if err := m.syncLXCIPv6Config(c); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(script); !os.IsNotExist(err) {
		t.Fatalf("stale NAT6 init survived mounted startup: %v", err)
	}
}
