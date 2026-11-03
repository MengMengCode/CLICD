//go:build linux

package lxc

import (
	"encoding/binary"
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

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
			if oldUID > 0 && root >= oldUID && root < oldUID+65536 {
				binary.LittleEndian.PutUint32(value[20:], root-oldUID+uint32(newUID))
			}
		}
		if name != "security.capability" && len(value) >= 4 {
			for offset := 4; offset+8 <= len(value); offset += 8 {
				tag := binary.LittleEndian.Uint16(value[offset:])
				id := binary.LittleEndian.Uint32(value[offset+4:])
				if tag == 0x02 && oldUID > 0 && id >= oldUID && id < oldUID+65536 {
					binary.LittleEndian.PutUint32(value[offset+4:], id-oldUID+uint32(newUID))
				}
				if tag == 0x08 && oldGID > 0 && id >= oldGID && id < oldGID+65536 {
					binary.LittleEndian.PutUint32(value[offset+4:], id-oldGID+uint32(newGID))
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
