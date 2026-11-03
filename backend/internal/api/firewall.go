package api

import (
	"encoding/json"
	"math/rand"
	"net/http"
	"net/netip"
	"strconv"
	"strings"

	"clicd/internal/config"
	"clicd/internal/lxc"
)

func generateFirewallRuleID() string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 8)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}

func getFirewall(w http.ResponseWriter, r *http.Request, id int) {
	c := config.FindContainer(id)
	if c == nil {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Container not found"})
		return
	}
	jsonResponse(w, http.StatusOK, APIResponse{
		Success: true,
		Data: map[string]interface{}{
			"enabled":        c.FirewallEnabled,
			"default_action": normalizeFirewallDefaultAction(c.FirewallDefaultAction),
			"rules":          c.FirewallRules,
		},
	})
}

func updateFirewall(w http.ResponseWriter, r *http.Request, id int) {
	c := config.FindContainer(id)
	if c == nil {
		jsonResponse(w, http.StatusNotFound, APIResponse{Success: false, Message: "Container not found"})
		return
	}

	var req struct {
		Enabled       *bool                  `json:"enabled"`
		DefaultAction *string                `json:"default_action"`
		Rules         *[]config.FirewallRule `json:"rules"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
		return
	}

	oldEnabled := c.FirewallEnabled
	oldDefaultAction := c.FirewallDefaultAction
	oldRules := append([]config.FirewallRule(nil), c.FirewallRules...)
	nextEnabled := oldEnabled
	nextDefaultAction := oldDefaultAction
	nextRules := c.FirewallRules

	if req.Enabled != nil {
		nextEnabled = *req.Enabled
	}
	if req.DefaultAction != nil {
		action := normalizeFirewallDefaultAction(*req.DefaultAction)
		if action == "" {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid default action"})
			return
		}
		nextDefaultAction = action
	} else if strings.TrimSpace(nextDefaultAction) == "" {
		nextDefaultAction = "DROP"
	}
	if req.Rules != nil {
		// Validate and assign IDs to new rules
		rules := append([]config.FirewallRule{}, (*req.Rules)...)
		for i := range rules {
			rules[i].Direction = strings.ToLower(strings.TrimSpace(rules[i].Direction))
			rules[i].Protocol = strings.ToLower(strings.TrimSpace(rules[i].Protocol))
			rules[i].Action = strings.ToUpper(strings.TrimSpace(rules[i].Action))
			rules[i].Network = normalizeFirewallNetwork(rules[i].Network)
			rules[i].SourceIP = strings.TrimSpace(rules[i].SourceIP)
			rules[i].Port = strings.TrimSpace(rules[i].Port)

			if rules[i].Network == "" {
				jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid network"})
				return
			}
			if rules[i].Direction != "in" && rules[i].Direction != "out" {
				jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid direction: " + rules[i].Direction})
				return
			}
			if rules[i].Protocol != "tcp" && rules[i].Protocol != "udp" && rules[i].Protocol != "icmp" && rules[i].Protocol != "all" {
				jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid protocol: " + rules[i].Protocol})
				return
			}
			if rules[i].Action != "ACCEPT" && rules[i].Action != "DROP" {
				jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid action: " + rules[i].Action})
				return
			}
			if rules[i].SourceIP != "" {
				if err := validateFirewallIPSpec(rules[i].SourceIP, rules[i].Network); err != nil {
					jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid IP: " + err.Error()})
					return
				}
			}
			if rules[i].ID == "" || strings.HasPrefix(rules[i].ID, "tmp-") {
				rules[i].ID = generateFirewallRuleID()
			}
			// Validate port spec
			if rules[i].Port != "" {
				if rules[i].Protocol != "tcp" && rules[i].Protocol != "udp" {
					jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Ports are only supported for TCP and UDP rules"})
					return
				}
				if err := validatePortSpec(rules[i].Port); err != nil {
					jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid port: " + err.Error()})
					return
				}
			}
		}
		nextRules = rules
	}
	// Commit only after the entire request has passed validation.
	c.FirewallEnabled = nextEnabled
	c.FirewallDefaultAction = nextDefaultAction
	c.FirewallRules = nextRules

	// Apply firewall rules to iptables if container is running
	if c.Status == "running" {
		if err := lxc.ApplyFirewallRules(id); err != nil {
			c.FirewallEnabled = oldEnabled
			c.FirewallDefaultAction = oldDefaultAction
			c.FirewallRules = oldRules
			_ = lxc.ApplyFirewallRules(id)
			config.SaveConfig()
			jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Failed to apply firewall rules: " + err.Error()})
			return
		}
	} else if !c.FirewallEnabled {
		// If disabled and not running, clean any lingering rules
		lxc.CleanFirewallRules(id)
	}
	config.SaveConfig()

	jsonResponse(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "Firewall updated",
		Data: map[string]interface{}{
			"enabled":        c.FirewallEnabled,
			"default_action": normalizeFirewallDefaultAction(c.FirewallDefaultAction),
			"rules":          c.FirewallRules,
		},
	})
}

func normalizeFirewallDefaultAction(action string) string {
	action = strings.ToUpper(strings.TrimSpace(action))
	if action == "ACCEPT" || action == "DROP" {
		return action
	}
	return ""
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
		return ""
	}
}

func validatePortSpec(port string) error {
	port = strings.TrimSpace(port)
	if port == "" {
		return nil
	}
	// Support: "22", "80,443", "8000-9000", "80,443,8000-9000"
	partCount := 0
	for _, part := range strings.Split(port, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return &portValidationError{port}
		}
		partCount++
		if strings.Contains(part, "-") {
			// Range
			bounds := strings.SplitN(part, "-", 2)
			lo, err := strconv.Atoi(strings.TrimSpace(bounds[0]))
			if err != nil || lo < 1 || lo > 65535 {
				return &portValidationError{part}
			}
			hi, err := strconv.Atoi(strings.TrimSpace(bounds[1]))
			if err != nil || hi < 1 || hi > 65535 {
				return &portValidationError{part}
			}
			if hi < lo {
				return &portValidationError{part}
			}
		} else {
			p, err := strconv.Atoi(part)
			if err != nil || p < 1 || p > 65535 {
				return &portValidationError{part}
			}
		}
	}
	if partCount > 15 {
		return &portValidationError{"too many ports; maximum 15 items per rule"}
	}
	return nil
}

func validateFirewallIPSpec(value string, network string) error {
	var addr netip.Addr
	if strings.Contains(value, "/") {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return err
		}
		addr = prefix.Addr()
	} else {
		parsed, err := netip.ParseAddr(value)
		if err != nil {
			return err
		}
		addr = parsed
	}
	switch network {
	case "ipv4":
		if !addr.Is4() {
			return &ipValidationError{"IPv4 rule requires an IPv4 address or CIDR: " + value}
		}
	case "ipv6":
		if !addr.Is6() || addr.Is4In6() {
			return &ipValidationError{"IPv6 rule requires an IPv6 address or CIDR: " + value}
		}
	}
	return nil
}

type ipValidationError struct {
	value string
}

func (e *ipValidationError) Error() string {
	return e.value
}

type portValidationError struct {
	port string
}

func (e *portValidationError) Error() string {
	return "invalid port value: " + e.port
}
