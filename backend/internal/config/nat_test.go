package config

import "testing"

func TestAllocateSSHPortUsesConfiguredNATRange(t *testing.T) {
	AppConfig = &ClicdConfig{
		NATPortStart: 30000,
		NATPortEnd:   30002,
		NextSSHPort:  22000,
		Containers: []Container{{
			PortMappings: []PortMapping{
				{HostPort: 30000},
				{HostPort: 30001},
			},
		}},
	}

	port, err := AllocateSSHPort()
	if err != nil {
		t.Fatal(err)
	}
	if port != 30002 {
		t.Fatalf("expected port 30002, got %d", port)
	}
	if AppConfig.NextSSHPort != 30000 {
		t.Fatalf("expected next port to wrap to 30000, got %d", AppConfig.NextSSHPort)
	}
}

func TestAllocateSSHPortErrorsWhenConfiguredRangeIsFull(t *testing.T) {
	AppConfig = &ClicdConfig{
		NATPortStart: 31000,
		NATPortEnd:   31001,
		NextSSHPort:  31000,
		Containers: []Container{{
			PortMappings: []PortMapping{
				{HostPort: 31000},
				{HostPort: 31001},
			},
		}},
	}

	if port, err := AllocateSSHPort(); err == nil {
		t.Fatalf("expected exhausted NAT range error, got port %d", port)
	}
}

func TestAllocateSSHPortExcludingRequestedMappings(t *testing.T) {
	previous := AppConfig
	t.Cleanup(func() { AppConfig = previous })
	AppConfig = &ClicdConfig{
		NATPortStart: 32000,
		NATPortEnd:   32002,
		NextSSHPort:  32000,
	}

	port, err := AllocateSSHPortExcluding([]int{32000, 32001})
	if err != nil {
		t.Fatal(err)
	}
	if port != 32002 {
		t.Fatalf("allocated port = %d, want 32002", port)
	}
}
