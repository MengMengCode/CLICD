package config

import (
	"reflect"
	"testing"
)

func TestNormalizePanelAccessPolicy(t *testing.T) {
	policy, err := NormalizePanelAccessPolicy(PanelAccessPolicy{
		Enabled:        true,
		AllowedSources: []string{" 192.0.2.8 ", "10.20.30.44/24", "192.0.2.8", "2001:db8::1"},
		TrustedProxies: []string{"127.0.0.1", "2001:db8:1::/64"},
	})
	if err != nil {
		t.Fatalf("NormalizePanelAccessPolicy() error = %v", err)
	}
	if want := []string{"192.0.2.8", "10.20.30.0/24", "2001:db8::1"}; !reflect.DeepEqual(policy.AllowedSources, want) {
		t.Fatalf("AllowedSources = %#v, want %#v", policy.AllowedSources, want)
	}
	if want := []string{"127.0.0.1", "2001:db8:1::/64"}; !reflect.DeepEqual(policy.TrustedProxies, want) {
		t.Fatalf("TrustedProxies = %#v, want %#v", policy.TrustedProxies, want)
	}
}

func TestNormalizePanelAccessPolicyRejectsEmptyEnabledPolicy(t *testing.T) {
	if _, err := NormalizePanelAccessPolicy(PanelAccessPolicy{Enabled: true}); err == nil {
		t.Fatal("expected enabled empty policy to fail")
	}
}

func TestEvaluatePanelAccess(t *testing.T) {
	base := PanelAccessPolicy{
		Enabled:        true,
		AllowedSources: []string{"192.0.2.0/24", "2001:db8::/32"},
		TrustedProxies: []string{"10.0.0.1", "127.0.0.1"},
	}
	tests := []struct {
		name          string
		policy        PanelAccessPolicy
		remote        string
		headers       ForwardedClientHeaders
		allowed       bool
		current       string
		usedForwarded bool
	}{
		{
			name:    "disabled",
			policy:  PanelAccessPolicy{},
			remote:  "198.51.100.9:44321",
			allowed: true,
			current: "198.51.100.9",
		},
		{
			name:    "direct CIDR match",
			policy:  base,
			remote:  "192.0.2.25:44321",
			allowed: true,
			current: "192.0.2.25",
		},
		{
			name:    "direct denied",
			policy:  base,
			remote:  "198.51.100.9:44321",
			allowed: false,
			current: "198.51.100.9",
		},
		{
			name:   "spoofed forwarding header ignored",
			policy: base,
			remote: "198.51.100.9:44321",
			headers: ForwardedClientHeaders{
				ForwardedFor: "192.0.2.10",
			},
			allowed: false,
			current: "198.51.100.9",
		},
		{
			name:   "trusted proxy forwards allowed source",
			policy: base,
			remote: "10.0.0.1:44321",
			headers: ForwardedClientHeaders{
				ForwardedFor: "192.0.2.10",
			},
			allowed:       true,
			current:       "192.0.2.10",
			usedForwarded: true,
		},
		{
			name:   "trusted proxy forwards denied source",
			policy: base,
			remote: "10.0.0.1:44321",
			headers: ForwardedClientHeaders{
				RealIP: "198.51.100.20",
			},
			allowed:       false,
			current:       "198.51.100.20",
			usedForwarded: true,
		},
		{
			name:          "direct loopback recovery",
			policy:        base,
			remote:        "127.0.0.1:44321",
			allowed:       true,
			current:       "127.0.0.1",
			usedForwarded: false,
		},
		{
			name:   "trusted loopback proxy is enforced",
			policy: base,
			remote: "127.0.0.1:44321",
			headers: ForwardedClientHeaders{
				ForwardedFor: "198.51.100.20",
			},
			allowed:       false,
			current:       "198.51.100.20",
			usedForwarded: true,
		},
		{
			name:    "IPv6 source",
			policy:  base,
			remote:  "[2001:db8::88]:44321",
			allowed: true,
			current: "2001:db8::88",
		},
		{
			name:   "trusted proxy chain",
			policy: base,
			remote: "10.0.0.1:44321",
			headers: ForwardedClientHeaders{
				ForwardedFor: "192.0.2.70, 10.0.0.1",
			},
			allowed:       true,
			current:       "192.0.2.70",
			usedForwarded: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EvaluatePanelAccess(tt.policy, tt.remote, tt.headers)
			if got.Allowed != tt.allowed || got.CurrentSource != tt.current || got.UsedForwarded != tt.usedForwarded {
				t.Fatalf("EvaluatePanelAccess() = %#v", got)
			}
		})
	}
}
