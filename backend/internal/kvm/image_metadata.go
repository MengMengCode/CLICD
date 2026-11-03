package kvm

import (
	"encoding/binary"
	"math"
	"os"
)

// ImageVirtualDiskGB reads only the QCOW2 header without opening backing files.
func ImageVirtualDiskGB(id string) float64 {
	f, err := os.Open(ImagePath(id))
	if err != nil {
		return 0
	}
	defer f.Close()
	var header [32]byte
	if _, err := f.ReadAt(header[:], 0); err != nil || string(header[:4]) != "QFI\xfb" {
		return 0
	}
	version := binary.BigEndian.Uint32(header[4:8])
	if version != 2 && version != 3 {
		return 0
	}
	return math.Ceil(float64(binary.BigEndian.Uint64(header[24:32])) / float64(1<<30))
}
