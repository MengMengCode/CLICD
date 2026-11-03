//go:build !linux

package lxc

import (
	"fmt"
	"os"
)

func transferFileID(os.FileInfo) ([2]uint64, bool) { return [2]uint64{}, false }

func remapTransferOwnership(string, os.FileInfo, int, int, uint32, uint32, int, int) error {
	return fmt.Errorf("LXC filesystem ownership restore requires Linux")
}
