package kvm

import (
	"crypto/ed25519"
	"crypto/rand"
	"path/filepath"
	"reflect"
	"testing"

	"clicd/internal/config"

	"golang.org/x/crypto/ssh"
)

func TestImagePathUsesAllowlistedImageID(t *testing.T) {
	for _, id := range []string{"", ".", "..", "../../etc/passwd", `..\\..\\windows`, "/absolute", "unknown-image"} {
		if got := filepath.Base(ImagePath(id)); got != "__invalid_image_id__.qcow2" {
			t.Fatalf("ImagePath(%q) basename = %q", id, got)
		}
	}
	validID := GetImages()[0].ID
	if got := filepath.Base(ImagePath(validID)); got != validID+".qcow2" {
		t.Fatalf("ImagePath(%q) basename = %q", validID, got)
	}
}

func TestLibvirtNetworkActiveParsesCLocaleOutput(t *testing.T) {
	tests := []struct {
		name string
		info string
		want bool
	}{
		{name: "active", info: "Name: default\nActive: yes\n", want: true},
		{name: "spacing and case", info: "  Active  :  YES  \r\n", want: true},
		{name: "inactive", info: "Name: default\nActive: no\n", want: false},
		{name: "missing field", info: "Name: default\nAutostart: yes\n", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := libvirtNetworkActive(tc.info); got != tc.want {
				t.Fatalf("libvirtNetworkActive(%q) = %v, want %v", tc.info, got, tc.want)
			}
		})
	}
}

func TestChpasswdStdinPreservesShellMetacharacters(t *testing.T) {
	password := `pa'";$(touch /tmp/pwned); echo #\\word`
	got, err := chpasswdStdin("root", password)
	if err != nil {
		t.Fatalf("chpasswdStdin returned error: %v", err)
	}

	want := []byte("root:" + password + "\n")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("chpasswdStdin = %#v, want %#v", got, want)
	}
}

func TestChpasswdStdinRejectsNewlines(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password string
	}{
		{name: "username newline", username: "root\nadmin", password: "safe"},
		{name: "username colon", username: "root:admin", password: "safe"},
		{name: "password newline", username: "root", password: "safe\nroot:evil"},
		{name: "password carriage return", username: "root", password: "safe\rroot:evil"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := chpasswdStdin(tc.username, tc.password); err == nil {
				t.Fatal("chpasswdStdin returned nil error")
			}
		})
	}
}

func TestVerifyKVMHostKeyCapturesAndRejectsMismatch(t *testing.T) {
	key1 := testSSHPublicKey(t)
	key2 := testSSHPublicKey(t)

	saves := 0
	c := &config.Container{}
	save := func() error {
		saves++
		return nil
	}

	if err := verifyKVMHostKey(c, key1, save); err != nil {
		t.Fatalf("first host key verification returned error: %v", err)
	}
	if c.SSHHostKey == "" {
		t.Fatal("first host key verification did not capture fingerprint")
	}
	if c.SSHHostKey != sshHostKeyFingerprint(key1) {
		t.Fatalf("captured fingerprint = %q, want %q", c.SSHHostKey, sshHostKeyFingerprint(key1))
	}
	if saves != 1 {
		t.Fatalf("save count = %d, want 1", saves)
	}

	if err := verifyKVMHostKey(c, key1, save); err != nil {
		t.Fatalf("same host key verification returned error: %v", err)
	}
	if saves != 1 {
		t.Fatalf("save count after same key = %d, want 1", saves)
	}

	if err := verifyKVMHostKey(c, key2, save); err == nil {
		t.Fatal("mismatched host key verification returned nil error")
	}
}

func testSSHPublicKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return signer.PublicKey()
}
