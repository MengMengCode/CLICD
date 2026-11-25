package lxc

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
)

func TestTransferIdentityWritesCannotEscapeGuestRoot(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("LXC restore is Linux only")
	}
	root, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "etc")); err != nil {
		t.Fatal(err)
	}
	if err := safeTransferWrite(root, "etc/hostname", "new-name\n", os.Getuid(), os.Getgid()); err == nil {
		t.Fatal("guest parent symlink escaped root")
	}
	entries, _ := os.ReadDir(outside)
	if len(entries) != 0 {
		t.Fatal("modified host path outside restored guest")
	}
}

func TestOwnershipRewriteHonorsCancellationAndPreservesUnchangedSetID(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux ownership semantics")
	}
	root := t.TempDir()
	file := filepath.Join(root, "owned")
	if err := os.WriteFile(file, []byte("data"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(file, 0755|os.ModeSetuid); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(file)
	if err := rewriteTransferIDs(context.Background(), root, 0, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(file)
	if before.Mode() != after.Mode() {
		t.Fatal("unchanged ownership cleared guest set-ID bits")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := rewriteTransferIDs(ctx, root, 0, 0, 0, 0); err != context.Canceled {
		t.Fatal("ownership pass ignored cancellation")
	}
}

func TestParseSubIDRangeRejectsOutOfRangeStarts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "subuid")
	for _, value := range []string{
		"-1:65536",          // negative start
		"4294967296:65536",  // start outside the 32-bit ID space
		"4294901761:65536",  // 65536-id range crossing 2^32
		"4294967295:65536",  // same, at the upper edge
		"100000:65535",      // fewer than 65536 ids
		"100000:4294967296", // count outside the 32-bit ID space
	} {
		if err := os.WriteFile(path, []byte("root:"+value+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := parseSubIDRange(path, "root"); err == nil {
			t.Fatalf("accepted invalid subordinate ID range %q", value)
		}
	}
	if err := os.WriteFile(path, []byte("root:100000:65536\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := parseSubIDRange(path, "root"); err != nil || got != 100000 {
		t.Fatalf("valid subordinate ID range = %d, %v", got, err)
	}
	// Starts above math.MaxInt32 are representable on 64-bit hosts only; the
	// parser must keep accepting them there instead of narrowing the range.
	if strconv.IntSize == 64 {
		if err := os.WriteFile(path, []byte("root:3000000000:65536\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if got, err := parseSubIDRange(path, "root"); err != nil || got != 3000000000 {
			t.Fatalf("valid 64-bit subordinate ID range = %d, %v", got, err)
		}
	}
}
