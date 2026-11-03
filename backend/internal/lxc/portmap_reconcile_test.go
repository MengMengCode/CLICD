package lxc

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"clicd/internal/config"
)

type mappingKernel struct {
	rules                []mappingRule
	writes, invalidated  [][]string
	failList, failInsert bool
}

func (k *mappingKernel) run(command string, args ...string) ([]byte, error) {
	if command == "conntrack" {
		k.invalidated = append(k.invalidated, append([]string(nil), args...))
		return nil, nil
	}
	table := ruleOption(args, "-t")
	if table == "" {
		table = "filter"
	}
	for i, arg := range args {
		switch arg {
		case "-S":
			if k.failList {
				return nil, errors.New("permission denied")
			}
			var lines []string
			for _, rule := range k.rules {
				if rule.table == table && rule.chain == args[i+1] {
					lines = append(lines, "-A "+rule.chain+" "+strings.Join(rule.args, " "))
				}
			}
			return []byte(strings.Join(lines, "\n")), nil
		case "-I":
			if k.failInsert {
				return nil, errors.New("xtables busy")
			}
			k.writes = append(k.writes, append([]string(nil), args...))
			rule := mappingRule{table, args[i+1], append([]string(nil), args[i+3:]...)}
			k.rules = append([]mappingRule{rule}, k.rules...)
			return nil, nil
		case "-D":
			k.writes = append(k.writes, append([]string(nil), args...))
			key := mappingRuleKey(mappingRule{table, args[i+1], args[i+2:]})
			for j, rule := range k.rules {
				if mappingRuleKey(rule) == key {
					k.rules = append(k.rules[:j], k.rules[j+1:]...)
					return nil, nil
				}
			}
			return nil, errors.New("missing rule")
		}
	}
	return nil, errors.New("unexpected command: " + strings.Join(args, " "))
}

func testMappingContainer() *config.Container {
	return &config.Container{ID: 12, Status: "running", IP: "192.168.122.20", PortMappingLimit: 3,
		PortMappings: []config.PortMapping{{HostPort: 32022, ContainerPort: 22, Protocol: "tcp"}, {HostPort: 32080, ContainerPort: 80, Protocol: "tcp"}}}
}

func testIPv4Rules(c *config.Container) []mappingRule {
	return ipv4MappingRules(c, "virbr0", "192.168.122.0/24", "eth0")
}
func testReconcile(k *mappingKernel, rules []mappingRule) (bool, error) {
	return reconcileMappingRules("iptables", "clicd-c12-", []mappingRule{{table: "nat", chain: "PREROUTING"}, {table: "nat", chain: "POSTROUTING"}, {table: "filter", chain: "FORWARD"}}, rules, k.run)
}

func TestMappingReconcilePreservesEstablishedFlowsAcrossPollsAndRestarts(t *testing.T) {
	c := testMappingContainer()
	desired := testIPv4Rules(c)
	k := &mappingKernel{rules: append([]mappingRule(nil), desired...)}
	// Simulate iptables' canonical output (implicit match, /32, different order).
	k.rules[0].args = []string{"-m", "comment", "--comment", "clicd-c12-any-32022", "-p", "tcp", "-m", "tcp", "--dport", "32022", "-j", "DNAT", "--to-destination", "192.168.122.20:22"}
	for poll := 0; poll < 20; poll++ {
		changed, err := testReconcile(k, testIPv4Rules(c))
		if err != nil || changed || len(k.writes) != 0 || len(k.invalidated) != 0 {
			t.Fatalf("poll %d changed healthy state: %v %v %+v", poll, changed, err, k)
		}
	}
}

func TestMappingReconcileRepairsMissingRuleWithoutClearingFlows(t *testing.T) {
	desired := testIPv4Rules(testMappingContainer())
	k := &mappingKernel{rules: append([]mappingRule(nil), desired[1:]...)}
	changed, err := testReconcile(k, desired)
	if err != nil || !changed || len(k.writes) != 1 || len(k.invalidated) != 0 {
		t.Fatalf("repair: %v %v %+v", changed, err, k)
	}
	k.writes = nil
	if changed, err = testReconcile(k, desired); err != nil || changed || len(k.writes) != 0 {
		t.Fatalf("repair not idempotent: %v %v", changed, err)
	}
}

func TestMappingChangeInvalidatesOnlyOldTranslatedEndpoint(t *testing.T) {
	c := testMappingContainer()
	k := &mappingKernel{rules: testIPv4Rules(c)}
	c.PortMappings[1].ContainerPort = 8080
	changed, err := testReconcile(k, testIPv4Rules(c))
	if err != nil || !changed || len(k.writes) != 2 || len(k.invalidated) != 1 {
		t.Fatalf("update: %v %v %+v", changed, err, k)
	}
	args := k.invalidated[0]
	if ruleOption(args, "--dport") != "32080" || ruleOption(args, "--reply-src") != c.IP || ruleOption(args, "--reply-port-src") != "80" || !strings.Contains(strings.Join(args, " "), "--dst-nat") {
		t.Fatalf("overbroad invalidation: %v", args)
	}
	if k.writes[0][4] != "-I" || k.writes[1][4] != "-D" {
		t.Fatal("removed old mapping before installing replacement")
	}
}

func TestMappingAddDeleteAndDescriptionChangesPreserveOtherPorts(t *testing.T) {
	c := testMappingContainer()
	k := &mappingKernel{rules: testIPv4Rules(c)}
	c.PortMappings = append(c.PortMappings, config.PortMapping{HostPort: 32053, ContainerPort: 53, Protocol: "udp"})
	if _, err := testReconcile(k, testIPv4Rules(c)); err != nil || len(k.invalidated) != 0 {
		t.Fatalf("add interrupted existing flows: %v", err)
	}
	c.PortMappings[0].Description = "renamed"
	k.writes = nil
	if changed, err := testReconcile(k, testIPv4Rules(c)); err != nil || changed {
		t.Fatalf("description changed kernel state: %v", err)
	}
	c.PortMappings = c.PortMappings[:2]
	if _, err := testReconcile(k, testIPv4Rules(c)); err != nil || len(k.invalidated) != 1 || ruleOption(k.invalidated[0], "-p") != "udp" {
		t.Fatalf("delete invalidation: %v %v", err, k.invalidated)
	}
}

func TestMappingReconcilePreservesForeignAndFirewallRules(t *testing.T) {
	desired := testIPv4Rules(testMappingContainer())
	k := &mappingKernel{rules: append([]mappingRule(nil), desired...)}
	foreign := []mappingRule{
		{"filter", "FORWARD", []string{"-j", "DROP", "-m", "comment", "--comment", "clicd-c12-fw-default-in"}},
		{"nat", "PREROUTING", []string{"-j", "ACCEPT", "-m", "comment", "--comment", "clicd-c123-any-32022"}},
		{"nat", "PREROUTING", []string{"-j", "ACCEPT", "-m", "comment", "--comment", "other-clicd-c12-note"}},
	}
	k.rules = append(k.rules, foreign...)
	if changed, err := testReconcile(k, desired); err != nil || changed || len(k.writes) != 0 {
		t.Fatalf("touched foreign rules: %v %v", changed, err)
	}
}

func TestMappingDuplicateCleanupPreservesConntrack(t *testing.T) {
	desired := testIPv4Rules(testMappingContainer())
	k := &mappingKernel{rules: append(append([]mappingRule(nil), desired...), desired[0])}
	if _, err := testReconcile(k, desired); err != nil || len(k.writes) != 1 || len(k.invalidated) != 0 {
		t.Fatalf("duplicate cleanup: %v %+v", err, k)
	}
}

func TestMappingFailuresKeepWorkingRuleAndRetry(t *testing.T) {
	for _, kind := range []string{"list", "insert"} {
		t.Run(kind, func(t *testing.T) {
			c := testMappingContainer()
			old := testIPv4Rules(c)
			k := &mappingKernel{rules: append([]mappingRule(nil), old...), failList: kind == "list", failInsert: kind == "insert"}
			c.IP = "192.168.122.21"
			if _, err := testReconcile(k, testIPv4Rules(c)); err == nil || len(k.writes) != 0 || len(k.invalidated) != 0 || !reflect.DeepEqual(k.rules, old) {
				t.Fatalf("failure damaged old rules: %v %+v", err, k)
			}
			k.failList, k.failInsert = false, false
			if _, err := testReconcile(k, testIPv4Rules(c)); err != nil || len(k.invalidated) != 2 {
				t.Fatalf("retry: %v %+v", err, k)
			}
			for _, args := range k.invalidated {
				if ruleOption(args, "--reply-src") != "192.168.122.20" {
					t.Fatalf("wrong old IP: %v", args)
				}
			}
		})
	}
}

func TestMappingRuleKeyCanonicalizesOnlyEquivalentRules(t *testing.T) {
	a := mappingRule{"filter", "FORWARD", []string{"-s", "10.0.3.2", "!", "-d", "10.0.3.0/24", "-j", "REJECT"}}
	b := mappingRule{"filter", "FORWARD", []string{"-j", "REJECT", "--reject-with", "icmp-port-unreachable", "!", "-d", "10.0.3.0/24", "-s", "10.0.3.2/32"}}
	if mappingRuleKey(a) != mappingRuleKey(b) {
		t.Fatal("equivalent canonical representations differ")
	}
	b.args = []string{"-s", "10.0.3.2/32", "-d", "10.0.3.0/24", "-j", "REJECT"}
	if mappingRuleKey(a) == mappingRuleKey(b) {
		t.Fatal("lost negation")
	}
}

func TestIPv6MappingReconcilePreservesUnchangedConnections(t *testing.T) {
	setupIPv6PortMappingTest(t, false)
	c := testMappingContainer()
	c.IPv6NATAddress = "fd00::2"
	desired, err := ipv6MappingRules(c)
	if err != nil {
		t.Fatal(err)
	}
	k := &mappingKernel{rules: append([]mappingRule(nil), desired...)}
	runIPv6PortMappingCommand = k.run
	if changed, err := reconcileIPv6PortMappingsLocked(c); err != nil || changed || len(k.writes) != 0 || len(k.invalidated) != 0 {
		t.Fatalf("IPv6 healthy: %v %v", changed, err)
	}
	c.PortMappings[1].ContainerPort = 8080
	if _, err := reconcileIPv6PortMappingsLocked(c); err != nil || len(k.invalidated) != 2 {
		t.Fatalf("IPv6 change: %v %v", err, k.invalidated)
	}
	for _, args := range k.invalidated {
		if ruleOption(args, "--dport") != "32080" || ruleOption(args, "--reply-src") != "fd00::2" || ruleOption(args, "-f") != "ipv6" {
			t.Fatalf("IPv6 scope: %v", args)
		}
	}
}

func TestFirewallHealthCheckDetectsLossOrderAndBroadAccepts(t *testing.T) {
	c := testMappingContainer()
	c.Virtualization = "kvm"
	c.FirewallEnabled, c.FirewallDefaultAction = true, "DROP"
	c.FirewallRules = []config.FirewallRule{{ID: "ssh", Enabled: true, Direction: "in", Protocol: "tcp", Port: "22", Action: "ACCEPT"}}
	plan, err := plannedFirewallRules(c)
	if err != nil {
		t.Fatal(err)
	}
	var healthy []mappingRule
	for i := len(plan) - 1; i >= 0; i-- {
		if plan[i].command == "iptables" {
			healthy = append(healthy, mappingRule{"filter", "FORWARD", plan[i].args[3:]})
		}
	}
	broad := mappingRule{"filter", "FORWARD", []string{"-o", "virbr0", "-j", "ACCEPT"}}
	old := runPortMappingCommand
	t.Cleanup(func() { runPortMappingCommand = old })
	for _, tc := range []struct {
		name  string
		rules []mappingRule
		want  bool
	}{
		{"healthy", append(append([]mappingRule(nil), healthy...), broad), true},
		{"missing_allow", healthy[1:], false},
		{"missing_all", nil, false},
		{"broad_accept_before_policy", append([]mappingRule{broad}, healthy...), false},
		{"reordered_policy", []mappingRule{healthy[1], healthy[0], healthy[2]}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k := &mappingKernel{rules: tc.rules}
			runPortMappingCommand = func(command string, args ...string) ([]byte, error) {
				if command == "ip6tables" {
					return nil, nil
				}
				return k.run(command, args...)
			}
			got, err := firewallRulesPresent(c, plan)
			if err != nil || got != tc.want || len(k.writes) != 0 {
				t.Fatalf("health=%v want=%v err=%v writes=%v", got, tc.want, err, k.writes)
			}
		})
	}
}

func TestMappingRepairNeverPlacesAcceptAheadOfExistingFirewall(t *testing.T) {
	k := &mappingKernel{rules: []mappingRule{{"filter", "FORWARD", []string{"-d", "fd00::2/128", "-j", "DROP", "-m", "comment", "--comment", "clicd-c12-fw-default-in"}}}}
	desired := []mappingRule{{"filter", "FORWARD", []string{"-d", "fd00::2/128", "-j", "ACCEPT", "-m", "comment", "--comment", "clicd-c12-nat6-port-tcp-30080"}}}
	_, err := reconcileMappingRules("ip6tables", "clicd-c12-nat6-port-", []mappingRule{{table: "filter", chain: "FORWARD"}}, desired, k.run)
	if err != nil || len(k.writes) != 1 || k.writes[0][6] != "2" {
		t.Fatalf("repair bypasses firewall: %v %v", err, k.writes)
	}
}

func TestBridgeRepairLeavesExistingRulesAndRespectsPolicyOrder(t *testing.T) {
	old := runPortMappingCommand
	t.Cleanup(func() { runPortMappingCommand = old })
	writes := 0
	runPortMappingCommand = func(_ string, args ...string) ([]byte, error) {
		if ruleOption(args, "-C") != "" {
			return nil, nil
		}
		writes++
		return nil, nil
	}
	ensureBridgeAcceptRule("FORWARD", []string{"-o", "virbr0", "-j", "ACCEPT"})
	if writes != 0 {
		t.Fatal("healthy bridge rules were rewritten")
	}
	runPortMappingCommand = func(_ string, args ...string) ([]byte, error) {
		if ruleOption(args, "-C") != "" {
			return nil, errors.New("missing")
		}
		if ruleOption(args, "-S") != "" {
			return []byte("-A FORWARD -d 192.168.122.20/32 -m comment --comment clicd-c12-fw-default-in -j DROP"), nil
		}
		writes++
		if args[4] != "2" {
			t.Fatalf("bridge accept before firewall: %v", args)
		}
		return nil, nil
	}
	ensureBridgeAcceptRule("FORWARD", []string{"-o", "virbr0", "-j", "ACCEPT"})
	if writes != 1 {
		t.Fatalf("repair writes=%d", writes)
	}
}

func TestAddingMappingPreservesIndependentPublicIPPassthroughPrecedence(t *testing.T) {
	specific := mappingRule{"nat", "PREROUTING", []string{"-p", "tcp", "-d", "203.0.113.10", "--dport", "32080", "-j", "DNAT", "--to-destination", "10.0.3.2:80", "-m", "comment", "--comment", "clicd-c12-203_0_113_10-32080"}}
	passthrough := mappingRule{"nat", "PREROUTING", []string{"-p", "tcp", "-d", "203.0.113.10", "-j", "DNAT", "--to-destination", "10.0.3.2", "-m", "comment", "--comment", "clicd-c12-203_0_113_10-all-tcp"}}
	k := &mappingKernel{rules: []mappingRule{passthrough}}
	if _, err := testReconcile(k, []mappingRule{specific, passthrough}); err != nil || len(k.writes) != 1 || k.writes[0][6] != "2" || len(k.invalidated) != 0 {
		t.Fatalf("new mapping overrode full passthrough: %v %v", err, k.writes)
	}
}
