package lxc

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"clicd/internal/config"
)

func setupIPv6PortMappingTest(t *testing.T, persistent bool) {
	t.Helper()
	previous, endpoint := config.AppConfig, nat6PortEndpoint
	save, command := saveIPv6PortMappingConfig, runIPv6PortMappingCommand
	t.Cleanup(func() {
		config.AppConfig, nat6PortEndpoint = previous, endpoint
		saveIPv6PortMappingConfig, runIPv6PortMappingCommand = save, command
	})
	runIPv6PortMappingCommand = func(string, ...string) ([]byte, error) { return nil, nil }
	if persistent {
		saveIPv6PortMappingConfig = func() error { return nil }
	}
	config.AppConfig = &config.ClicdConfig{NATPortStart: 20000, NATPortEnd: 65535, IPv6NAT: config.IPv6NATSettings{Enabled: true}}
	nat6PortEndpoint = func(c *config.Container) (string, string, string, bool) {
		if c == nil || c.IsKVM() || c.IPv6NATAddress == "" || !config.AppConfig.IPv6NAT.Enabled {
			return "", "", "", false
		}
		return c.IPv6NATAddress, "2001:db8::10", "eth0", true
	}
}

func TestIPv6MappingSynchronizationAndIndependentCRUD(t *testing.T) {
	setupIPv6PortMappingTest(t, true)
	config.AppConfig.Containers = []config.Container{{ID: 1, Name: "dual", IPv6NATAddress: "fd00::2", PortMappingLimit: 2,
		PortMappings: []config.PortMapping{{HostPort: 30022, ContainerPort: 22, Protocol: "tcp", Description: "SSH"}, {HostPort: 30080, ContainerPort: 80, Protocol: "tcp"}}}}
	m := NewManager()
	state, err := GetIPv6PortMappings(1)
	if err != nil || !state.SyncIPv4 || len(state.Mappings) != 2 || len(config.AppConfig.Containers[0].IPv6PortMappings) != 0 {
		t.Fatalf("default synchronization did not derive IPv6 without extra quota entries: %+v %v", state, err)
	}
	if _, err := m.AddIPv6PortMapping(1, config.PortMapping{HostPort: 30443, ContainerPort: 443}); err == nil {
		t.Fatal("synchronized IPv6 inventory accepted an independent edit")
	}
	state, err = m.SetIPv6NATSync(1, false)
	if err != nil || state.SyncIPv4 || len(state.Mappings) != 2 {
		t.Fatalf("disabling synchronization did not retain existing IPv6 services: %+v %v", state, err)
	}
	if _, err := m.UpdateIPv6PortMapping(1, 1, config.PortMapping{HostPort: 30080, ContainerPort: 8080, Protocol: "tcp"}); err != nil {
		t.Fatal(err)
	}
	if config.AppConfig.Containers[0].PortMappings[1].ContainerPort != 80 {
		t.Fatal("independent IPv6 edit changed the IPv4 target")
	}
	if _, err := m.AddIPv6PortMapping(1, config.PortMapping{HostPort: 30443, ContainerPort: 443}); err == nil {
		t.Fatal("independent IPv6 quota was not enforced")
	}
	if _, err := m.DeleteIPv6PortMapping(1, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AddIPv6PortMapping(1, config.PortMapping{HostPort: 30443, ContainerPort: 443, Protocol: "udp"}); err != nil {
		t.Fatal(err)
	}
	state, err = m.SetIPv6NATSync(1, true)
	if err != nil || len(state.Mappings) != 2 || state.Mappings[1].HostPort != 30080 {
		t.Fatalf("enabling synchronization did not restore the IPv4-derived view: %+v %v", state, err)
	}
}

func TestIPv4MutationRejectsSharedIPv6PortConflictBeforeChangingIPv4(t *testing.T) {
	setupIPv6PortMappingTest(t, false)
	config.AppConfig.Containers = []config.Container{
		{ID: 1, Name: "one", IPv6NATAddress: "fd00::2", PortMappingLimit: 2, PublicIPv4s: []config.PublicIPv4Assignment{{Address: "198.51.100.10"}}},
		{ID: 2, Name: "two", IPv6NATAddress: "fd00::3", PortMappingLimit: 2, PublicIPv4s: []config.PublicIPv4Assignment{{Address: "198.51.100.11"}},
			PortMappings: []config.PortMapping{{HostIP: "198.51.100.11", HostPort: 30080, ContainerPort: 80, Protocol: "tcp"}}},
	}
	_, err := NewManager().AddPortMapping(1, config.PortMapping{HostIP: "198.51.100.10", HostPort: 30080, ContainerPort: 8080, Protocol: "tcp"})
	if err == nil || !strings.Contains(err.Error(), "IPv6") || len(config.AppConfig.Containers[0].PortMappings) != 0 {
		t.Fatalf("IPv4 mutation bypassed synchronized IPv6 conflict: %v", err)
	}
}

func TestIPv6SynchronizationRejectsDifferentIPv4TargetsOnOnePort(t *testing.T) {
	setupIPv6PortMappingTest(t, false)
	c := &config.Container{PortMappings: []config.PortMapping{
		{HostIP: "198.51.100.10", HostPort: 30080, ContainerPort: 80, Protocol: "tcp"},
		{HostIP: "198.51.100.11", HostPort: 30080, ContainerPort: 8080, Protocol: "tcp"},
	}}
	if _, err := effectiveIPv6Mappings(c, "2001:db8::10"); err == nil {
		t.Fatal("different IPv4 targets silently collapsed to the shared IPv6 port")
	}
	c.PortMappings[1].ContainerPort = 80
	if mappings, err := effectiveIPv6Mappings(c, "2001:db8::10"); err != nil || len(mappings) != 1 {
		t.Fatalf("identical IPv4 services were not safely deduplicated: %v %v", mappings, err)
	}
}

func TestNAT6OnlyMappingsDoNotCreateIPv4Ports(t *testing.T) {
	setupIPv6PortMappingTest(t, true)
	syncIPv4 := false
	config.AppConfig.Containers = []config.Container{{ID: 1, Name: "ipv6-only", IPv6NATAddress: "fd00::2", IPv6PortMappingLimit: 1, IPv6NATSyncIPv4: &syncIPv4}}
	if _, err := NewManager().AddIPv6PortMapping(1, config.PortMapping{HostPort: 30080, ContainerPort: 80}); err != nil {
		t.Fatal(err)
	}
	c := &config.AppConfig.Containers[0]
	if len(c.PortMappings) != 0 || c.PortMappingLimit != 0 || containerAllowsPublicIPv4Egress(c) {
		t.Fatal("IPv6-only mapping implicitly enabled IPv4 NAT")
	}
}

func TestIPv6NATRuleAndConntrackFamilies(t *testing.T) {
	mapping := config.PortMapping{HostIP: "2001:db8::10", HostPort: 30080, ContainerPort: 8080, Protocol: "tcp"}
	rules := ipv6PortMappingRuleArgs(12, "fd00::2", "2001:db8::10", mapping)
	if len(rules) != 3 {
		t.Fatalf("missing remote/host-local DNAT or translated ingress permission: %v", rules)
	}
	for _, args := range rules[:2] {
		joined := strings.Join(args, " ")
		for _, required := range []string{"-d 2001:db8::10/128", "--dport 30080", "--to-destination [fd00::2]:8080", "clicd-c12-nat6-port-tcp-30080"} {
			if !strings.Contains(joined, required) {
				t.Fatalf("IPv6 NAT rule missing %q: %v", required, args)
			}
		}
	}
	if forward := strings.Join(rules[2], " "); !strings.Contains(forward, "--ctstate DNAT") || !strings.Contains(forward, "--dport 8080") || !strings.Contains(forward, "-d fd00::2/128") {
		t.Fatalf("translated ingress was not restricted to the actual guest service: %s", forward)
	}
	want := []string{"-D", "-f", "ipv6", "--dst-nat", "--reply-src", "fd00::2", "-p", "tcp", "--dport", "30080", "--dst", "2001:db8::10/128", "--reply-port-src", "8080"}
	if got := mappingRuleConntrackArgs("ip6tables", mappingRule{"nat", "PREROUTING", rules[0][7:]}); !reflect.DeepEqual(got, want) {
		t.Fatalf("IPv6 cleanup could target another family: %v", got)
	}
	c := &config.Container{IPv6NATAddress: "fd00::2"}
	if got := firewallIPv6Addresses(c); !reflect.DeepEqual(got, []string{"fd00::2"}) {
		t.Fatalf("IPv6 firewall omits the translated guest address: %v", got)
	}
}

func TestIPv6PortConflictDoesNotConflateProtocolsOrFamilies(t *testing.T) {
	setupIPv6PortMappingTest(t, false)
	syncIPv4 := false
	config.AppConfig.Containers = []config.Container{
		{ID: 1, Name: "one", IPv6NATAddress: "fd00::2", IPv6NATSyncIPv4: &syncIPv4, IPv6PortMappingLimit: 2},
		{ID: 2, Name: "two", IPv6NATAddress: "fd00::3", IPv6NATSyncIPv4: &syncIPv4, IPv6PortMappingLimit: 2,
			IPv6PortMappings: []config.PortMapping{{HostPort: 30080, ContainerPort: 80, Protocol: "tcp"}}},
	}
	if IPv6HostPortAvailable(&config.AppConfig.Containers[0], 30080, "tcp") || !IPv6HostPortAvailable(&config.AppConfig.Containers[0], 30080, "udp") {
		t.Fatal("IPv6 port availability ignored TCP/UDP namespaces")
	}
	if !HostPortAvailable(&config.AppConfig.Containers[0], "", 30080, "tcp") {
		t.Fatal("independent IPv6 mapping consumed an IPv4 port")
	}
}

func TestIPv6SynchronizationChecksPendingCreateReservationFamily(t *testing.T) {
	setupIPv6PortMappingTest(t, false)
	previousReservations, previousOwners, previousMirrors := createNATReservations, activeCreateNATOwners, activeCreateNATMirrorsIPv6
	t.Cleanup(func() {
		createNATReservations, activeCreateNATOwners, activeCreateNATMirrorsIPv6 = previousReservations, previousOwners, previousMirrors
	})
	createNATReservations = map[uint64][]config.PortMapping{99: {{HostPort: 30080, Protocol: "tcp"}}}
	activeCreateNATOwners = map[uint64]string{99: "creating"}
	activeCreateNATMirrorsIPv6 = map[uint64]bool{99: true}
	config.AppConfig.Containers = []config.Container{{ID: 1, Name: "one", IPv6NATAddress: "fd00::2", PortMappingLimit: 1,
		PortMappings: []config.PortMapping{{HostPort: 30080, ContainerPort: 80, Protocol: "tcp"}}}}
	if err := ValidateIPv6NATPortMappings(); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("pending synchronized creation was not protected: %v", err)
	}
	activeCreateNATMirrorsIPv6[99] = false
	if err := ValidateIPv6NATPortMappings(); err != nil {
		t.Fatalf("independent IPv4-only reservation consumed an IPv6 port: %v", err)
	}
}

func TestIPv6SyncToggleConflictAndSaveFailureDoNotChangeConfiguration(t *testing.T) {
	setupIPv6PortMappingTest(t, true)
	syncIPv4 := false
	config.AppConfig.Containers = []config.Container{
		{ID: 1, Name: "one", IPv6NATAddress: "fd00::2", PortMappingLimit: 2, IPv6NATSyncIPv4: &syncIPv4,
			PortMappings: []config.PortMapping{{HostPort: 30080, ContainerPort: 80, Protocol: "tcp"}}},
		{ID: 2, Name: "two", IPv6NATAddress: "fd00::3", PortMappingLimit: 2, IPv6NATSyncIPv4: &syncIPv4,
			IPv6PortMappings: []config.PortMapping{{HostPort: 30080, ContainerPort: 8080, Protocol: "tcp"}}},
	}
	if _, err := NewManager().SetIPv6NATSync(1, true); err == nil || ipv6MappingsSyncIPv4(&config.AppConfig.Containers[0]) {
		t.Fatalf("failed global preflight changed the synchronization setting: %v", err)
	}
	config.AppConfig.Containers = config.AppConfig.Containers[:1]
	saveIPv6PortMappingConfig = func() error { return errors.New("disk full") }
	if _, err := NewManager().SetIPv6NATSync(1, true); err == nil || ipv6MappingsSyncIPv4(&config.AppConfig.Containers[0]) {
		t.Fatalf("failed persistence changed the synchronization setting: %v", err)
	}
}

func TestIPv6PortCleanupOwnsOnlyItsContainerAndPortRules(t *testing.T) {
	setupIPv6PortMappingTest(t, false)
	var deleted []string
	runIPv6PortMappingCommand = func(command string, args ...string) ([]byte, error) {
		joined := strings.Join(args, " ")
		if strings.Contains(joined, " -S ") {
			chain := args[len(args)-1]
			return []byte("-A " + chain + " -m comment --comment clicd-c12-nat6-port-tcp-30080 -j ACCEPT\n" +
				"-A " + chain + " -m comment --comment clicd-c123-nat6-port-tcp-30080 -j ACCEPT\n" +
				"-A " + chain + " -m comment --comment clicd-ct-12-nat6-core-egress -j ACCEPT\n" +
				"-A " + chain + " -m comment --comment clicd-c12-fw-default-in -j DROP\n"), nil
		}
		deleted = append(deleted, command+" "+joined)
		return nil, nil
	}
	if err := CleanIPv6PortMappings(12); err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 3 {
		t.Fatalf("cleanup did not visit both NAT chains and the filter chain: %v", deleted)
	}
	for _, command := range deleted {
		if strings.Contains(command, "clicd-c123-") || strings.Contains(command, "core-egress") || strings.Contains(command, "fw-default") {
			t.Fatalf("cleanup removed another owner's rule: %s", command)
		}
	}
}

func TestIPv6ConfigurationWaitsForNAT6OnlyCreation(t *testing.T) {
	setupIPv6PortMappingTest(t, false)
	oldActive, oldQueued := createNATReservations, queuedCreateNATReservations
	createNATReservations, queuedCreateNATReservations = map[uint64][]config.PortMapping{}, map[string][]config.PortMapping{}
	t.Cleanup(func() { createNATReservations, queuedCreateNATReservations = oldActive, oldQueued })
	off := false
	port, release, err := ReserveCreateNATPorts(ContainerConfig{Name: "nat6-only", AssignNAT: &off})
	if err != nil || port != 0 || len(createNATReservations) != 1 {
		t.Fatalf("IPv6-only creation must reserve coordination without IPv4 ports: %d %v", port, err)
	}
	defer release()
	if finish, err := BeginIPv6NATConfiguration(); err == nil {
		finish()
		t.Fatal("configuration bypassed an active IPv6-only creation")
	}
	release()
	finish, err := BeginIPv6NATConfiguration()
	if err != nil {
		t.Fatal(err)
	}
	defer finish()
	if _, _, err := ReserveCreateNATPorts(ContainerConfig{Name: "blocked", AssignNAT: &off}); err == nil {
		t.Fatal("creation entered while host IPv6 configuration was changing")
	}
	if err := ValidateIPv6NATPortMappingsUnlocked(); err != nil {
		t.Fatal(err)
	}
	finish()
	if IPv6NATConfigurationInProgress() {
		t.Fatal("configuration transition remained active")
	}
}

func TestIPv6StoppedMappingsAndUnavailableConntrackHaveNoSideEffects(t *testing.T) {
	setupIPv6PortMappingTest(t, false)
	calls := 0
	runIPv6PortMappingCommand = func(string, ...string) ([]byte, error) { calls++; return nil, nil }
	c := &config.Container{ID: 1, Status: "stopped", IPv6NATAddress: "fd00::2", PortMappingLimit: 1,
		PortMappings: []config.PortMapping{{HostPort: 30080, ContainerPort: 80, Protocol: "tcp"}}}
	if err := applyIPv6PortMappingsLocked(c); err != nil {
		t.Fatal(err)
	}
	clearMappingRuleConntrack("ip6tables", mappingRule{"nat", "PREROUTING", []string{"-j", "DNAT", "--to-destination", ""}}, runIPv6PortMappingCommand)
	if calls != 0 {
		t.Fatal("stopped guest installed DNAT or unavailable host triggered global conntrack deletion")
	}
}

func TestIPv6HostBootRestoresRunningNAT6RoutingWithStableIDs(t *testing.T) {
	setupIPv6PortMappingTest(t, false)
	clean, ports, routing := restorePortMappingOrphans, restoreContainerPortMappings, restoreContainerIPv6
	t.Cleanup(func() {
		restorePortMappingOrphans, restoreContainerPortMappings, restoreContainerIPv6 = clean, ports, routing
	})
	config.AppConfig.Containers = []config.Container{
		{ID: 1, Name: "first", Status: "running", IPv6NATAddress: "fd00::2"},
		{ID: 2, Name: "second", Status: "running", IPv6NATAddress: "fd00::3"},
	}
	restorePortMappingOrphans = func(*Manager) {}
	restoreContainerPortMappings = func(_ *Manager, id int) error {
		if id == 1 {
			copy(config.AppConfig.Containers, config.AppConfig.Containers[1:])
			config.AppConfig.Containers = config.AppConfig.Containers[:1]
		}
		return nil
	}
	var restored []int
	restoreContainerIPv6 = func(_ *Manager, id int) error { restored = append(restored, id); return nil }
	EnsureAllRunningPortMappings()
	if !reflect.DeepEqual(restored, []int{1, 2}) {
		t.Fatalf("boot restoration skipped IPv6 routing or used a shifted container identity: %v", restored)
	}
}
