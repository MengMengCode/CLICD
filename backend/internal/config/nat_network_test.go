package config

import "testing"

func TestParseNATNetwork(t *testing.T) {
	network, err := ParseNATNetwork("172.28.40.0/24")
	if err != nil {
		t.Fatalf("ParseNATNetwork returned error: %v", err)
	}
	if network.Subnet != "172.28.40.0/24" ||
		network.Gateway != "172.28.40.1" ||
		network.Netmask != "255.255.255.0" ||
		network.DHCPStart != "172.28.40.2" ||
		network.DHCPEnd != "172.28.40.254" ||
		network.DHCPMax != 253 {
		t.Fatalf("unexpected network values: %+v", network)
	}
}

func TestParseNATNetworkMasksHostBits(t *testing.T) {
	network, err := ParseNATNetwork("10.44.8.99/20")
	if err != nil {
		t.Fatalf("ParseNATNetwork returned error: %v", err)
	}
	if network.Subnet != "10.44.0.0/20" || network.Gateway != "10.44.0.1" || network.DHCPEnd != "10.44.15.254" {
		t.Fatalf("unexpected masked network values: %+v", network)
	}
}

func TestParseNATNetworkRejectsUnsafeRanges(t *testing.T) {
	for _, raw := range []string{
		"203.0.113.0/24",
		"10.0.0.0/15",
		"10.0.0.0/29",
		"not-a-subnet",
	} {
		if _, err := ParseNATNetwork(raw); err == nil {
			t.Fatalf("ParseNATNetwork(%q) unexpectedly succeeded", raw)
		}
	}
}

func TestNormalizeNATNetworkDefaultsUsesEnvironment(t *testing.T) {
	t.Setenv("CLICD_LXC_SUBNET", "172.30.8.0/24")
	t.Setenv("CLICD_KVM_SUBNET", "10.230.0.0/20")
	previous := AppConfig
	AppConfig = &ClicdConfig{}
	t.Cleanup(func() { AppConfig = previous })

	if !normalizeNATNetworkDefaults() {
		t.Fatal("expected defaults to change")
	}
	if AppConfig.LXCNATSubnet != "172.30.8.0/24" || AppConfig.KVMNATSubnet != "10.230.0.0/20" {
		t.Fatalf("unexpected configured subnets: LXC=%s KVM=%s", AppConfig.LXCNATSubnet, AppConfig.KVMNATSubnet)
	}
}
