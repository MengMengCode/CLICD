package lxc

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestCheckIPv6Connectivity_TCPSuccess(t *testing.T) {
	// Start a local TCP listener to simulate an open port
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer l.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ok := checkIPv6Connectivity(ctx, []string{l.Addr().String()}, nil)
	if !ok {
		t.Errorf("expected checkIPv6Connectivity to succeed with active TCP listener")
	}
}

func TestCheckIPv6Connectivity_PingCommandSuccess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// "go version" always exits 0
	ok := checkIPv6Connectivity(ctx, nil, [][]string{{"go", "version"}})
	if !ok {
		t.Errorf("expected checkIPv6Connectivity to succeed when ping-like command exits 0")
	}
}

func TestCheckIPv6Connectivity_ICMPFailsButTCPSucceeds(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer l.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Ping command points to a command that fails (exit 1), but TCP succeeds
	failingCmd := [][]string{{"go", "tool", "non_existent_binary_xyz_123"}}
	ok := checkIPv6Connectivity(ctx, []string{l.Addr().String()}, failingCmd)
	if !ok {
		t.Errorf("expected checkIPv6Connectivity to succeed when ICMP fails but TCP succeeds")
	}
}

func TestCheckIPv6Connectivity_AllFail(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Unreachable TCP address and failing command
	failingCmd := [][]string{{"go", "tool", "non_existent_binary_xyz_123"}}
	unreachableTCP := []string{"127.0.0.1:1"} // Port 1 is not listening

	ok := checkIPv6Connectivity(ctx, unreachableTCP, failingCmd)
	if ok {
		t.Errorf("expected checkIPv6Connectivity to fail when all targets fail")
	}
}
