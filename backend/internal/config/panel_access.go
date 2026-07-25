package config

import (
	"fmt"
	"net"
	"net/netip"
	"strings"
)

// PanelAccessPolicy limits access to the complete web panel and API surface.
type PanelAccessPolicy struct {
	Enabled        bool     `json:"enabled"`
	AllowedSources []string `json:"allowed_sources"`
	TrustedProxies []string `json:"trusted_proxies"`
}

// ForwardedClientHeaders contains proxy-provided client address headers.
type ForwardedClientHeaders struct {
	ForwardedFor   string
	RealIP         string
	CFConnectingIP string
}

// PanelAccessDecision describes the address used by the access policy.
type PanelAccessDecision struct {
	Allowed       bool
	DirectSource  string
	CurrentSource string
	UsedForwarded bool
}

func NormalizePanelAccessPolicy(policy PanelAccessPolicy) (PanelAccessPolicy, error) {
	allowed, err := normalizeIPRanges(policy.AllowedSources, "allowed source")
	if err != nil {
		return PanelAccessPolicy{}, err
	}
	trusted, err := normalizeIPRanges(policy.TrustedProxies, "trusted proxy")
	if err != nil {
		return PanelAccessPolicy{}, err
	}
	if policy.Enabled && len(allowed) == 0 {
		return PanelAccessPolicy{}, fmt.Errorf("at least one allowed IP address or CIDR is required")
	}
	return PanelAccessPolicy{
		Enabled:        policy.Enabled,
		AllowedSources: allowed,
		TrustedProxies: trusted,
	}, nil
}

func normalizeIPRanges(values []string, label string) ([]string, error) {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		normalized, err := normalizeIPRange(value)
		if err != nil {
			return nil, fmt.Errorf("invalid %s %q: %w", label, value, err)
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	return result, nil
}

func normalizeIPRange(value string) (string, error) {
	if strings.Contains(value, "/") {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return "", err
		}
		if prefix.Addr().Zone() != "" {
			return "", fmt.Errorf("IPv6 zones are not supported")
		}
		return prefix.Masked().String(), nil
	}
	addr, err := netip.ParseAddr(value)
	if err != nil {
		return "", err
	}
	if addr.Zone() != "" {
		return "", fmt.Errorf("IPv6 zones are not supported")
	}
	return addr.Unmap().String(), nil
}

func panelAccessPoliciesEqual(a, b PanelAccessPolicy) bool {
	return a.Enabled == b.Enabled &&
		stringSlicesEqual(a.AllowedSources, b.AllowedSources) &&
		stringSlicesEqual(a.TrustedProxies, b.TrustedProxies)
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// EvaluatePanelAccess resolves the effective client address and applies policy.
// Forwarded headers are only considered when the TCP peer is trusted.
func EvaluatePanelAccess(policy PanelAccessPolicy, remoteAddr string, headers ForwardedClientHeaders) PanelAccessDecision {
	direct, ok := parseRemoteIP(remoteAddr)
	decision := PanelAccessDecision{}
	if ok {
		decision.DirectSource = direct.String()
		decision.CurrentSource = direct.String()
	}
	if !policy.Enabled {
		decision.Allowed = true
		return decision
	}
	if !ok {
		return decision
	}

	current := direct
	if ipInRanges(direct, policy.TrustedProxies) {
		if forwarded, forwardedOK := resolveForwardedIP(direct, policy.TrustedProxies, headers); forwardedOK {
			current = forwarded
			decision.CurrentSource = forwarded.String()
			decision.UsedForwarded = true
		}
	}

	// A direct local connection remains an emergency recovery path. When a
	// trusted local reverse proxy forwards a client address, that client is
	// still checked normally.
	if current.IsLoopback() && !decision.UsedForwarded {
		decision.Allowed = true
		return decision
	}
	decision.Allowed = ipInRanges(current, policy.AllowedSources)
	return decision
}

func parseRemoteIP(value string) (netip.Addr, bool) {
	value = strings.TrimSpace(value)
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	value = strings.TrimPrefix(strings.TrimSuffix(value, "]"), "[")
	addr, err := netip.ParseAddr(value)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

func resolveForwardedIP(direct netip.Addr, trusted []string, headers ForwardedClientHeaders) (netip.Addr, bool) {
	for _, raw := range []string{headers.CFConnectingIP, headers.RealIP} {
		if addr, ok := parseRemoteIP(strings.TrimSpace(strings.Split(raw, ",")[0])); ok {
			return addr, true
		}
	}

	parts := strings.Split(headers.ForwardedFor, ",")
	current := direct
	found := false
	for i := len(parts) - 1; i >= 0 && ipInRanges(current, trusted); i-- {
		addr, ok := parseRemoteIP(strings.TrimSpace(parts[i]))
		if !ok {
			continue
		}
		current = addr
		found = true
	}
	return current, found
}

func ipInRanges(addr netip.Addr, ranges []string) bool {
	addr = addr.Unmap()
	for _, raw := range ranges {
		if strings.Contains(raw, "/") {
			prefix, err := netip.ParsePrefix(raw)
			if err == nil && prefix.Contains(addr) {
				return true
			}
			continue
		}
		candidate, err := netip.ParseAddr(raw)
		if err == nil && candidate.Unmap() == addr {
			return true
		}
	}
	return false
}
