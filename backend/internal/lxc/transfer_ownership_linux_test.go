//go:build linux

package lxc

import "testing"

func TestTransferOwnershipIDConversionRejectsInvalidValues(t *testing.T) {
	if _, err := checkedTransferOwnershipID(-1); err == nil {
		t.Fatal("negative ownership ID was accepted")
	}
	if got, err := checkedTransferOwnershipID(100000); err != nil || got != 100000 {
		t.Fatalf("valid ownership ID conversion = %d, %v", got, err)
	}
}

func TestTransferOwnershipIDRemapRejectsUint32Overflow(t *testing.T) {
	if _, _, err := remapTransferOwnershipID(102, 100, ^uint32(0)-1); err == nil {
		t.Fatal("ownership ID remap overflow was accepted")
	}
	if got, changed, err := remapTransferOwnershipID(101, 100, ^uint32(0)-1); err != nil || !changed || got != ^uint32(0) {
		t.Fatalf("boundary ownership ID remap = %d, %t, %v", got, changed, err)
	}
	if got, changed, err := remapTransferOwnershipID(42, 0, 100000); err != nil || !changed || got != 100042 {
		t.Fatalf("unmapped guest ownership ID remap = %d, %t, %v", got, changed, err)
	}
}
