package safehttp

import (
	"context"
	"net/netip"
	"testing"
	"time"
)

func TestValidateURLRejectsUnsafeDestinations(t *testing.T) {
	t.Parallel()
	for _, rawURL := range []string{
		"file:///etc/passwd",
		"http://user:pass@example.com/image",
		"http://127.0.0.1/image",
		"http://[::1]/image",
		"http://169.254.169.254/latest/meta-data",
		"http://10.0.0.1/image",
		"http://192.168.1.10/image",
		"http://100.64.0.1/image",
		"http://example.com:99999/image",
	} {
		if _, err := ValidateURL(rawURL); err == nil {
			t.Fatalf("ValidateURL(%q) succeeded, want rejection", rawURL)
		}
	}
}

func TestValidateURLAcceptsPublicHTTPURL(t *testing.T) {
	t.Parallel()
	parsed, err := ValidateURL("https://example.com/images/rootfs.tar.xz?variant=default")
	if err != nil {
		t.Fatalf("ValidateURL returned error: %v", err)
	}
	if parsed.Hostname() != "example.com" {
		t.Fatalf("hostname = %q, want example.com", parsed.Hostname())
	}
}

func TestIsPublicAddress(t *testing.T) {
	t.Parallel()
	tests := map[string]bool{
		"8.8.8.8":              true,
		"1.1.1.1":              true,
		"2606:4700:4700::1111": true,
		"127.0.0.1":            false,
		"10.0.0.1":             false,
		"100.64.0.1":           false,
		"169.254.169.254":      false,
		"192.0.2.1":            false,
		"198.18.0.1":           false,
		"::1":                  false,
		"64:ff9b::127.0.0.1":   false,
		"2002:7f00:1::1":       false,
		"fc00::1":              false,
		"fec0::1":              false,
		"fe80::1":              false,
		"2001:db8::1":          false,
	}
	for raw, expected := range tests {
		if actual := isPublicAddress(netip.MustParseAddr(raw)); actual != expected {
			t.Errorf("isPublicAddress(%s) = %v, want %v", raw, actual, expected)
		}
	}
}

func TestGetRejectsLoopbackBeforeRequest(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := Get(ctx, "http://127.0.0.1:1/image", "test", time.Second); err == nil {
		t.Fatal("Get accepted a loopback destination")
	}
}
