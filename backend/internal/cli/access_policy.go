package cli

import (
	"flag"
	"fmt"
	"strings"

	"clicd/internal/config"
)

// RunAccessPolicyCommand manages the panel source policy without requiring the
// interactive menu. It is intended to remain usable over SSH as a recovery path.
func RunAccessPolicyCommand(args []string) error {
	action := "show"
	if len(args) > 0 {
		action = strings.ToLower(strings.TrimSpace(args[0]))
		args = args[1:]
	}

	switch action {
	case "show":
		printPanelAccessPolicy(config.AppConfig.PanelAccessPolicy)
		return nil
	case "disable", "off":
		next := config.AppConfig.PanelAccessPolicy
		next.Enabled = false
		if err := savePanelAccessPolicy(next); err != nil {
			return err
		}
		fmt.Println("Panel access allowlist disabled.")
		return reloadPanelAfterAccessPolicyCommand()
	case "set", "enable":
		flags := flag.NewFlagSet("clicd access-policy set", flag.ContinueOnError)
		flags.SetOutput(new(strings.Builder))
		var allowed string
		var trusted string
		flags.StringVar(&allowed, "allow", "", "comma-separated allowed IP/CIDR values")
		flags.StringVar(&trusted, "trusted-proxy", "", "comma-separated trusted proxy IP/CIDR values")
		if err := flags.Parse(args); err != nil {
			return fmt.Errorf("invalid access-policy arguments: %w", err)
		}
		next := config.PanelAccessPolicy{
			Enabled:        true,
			AllowedSources: splitPanelAccessEntries(allowed),
			TrustedProxies: splitPanelAccessEntries(trusted),
		}
		if err := savePanelAccessPolicy(next); err != nil {
			return err
		}
		fmt.Println("Panel access allowlist saved.")
		printPanelAccessPolicy(config.AppConfig.PanelAccessPolicy)
		return reloadPanelAfterAccessPolicyCommand()
	default:
		return fmt.Errorf("unknown access-policy action %q; use show, set, or disable", action)
	}
}

func savePanelAccessPolicy(policy config.PanelAccessPolicy) error {
	normalized, err := config.NormalizePanelAccessPolicy(policy)
	if err != nil {
		return err
	}
	previous := config.AppConfig.PanelAccessPolicy
	config.AppConfig.PanelAccessPolicy = normalized
	if err := config.SaveConfig(); err != nil {
		config.AppConfig.PanelAccessPolicy = previous
		return fmt.Errorf("save panel access policy: %w", err)
	}
	return nil
}

func reloadPanelAfterAccessPolicyCommand() error {
	if !isWebPanelRunning() {
		return nil
	}
	if err := restartService("clicd"); err != nil {
		return fmt.Errorf("policy was saved but clicd service restart failed: %w", err)
	}
	return nil
}

func printPanelAccessPolicy(policy config.PanelAccessPolicy) {
	fmt.Printf("Enabled: %t\n", policy.Enabled)
	fmt.Printf("Allowed sources: %s\n", strings.Join(policy.AllowedSources, ", "))
	fmt.Printf("Trusted proxies: %s\n", strings.Join(policy.TrustedProxies, ", "))
}
