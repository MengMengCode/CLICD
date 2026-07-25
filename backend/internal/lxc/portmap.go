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
	createNATReservationMu      sync.Mutex
	createNATReservationNextID  uint64
	createNATReservations       = map[uint64][]config.PortMapping{}
	queuedCreateNATReservations = map[string][]config.PortMapping{}
)

// ApplyPortMappings applies iptables DNAT rules for a container's port mappings
func (m *Manager) ApplyPortMappings(id int) error {
	c := config.FindContainer(id)
	if c == nil {
		return fmt.Errorf("container not found: %d", id)
	}
	if c.IP == "" {
		return fmt.Errorf("container has no IP")
	}
	EnsureAssignedPublicIPv4s(c.PublicIPv4s)
	tag := clicdTag(id)
	bridge := "lxcbr0"
	subnet := config.LXCNATNetwork().Subnet
	if c.IsKVM() {
		bridge = "virbr0"
		subnet = config.KVMNATNetwork().Subnet
	}

	EnsureForwardRules(bridge)
	if err := m.CleanPortMappings(id); err != nil {
		return fmt.Errorf("clean existing port mappings for container %d: %w", id, err)
	}
	deleteBridgeMasquerade(subnet)

	for _, pm := range c.PortMappings {
		for _, hostIP := range expandPortMappingHostIPs(c, pm) {
			args := []string{
				"-t", "nat",
				"-I", "PREROUTING", "1",
				"-p", pm.Protocol,
			}
			if hostIP != "" {
				args = append(args, "-d", hostIP)
			}
			args = append(args,
				"--dport", fmt.Sprintf("%d", pm.HostPort),
				"-j", "DNAT",
				"--to-destination", fmt.Sprintf("%s:%d", c.IP, pm.ContainerPort),
				"-m", "comment", "--comment", fmt.Sprintf("clicd-%s-%s-%d", tag, natRuleIPTag(hostIP), pm.HostPort),
			)
			cmd := exec.Command("iptables", args...)
			output, err := cmd.CombinedOutput()
			if err != nil {
				fmt.Printf("Warning: failed to apply port mapping %s:%d->%s:%d: %v, output: %s\n",
					displayHostIP(hostIP), pm.HostPort, c.IP, pm.ContainerPort, err, string(output))
				continue
			}
			fmt.Printf("Port mapping: %s:%d -> %s:%d\n", displayHostIP(hostIP), pm.HostPort, c.IP, pm.ContainerPort)
		}
	}

	// When container has public IPv4, apply full port passthrough DNAT so the
	// container owns all ports on its public IP (no NAT management needed).
	if len(c.PublicIPv4s) > 0 {
		ensureIndependentIPv4Ingress(c, tag)
	}

	applyIPv4EgressPolicy(c, bridge, subnet, tag)

	if err := ApplyFirewallRules(id); err != nil {
		return err
	}

	return nil
}

func ensureIndependentIPv4Ingress(c *config.Container, tag string) {
	if c == nil || c.IP == "" || len(c.PublicIPv4s) == 0 {
		return
	}

	for _, assignment := range c.PublicIPv4s {
		hostIP := strings.TrimSpace(assignment.Address)
		if hostIP == "" {
			continue
		}
		// Full port passthrough: DNAT all TCP+UDP traffic on this public IP to the container.
		for _, proto := range []string{"tcp", "udp"} {
			args := []string{
				"-t", "nat",
				"-I", "PREROUTING", "1",
				"-d", hostIP,
				"-p", proto,
				"-j", "DNAT",
				"--to-destination", c.IP,
				"-m", "comment", "--comment", fmt.Sprintf("clicd-%s-%s-all-%s", tag, natRuleIPTag(hostIP), proto),
			}
			cmd := exec.Command("iptables", args...)
			output, err := cmd.CombinedOutput()
			if err != nil {
				fmt.Printf("Warning: failed to apply %s passthrough %s->%s: %v, output: %s\n",
					proto, hostIP, c.IP, err, string(output))
				continue
			}
			fmt.Printf("IPv4 passthrough (%s): %s -> %s (all ports)\n", proto, hostIP, c.IP)
		}
	}
}

func applyIPv4EgressPolicy(c *config.Container, bridge, subnet, tag string) {
	if c == nil || strings.TrimSpace(c.IP) == "" {
		return
	}
	if containerAllowsPublicIPv4Egress(c) {
		if _, ok := primaryPublicIPv4Assignment(c); ok {
			applyPublicIPv4SNAT(c, tag)
			return
		}
		ensureContainerMasquerade(c, tag)
		return
	}
	ensureIPv4EgressBlocked(c, bridge, subnet, tag)
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

func ensureContainerMasquerade(c *config.Container, tag string) {
	args := []string{
		"-s", c.IP + "/32",
		"-m", "comment", "--comment", fmt.Sprintf("clicd-%s-masq", tag),
		"-j", "MASQUERADE",
	}
	if host := DetectPublicIPv4(); strings.TrimSpace(host.Interface) != "" {
		args = append([]string{"-o", strings.TrimSpace(host.Interface)}, args...)
	} else {
		args = append([]string{"-o", "eth+"}, args...)
	}
	ensureNATRule("POSTROUTING", args)
}

func ensureIPv4EgressBlocked(c *config.Container, bridge, subnet, tag string) {
	args := []string{
		"-i", bridge,
		"-s", c.IP + "/32",
		"!", "-d", subnet,
		"-m", "comment", "--comment", fmt.Sprintf("clicd-%s-v4-egress-block", tag),
		"-j", "REJECT",
	}
	ensureFilterRule("FORWARD", args)
}

func ensureNATRule(chain string, args []string) {
	check := append([]string{"-t", "nat", "-C", chain}, args...)
	if exec.Command("iptables", check...).Run() == nil {
		return
	}
	add := append([]string{"-t", "nat", "-I", chain, "1"}, args...)
	exec.Command("iptables", add...).Run()
}

func ensureFilterRule(chain string, args []string) {
	check := append([]string{"-C", chain}, args...)
	if exec.Command("iptables", check...).Run() == nil {
		return
	}
	add := append([]string{"-I", chain, "1"}, args...)
	exec.Command("iptables", add...).Run()
}

func deleteBridgeMasquerade(subnet string) {
	for exec.Command("iptables", "-t", "nat", "-D", "POSTROUTING", "-s", subnet, "-o", "eth+", "-j", "MASQUERADE").Run() == nil {
	}
}

func applyPublicIPv4SNAT(c *config.Container, tag string) {
	if c == nil || strings.TrimSpace(c.IP) == "" {
		return
	}
	assignment, ok := primaryPublicIPv4Assignment(c)
	if !ok {
		return
	}
	hostIP := strings.TrimSpace(assignment.Address)
	if hostIP == "" {
		return
	}
	iface := strings.TrimSpace(assignment.Interface)
	if iface == "" {
		if info, ok := publicIPv4InfoByAddress(hostIP); ok {
			iface = strings.TrimSpace(info.Interface)
		}
	}
	if iface == "" {
		if host := DetectPublicIPv4(); host.Interface != "" {
			iface = host.Interface
		}
	}
	args := []string{
		"-t", "nat",
		"-I", "POSTROUTING", "1",
		"-s", c.IP + "/32",
	}
	if iface != "" {
		args = append(args, "-o", iface)
	}
	args = append(args,
		"-m", "comment", "--comment", fmt.Sprintf("clicd-%s-snat-%s", tag, natRuleIPTag(hostIP)),
		"-j", "SNAT", "--to-source", hostIP,
	)
	if output, err := exec.Command("iptables", args...).CombinedOutput(); err != nil {
		fmt.Printf("Warning: failed to apply public IPv4 SNAT %s -> %s: %v, output: %s\n", c.IP, hostIP, err, string(output))
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

func EnsureAllRunningPortMappings() {
	m := NewManager()
	m.cleanOrphanedPortMappings()
	for i := range config.AppConfig.Containers {
		c := &config.AppConfig.Containers[i]
		if c.Status != "running" || strings.TrimSpace(c.IP) == "" {
			if err := m.CleanPortMappings(c.ID); err != nil {
				fmt.Printf("Warning: failed to clean inactive port mappings for %s: %v\n", c.Name, err)
			}
			continue
		}
		if err := m.ApplyPortMappings(c.ID); err != nil {
			fmt.Printf("Warning: failed to restore port mappings for %s: %v\n", c.Name, err)
		}
	}
}

var taggedContainerIDPattern = regexp.MustCompile(`clicd-c([0-9]+)-`)

func (m *Manager) cleanOrphanedPortMappings() {
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

// EnsureForwardRules makes sure iptables FORWARD chain allows bridge traffic.
func EnsureForwardRules(bridge string) {
	if bridge == "" {
		bridge = "lxcbr0"
	}
	ensureLibvirtForwardRules(bridge)
	rules := [][]string{
		{"-i", bridge, "-j", "ACCEPT"},
		{"-o", bridge, "-j", "ACCEPT"},
		{"-i", bridge, "-o", bridge, "-j", "ACCEPT"},
	}
	for _, args := range rules {
		for {
			deleteArgs := append([]string{"-D", "FORWARD"}, args...)
			if exec.Command("iptables", deleteArgs...).Run() != nil {
				break
			}
		}
		appendArgs := append([]string{"-A", "FORWARD"}, args...)
		exec.Command("iptables", appendArgs...).Run()
	}
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
		for {
			deleteArgs := append([]string{"-D", rule.chain}, rule.args...)
			if exec.Command("iptables", deleteArgs...).Run() != nil {
				break
			}
		}
		insertArgs := append([]string{"-I", rule.chain, "1"}, rule.args...)
		exec.Command("iptables", insertArgs...).Run()
	}
}

// CleanPortMappings removes all iptables rules for a container
func (m *Manager) CleanPortMappings(id int) error {
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
		for _, mapping := range c.PortMappings {
			clearPortMappingConntrack(mapping)
		}
	}
	return errors.Join(cleanupErrors...)
}

func deleteTaggedIPTablesRules(table, chain, marker string) error {
	listArgs := []string{"-w", "5"}
	if table != "" {
		listArgs = append(listArgs, "-t", table)
	}
	listArgs = append(listArgs, "-L", chain, "-n", "--line-numbers")
	output, err := exec.Command("iptables", listArgs...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("list iptables %s/%s: %w: %s", tableName(table), chain, err, strings.TrimSpace(string(output)))
	}

	var deleteErrors []error
	for _, lineNumber := range taggedRuleLineNumbers(output, marker) {
		deleteArgs := []string{"-w", "5"}
		if table != "" {
			deleteArgs = append(deleteArgs, "-t", table)
		}
		deleteArgs = append(deleteArgs, "-D", chain, strconv.Itoa(lineNumber))
		if output, err := exec.Command("iptables", deleteArgs...).CombinedOutput(); err != nil {
			deleteErrors = append(deleteErrors, fmt.Errorf(
				"delete iptables %s/%s rule %d: %w: %s",
				tableName(table), chain, lineNumber, err, strings.TrimSpace(string(output)),
			))
		}
	}
	return errors.Join(deleteErrors...)
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

func clearPortMappingConntrack(mapping config.PortMapping) {
	args := portMappingConntrackDeleteArgs(mapping)
	if len(args) == 0 {
		return
	}
	// conntrack exits non-zero when no matching flow exists; that is already clean.
	_ = exec.Command("conntrack", args...).Run()
}

func portMappingConntrackDeleteArgs(mapping config.PortMapping) []string {
	protocol := strings.ToLower(strings.TrimSpace(mapping.Protocol))
	if protocol != "tcp" && protocol != "udp" {
		return nil
	}
	if mapping.HostPort < 1 || mapping.HostPort > 65535 {
		return nil
	}
	args := []string{
		"-D",
		"-p", protocol,
		"--dport", strconv.Itoa(mapping.HostPort),
	}
	if hostIP := strings.TrimSpace(mapping.HostIP); hostIP != "" {
		args = append(args, "--dst", hostIP)
	}
	return args
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
	c.PortMappings = append(c.PortMappings, normalized)
	if err := persistAndReloadMappings(m, c); err != nil {
		return nil, err
	}
	return c.PortMappings, nil
}

// UpdatePortMapping updates an existing NAT rule
func (m *Manager) UpdatePortMapping(id int, index int, pm config.PortMapping) ([]config.PortMapping, error) {
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
	c.PortMappings[index] = normalized
	if err := persistAndReloadMappings(m, c); err != nil {
		return nil, err
	}
	clearPortMappingConntrack(existing)
	return c.PortMappings, nil
}

// DeletePortMapping removes a NAT rule
func (m *Manager) DeletePortMapping(id int, index int) ([]config.PortMapping, error) {
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
	c.PortMappings = append(c.PortMappings[:index], c.PortMappings[index+1:]...)
	if err := persistAndReloadMappings(m, c); err != nil {
		return nil, err
	}
	clearPortMappingConntrack(removed)
	return c.PortMappings, nil
}

func persistAndReloadMappings(m *Manager, c *config.Container) error {
	syncContainerSSHPort(c)
	config.SaveConfig()
	if c.Status == "running" && c.IP != "" {
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
	if !cfg.WantsNAT() {
		return 0, func() {}, nil
	}

	createNATReservationMu.Lock()
	defer createNATReservationMu.Unlock()

	owner := createNATReservationOwner(cfg.Name)
	requestedReservations := createNATReservationMappings(cfg, cfg.ManagementPort)
	if queued, ok := queuedCreateNATReservations[owner]; ok {
		if !sameCreateNATReservations(queued, requestedReservations) {
			return 0, nil, fmt.Errorf("queued NAT port plan for %s no longer matches the create task", cfg.Name)
		}
		delete(queuedCreateNATReservations, owner)
		return activateCreateNATReservationLocked(cfg.ManagementPort, queued)
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
	return activateCreateNATReservationLocked(managementPort, reservations)
}

// ReserveBatchCreateNATPorts resolves every automatic NAT port and reserves
// the complete batch before any create task is enqueued.
func ReserveBatchCreateNATPorts(configs []ContainerConfig) ([]ContainerConfig, error) {
	createNATReservationMu.Lock()
	defer createNATReservationMu.Unlock()

	planned := append([]ContainerConfig(nil), configs...)
	addedOwners := make([]string, 0, len(planned))
	rollback := func() {
		for _, owner := range addedOwners {
			delete(queuedCreateNATReservations, owner)
		}
	}

	for i := range planned {
		cfg := &planned[i]
		cfg.NATPortMappings = append([]config.PortMapping(nil), cfg.NATPortMappings...)
		if !cfg.WantsNAT() {
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
		if err := validateCreateNATReservationsAvailableLocked(reservations, owner); err != nil {
			rollback()
			return nil, fmt.Errorf("%s: %w", cfg.Name, err)
		}
		queuedCreateNATReservations[owner] = reservations
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
	createNATReservationMu.Unlock()
}

func activateCreateNATReservationLocked(managementPort int, reservations []config.PortMapping) (int, func(), error) {
	createNATReservationNextID++
	reservationID := createNATReservationNextID
	createNATReservations[reservationID] = append([]config.PortMapping(nil), reservations...)

	var once sync.Once
	release := func() {
		once.Do(func() {
			createNATReservationMu.Lock()
			delete(createNATReservations, reservationID)
			createNATReservationMu.Unlock()
		})
	}
	return managementPort, release, nil
}

func createNATReservationOwner(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func createNATReservationMappings(cfg ContainerConfig, managementPort int) []config.PortMapping {
	reservations := make([]config.PortMapping, 0, len(cfg.NATPortMappings)+1)
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
		if unavailable[port] || !HostPortAvailable(candidate, "", port, "tcp") {
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
	c := config.FindContainer(id)
	if c == nil {
		return fmt.Errorf("container not found: %d", id)
	}

	// Always clean existing firewall rules first
	CleanFirewallRules(id)

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
	tag := clicdTag(id)

	defaultAction := normalizeFirewallDefaultAction(c.FirewallDefaultAction)
	if defaultAction == "DROP" {
		if containerIP != "" {
			if err := applyDefaultFirewallPolicy(tag, bridge, containerIP); err != nil {
				return err
			}
		}
		if err := applyDefaultFirewallIPv6Policy(tag, bridge, containerIPv6s); err != nil {
			return err
		}
	}

	for i := len(c.FirewallRules) - 1; i >= 0; i-- {
		rule := c.FirewallRules[i]
		if !rule.Enabled {
			continue
		}
		if containerIP != "" && firewallRuleAppliesToFamily(rule, true) {
			if err := applyOneFirewallRule(tag, bridge, containerIP, rule); err != nil {
				return fmt.Errorf("failed to apply firewall rule %s for container %d: %w", rule.ID, id, err)
			}
		}
		if len(containerIPv6s) > 0 && firewallRuleAppliesToFamily(rule, false) {
			if err := applyOneFirewallIPv6Rule(tag, bridge, containerIPv6s, rule); err != nil {
				return fmt.Errorf("failed to apply IPv6 firewall rule %s for container %d: %w", rule.ID, id, err)
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
	return result
}

func applyOneFirewallRule(tag, bridge, containerIP string, rule config.FirewallRule) error {
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

	cmd := exec.Command("iptables", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("iptables error: %s", string(output))
	}
	return nil
}

func applyOneFirewallIPv6Rule(tag, bridge string, containerIPs []string, rule config.FirewallRule) error {
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

		cmd := exec.Command("ip6tables", args...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("ip6tables error: %s", string(output))
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

func applyDefaultFirewallPolicy(tag, bridge, containerIP string) error {
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
		cmd := exec.Command("iptables", args...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("iptables default firewall error: %s", string(output))
		}
	}
	return nil
}

func applyDefaultFirewallIPv6Policy(tag, bridge string, containerIPs []string) error {
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
			cmd := exec.Command("ip6tables", args...)
			output, err := cmd.CombinedOutput()
			if err != nil {
				return fmt.Errorf("ip6tables default firewall error: %s", string(output))
			}
		}
	}
	return nil
}

func firewallCommentIPTag(ip string) string {
	replacer := strings.NewReplacer(":", "_", ".", "_", "/", "_")
	return replacer.Replace(ip)
}
