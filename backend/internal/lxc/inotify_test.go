package lxc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureInotifyInstances(t *testing.T) {
	for _, tc := range []struct {
		name    string
		initial string
		want    string
		wantErr bool
	}{
		{name: "low limit", initial: "128\n", want: "2048\n"},
		{name: "at minimum", initial: "2048\n", want: "2048\n"},
		{name: "higher administrator limit", initial: "8192\n", want: "8192\n"},
		{name: "invalid limit", initial: "unknown\n", want: "unknown\n", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "max_user_instances")
			if err := os.WriteFile(path, []byte(tc.initial), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := ensureInotifyInstances(path); (err != nil) != tc.wantErr {
				t.Fatalf("ensureInotifyInstances error = %v, want error: %t", err, tc.wantErr)
			}
			result, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(result) != tc.want {
				t.Fatalf("limit = %q, want %q", result, tc.want)
			}
		})
	}
}

func TestLXCStartupErrorInotifyHint(t *testing.T) {
	dir := t.TempDir()
	console := filepath.Join(dir, "console.log")
	for _, tc := range []struct {
		console string
		want    bool
	}{
		{"Failed to create control group inotify object: Too many open files", true},
		{"Failed to open rootfs: Too many open files", false},
		{"Failed to create control group inotify object: Permission denied", false},
	} {
		if err := os.WriteFile(console, []byte(tc.console), 0600); err != nil {
			t.Fatal(err)
		}
		message := lxcStartupError("container exited", filepath.Join(dir, "start.log"), console).Error()
		if strings.Contains(message, InotifyInstancesKey) != tc.want || !strings.Contains(message, tc.console) {
			t.Errorf("unexpected diagnostic for %q: %s", tc.console, message)
		}
	}
}
