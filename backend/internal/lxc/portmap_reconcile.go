package lxc

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os/exec"
	"slices"
	"sort"
	"strconv"
	"strings"

	"clicd/internal/config"
)

var runPortMappingCommand = func(command string, args ...string) ([]byte, error) {
	return exec.Command(command, args...).CombinedOutput()
}

type mappingRule struct {
	table, chain string
	args         []string
}

// Compare ordered firewall rules against the kernel, including their precedence
// over broad bridge accepts. This keeps firewall self-healing after an external
// flush without deleting/reinserting healthy rules during routine NAT polling.
func firewallRulesPresent(c *config.Container, rules []firewallCommand) (bool, error) {
	marker := "clicd-" + clicdTag(c.ID) + "-fw-"
	bridge := "lxcbr0"
	if c.IsKVM() {
		bridge = "virbr0"
	}
	for _, command := range []string{"iptables", "ip6tables"} {
		var wanted []string
		for _, rule := range rules {
			if rule.command == command {
				wanted = append(wanted, mappingRuleKey(mappingRule{"filter", "FORWARD", rule.args[3:]}))
			}
		}
		slices.Reverse(wanted) // Each original command inserts at position 1.
		output, err := runPortMappingCommand(command, "-w", "5", "-S", "FORWARD")
		if err != nil {
			if command == "ip6tables" && len(wanted) == 0 && len(firewallIPv6Addresses(c)) == 0 {
				continue // Hosts with the IPv6 module disabled need no IPv6 policy.
			}
			return false, fmt.Errorf("inspect %s firewall: %w: %s", command, err, output)
		}
		var found []string
		broadAccept := false
		for _, line := range strings.Split(string(output), "\n") {
			fields := parseIPTablesRuleFields(line)
			if len(fields) < 3 || fields[0] != "-A" || fields[1] != "FORWARD" {
				continue
			}
			args := fields[2:]
			if strings.HasPrefix(ruleOption(args, "--comment"), marker) {
				if broadAccept {
					return false, nil
				}
				found = append(found, mappingRuleKey(mappingRule{"filter", "FORWARD", args}))
			} else if ruleOption(args, "-j") == "ACCEPT" && ruleOption(args, "-s") == "" && ruleOption(args, "-d") == "" &&
				(ruleOption(args, "-i") == bridge || ruleOption(args, "-o") == bridge) {
				broadAccept = true
			}
		}
		if !slices.Equal(found, wanted) {
			return false, nil
		}
	}
	return true, nil
}

func ensureBridgeAcceptRule(chain string, args []string) {
	if _, err := runPortMappingCommand("iptables", append([]string{"-w", "5", "-C", chain}, args...)...); err == nil {
		return
	}
	output, err := runPortMappingCommand("iptables", "-w", "5", "-S", chain)
	if err != nil {
		return
	}
	position, count := 1, 0
	for _, line := range strings.Split(string(output), "\n") {
		fields := parseIPTablesRuleFields(line)
		if len(fields) < 3 || fields[0] != "-A" || fields[1] != chain {
			continue
		}
		count++
		if strings.HasPrefix(ruleOption(fields[2:], "--comment"), "clicd-") {
			position = count + 1
		}
	}
	_, _ = runPortMappingCommand("iptables", append([]string{"-w", "5", "-I", chain, strconv.Itoa(position)}, args...)...)
}

func ruleOption(args []string, option string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == option {
			return args[i+1]
		}
	}
	return ""
}

// iptables -S canonicalizes addresses, moves match modules, and adds implicit
// tcp/udp matches. Compare these representations without depending on ordering.
func mappingRuleKey(rule mappingRule) string {
	parts := []string{rule.table, rule.chain}
	negated := false
	for i := 0; i < len(rule.args); i++ {
		key := rule.args[i]
		if key == "!" {
			negated = true
			continue
		}
		value := ""
		if i+1 < len(rule.args) && !strings.HasPrefix(rule.args[i+1], "-") && rule.args[i+1] != "!" {
			i++
			value = rule.args[i]
		}
		if !negated && ((key == "-m" && (value == "tcp" || value == "udp")) ||
			(key == "--reject-with" && value == "icmp-port-unreachable")) {
			continue
		}
		if key == "-s" || key == "-d" {
			if prefix, err := netip.ParsePrefix(value); err == nil {
				value = prefix.Masked().String()
			} else if address, err := netip.ParseAddr(value); err == nil {
				value = netip.PrefixFrom(address, address.BitLen()).String()
			}
		}
		if key == "--ctstate" {
			states := strings.Split(value, ",")
			sort.Strings(states)
			value = strings.Join(states, ",")
		}
		if negated {
			key = "!" + key
			negated = false
		}
		parts = append(parts, key+"="+value)
	}
	sort.Strings(parts[2:])
	return strings.Join(parts, "\x00")
}

// Reconcile only this owner's network rules; firewall rules have their own
// ordering and lifecycle. Read every chain before changing anything. Existing
// matching rules (and their counters/conntrack entries) are left in place.
func reconcileMappingRules(command, marker string, targets, desired []mappingRule, run func(string, ...string) ([]byte, error)) (bool, error) {
	wanted := make(map[string]bool, len(desired))
	priority := make(map[string]int, len(desired))
	for i, rule := range desired {
		wanted[mappingRuleKey(rule)] = true
		priority[mappingRuleKey(rule)] = i
	}
	present := map[string]bool{}
	liveRules := map[string][]mappingRule{}
	filterPosition := map[string]int{}
	var stale []mappingRule
	for _, target := range targets {
		output, err := run(command, "-w", "5", "-t", target.table, "-S", target.chain)
		if err != nil {
			return false, fmt.Errorf("list %s %s/%s: %w: %s", command, target.table, target.chain, err, output)
		}
		position := 0
		for _, line := range strings.Split(string(output), "\n") {
			fields := parseIPTablesRuleFields(line)
			if len(fields) < 3 || fields[0] != "-A" || fields[1] != target.chain {
				continue
			}
			position++
			liveRules[target.table+"/"+target.chain] = append(liveRules[target.table+"/"+target.chain], mappingRule{target.table, target.chain, fields[2:]})
			comment := ruleOption(fields[2:], "--comment")
			if target.table == "filter" && strings.HasPrefix(comment, "clicd-") && strings.Contains(comment, "-fw-") {
				filterPosition[target.chain] = position + 1
			}
			if !strings.HasPrefix(comment, marker) || strings.HasPrefix(comment, marker+"fw-") {
				continue
			}
			rule := mappingRule{target.table, target.chain, fields[2:]}
			key := mappingRuleKey(rule)
			if !wanted[key] || present[key] {
				stale = append(stale, rule)
			} else {
				present[key] = true
			}
		}
	}
	changed := false
	for _, rule := range desired {
		key := mappingRuleKey(rule)
		if present[key] {
			continue
		}
		position := 1
		if rule.table == "filter" && filterPosition[rule.chain] > 0 {
			position = filterPosition[rule.chain]
		}
		chainKey := rule.table + "/" + rule.chain
		// Retain the ordering produced by the original insert-at-head plan,
		// notably independent-public-IP passthrough ahead of explicit mappings.
		for i, existing := range liveRules[chainKey] {
			if rank, ok := priority[mappingRuleKey(existing)]; ok && rank > priority[key] && i+2 > position {
				position = i + 2
			}
		}
		args := append([]string{"-w", "5", "-t", rule.table, "-I", rule.chain, strconv.Itoa(position)}, rule.args...)
		if output, err := run(command, args...); err != nil {
			// Keep the old working mapping if installing its replacement fails.
			return changed, fmt.Errorf("install %s mapping: %w: %s", command, err, output)
		}
		present[key], changed = true, true
		liveRules[chainKey] = slices.Insert(liveRules[chainKey], position-1, rule)
	}
	var failures []error
	for _, rule := range stale {
		args := append([]string{"-w", "5", "-t", rule.table, "-D", rule.chain}, rule.args...)
		if output, err := run(command, args...); err != nil {
			failures = append(failures, fmt.Errorf("remove obsolete %s mapping: %w: %s", command, err, output))
			continue
		}
		changed = true
		if !wanted[mappingRuleKey(rule)] {
			clearMappingRuleConntrack(command, rule, run)
		}
	}
	return changed, errors.Join(failures...)
}

// Match both the original endpoint and the translated endpoint. A wildcard
// host binding must never delete unrelated outbound flows to the same port.
func mappingRuleConntrackArgs(command string, rule mappingRule) []string {
	if rule.table != "nat" || ruleOption(rule.args, "-j") != "DNAT" {
		return nil
	}
	family := "ipv4"
	if command == "ip6tables" {
		family = "ipv6"
	}
	target := ruleOption(rule.args, "--to-destination")
	guest, port, err := net.SplitHostPort(target)
	if err != nil {
		guest, port = target, ""
	}
	if _, err := netip.ParseAddr(guest); err != nil {
		return nil
	}
	args := []string{"-D", "-f", family, "--dst-nat", "--reply-src", guest}
	if protocol := ruleOption(rule.args, "-p"); protocol != "" {
		args = append(args, "-p", protocol)
	}
	if hostPort := ruleOption(rule.args, "--dport"); hostPort != "" {
		args = append(args, "--dport", hostPort)
	}
	if host := ruleOption(rule.args, "-d"); host != "" {
		args = append(args, "--dst", host)
	}
	if port != "" {
		args = append(args, "--reply-port-src", port)
	}
	return args
}

func clearMappingRuleConntrack(command string, rule mappingRule, run func(string, ...string) ([]byte, error)) {
	if args := mappingRuleConntrackArgs(command, rule); len(args) > 0 {
		// conntrack returns nonzero when no matching flow remains.
		_, _ = run("conntrack", args...)
	}
}

func ipv4MappingRules(c *config.Container, bridge, subnet, iface string) []mappingRule {
	tag := clicdTag(c.ID)
	var rules []mappingRule
	for _, pm := range c.PortMappings {
		for _, hostIP := range expandPortMappingHostIPs(c, pm) {
			args := []string{"-p", pm.Protocol}
			if hostIP != "" {
				args = append(args, "-d", hostIP)
			}
			args = append(args, "--dport", strconv.Itoa(pm.HostPort), "-j", "DNAT", "--to-destination", net.JoinHostPort(c.IP, strconv.Itoa(pm.ContainerPort)),
				"-m", "comment", "--comment", fmt.Sprintf("clicd-%s-%s-%d", tag, natRuleIPTag(hostIP), pm.HostPort))
			rules = append(rules, mappingRule{"nat", "PREROUTING", args})
		}
	}
	for _, assignment := range c.PublicIPv4s {
		if host := strings.TrimSpace(assignment.Address); host != "" {
			for _, protocol := range []string{"tcp", "udp"} {
				rules = append(rules, mappingRule{"nat", "PREROUTING", []string{"-d", host, "-p", protocol, "-j", "DNAT", "--to-destination", c.IP,
					"-m", "comment", "--comment", fmt.Sprintf("clicd-%s-%s-all-%s", tag, natRuleIPTag(host), protocol)}})
			}
		}
	}
	if !containerAllowsPublicIPv4Egress(c) {
		return append(rules, mappingRule{"filter", "FORWARD", []string{"-i", bridge, "-s", c.IP + "/32", "!", "-d", subnet,
			"-m", "comment", "--comment", "clicd-" + tag + "-v4-egress-block", "-j", "REJECT"}})
	}
	args := []string{"-s", c.IP + "/32"}
	if assignment, ok := primaryPublicIPv4Assignment(c); ok {
		if assignment.Interface != "" {
			iface = assignment.Interface
		} else if info, ok := publicIPv4InfoByAddress(assignment.Address); ok && info.Interface != "" {
			iface = info.Interface
		}
		if iface != "" {
			args = append(args, "-o", iface)
		}
		args = append(args, "-m", "comment", "--comment", "clicd-"+tag+"-snat-"+natRuleIPTag(assignment.Address), "-j", "SNAT", "--to-source", assignment.Address)
	} else {
		if iface == "" {
			iface = "eth+"
		}
		args = append(args, "-o", iface, "-m", "comment", "--comment", "clicd-"+tag+"-masq", "-j", "MASQUERADE")
	}
	return append(rules, mappingRule{"nat", "POSTROUTING", args})
}

func clearChangedIPv4Mappings(previous, current *config.Container) {
	old := ipv4MappingRules(previous, "", "", "")
	next := map[string]bool{}
	for _, rule := range ipv4MappingRules(current, "", "", "") {
		next[mappingRuleKey(rule)] = true
	}
	for _, rule := range old {
		if !next[mappingRuleKey(rule)] {
			clearMappingRuleConntrack("iptables", rule, runPortMappingCommand)
		}
	}
}
