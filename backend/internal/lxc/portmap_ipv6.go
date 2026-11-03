package lxc

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"clicd/internal/config"
)

// Keep validation and changes to both port inventories in one critical section.
var portMappingMutationMu sync.Mutex
var ipv6NATConfigurationChanging atomic.Bool

var nat6PortEndpoint = NAT6Endpoint
var saveIPv6PortMappingConfig = config.SaveConfig
var runIPv6PortMappingCommand = func(command string, args ...string) ([]byte, error) {
	return exec.Command(command, args...).CombinedOutput()
}

type IPv6PortMappingState struct {
	Enabled   bool                 `json:"enabled"`
	SyncIPv4  bool                 `json:"sync_ipv4"`
	HostIPv6  string               `json:"host_ipv6"`
	GuestIPv6 string               `json:"guest_ipv6"`
	Limit     int                  `json:"limit"`
	Mappings  []config.PortMapping `json:"mappings"`
}

func ipv6MappingsSyncIPv4(c *config.Container) bool {
	return c != nil && (c.IPv6NATSyncIPv4 == nil || *c.IPv6NATSyncIPv4)
}

func ipv6MappingLimit(c *config.Container) int {
	if c.IPv6PortMappingLimit > 0 {
		return c.IPv6PortMappingLimit
	}
	return c.PortMappingLimit
}

func GetIPv6PortMappings(id int) (IPv6PortMappingState, error) {
	c := config.FindContainer(id)
	if c == nil {
		return IPv6PortMappingState{}, fmt.Errorf("container not found: %d", id)
	}
	snapshot := *c
	c = &snapshot
	guest, host, _, enabled := nat6PortEndpoint(c)
	state := IPv6PortMappingState{Enabled: enabled, SyncIPv4: ipv6MappingsSyncIPv4(c),
		HostIPv6: host, GuestIPv6: guest, Limit: ipv6MappingLimit(c), Mappings: []config.PortMapping{}}
	if enabled {
		var err error
		state.Mappings, err = effectiveIPv6Mappings(c, host)
		if err != nil {
			return state, err
		}
	}
	return state, nil
}

// effectiveIPv6Mappings is a view, never a second copy of IPv4 quota entries.
func effectiveIPv6Mappings(c *config.Container, host string) ([]config.PortMapping, error) {
	mappings := c.IPv6PortMappings
	if ipv6MappingsSyncIPv4(c) {
		mappings = c.PortMappings
	}
	result := make([]config.PortMapping, 0, len(mappings))
	for _, mapping := range mappings {
		mapping.HostIP = host
		mapping.Protocol = strings.ToLower(strings.TrimSpace(mapping.Protocol))
		if mapping.Protocol == "" {
			mapping.Protocol = "tcp"
		}
		if mapping.Protocol != "tcp" && mapping.Protocol != "udp" {
			return nil, fmt.Errorf("IPv6 NAT supports TCP or UDP mappings only")
		}
		if mapping.HostPort < 1 || mapping.HostPort > 65535 || mapping.ContainerPort < 1 || mapping.ContainerPort > 65535 {
			return nil, fmt.Errorf("IPv6 NAT ports must be 1-65535")
		}
		duplicate := false
		for _, existing := range result {
			if mapping.HostPort == existing.HostPort && protocolsOverlap(mapping.Protocol, existing.Protocol) {
				if !ipv6MappingsSyncIPv4(c) {
					return nil, fmt.Errorf("IPv6 port %d/%s is already mapped in this container", mapping.HostPort, mapping.Protocol)
				}
				if mapping.ContainerPort != existing.ContainerPort {
					return nil, fmt.Errorf("IPv4 mappings conflict when mirrored to IPv6 port %d/%s; disable IPv6 synchronization or choose different ports", mapping.HostPort, mapping.Protocol)
				}
				duplicate = true
				break
			}
		}
		if !duplicate {
			result = append(result, mapping)
		}
	}
	return result, nil
}

func validateIPv6MappingInventory(candidate *config.Container) error {
	// Port ownership is persistent even while the uplink is temporarily down.
	// Do not allow an IPv4 edit to create an IPv6 collision during that outage.
	if config.AppConfig == nil || !config.AppConfig.IPv6NAT.Enabled || !ipv6NATEligible(candidate) || candidate.IPv6NATAddress == "" {
		return nil
	}
	mappings, err := effectiveIPv6Mappings(candidate, "")
	if err != nil {
		return err
	}
	if !ipv6MappingsSyncIPv4(candidate) && len(mappings) > ipv6MappingLimit(candidate) {
		return fmt.Errorf("IPv6 port mapping quota exceeded: %d/%d", len(mappings), ipv6MappingLimit(candidate))
	}
	for _, other := range config.AppConfig.Containers {
		if other.ID == candidate.ID || other.IsKVM() || other.IPv6NATAddress == "" {
			continue
		}
		others, err := effectiveIPv6Mappings(&other, "")
		if err != nil {
			return fmt.Errorf("container %s: %w", other.Name, err)
		}
		for _, mapping := range mappings {
			for _, existing := range others {
				if mapping.HostPort == existing.HostPort && protocolsOverlap(mapping.Protocol, existing.Protocol) {
					return fmt.Errorf("IPv6 host port %d/%s is already mapped by container %s; disable IPv6 synchronization or choose another port", mapping.HostPort, mapping.Protocol, other.Name)
				}
			}
		}
	}
	createNATReservationMu.Lock()
	defer createNATReservationMu.Unlock()
	owner := createNATReservationOwner(candidate.Name)
	for id, reserved := range createNATReservations {
		if activeCreateNATOwners[id] == owner || !activeCreateNATMirrorsIPv6[id] {
			continue
		}
		for _, mapping := range mappings {
			if conflictingCreateNATReservation(mapping, reserved) {
				return fmt.Errorf("IPv6 host port %d/%s is reserved by an active container creation", mapping.HostPort, mapping.Protocol)
			}
		}
	}
	for reservedOwner, reserved := range queuedCreateNATReservations {
		if reservedOwner == owner || !queuedCreateNATMirrorsIPv6[reservedOwner] {
			continue
		}
		for _, mapping := range mappings {
			if conflictingCreateNATReservation(mapping, reserved) {
				return fmt.Errorf("IPv6 host port %d/%s is reserved by queued container creation %s", mapping.HostPort, mapping.Protocol, reservedOwner)
			}
		}
	}
	return nil
}

func createUsesIPv6PortSynchronization(cfg ContainerConfig) bool {
	return config.AppConfig != nil && config.AppConfig.IPv6NAT.Enabled &&
		config.NormalizeVirtualization(cfg.Virtualization) != config.VirtualizationKVM && !cfg.WantsLANIPv4() &&
		(cfg.IPv6NATSyncIPv4 == nil || *cfg.IPv6NATSyncIPv4)
}

func createNAT6PortAvailable(cfg ContainerConfig, port int, protocol string) bool {
	if !createUsesIPv6PortSynchronization(cfg) {
		return true
	}
	for _, container := range config.AppConfig.Containers {
		if container.IsKVM() || container.IPv6NATAddress == "" {
			continue
		}
		mappings, err := effectiveIPv6Mappings(&container, "")
		if err != nil {
			return false
		}
		for _, mapping := range mappings {
			if mapping.HostPort == port && protocolsOverlap(mapping.Protocol, protocol) {
				return false
			}
		}
	}
	return true
}

func validateCreateNATIPv6PortAvailability(cfg ContainerConfig, managementPort int) error {
	for _, mapping := range createNATReservationMappings(cfg, managementPort) {
		if !createNAT6PortAvailable(cfg, mapping.HostPort, mapping.Protocol) {
			return fmt.Errorf("IPv6 host port %d/%s is already mapped; choose another IPv4 NAT port or disable IPv6 synchronization", mapping.HostPort, mapping.Protocol)
		}
	}
	return nil
}

// ValidateIPv6NATPortMappings validates a prospective global configuration before
// ConfigureIPv6NAT changes any host networking.
func ValidateIPv6NATPortMappings() error {
	portMappingMutationMu.Lock()
	defer portMappingMutationMu.Unlock()
	return ValidateIPv6NATPortMappingsUnlocked()
}

// BeginIPv6NATConfiguration serializes a complete global network change with
// mapping edits and creation reservations, including creations with IPv4 off.
func BeginIPv6NATConfiguration() (func(), error) {
	portMappingMutationMu.Lock()
	createNATReservationMu.Lock()
	busy := len(createNATReservations) > 0 || len(queuedCreateNATReservations) > 0
	createNATReservationMu.Unlock()
	if busy {
		portMappingMutationMu.Unlock()
		return nil, fmt.Errorf("container creation is pending; retry the IPv6 NAT configuration after it finishes")
	}
	ipv6NATConfigurationChanging.Store(true)
	var once sync.Once
	return func() {
		once.Do(func() {
			ipv6NATConfigurationChanging.Store(false)
			portMappingMutationMu.Unlock()
		})
	}, nil
}

func IPv6NATConfigurationInProgress() bool { return ipv6NATConfigurationChanging.Load() }

// ValidateIPv6NATPortMappingsUnlocked is for the caller that already holds the
// global configuration transaction returned by BeginIPv6NATConfiguration.
func ValidateIPv6NATPortMappingsUnlocked() error {
	for i := range config.AppConfig.Containers {
		if err := validateIPv6MappingInventory(&config.AppConfig.Containers[i]); err != nil {
			return err
		}
	}
	return nil
}

func normalizeIPv6PortMapping(c *config.Container, mapping config.PortMapping) (config.PortMapping, error) {
	_, host, _, enabled := nat6PortEndpoint(c)
	if !enabled {
		return mapping, fmt.Errorf("IPv6 NAT is not available for this container")
	}
	if ipv6MappingsSyncIPv4(c) {
		return mapping, fmt.Errorf("IPv6 mappings are synchronized with IPv4; edit the IPv4 mapping or disable synchronization first")
	}
	if mapping.HostIP != "" {
		address, err := netip.ParseAddr(strings.TrimSpace(mapping.HostIP))
		if err != nil || !address.Is6() || address.Is4In6() || address.String() != host {
			return mapping, fmt.Errorf("host_ip must be the configured IPv6 NAT host address")
		}
	}
	mapping.HostIP = "" // Follow the configured node address rather than a stale snapshot.
	mapping.Protocol = strings.ToLower(strings.TrimSpace(mapping.Protocol))
	if mapping.Protocol == "" {
		mapping.Protocol = "tcp"
	}
	if mapping.Protocol != "tcp" && mapping.Protocol != "udp" {
		return mapping, fmt.Errorf("protocol must be tcp or udp")
	}
	if mapping.ContainerPort < 1 || mapping.ContainerPort > 65535 {
		return mapping, fmt.Errorf("container port must be 1-65535")
	}
	if mapping.HostPort == 0 {
		mapping.HostPort = mapping.ContainerPort
	}
	if !config.NATPortInRange(mapping.HostPort) {
		start, end := config.NATPortRange()
		return mapping, fmt.Errorf("host port must be within configured NAT range %d-%d", start, end)
	}
	if mapping.Description == "" {
		mapping.Description = fmt.Sprintf("Port-%d", mapping.ContainerPort)
	}
	return mapping, nil
}

func (m *Manager) AddIPv6PortMapping(id int, mapping config.PortMapping) ([]config.PortMapping, error) {
	return m.changeIPv6PortMapping(id, -1, &mapping)
}

func (m *Manager) UpdateIPv6PortMapping(id, index int, mapping config.PortMapping) ([]config.PortMapping, error) {
	if index < 0 {
		return nil, fmt.Errorf("invalid IPv6 port mapping index: %d", index)
	}
	return m.changeIPv6PortMapping(id, index, &mapping)
}

func (m *Manager) DeleteIPv6PortMapping(id, index int) ([]config.PortMapping, error) {
	if index < 0 {
		return nil, fmt.Errorf("invalid IPv6 port mapping index: %d", index)
	}
	return m.changeIPv6PortMapping(id, index, nil)
}

func (m *Manager) changeIPv6PortMapping(id, index int, mapping *config.PortMapping) ([]config.PortMapping, error) {
	portMappingMutationMu.Lock()
	defer portMappingMutationMu.Unlock()
	c := config.FindContainer(id)
	if c == nil {
		return nil, fmt.Errorf("container not found: %d", id)
	}
	snapshot := *c
	c = &snapshot
	if ipv6MappingsSyncIPv4(c) {
		return nil, fmt.Errorf("disable IPv6 synchronization before editing independent IPv6 mappings")
	}
	if _, _, _, enabled := nat6PortEndpoint(c); !enabled {
		return nil, fmt.Errorf("IPv6 NAT is not available for this container")
	}
	if index >= len(c.IPv6PortMappings) {
		return nil, fmt.Errorf("invalid IPv6 port mapping index: %d", index)
	}
	candidate := *c
	candidate.IPv6PortMappings = append([]config.PortMapping(nil), c.IPv6PortMappings...)
	if mapping == nil {
		candidate.IPv6PortMappings = append(candidate.IPv6PortMappings[:index], candidate.IPv6PortMappings[index+1:]...)
	} else {
		normalized, err := normalizeIPv6PortMapping(c, *mapping)
		if err != nil {
			return nil, err
		}
		if index < 0 {
			if ipv6MappingLimit(c) <= 0 || len(candidate.IPv6PortMappings) >= ipv6MappingLimit(c) {
				return nil, fmt.Errorf("IPv6 port mapping quota exceeded")
			}
			candidate.IPv6PortMappings = append(candidate.IPv6PortMappings, normalized)
		} else {
			candidate.IPv6PortMappings[index] = normalized
		}
	}
	if err := validateIPv6MappingInventory(&candidate); err != nil {
		return nil, err
	}
	if err := persistIPv6MappingChange(m, c, candidate); err != nil {
		return nil, err
	}
	state, err := GetIPv6PortMappings(id)
	return state.Mappings, err
}

func (m *Manager) SetIPv6NATSync(id int, enabled bool) (IPv6PortMappingState, error) {
	portMappingMutationMu.Lock()
	defer portMappingMutationMu.Unlock()
	c := config.FindContainer(id)
	if c == nil {
		return IPv6PortMappingState{}, fmt.Errorf("container not found: %d", id)
	}
	snapshot := *c
	c = &snapshot
	_, host, _, available := nat6PortEndpoint(c)
	if !available {
		return IPv6PortMappingState{}, fmt.Errorf("IPv6 NAT is not available for this container")
	}
	candidate := *c
	if !enabled && ipv6MappingsSyncIPv4(c) {
		mappings, err := effectiveIPv6Mappings(c, host)
		if err != nil {
			return IPv6PortMappingState{}, err
		}
		for i := range mappings {
			mappings[i].HostIP = ""
		}
		candidate.IPv6PortMappings = mappings
	}
	candidate.IPv6NATSyncIPv4 = &enabled
	if err := validateIPv6MappingInventory(&candidate); err != nil {
		return IPv6PortMappingState{}, err
	}
	if err := persistIPv6MappingChange(m, c, candidate); err != nil {
		return IPv6PortMappingState{}, err
	}
	return GetIPv6PortMappings(id)
}

func persistIPv6MappingChange(m *Manager, current *config.Container, candidate config.Container) error {
	current = config.FindContainer(candidate.ID)
	if current == nil {
		return fmt.Errorf("container not found: %d", candidate.ID)
	}
	previous := *current
	current.IPv6PortMappings = candidate.IPv6PortMappings
	current.IPv6NATSyncIPv4 = candidate.IPv6NATSyncIPv4
	if err := saveIPv6PortMappingConfig(); err != nil {
		if current = config.FindContainer(candidate.ID); current != nil {
			current.IPv6PortMappings, current.IPv6NATSyncIPv4 = previous.IPv6PortMappings, previous.IPv6NATSyncIPv4
		}
		return err
	}
	if candidate.Status == "running" {
		if err := m.ApplyIPv6PortMappings(candidate.ID); err != nil {
			return fmt.Errorf("IPv6 mapping configuration saved, but applying it failed: %w", err)
		}
	}
	clearChangedIPv6Mappings(&previous, &candidate)
	return nil
}

func IPv6HostPortAvailable(c *config.Container, port int, protocol string) bool {
	if c == nil || !config.NATPortInRange(port) {
		return false
	}
	_, host, _, available := nat6PortEndpoint(c)
	if !available {
		return false
	}
	for _, container := range config.AppConfig.Containers {
		if container.IsKVM() || container.IPv6NATAddress == "" {
			continue
		}
		mappings, err := effectiveIPv6Mappings(&container, host)
		if err != nil {
			return false
		}
		for _, mapping := range mappings {
			if mapping.HostPort == port && protocolsOverlap(mapping.Protocol, protocol) {
				return false
			}
		}
	}
	return true
}

func (m *Manager) ApplyIPv6PortMappings(id int) error {
	iptablesMu.Lock()
	defer iptablesMu.Unlock()
	c := config.FindContainer(id)
	if c == nil {
		return fmt.Errorf("container not found: %d", id)
	}
	snapshot := *c
	c = &snapshot
	if err := validateIPv6MappingInventory(c); err != nil {
		return err
	}
	if _, err := reconcileIPv6PortMappingsLocked(c); err != nil {
		return err
	}
	return applyFirewallRulesLocked(id)
}

func ApplyIPv6PortMappings(id int) error {
	return NewManager().ApplyIPv6PortMappings(id)
}

func applyIPv6PortMappingsLocked(c *config.Container) error {
	if c.Status != "running" {
		return nil
	}
	_, err := reconcileIPv6PortMappingsLocked(c)
	return err
}

func ipv6MappingRules(c *config.Container) ([]mappingRule, error) {
	guest, host, _, enabled := nat6PortEndpoint(c)
	if !enabled || c.Status != "running" {
		return nil, nil
	}
	if err := validateIPv6MappingInventory(c); err != nil {
		return nil, err
	}
	mappings, err := effectiveIPv6Mappings(c, host)
	if err != nil {
		return nil, err
	}
	var rules []mappingRule
	for _, mapping := range mappings {
		for _, args := range ipv6PortMappingRuleArgs(c.ID, guest, host, mapping) {
			table, start := "filter", 2
			if args[start] == "-t" {
				table = args[start+1]
				start += 2
			}
			rules = append(rules, mappingRule{table, args[start+1], args[start+3:]})
		}
	}
	return rules, nil
}

func reconcileIPv6PortMappingsLocked(c *config.Container) (bool, error) {
	rules, err := ipv6MappingRules(c)
	if err != nil {
		return false, err
	}
	return reconcileMappingRules("ip6tables", "clicd-"+clicdTag(c.ID)+"-nat6-port-",
		[]mappingRule{{table: "nat", chain: "PREROUTING"}, {table: "nat", chain: "OUTPUT"}, {table: "filter", chain: "FORWARD"}}, rules, runIPv6PortMappingCommand)
}

func clearChangedIPv6Mappings(previous, current *config.Container) {
	old, err := ipv6MappingRules(previous)
	if err != nil {
		return
	}
	next, err := ipv6MappingRules(current)
	if err != nil {
		return
	}
	wanted := map[string]bool{}
	for _, rule := range next {
		wanted[mappingRuleKey(rule)] = true
	}
	for _, rule := range old {
		if !wanted[mappingRuleKey(rule)] {
			clearMappingRuleConntrack("ip6tables", rule, runIPv6PortMappingCommand)
		}
	}
}

func ipv6PortMappingRuleArgs(id int, guest, host string, mapping config.PortMapping) [][]string {
	marker := fmt.Sprintf("clicd-%s-nat6-port-%s-%d", clicdTag(id), mapping.Protocol, mapping.HostPort)
	rules := make([][]string, 0, 3)
	for _, chain := range []string{"PREROUTING", "OUTPUT"} {
		rules = append(rules, []string{"-w", "5", "-t", "nat", "-I", chain, "1", "-d", host + "/128", "-p", mapping.Protocol,
			"--dport", strconv.Itoa(mapping.HostPort), "-j", "DNAT", "--to-destination", net.JoinHostPort(guest, strconv.Itoa(mapping.ContainerPort)),
			"-m", "comment", "--comment", marker})
	}
	// Permit new connections only after DNAT. The container firewall is
	// replayed after this rule and continues to take precedence.
	rules = append(rules, []string{"-w", "5", "-I", "FORWARD", "1", "-o", NAT6BridgeName, "-d", guest + "/128", "-p", mapping.Protocol,
		"--dport", strconv.Itoa(mapping.ContainerPort), "-m", "conntrack", "--ctstate", "DNAT", "-j", "ACCEPT", "-m", "comment", "--comment", marker})
	return rules
}

func (m *Manager) CleanIPv6PortMappings(id int) error {
	iptablesMu.Lock()
	defer iptablesMu.Unlock()
	return cleanIPv6PortMappingsLocked(id)
}

func CleanIPv6PortMappings(id int) error {
	return NewManager().CleanIPv6PortMappings(id)
}

func cleanIPv6PortMappingsLocked(id int) error {
	marker := fmt.Sprintf("clicd-%s-nat6-port-", clicdTag(id))
	var cleanupErrors []error
	for _, target := range []struct{ table, chain string }{{"nat", "PREROUTING"}, {"nat", "OUTPUT"}, {"filter", "FORWARD"}} {
		out, err := runIPv6PortMappingCommand("ip6tables", "-w", "5", "-t", target.table, "-S", target.chain)
		if err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("list IPv6 NAT rules: %w: %s", err, strings.TrimSpace(string(out))))
			continue
		}
		for _, args := range taggedRuleSpecifications(out, marker) {
			if output, err := runIPv6PortMappingCommand("ip6tables", append([]string{"-w", "5", "-t", target.table}, args...)...); err != nil && !isIPTablesRuleNotFoundError(string(output)) {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("remove IPv6 NAT rule: %w: %s", err, strings.TrimSpace(string(output))))
			} else {
				clearMappingRuleConntrack("ip6tables", mappingRule{target.table, target.chain, args[2:]}, runIPv6PortMappingCommand)
			}
		}
	}
	if c := config.FindContainer(id); c != nil {
		snapshot := *c
		c = &snapshot
		rules, _ := ipv6MappingRules(c)
		for _, rule := range rules {
			clearMappingRuleConntrack("ip6tables", rule, runIPv6PortMappingCommand)
		}
	}
	return errors.Join(cleanupErrors...)
}
