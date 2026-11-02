package server

import (
	"net"
	"testing"
)

func TestServerListenAddress(t *testing.T) {
	for _, tc := range []struct {
		name string
		host string
		want string
	}{
		{"default", "", "0.0.0.0:8080"},
		{"blank", " \t ", "0.0.0.0:8080"},
		{"IPv4", " 127.0.0.1 ", "127.0.0.1:8080"},
		{"hostname", "localhost", "localhost:8080"},
		{"IPv6 loopback", "::1", "[::1]:8080"},
		{"IPv6 wildcard", "::", "[::]:8080"},
		{"IPv6 zone", "fe80::1%eth0", "[fe80::1%eth0]:8080"},
		{"bracketed IPv6", "[::1]", "[::1]:8080"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			addr := serverListenAddress(tc.host, 8080)
			if addr != tc.want {
				t.Fatalf("serverListenAddress(%q, 8080) = %q, want %q", tc.host, addr, tc.want)
			}
			if _, _, err := net.SplitHostPort(addr); err != nil {
				t.Fatalf("invalid listen address %q: %v", addr, err)
			}
		})
	}
}
