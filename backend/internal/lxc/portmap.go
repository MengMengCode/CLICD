package lxc

import (
	"fmt"
	"net/netip"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"clicd/internal/config"
)

var (
	createNATReservationMu     sync.Mutex
	createNATReservationNextID uint64
	createNATReservations      = map[uint64][]config.PortMapping{}
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
	subnet := "10.0.3.0/24"
	if c.IsKVM() {
		bridge = "virbr0"
		subnet = "192.168.122.0/24"
	}

	EnsureForwardRules(bridge)
	m.CleanPortMappings(id)
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
	for i := range config.AppConfig.Containers {
		c := &config.AppConfig.Containers[i]
		if c.Status != "running" || strings.TrimSpace(c.IP) == "" {
			continue
		}
		if err := m.ApplyPortMappings(c.ID); err != nil {
			fmt.Printf("Warning: failed to restore port mappings for %s: %v\n", c.Name, err)
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
	tag := clicdTag(id)
	for _, chain := range []string{"PREROUTING", "POSTROUTING"} {
		cmd := exec.Command("sh", "-c",
			fmt.Sprintf("iptables -t nat -L %s -n --line-numbers 2>/dev/null | grep 'clicd-%s-' | awk '{print $1}' | sort -rn | while read num; do iptables -t nat -D %s $num; done", chain, tag, chain))
		cmd.Run()
	}
	cmd := exec.Command("sh", "-c",
		fmt.Sprintf("iptables -S FORWARD 2>/dev/null | grep 'clicd-%s-' | sed 's/^-A /-D /' | while read rule; do iptables $rule; done", tag))
	cmd.Run()
	return nil
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
	normalized, err := normalizePortMapping(c, index, pm)
	if err != nil {
		return nil, err
	}
	c.PortMappings[index] = normalized
	if err := persistAndReloadMappings(m, c); err != nil {
		return nil, err
	}
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
	if c.PortMappings[index].Description == "SSH" {
		return nil, fmt.Errorf("SSH default mapping cannot be deleted")
	}
	c.PortMappings = append(c.PortMappings[:index], c.PortMappings[index+1:]...)
	if err := persistAndReloadMappings(m, c); err != nil {
		return nil, err
	}
	return c.PortMappings, nil
}

func persistAndReloadMappings(m *Manager, c *config.Container) error {
	config.SaveConfig()
	if c.Status == "running" && c.IP != "" {
		return m.ApplyPortMappings(c.ID)
	}
	return nil
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

	if err := ValidateCreateNATPortAvailability(cfg); err != nil {
		return 0, nil, err
	}
	requestedReservations := append([]config.PortMapping(nil), cfg.NATPortMappings...)
	if cfg.ManagementPort > 0 {
		requestedReservations = append(requestedReservations, config.PortMapping{
			HostPort: cfg.ManagementPort,
			Protocol: "tcp",
		})
	}
	for _, requested := range requestedReservations {
		for _, reservations := range createNATReservations {
			for _, reserved := range reservations {
				if requested.HostPort == reserved.HostPort && protocolsOverlap(requested.Protocol, reserved.Protocol) {
					return 0, nil, fmt.Errorf("NAT host port %d/%s is reserved by another create task", requested.HostPort, requested.Protocol)
				}
			}
		}
	}

	excluded := cfg.RequestedNATHostPorts()
	for _, reservations := range createNATReservations {
		for _, reserved := range reservations {
			excluded = append(excluded, reserved.HostPort)
		}
	}
	managementPort := cfg.ManagementPort
	if managementPort == 0 {
		var err error
		managementPort, err = config.AllocateSSHPortExcluding(excluded)
		if err != nil {
			return 0, nil, err
		}
	}

	createNATReservationNextID++
	reservationID := createNATReservationNextID
	reservations := make([]config.PortMapping, 0, len(cfg.NATPortMappings)+1)
	reservations = append(reservations, config.PortMapping{HostPort: managementPort, Protocol: "tcp"})
	reservations = append(reservations, cfg.NATPortMappings...)
	createNATReservations[reservationID] = reservations

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
