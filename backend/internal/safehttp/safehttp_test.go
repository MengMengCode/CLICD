package safehttp

import (
	"context"
	"io"
	"net"
	"net/http"
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
		"http://100.100.100.200/latest/meta-data",
		"http://[fd00:ec2::254]/latest/meta-data",
		"http://example.com:99999/image",
	} {
		if _, err := ValidateURL(rawURL); err == nil {
			t.Fatalf("ValidateURL(%q) succeeded, want rejection", rawURL)
		}
	}
}

func TestValidateURLAcceptsPrivateNetworkMirror(t *testing.T) {
	t.Parallel()
	for _, rawURL := range []string{
		"http://10.0.0.10/images/rootfs.tar.xz",
		"http://172.16.20.30:8080/images/vm.qcow2",
		"https://192.168.1.10/image.iso",
		"http://100.64.0.10/image.qcow2",
		"http://[fd00::10]/rootfs.tar.xz",
	} {
		if _, err := ValidateURL(rawURL); err != nil {
			t.Errorf("ValidateURL(%q) returned error: %v", rawURL, err)
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

func TestIsAllowedDownloadAddress(t *testing.T) {
	t.Parallel()
	tests := map[string]bool{
		"8.8.8.8":              true,
		"1.1.1.1":              true,
		"2606:4700:4700::1111": true,
		"127.0.0.1":            false,
		"10.0.0.1":             true,
		"100.64.0.1":           true,
		"172.16.0.1":           true,
		"192.168.1.1":          true,
		"169.254.169.254":      false,
		"100.100.100.200":      false,
		"192.0.2.1":            false,
		"198.18.0.1":           false,
		"::1":                  false,
		"64:ff9b::127.0.0.1":   false,
		"2002:7f00:1::1":       false,
		"fc00::1":              true,
		"fd00:ec2::254":        false,
		"fec0::1":              false,
		"fe80::1":              false,
		"2001:db8::1":          false,
	}
	for raw, expected := range tests {
		if actual := isAllowedDownloadAddress(netip.MustParseAddr(raw)); actual != expected {
			t.Errorf("isAllowedDownloadAddress(%s) = %v, want %v", raw, actual, expected)
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

func TestGetAllowsPrivateNetworkMirror(t *testing.T) {
	privateIP := privateInterfaceIPv4(t)
	listener, err := net.Listen("tcp4", net.JoinHostPort(privateIP.String(), "0"))
	if err != nil {
		t.Fatalf("failed to listen on private interface: %v", err)
	}
	defer listener.Close()

	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "private mirror ok")
	})}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	response, err := Get(ctx, "http://"+listener.Addr().String()+"/image", "test", 5*time.Second)
	if err != nil {
		t.Fatalf("Get rejected private mirror: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "private mirror ok" {
		t.Fatalf("body = %q, want private mirror response", body)
	}
}

func privateInterfaceIPv4(t *testing.T) netip.Addr {
	t.Helper()
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatal(err)
	}
	for _, rawAddress := range addresses {
		prefix, err := netip.ParsePrefix(rawAddress.String())
		if err != nil {
			continue
		}
		address := prefix.Addr().Unmap()
		if address.Is4() && address.IsPrivate() && isAllowedDownloadAddress(address) {
			return address
		}
	}
	t.Skip("no private IPv4 interface is available")
	return netip.Addr{}
}
