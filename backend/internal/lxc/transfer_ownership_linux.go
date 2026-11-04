//go:build linux

package lxc

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

const maxTransferOwnershipID = uint64(^uint32(0))

func checkedTransferOwnershipID(value int) (uint32, error) {
	if value < 0 || uint64(value) > maxTransferOwnershipID {
		return 0, fmt.Errorf("ownership ID %d is outside the uint32 range", value)
	}
	return uint32(value), nil
}

func remapTransferOwnershipID(id, oldBase, newBase uint32) (uint32, bool, error) {
	var offset uint64
	switch {
	case oldBase > 0 && id >= oldBase && uint64(id-oldBase) < 65536:
		offset = uint64(id - oldBase)
	case id < 65536:
		offset = uint64(id)
	default:
		return id, false, nil
	}
	mapped := uint64(newBase) + offset
	if mapped > maxTransferOwnershipID {
		return 0, false, fmt.Errorf("remapped ownership ID exceeds uint32 range")
	}
	return uint32(mapped), true, nil
}

func transferFileID(info os.FileInfo) ([2]uint64, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return [2]uint64{}, false
	}
	return [2]uint64{uint64(st.Dev), st.Ino}, true
}

// chown clears set-ID bits and file capabilities even when cloning an ext4
// image. Preserve them while translating the subordinate-UID root ID.
func remapTransferOwnership(path string, st os.FileInfo, uid, gid int, oldUID, oldGID uint32, newUID, newGID int) error {
	newUID32, err := checkedTransferOwnershipID(newUID)
	if err != nil {
		return err
	}
	newGID32, err := checkedTransferOwnershipID(newGID)
	if err != nil {
		return err
	}
	xattrs := map[string][]byte{}
	for _, name := range []string{"security.capability", "system.posix_acl_access", "system.posix_acl_default"} {
		size, err := unix.Lgetxattr(path, name, nil)
		if errors.Is(err, unix.ENODATA) || errors.Is(err, unix.ENOTSUP) {
			continue
		}
		if err != nil {
			return err
		}
		value := make([]byte, size)
		if _, err = unix.Lgetxattr(path, name, value); err != nil {
			return err
		}
		if name == "security.capability" && len(value) == 24 && binary.LittleEndian.Uint32(value[:4])&0xff000000 == 0x03000000 {
			root := binary.LittleEndian.Uint32(value[20:])
			if mapped, changed, err := remapTransferOwnershipID(root, oldUID, newUID32); err != nil {
				return err
			} else if changed {
				binary.LittleEndian.PutUint32(value[20:], mapped)
			}
		}
		if name != "security.capability" && len(value) >= 4 {
			for offset := 4; offset+8 <= len(value); offset += 8 {
				tag := binary.LittleEndian.Uint16(value[offset:])
				id := binary.LittleEndian.Uint32(value[offset+4:])
				if tag == 0x02 {
					mapped, changed, err := remapTransferOwnershipID(id, oldUID, newUID32)
					if err != nil {
						return err
					}
					if changed {
						binary.LittleEndian.PutUint32(value[offset+4:], mapped)
					}
				}
				if tag == 0x08 {
					mapped, changed, err := remapTransferOwnershipID(id, oldGID, newGID32)
					if err != nil {
						return err
					}
					if changed {
						binary.LittleEndian.PutUint32(value[offset+4:], mapped)
					}
				}
			}
		}
		xattrs[name] = value
	}
	if err := os.Lchown(path, uid, gid); err != nil {
		return err
	}
	if st.Mode()&os.ModeSymlink == 0 {
		if err := os.Chmod(path, st.Mode()); err != nil {
			return err
		}
	}
	for name, value := range xattrs {
		if err := unix.Lsetxattr(path, name, value, 0); err != nil {
			return err
		}
	}
	return nil
}
