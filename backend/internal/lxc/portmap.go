package lxc

import (
	"errors"
	"fmt"
	"net/netip"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"clicd/internal/config"
)

var (
	iptablesMu                  sync.Mutex
	createNATReservationMu      sync.Mutex
	createNATReservationNextID  uint64
	createNATReservations       = map[uint64][]config.PortMapping{}
	queuedCreateNATReservations = map[string][]config.PortMapping{}
	activeCreateNATOwners       = map[uint64]string{}
	activeCreateNATMirrorsIPv6  = map[uint64]bool{}
	queuedCreateNATMirrorsIPv6  = map[string]bool{}
)

// ApplyPortMappings reconciles network rules and reapplies the configured firewall.
func (m *Manager) ApplyPortMappings(id int) error {
	return m.reconcilePortMappings(id)
}

// EnsurePortMappings repairs missing/stale network rules without disturbing
// healthy connections or replaying the firewall on every background poll.
func (m *Manager) EnsurePortMappings(id int) error {
	return m.reconcilePortMappings(id)
}

func (m *Manager) reconcilePortMappings(id int) error {
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
	if c.IP != "" {
		EnsureAssignedPublicIPv4s(c.PublicIPv4s)
		bridge, subnet := "lxcbr0", config.LXCNATNetwork().Subnet
		if c.IsKVM() {
			bridge, subnet = "virbr0", config.KVMNATNetwork().Subnet
		}
		EnsureForwardRules(bridge)
		var err error
		_, err = reconcileMappingRules("iptables", "clicd-"+clicdTag(id)+"-",
			[]mappingRule{{table: "nat", chain: "PREROUTING"}, {table: "nat", chain: "POSTROUTING"}, {table: "filter", chain: "FORWARD"}},
			ipv4MappingRules(c, bridge, subnet, DetectPublicIPv4().Interface), runPortMappingCommand)
		if err != nil {
			return err
		}
		deleteBridgeMasquerade(subnet)
	} else if _, _, _, enabled := nat6PortEndpoint(c); !enabled {
		return fmt.Errorf("container has no IP")
	}
	if c.IPv6NATAddress != "" {
		_, err := reconcileIPv6PortMappingsLocked(c)
		if err != nil {
			return err
		}
	}
	return applyFirewallRulesLocked(id)
}

func containerAllowsPublicIPv4Egress(c *config.Container) bool {
	if c == nil {
		return false
	}
	if len(c.PublicIPv4s) > 0 {
		return true
	}
	return c.PortMappingLimit > 0 || len(c.PortMappings) > 0
}

func deleteBridgeMasquerade(subnet string) {
	for exec.Command("iptables", "-t", "nat", "-D", "POSTROUTING", "-s", subnet, "-o", "eth+", "-j", "MASQUERADE").Run() == nil {
	}
}

func primaryPublicIPv4Assignment(c *config.Container) (config.PublicIPv4Assignment, bool) {
	if c == nil {
		return config.PublicIPv4Assignment{}, false
	}
	for _, item := range c.PublicIPv4s {
		if strings.TrimSpace(item.Address) != "" {
			return item, true
		}
	}
	return config.PublicIPv4Assignment{}, false
}

func clicdTag(id int) string { return "c" + strconv.Itoa(id) }

var restorePortMappingOrphans = (*Manager).cleanOrphanedPortMappings
var restoreContainerPortMappings = (*Manager).ApplyPortMappings
var restoreContainerIPv6 = (*Manager).ApplyIPv6

func EnsureAllRunningPortMappings() {
	m := NewManager()
	restorePortMappingOrphans(m)
	containers := append([]config.Container(nil), config.AppConfig.Containers...)
	for i := range containers {
		c := &containers[i]
		if c.Status != "running" || (strings.TrimSpace(c.IP) == "" && c.IPv6NATAddress == "") {
			if err := m.CleanPortMappings(c.ID); err != nil {
				fmt.Printf("Warning: failed to clean inactive port mappings for %s: %v\n", c.Name, err)
			}
			continue
		}
		if err := restoreContainerPortMappings(m, c.ID); err != nil {
			fmt.Printf("Warning: failed to restore port mappings for %s: %v\n", c.Name, err)
		}
		if !c.IsKVM() && c.IPv6NATAddress != "" {
			if err := restoreContainerIPv6(m, c.ID); err != nil {
				fmt.Printf("Warning: failed to restore IPv6 NAT routing for %s: %v\n", c.Name, err)
			}
		}
	}
}

var taggedContainerIDPattern = regexp.MustCompile(`clicd-c([0-9]+)-`)

func (m *Manager) cleanOrphanedPortMappings() {
	if output, err := exec.Command("ip6tables-save", "-t", "nat").Output(); err == nil {
		for _, match := range taggedContainerIDPattern.FindAllSubmatch(output, -1) {
			id, err := strconv.Atoi(string(match[1]))
			if err == nil && config.FindContainer(id) == nil {
				if err := m.CleanIPv6PortMappings(id); err != nil {
					fmt.Printf("Warning: failed to clean orphaned IPv6 port mappings: %v\n", err)
				}
			}
		}
	}
	output, err := exec.Command("iptables-save").Output()
	if err != nil {
		return
	}
	configured := make(map[int]bool, len(config.AppConfig.Containers))
	for i := range config.AppConfig.Containers {
		configured[config.AppConfig.Containers[i].ID] = true
	}
	seen := map[int]bool{}
	for _, match := range taggedContainerIDPattern.FindAllSubmatch(output, -1) {
		if len(match) < 2 {
			continue
		}
		id, err := strconv.Atoi(string(match[1]))
		if err != nil || configured[id] || seen[id] {
			continue
		}
		seen[id] = true
		if err := m.CleanPortMappings(id); err != nil {
			fmt.Printf("Warning: failed to clean orphaned port mappings for container %d: %v\n", id, err)
		}
	}
}

// EnsureForwardRules makes sure iptables FORWARD and INPUT chains allow bridge traffic,
// and ensures UFW rules allow bridge traffic when UFW is active.
func EnsureForwardRules(bridge string) {
	if bridge == "" {
		bridge = "lxcbr0"
	}
	ensureLibvirtForwardRules(bridge)
	ensureUFWRules(bridge)

	// Allow bridge traffic in INPUT chain so DHCP (UDP 67) and DNS (UDP 53) reach host services.
	inputRules := [][]string{
		{"-i", bridge, "-j", "ACCEPT"},
	}
	for _, args := range inputRules {
		ensureBridgeAcceptRule("INPUT", args)
	}

	forwardRules := [][]string{
		{"-i", bridge, "-j", "ACCEPT"},
		{"-o", bridge, "-j", "ACCEPT"},
		{"-i", bridge, "-o", bridge, "-j", "ACCEPT"},
	}
	for _, args := range forwardRules {
		ensureBridgeAcceptRule("FORWARD", args)
	}
}

var ufwBridgeConfigured sync.Map

func ensureUFWRules(bridge string) {
	if bridge == "" {
		return
	}
	if _, loaded := ufwBridgeConfigured.LoadOrStore(bridge, true); loaded {
		return
	}
	out, err := exec.Command("ufw", "status").CombinedOutput()
	if err != nil || !strings.Contains(strings.ToLower(string(out)), "status: active") {
		return
	}
	exec.Command("ufw", "allow", "in", "on", bridge).Run()
	exec.Command("ufw", "route", "allow", "in", "on", bridge).Run()
	exec.Command("ufw", "route", "allow", "out", "on", bridge).Run()
}

func ensureLibvirtForwardRules(bridge string) {
	if bridge != "virbr0" || exec.Command("iptables", "-L", "LIBVIRT_FWI", "-n").Run() != nil {
		return
	}
	rules := []struct {
		chain string
		args  []string
	}{
		{chain: "LIBVIRT_FWI", args: []string{"-o", bridge, "-j", "ACCEPT"}},
		{chain: "LIBVIRT_FWO", args: []string{"-i", bridge, "-j", "ACCEPT"}},
		{chain: "LIBVIRT_FWX", args: []string{"-i", bridge, "-o", bridge, "-j", "ACCEPT"}},
	}
	for _, rule := range rules {
		if exec.Command("iptables", "-L", rule.chain, "-n").Run() != nil {
			continue
		}
		ensureBridgeAcceptRule(rule.chain, rule.args)
	}
}

// CleanPortMappings removes all iptables rules for a container
func (m *Manager) CleanPortMappings(id int) error {
	iptablesMu.Lock()
	defer iptablesMu.Unlock()
	return m.cleanPortMappingsLocked(id)
}

func (m *Manager) cleanPortMappingsLocked(id int) error {
	marker := "clicd-" + clicdTag(id) + "-"
	var cleanupErrors []error
	for _, target := range []struct {
		table string
		chain string
	}{
		{table: "nat", chain: "PREROUTING"},
		{table: "nat", chain: "POSTROUTING"},
		{chain: "FORWARD"},
	} {
		if err := deleteTaggedIPTablesRules(target.table, target.chain, marker); err != nil {
			cleanupErrors = append(cleanupErrors, err)
		}
	}
	if c := config.FindContainer(id); c != nil {
		snapshot := *c
		c = &snapshot
		if c.IPv6NATAddress != "" {
			if err := cleanIPv6PortMappingsLocked(id); err != nil {
				cleanupErrors = append(cleanupErrors, err)
			}
		}
		for _, rule := range ipv4MappingRules(c, "", "", "") {
			clearMappingRuleConntrack("iptables", rule, runPortMappingCommand)
		}
	}
	return errors.Join(cleanupErrors...)
}

func deleteTaggedIPTablesRules(table, chain, marker string) error {
	listArgs := []string{"-w", "5"}
	if table != "" {
		listArgs = append(listArgs, "-t", table)
	}
	listArgs = append(listArgs, "-S", chain)
	output, err := exec.Command("iptables", listArgs...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("list iptables %s/%s: %w: %s", tableName(table), chain, err, strings.TrimSpace(string(output)))
	}

	var deleteErrors []error
	specs := taggedRuleSpecifications(output, marker)
	for _, spec := range specs {
		deleteArgs := []string{"-w", "5"}
		if table != "" {
			deleteArgs = append(deleteArgs, "-t", table)
		}
		deleteArgs = append(deleteArgs, spec...)
		if out, err := exec.Command("iptables", deleteArgs...).CombinedOutput(); err != nil {
			outStr := strings.TrimSpace(string(out))
			if isIPTablesRuleNotFoundError(outStr) {
				continue
			}
			deleteErrors = append(deleteErrors, fmt.Errorf(
				"delete iptables %s/%s rule [%s]: %w: %s",
				tableName(table), chain, strings.Join(spec, " "), err, outStr,
			))
		} else {
			clearMappingRuleConntrack("iptables", mappingRule{table, chain, spec[2:]}, runPortMappingCommand)
		}
	}
	return errors.Join(deleteErrors...)
}

func parseIPTablesRuleFields(line string) []string {
	var fields []string
	var current strings.Builder
	inQuotes := false

	for i := 0; i < len(line); i++ {
		ch := line[i]
		if ch == '"' {
			inQuotes = !inQuotes
			continue
		}
		if !inQuotes && (ch == ' ' || ch == '\t') {
			if current.Len() > 0 {
				fields = append(fields, current.String())
				current.Reset()
			}
			continue
		}
		current.WriteByte(ch)
	}
	if current.Len() > 0 {
		fields = append(fields, current.String())
	}
	return fields
}

func taggedRuleSpecifications(output []byte, marker string) [][]string {
	var specs [][]string
	for _, line := range strings.Split(string(output), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		fields := parseIPTablesRuleFields(trimmed)
		if len(fields) >= 2 && fields[0] == "-A" && strings.HasPrefix(ruleOption(fields[2:], "--comment"), marker) {
			fields[0] = "-D"
			specs = append(specs, fields)
		}
	}
	return specs
}

func isIPTablesRuleNotFoundError(errStr string) bool {
	lower := strings.ToLower(errStr)
	return strings.Contains(lower, "does a matching rule exist") ||
		strings.Contains(lower, "no chain/target/match by that name") ||
		strings.Contains(lower, "bad rule")
}

func taggedRuleLineNumbers(output []byte, marker string) []int {
	lineNumbers := make([]int, 0)
	for _, line := range strings.Split(string(output), "\n") {
		if !strings.Contains(line, marker) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		lineNumber, err := strconv.Atoi(fields[0])
		if err == nil && lineNumber > 0 {
			lineNumbers = append(lineNumbers, lineNumber)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(lineNumbers)))
	return lineNumbers
}

func tableName(table string) string {
	if table == "" {
		return "filter"
	}
	return table
}

// SetupDefaultPortMappings creates default port mappings
func SetupDefaultPortMappings(sshPort int) []config.PortMapping {
	return []config.PortMapping{
		{ContainerPort: 22, HostPort: sshPort, Protocol: "tcp", Description: "SSH"},
	}
}

func DefaultPortMappingHostIP(assignments []config.PublicIPv4Assignment) string {
	if len(assignments) == 1 {
		return strings.TrimSpace(assignments[0].Address)
	}
	return ""
}

func defaultPortMappingHostIP(assignments []config.PublicIPv4Assignment) string {
	return DefaultPortMappingHostIP(assignments)
}

// AddPortMapping adds a NAT rule to a container
func (m *Manager) AddPortMapping(id int, pm config.PortMapping) ([]config.PortMapping, error) {
	portMappingMutationMu.Lock()
	defer portMappingMutationMu.Unlock()
	c := config.FindContainer(id)
	if c == nil {
		return nil, fmt.Errorf("container not found: %d", id)
	}
	if c.PortMappingLimit <= 0 {
		return nil, fmt.Errorf("container has no IPv4 NAT port quota")
	}
	if c.PortMappingLimit > 0 && len(c.PortMappings) >= c.PortMappingLimit {
		return nil, fmt.Errorf("port mapping quota exceeded: %d/%d", len(c.PortMappings), c.PortMappingLimit)
	}
	normalized, err := normalizePortMapping(c, -1, pm)
	if err != nil {
		return nil, err
	}
	previous := *c
	candidate := *c
	candidate.PortMappings = append(append([]config.PortMapping(nil), c.PortMappings...), normalized)
	if err := validateIPv6MappingInventory(&candidate); err != nil {
		return nil, err
	}
	c.PortMappings = candidate.PortMappings
	if err := persistAndReloadMappings(m, c); err != nil {
		return nil, err
	}
	clearChangedIPv4Mappings(&previous, c)
	clearChangedIPv6Mappings(&previous, c)
	return c.PortMappings, nil
}

// UpdatePortMapping updates an existing NAT rule
func (m *Manager) UpdatePortMapping(id int, index int, pm config.PortMapping) ([]config.PortMapping, error) {
	portMappingMutationMu.Lock()
	defer portMappingMutationMu.Unlock()
	c := config.FindContainer(id)
	if c == nil {
		return nil, fmt.Errorf("container not found: %d", id)
	}
	if index < 0 || index >= len(c.PortMappings) {
		return nil, fmt.Errorf("invalid port mapping index: %d", index)
	}
	existing := c.PortMappings[index]
	normalized, err := normalizePortMapping(c, index, pm)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(existing.Description, "SSH") {
		normalized.Description = "SSH"
	}
	previous := *c
	candidate := *c
	candidate.PortMappings = append([]config.PortMapping(nil), c.PortMappings...)
	candidate.PortMappings[index] = normalized
	if err := validateIPv6MappingInventory(&candidate); err != nil {
		return nil, err
	}
	c.PortMappings = candidate.PortMappings
	if err := persistAndReloadMappings(m, c); err != nil {
		return nil, err
	}
	clearChangedIPv4Mappings(&previous, c)
	clearChangedIPv6Mappings(&previous, c)
	return c.PortMappings, nil
}

// DeletePortMapping removes a NAT rule
func (m *Manager) DeletePortMapping(id int, index int) ([]config.PortMapping, error) {
	portMappingMutationMu.Lock()
	defer portMappingMutationMu.Unlock()
	c := config.FindContainer(id)
	if c == nil {
		return nil, fmt.Errorf("container not found: %d", id)
	}
	if index < 0 || index >= len(c.PortMappings) {
		return nil, fmt.Errorf("invalid port mapping index: %d", index)
	}
	removed := c.PortMappings[index]
	if strings.EqualFold(removed.Description, "SSH") {
		return nil, fmt.Errorf("SSH default mapping cannot be deleted")
	}
	previous := *c
	c.PortMappings = append(append([]config.PortMapping(nil), c.PortMappings[:index]...), c.PortMappings[index+1:]...)
	if err := persistAndReloadMappings(m, c); err != nil {
		return nil, err
	}
	clearChangedIPv4Mappings(&previous, c)
	clearChangedIPv6Mappings(&previous, c)
	return c.PortMappings, nil
}

func persistAndReloadMappings(m *Manager, c *config.Container) error {
	syncContainerSSHPort(c)
	config.SaveConfig()
	if c.Status == "running" && (c.IP != "" || c.IPv6NATAddress != "") {
		return m.ApplyPortMappings(c.ID)
	}
	return nil
}

func syncContainerSSHPort(c *config.Container) {
	if c == nil {
		return
	}
	for _, mapping := range c.PortMappings {
		if strings.EqualFold(mapping.Description, "SSH") {
			c.SSHPort = mapping.HostPort
			return
		}
	}
}

func (m *Manager) UpdatePublicIPv4Assignments(id int, requested []string, count int, auto bool) (*config.Container, error) {
	c := config.FindContainer(id)
	if c == nil {
		return nil, fmt.Errorf("container not found: %d", id)
	}
	if c.UsesLANIPv4() {
		return nil, fmt.Errorf("public IPv4 cannot be assigned while LAN IPv4 mode is enabled")
	}

	assignments := []config.PublicIPv4Assignment{}
	if auto || len(requested) > 0 {
		allocated, err := AllocatePublicIPv4Assignments(id, requested, count, auto)
		if err != nil {
			return nil, err
		}
		assignments = allocated
	}

	c.PublicIPv4s = assignments
	reconcilePortMappingHostIPs(c)
	c.NormalizeNetworkAssignments()
	config.SaveConfig()

	_ = m.CleanPortMappings(id)
	EnsureAssignedPublicIPv4s(c.PublicIPv4s)
	if c.Status == "running" && c.IP != "" {
		if err := m.ApplyPortMappings(id); err != nil {
			return nil, err
		}
	}
	return c, nil
}

func reconcilePortMappingHostIPs(c *config.Container) {
	if c == nil {
		return
	}
	assigned := map[string]bool{}
	for _, item := range c.PublicIPv4s {
		if addr := strings.TrimSpace(item.Address); addr != "" {
			assigned[addr] = true
		}
	}
	replacement := ""
	if len(assigned) == 1 {
		for addr := range assigned {
			replacement = addr
		}
	}
	for i := range c.PortMappings {
		hostIP := strings.TrimSpace(c.PortMappings[i].HostIP)
		if hostIP == "" || assigned[hostIP] {
			continue
		}
		c.PortMappings[i].HostIP = replacement
	}
}

func normalizePortMapping(c *config.Container, skipIndex int, pm config.PortMapping) (config.PortMapping, error) {
	if pm.ContainerPort < 1 || pm.ContainerPort > 65535 {
		return pm, fmt.Errorf("container port must be 1-65535")
	}
	if pm.Protocol == "" {
		pm.Protocol = "tcp"
	}
	pm.Protocol = strings.ToLower(strings.TrimSpace(pm.Protocol))
	pm.HostIP = strings.TrimSpace(pm.HostIP)
	if pm.HostIP != "" {
		addr, err := netip.ParseAddr(pm.HostIP)
		if err != nil || !addr.Is4() {
			return pm, fmt.Errorf("host_ip must be a valid IPv4 address")
		}
		if !containerHasPublicIPv4(c, pm.HostIP) {
			return pm, fmt.Errorf("host_ip %s is not assigned to this container", pm.HostIP)
		}
	}
	if pm.Description == "" {
		pm.Description = fmt.Sprintf("Port-%d", pm.ContainerPort)
	}
	if pm.HostPort <= 0 {
		pm.HostPort = pm.ContainerPort
	}
	if pm.HostIP == "" && !config.NATPortInRange(pm.HostPort) {
		start, end := config.NATPortRange()
		return pm, fmt.Errorf("host port must be within configured NAT4 range %d-%d", start, end)
	}
	// Check current container's own mappings
	for i, existing := range c.PortMappings {
		if i == skipIndex {
			continue
		}
		if portMappingsConflict(c, pm, c, existing) {
			return pm, fmt.Errorf("host port %d/%s already mapped on the same IPv4 in this container", pm.HostPort, pm.Protocol)
		}
	}
	// Check all other containers (LXC + KVM) for port conflicts
	for _, oc := range config.AppConfig.Containers {
		if oc.ID == c.ID {
			continue
		}
		for _, existing := range oc.PortMappings {
			oc := oc
			if portMappingsConflict(c, pm, &oc, existing) {
				return pm, fmt.Errorf("host port %d/%s already used on the same IPv4 by container %s (ID: %d)", pm.HostPort, pm.Protocol, oc.Name, oc.ID)
			}
		}
	}
	return pm, nil
}

// SetupCreatePortMappings appends validated custom or automatically allocated
// mappings to a container's management port mapping.
func SetupCreatePortMappings(c *config.Container, cfg ContainerConfig) ([]config.PortMapping, error) {
	if c == nil {
		return nil, fmt.Errorf("container is required")
	}
	requested := append([]config.PortMapping(nil), cfg.NATPortMappings...)
	if len(requested) == 0 && cfg.PortMappingCount > 1 {
		for _, port := range allocateDefaultEqualPorts(c, cfg.PortMappingCount-1) {
			requested = append(requested, config.PortMapping{
				ContainerPort: port,
				HostPort:      port,
				Protocol:      "tcp",
				Description:   fmt.Sprintf("Port-%d", port),
			})
		}
	}
	for _, mapping := range requested {
		pm, err := normalizePortMapping(c, -1, mapping)
		if err != nil {
			return nil, err
		}
		c.PortMappings = append(c.PortMappings, pm)
	}
	return c.PortMappings, nil
}

// ReserveCreateNATPorts keeps concurrent create tasks from selecting each
// other's custom or management ports before their containers are persisted.
func ReserveCreateNATPorts(cfg ContainerConfig) (int, func(), error) {
	if !cfg.WantsNAT() && (config.NormalizeVirtualization(cfg.Virtualization) == config.VirtualizationKVM || cfg.WantsLANIPv4()) {
		return 0, func() {}, nil
	}
	if IPv6NATConfigurationInProgress() {
		return 0, nil, fmt.Errorf("IPv6 NAT configuration is changing; retry container creation")
	}

	portMappingMutationMu.Lock()
	defer portMappingMutationMu.Unlock()
	createNATReservationMu.Lock()
	defer createNATReservationMu.Unlock()

	owner := createNATReservationOwner(cfg.Name)
	requestedReservations := createNATReservationMappings(cfg, cfg.ManagementPort)
	if queued, ok := queuedCreateNATReservations[owner]; ok {
		if !sameCreateNATReservations(queued, requestedReservations) {
			return 0, nil, fmt.Errorf("queued NAT port plan for %s no longer matches the create task", cfg.Name)
		}
		delete(queuedCreateNATReservations, owner)
		delete(queuedCreateNATMirrorsIPv6, owner)
		if err := validateCreateNATIPv6PortAvailability(cfg, cfg.ManagementPort); err != nil {
			return 0, nil, err
		}
		return activateCreateNATReservationLocked(cfg.ManagementPort, queued, owner, createUsesIPv6PortSynchronization(cfg))
	}
	if !cfg.WantsNAT() {
		return activateCreateNATReservationLocked(0, nil, owner, false)
	}

	if err := ValidateCreateNATPortAvailability(cfg); err != nil {
		return 0, nil, err
	}
	if err := validateCreateNATReservationsAvailableLocked(requestedReservations, owner); err != nil {
		return 0, nil, err
	}

	excluded := cfg.RequestedNATHostPorts()
	excluded = append(excluded, allReservedCreateNATHostPortsLocked(owner)...)
	managementPort := cfg.ManagementPort
	if managementPort == 0 {
		var err error
		managementPort, err = config.AllocateSSHPortExcluding(excluded)
		if err != nil {
			return 0, nil, err
		}
	}

	reservations := createNATReservationMappings(cfg, managementPort)
	if err := validateCreateNATIPv6PortAvailability(cfg, managementPort); err != nil {
		return 0, nil, err
	}
	return activateCreateNATReservationLocked(managementPort, reservations, owner, createUsesIPv6PortSynchronization(cfg))
}

// ReserveBatchCreateNATPorts resolves every automatic NAT port and reserves
// the complete batch before any create task is enqueued.
func ReserveBatchCreateNATPorts(configs []ContainerConfig) ([]ContainerConfig, error) {
	if IPv6NATConfigurationInProgress() {
		return nil, fmt.Errorf("IPv6 NAT configuration is changing; retry container creation")
	}
	portMappingMutationMu.Lock()
	defer portMappingMutationMu.Unlock()
	createNATReservationMu.Lock()
	defer createNATReservationMu.Unlock()

	planned := append([]ContainerConfig(nil), configs...)
	addedOwners := make([]string, 0, len(planned))
	rollback := func() {
		for _, owner := range addedOwners {
			delete(queuedCreateNATReservations, owner)
			delete(queuedCreateNATMirrorsIPv6, owner)
		}
	}

	for i := range planned {
		cfg := &planned[i]
		cfg.NATPortMappings = append([]config.PortMapping(nil), cfg.NATPortMappings...)
		if !cfg.WantsNAT() {
			if config.NormalizeVirtualization(cfg.Virtualization) != config.VirtualizationKVM && !cfg.WantsLANIPv4() {
				owner := createNATReservationOwner(cfg.Name)
				if owner == "" {
					rollback()
					return nil, fmt.Errorf("container name is required for IPv6 NAT configuration coordination")
				}
				if _, exists := queuedCreateNATReservations[owner]; exists {
					rollback()
					return nil, fmt.Errorf("container creation already has a reserved network plan: %s", cfg.Name)
				}
				queuedCreateNATReservations[owner] = nil
				queuedCreateNATMirrorsIPv6[owner] = false
				addedOwners = append(addedOwners, owner)
			}
			continue
		}
		owner := createNATReservationOwner(cfg.Name)
		if owner == "" {
			rollback()
			return nil, fmt.Errorf("container name is required for NAT port reservation")
		}
		if _, exists := queuedCreateNATReservations[owner]; exists {
			rollback()
			return nil, fmt.Errorf("container creation already has reserved NAT ports: %s", cfg.Name)
		}
		if err := ValidateCreateNATPortAvailability(*cfg); err != nil {
			rollback()
			return nil, fmt.Errorf("%s: %w", cfg.Name, err)
		}

		explicit := createNATReservationMappings(*cfg, cfg.ManagementPort)
		if err := validateCreateNATReservationsAvailableLocked(explicit, owner); err != nil {
			rollback()
			return nil, fmt.Errorf("%s: %w", cfg.Name, err)
		}

		excluded := cfg.RequestedNATHostPorts()
		excluded = append(excluded, allReservedCreateNATHostPortsLocked(owner)...)
		if cfg.ManagementPort == 0 {
			port, err := config.AllocateSSHPortExcluding(excluded)
			if err != nil {
				rollback()
				return nil, fmt.Errorf("%s: %w", cfg.Name, err)
			}
			cfg.ManagementPort = port
		}

		if len(cfg.NATPortMappings) == 0 && cfg.PortMappingCount > 1 {
			generated, err := planDefaultCreateNATMappingsLocked(*cfg, cfg.PortMappingCount-1, owner)
			if err != nil {
				rollback()
				return nil, fmt.Errorf("%s: %w", cfg.Name, err)
			}
			cfg.NATPortMappings = generated
			cfg.PortMappingCount = len(generated) + 1
		}

		reservations := createNATReservationMappings(*cfg, cfg.ManagementPort)
		if err := validateCreateNATIPv6PortAvailability(*cfg, cfg.ManagementPort); err != nil {
			rollback()
			return nil, fmt.Errorf("%s: %w", cfg.Name, err)
		}
		if err := validateCreateNATReservationsAvailableLocked(reservations, owner); err != nil {
			rollback()
			return nil, fmt.Errorf("%s: %w", cfg.Name, err)
		}
		queuedCreateNATReservations[owner] = reservations
		queuedCreateNATMirrorsIPv6[owner] = createUsesIPv6PortSynchronization(*cfg)
		addedOwners = append(addedOwners, owner)
	}
	return planned, nil
}

func ReleaseQueuedCreateNATPorts(name string) {
	owner := createNATReservationOwner(name)
	if owner == "" {
		return
	}
	createNATReservationMu.Lock()
	delete(queuedCreateNATReservations, owner)
	delete(queuedCreateNATMirrorsIPv6, owner)
	createNATReservationMu.Unlock()
}

func activateCreateNATReservationLocked(managementPort int, reservations []config.PortMapping, owner string, mirrorsIPv6 bool) (int, func(), error) {
	createNATReservationNextID++
	reservationID := createNATReservationNextID
	createNATReservations[reservationID] = append([]config.PortMapping(nil), reservations...)
	activeCreateNATOwners[reservationID] = owner
	activeCreateNATMirrorsIPv6[reservationID] = mirrorsIPv6

	var once sync.Once
	release := func() {
		once.Do(func() {
			createNATReservationMu.Lock()
			delete(createNATReservations, reservationID)
			delete(activeCreateNATOwners, reservationID)
			delete(activeCreateNATMirrorsIPv6, reservationID)
			createNATReservationMu.Unlock()
		})
	}
	return managementPort, release, nil
}

func createNATReservationOwner(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func createNATReservationMappings(cfg ContainerConfig, managementPort int) []config.PortMapping {
	reservations := make([]config.PortMapping, 0, len(cfg.NATPortMappings))
	if managementPort > 0 {
		reservations = append(reservations, config.PortMapping{HostPort: managementPort, Protocol: "tcp"})
	}
	reservations = append(reservations, cfg.NATPortMappings...)
	return reservations
}

func validateCreateNATReservationsAvailableLocked(requested []config.PortMapping, exceptOwner string) error {
	for _, candidate := range requested {
		for _, reservations := range createNATReservations {
			if conflictingCreateNATReservation(candidate, reservations) {
				return fmt.Errorf("NAT host port %d/%s is reserved by another create task", candidate.HostPort, candidate.Protocol)
			}
		}
		for owner, reservations := range queuedCreateNATReservations {
			if owner == exceptOwner {
				continue
			}
			if conflictingCreateNATReservation(candidate, reservations) {
				return fmt.Errorf("NAT host port %d/%s is reserved by queued create task %s", candidate.HostPort, candidate.Protocol, owner)
			}
		}
	}
	return nil
}

func conflictingCreateNATReservation(candidate config.PortMapping, reservations []config.PortMapping) bool {
	for _, reserved := range reservations {
		if candidate.HostPort == reserved.HostPort && protocolsOverlap(candidate.Protocol, reserved.Protocol) {
			return true
		}
	}
	return false
}

func allReservedCreateNATHostPortsLocked(exceptOwner string) []int {
	ports := make([]int, 0)
	for _, reservations := range createNATReservations {
		for _, reserved := range reservations {
			ports = append(ports, reserved.HostPort)
		}
	}
	for owner, reservations := range queuedCreateNATReservations {
		if owner == exceptOwner {
			continue
		}
		for _, reserved := range reservations {
			ports = append(ports, reserved.HostPort)
		}
	}
	return ports
}

func planDefaultCreateNATMappingsLocked(cfg ContainerConfig, count int, owner string) ([]config.PortMapping, error) {
	if count <= 0 {
		return nil, nil
	}
	unavailable := map[int]bool{cfg.ManagementPort: true}
	for _, port := range allReservedCreateNATHostPortsLocked(owner) {
		unavailable[port] = true
	}
	for _, mapping := range cfg.NATPortMappings {
		unavailable[mapping.HostPort] = true
	}

	candidate := &config.Container{ID: -1}
	start, end := config.NATPortRange()
	mappings := make([]config.PortMapping, 0, count)
	for port := start; port <= end && len(mappings) < count; port++ {
		if unavailable[port] || !HostPortAvailable(candidate, "", port, "tcp") || !createNAT6PortAvailable(cfg, port, "tcp") {
			continue
		}
		unavailable[port] = true
		mappings = append(mappings, config.PortMapping{
			HostPort:      port,
			ContainerPort: port,
			Protocol:      "tcp",
			Description:   fmt.Sprintf("Port-%d", port),
		})
	}
	if len(mappings) != count {
		return nil, fmt.Errorf("not enough free NAT4 host ports for %d automatic mappings", count)
	}
	return mappings, nil
}

func sameCreateNATReservations(left, right []config.PortMapping) bool {
	if len(left) != len(right) {
		return false
	}
	counts := make(map[string]int, len(left))
	for _, mapping := range left {
		counts[fmt.Sprintf("%d/%s", mapping.HostPort, strings.ToLower(mapping.Protocol))]++
	}
	for _, mapping := range right {
		key := fmt.Sprintf("%d/%s", mapping.HostPort, strings.ToLower(mapping.Protocol))
		if counts[key] == 0 {
			return false
		}
		counts[key]--
	}
	return true
}

func allocateDefaultEqualPorts(c *config.Container, count int) []int {
	if count <= 0 {
		return nil
	}
	used := map[int]bool{}
	// Mark current container's ports
	for _, pm := range c.PortMappings {
		for _, hostIP := range expandPortMappingHostIPs(c, pm) {
			used[hostPortKey(hostIP, pm.HostPort)] = true
		}
		used[pm.ContainerPort] = true
	}
	// Also mark all other containers' host ports (LXC + KVM)
	for _, oc := range config.AppConfig.Containers {
		if oc.ID == c.ID {
			continue
		}
		for _, pm := range oc.PortMappings {
			oc := oc
			for _, hostIP := range expandPortMappingHostIPs(&oc, pm) {
				used[hostPortKey(hostIP, pm.HostPort)] = true
			}
		}
	}
	ports := make([]int, 0, count)
	start, end := config.NATPortRange()
	for next := start; next <= end && len(ports) < count; next++ {
		hostIP := c.PrimaryPublicIPv4()
		if !used[hostPortKey(hostIP, next)] && !used[next] {
			ports = append(ports, next)
		}
	}
	return ports
}

func HostPortAvailable(c *config.Container, hostIP string, hostPort int, protocol string) bool {
	if c == nil || hostPort <= 0 {
		return false
	}
	pm := config.PortMapping{HostIP: strings.TrimSpace(hostIP), HostPort: hostPort, Protocol: protocol}
	for _, existing := range c.PortMappings {
		if portMappingsConflict(c, pm, c, existing) {
			return false
		}
	}
	for _, oc := range config.AppConfig.Containers {
		if oc.ID == c.ID {
			continue
		}
		oc := oc
		for _, existing := range oc.PortMappings {
			if portMappingsConflict(c, pm, &oc, existing) {
				return false
			}
		}
	}
	if c.IPv6NATAddress != "" && ipv6MappingsSyncIPv4(c) && !IPv6HostPortAvailable(c, hostPort, protocol) {
		return false
	}
	return true
}

func expandPortMappingHostIPs(c *config.Container, pm config.PortMapping) []string {
	if strings.TrimSpace(pm.HostIP) != "" {
		return []string{strings.TrimSpace(pm.HostIP)}
	}
	if c != nil && len(c.PublicIPv4s) > 0 {
		values := make([]string, 0, len(c.PublicIPv4s))
		for _, item := range c.PublicIPv4s {
			if strings.TrimSpace(item.Address) != "" {
				values = append(values, strings.TrimSpace(item.Address))
			}
		}
		if len(values) > 0 {
			return values
		}
	}
	return []string{""}
}

func containerHasPublicIPv4(c *config.Container, hostIP string) bool {
	if c == nil {
		return false
	}
	for _, item := range c.PublicIPv4s {
		if item.Address == hostIP {
			return true
		}
	}
	return false
}

func portMappingsConflict(aContainer *config.Container, a config.PortMapping, bContainer *config.Container, b config.PortMapping) bool {
	if a.HostPort != b.HostPort || !protocolsOverlap(a.Protocol, b.Protocol) {
		return false
	}
	aIPs := expandPortMappingHostIPs(aContainer, a)
	bIPs := expandPortMappingHostIPs(bContainer, b)
	for _, aIP := range aIPs {
		for _, bIP := range bIPs {
			if aIP == "" || bIP == "" || aIP == bIP {
				return true
			}
		}
	}
	return false
}

func protocolsOverlap(a, b string) bool {
	a = strings.ToLower(strings.TrimSpace(a))
	b = strings.ToLower(strings.TrimSpace(b))
	if a == "" {
		a = "tcp"
	}
	if b == "" {
		b = "tcp"
	}
	if a == b || a == "all" || b == "all" {
		return true
	}
	return (a == "tcp+udp" && (b == "tcp" || b == "udp")) ||
		(b == "tcp+udp" && (a == "tcp" || a == "udp"))
}

func natRuleIPTag(ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return "any"
	}
	return strings.ReplaceAll(ip, ".", "_")
}

func displayHostIP(ip string) string {
	if strings.TrimSpace(ip) == "" {
		return "host"
	}
	return ip
}

func hostPortKey(hostIP string, port int) int {
	if hostIP == "" {
		return port
	}
	sum := 0
	for _, r := range hostIP {
		sum = sum*31 + int(r)
	}
	if sum < 0 {
		sum = -sum
	}
	return port + (sum % 1000000 * 100000)
}

// CleanFirewallRules removes all firewall rules for a container from the FORWARD chain.
func CleanFirewallRules(id int) {
	iptablesMu.Lock()
	defer iptablesMu.Unlock()
	cleanFirewallRulesLocked(id)
}

func cleanFirewallRulesLocked(id int) {
	tag := clicdTag(id)
	// Remove all rules with the firewall tag prefix
	cmd := exec.Command("bash", "-c",
		fmt.Sprintf("iptables -S FORWARD 2>/dev/null | grep 'clicd-%s-fw-' | sed 's/^-A /-D /' | while read rule; do iptables $rule; done", tag))
	cmd.CombinedOutput()
	cmd = exec.Command("bash", "-c",
		fmt.Sprintf("ip6tables -S FORWARD 2>/dev/null | grep 'clicd-%s-fw-' | sed 's/^-A /-D /' | while read rule; do ip6tables $rule; done", tag))
	cmd.CombinedOutput()

	// Also remove legacy default policy rules (without specific rule ID)
	for _, suffix := range []string{"default-in", "default-out"} {
		for _, proto := range []string{"tcp", "udp"} {
			exec.Command("iptables", "-D", "FORWARD",
				"-m", "comment", "--comment", fmt.Sprintf("clicd-%s-fw-%s-%s", tag, suffix, proto),
			).CombinedOutput()
		}
		exec.Command("ip6tables", "-D", "FORWARD",
			"-m", "comment", "--comment", fmt.Sprintf("clicd-%s-fw-%s", tag, suffix),
		).CombinedOutput()
	}
}

// ApplyFirewallRules applies iptables FORWARD rules for a container's firewall configuration.
func ApplyFirewallRules(id int) error {
	iptablesMu.Lock()
	defer iptablesMu.Unlock()
	return applyFirewallRulesLocked(id)
}

func applyFirewallRulesLocked(id int) error {
	c := config.FindContainer(id)
	if c == nil {
		return fmt.Errorf("container not found: %d", id)
	}
	snapshot := *c
	c = &snapshot

	rules, err := plannedFirewallRules(c)
	if err != nil {
		return err
	}
	healthy, err := firewallRulesPresent(c, rules)
	if err != nil || healthy {
		return err
	}
	cleanFirewallRulesLocked(id)
	for _, rule := range rules {
		if output, err := runPortMappingCommand(rule.command, rule.args...); err != nil {
			return fmt.Errorf("apply firewall: %w: %s", err, output)
		}
	}
	return nil
}

type firewallCommand struct {
	command string
	args    []string
}

func plannedFirewallRules(c *config.Container) ([]firewallCommand, error) {
	var rules []firewallCommand
	emit := func(command string, args []string) error {
		rules = append(rules, firewallCommand{command, append([]string(nil), args...)})
		return nil
	}
	err := walkFirewallRules(c, emit)
	return rules, err
}

func walkFirewallRules(c *config.Container, emit func(string, []string) error) error {
	// If firewall is disabled or no rules, nothing to apply
	if !c.FirewallEnabled {
		return nil
	}

	bridge := "lxcbr0"
	if c.IsKVM() {
		bridge = "virbr0"
	}
	containerIP := strings.TrimSpace(c.IP)
	containerIPv6s := firewallIPv6Addresses(c)
	if containerIP == "" && len(containerIPv6s) == 0 {
		return nil
	}
	tag := clicdTag(c.ID)

	defaultAction := normalizeFirewallDefaultAction(c.FirewallDefaultAction)
	if defaultAction == "DROP" {
		if containerIP != "" {
			if err := applyDefaultFirewallPolicy(tag, bridge, containerIP, emit); err != nil {
				return err
			}
		}
		if err := applyDefaultFirewallIPv6Policy(tag, bridge, containerIPv6s, emit); err != nil {
			return err
		}
	}

	for i := len(c.FirewallRules) - 1; i >= 0; i-- {
		rule := c.FirewallRules[i]
		if !rule.Enabled {
			continue
		}
		if containerIP != "" && firewallRuleAppliesToFamily(rule, true) {
			if err := applyOneFirewallRule(tag, bridge, containerIP, rule, emit); err != nil {
				return fmt.Errorf("failed to apply firewall rule %s for container %d: %w", rule.ID, c.ID, err)
			}
		}
		if len(containerIPv6s) > 0 && firewallRuleAppliesToFamily(rule, false) {
			if err := applyOneFirewallIPv6Rule(tag, bridge, containerIPv6s, rule, emit); err != nil {
				return fmt.Errorf("failed to apply IPv6 firewall rule %s for container %d: %w", rule.ID, c.ID, err)
			}
		}
	}

	return nil
}

func normalizeFirewallDefaultAction(action string) string {
	action = strings.ToUpper(strings.TrimSpace(action))
	if action == "ACCEPT" {
		return "ACCEPT"
	}
	return "DROP"
}

func normalizeFirewallNetwork(network string) string {
	network = strings.ToLower(strings.TrimSpace(network))
	switch network {
	case "", "ipv4", "nat4":
		return "ipv4"
	case "ipv6":
		return "ipv6"
	case "all", "both":
		return "all"
	default:
		return "ipv4"
	}
}

func firewallRuleAppliesToFamily(rule config.FirewallRule, ipv4 bool) bool {
	network := normalizeFirewallNetwork(rule.Network)
	if network == "ipv4" {
		return ipv4
	}
	if network == "ipv6" {
		return !ipv4
	}
	if rule.SourceIP == "" {
		return true
	}
	addr := firewallIPSpecAddr(rule.SourceIP)
	if !addr.IsValid() {
		return true
	}
	if ipv4 {
		return addr.Is4()
	}
	return addr.Is6() && !addr.Is4In6()
}

func firewallIPSpecAddr(value string) netip.Addr {
	value = strings.TrimSpace(value)
	if value == "" {
		return netip.Addr{}
	}
	if strings.Contains(value, "/") {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return netip.Addr{}
		}
		return prefix.Addr()
	}
	addr, err := netip.ParseAddr(value)
	if err != nil {
		return netip.Addr{}
	}
	return addr
}

func firewallIPv6Addresses(c *config.Container) []string {
	if c == nil {
		return nil
	}
	c.NormalizeNetworkAssignments()
	seen := map[string]bool{}
	result := []string{}
	for _, assignment := range c.IPv6Addresses {
		ip := strings.TrimSpace(assignment.Address)
		if ip == "" || seen[ip] {
			continue
		}
		if addr, err := netip.ParseAddr(ip); err == nil && addr.Is6() && !addr.Is4In6() {
			seen[ip] = true
			result = append(result, ip)
		}
	}
	if address, err := netip.ParseAddr(c.IPv6NATAddress); err == nil && address.Is6() && !address.Is4In6() && !seen[address.String()] {
		result = append(result, address.String())
	}
	return result
}

func applyOneFirewallRule(tag, bridge, containerIP string, rule config.FirewallRule, emit func(string, []string) error) error {
	commentTag := fmt.Sprintf("clicd-%s-fw-%s", tag, rule.ID)

	// Build base iptables args
	args := []string{"-I", "FORWARD", "1"}

	// Direction: in = traffic arriving at container (-o bridge -d containerIP)
	//            out = traffic leaving container (-i bridge -s containerIP)
	switch rule.Direction {
	case "in":
		args = append(args, "-o", bridge, "-d", containerIP+"/32")
	case "out":
		args = append(args, "-i", bridge, "-s", containerIP+"/32")
	default:
		return fmt.Errorf("invalid direction: %s", rule.Direction)
	}

	// Protocol
	switch rule.Protocol {
	case "tcp", "udp":
		args = append(args, "-p", rule.Protocol)
	case "icmp":
		args = append(args, "-p", "icmp")
	case "all":
		// no protocol filter
	default:
		return fmt.Errorf("invalid protocol: %s", rule.Protocol)
	}

	// Port matching (only for tcp/udp)
	if rule.Port != "" && (rule.Protocol == "tcp" || rule.Protocol == "udp") {
		// For "in" direction, traffic going TO the container uses --dport
		// For "out" direction, traffic going FROM the container uses --dport (destination port on remote)
		args = append(args, firewallPortArgs(rule.Port)...)
	}

	// Source IP filter (for "out" direction, this matches the remote source; for "in", it matches the sender)
	if rule.SourceIP != "" {
		switch rule.Direction {
		case "in":
			args = append(args, "-s", rule.SourceIP)
		case "out":
			args = append(args, "-d", rule.SourceIP)
		}
	}

	// Action
	action := "DROP"
	if rule.Action == "ACCEPT" {
		action = "ACCEPT"
	}
	args = append(args, "-j", action)

	// Comment tag for cleanup
	args = append(args, "-m", "comment", "--comment", commentTag)

	if err := emit("iptables", args); err != nil {
		return err
	}
	return nil
}

func applyOneFirewallIPv6Rule(tag, bridge string, containerIPs []string, rule config.FirewallRule, emit func(string, []string) error) error {
	for _, containerIP := range containerIPs {
		commentTag := fmt.Sprintf("clicd-%s-fw-%s-v6-%s", tag, rule.ID, firewallCommentIPTag(containerIP))
		args := []string{"-I", "FORWARD", "1"}

		switch rule.Direction {
		case "in":
			args = append(args, "-o", bridge, "-d", containerIP+"/128")
		case "out":
			args = append(args, "-i", bridge, "-s", containerIP+"/128")
		default:
			return fmt.Errorf("invalid direction: %s", rule.Direction)
		}

		switch rule.Protocol {
		case "tcp", "udp":
			args = append(args, "-p", rule.Protocol)
		case "icmp":
			args = append(args, "-p", "ipv6-icmp")
		case "all":
		default:
			return fmt.Errorf("invalid protocol: %s", rule.Protocol)
		}

		if rule.Port != "" && (rule.Protocol == "tcp" || rule.Protocol == "udp") {
			args = append(args, firewallPortArgs(rule.Port)...)
		}

		if rule.SourceIP != "" {
			switch rule.Direction {
			case "in":
				args = append(args, "-s", rule.SourceIP)
			case "out":
				args = append(args, "-d", rule.SourceIP)
			}
		}

		action := "DROP"
		if rule.Action == "ACCEPT" {
			action = "ACCEPT"
		}
		args = append(args, "-j", action)
		args = append(args, "-m", "comment", "--comment", commentTag)

		if err := emit("ip6tables", args); err != nil {
			return err
		}
	}
	return nil
}

func firewallPortArgs(port string) []string {
	spec := normalizePortSpec(port)
	if strings.Contains(spec, ",") {
		return []string{"-m", "multiport", "--dports", spec}
	}
	return []string{"--dport", spec}
}

// normalizePortSpec converts user port input to iptables-compatible port spec.
// "80,443" -> "80,443", "8000-9000" -> "8000:9000", "80,443,8000-9000" -> "80,443,8000:9000"
func normalizePortSpec(port string) string {
	port = strings.TrimSpace(port)
	if port == "" {
		return ""
	}
	parts := strings.Split(port, ",")
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if strings.Contains(part, "-") && !strings.Contains(part, ":") {
			bounds := strings.SplitN(part, "-", 2)
			if len(bounds) == 2 {
				part = strings.TrimSpace(bounds[0]) + ":" + strings.TrimSpace(bounds[1])
			}
		}
		parts[i] = part
	}
	return strings.Join(parts, ",")
}

func applyDefaultFirewallPolicy(tag, bridge, containerIP string, emit func(string, []string) error) error {
	defaults := [][]string{
		{
			"-I", "FORWARD", "1",
			"-o", bridge,
			"-d", containerIP + "/32",
			"-j", "DROP",
			"-m", "comment", "--comment", fmt.Sprintf("clicd-%s-fw-default-in", tag),
		},
		{
			"-I", "FORWARD", "1",
			"-i", bridge,
			"-s", containerIP + "/32",
			"-j", "DROP",
			"-m", "comment", "--comment", fmt.Sprintf("clicd-%s-fw-default-out", tag),
		},
	}
	for _, args := range defaults {
		if err := emit("iptables", args); err != nil {
			return err
		}
	}
	return nil
}

func applyDefaultFirewallIPv6Policy(tag, bridge string, containerIPs []string, emit func(string, []string) error) error {
	for _, containerIP := range containerIPs {
		defaults := [][]string{
			{
				"-I", "FORWARD", "1",
				"-o", bridge,
				"-d", containerIP + "/128",
				"-j", "DROP",
				"-m", "comment", "--comment", fmt.Sprintf("clicd-%s-fw-default-in-v6-%s", tag, firewallCommentIPTag(containerIP)),
			},
			{
				"-I", "FORWARD", "1",
				"-i", bridge,
				"-s", containerIP + "/128",
				"-j", "DROP",
				"-m", "comment", "--comment", fmt.Sprintf("clicd-%s-fw-default-out-v6-%s", tag, firewallCommentIPTag(containerIP)),
			},
		}
		for _, args := range defaults {
			if err := emit("ip6tables", args); err != nil {
				return err
			}
		}
	}
	return nil
}

func firewallCommentIPTag(ip string) string {
	replacer := strings.NewReplacer(":", "_", ".", "_", "/", "_")
	return replacer.Replace(ip)
}
