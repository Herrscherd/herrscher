//go:build unix

package keyproxy

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

var errKeyDirUnsafe = errors.New("key file directory must belong to the user or root, and must not be writable by its group or others")

func openKeyFile(path string) (*os.File, error) {
	return os.OpenFile(filepath.Clean(path), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
}

func checkKeyDirectory(dir string, info fs.FileInfo, uid int64) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("%s: %w", dir, errKeyDirUnsafe)
	}
	owner := int64(stat.Uid)
	if owner != 0 && owner != uid {
		return fmt.Errorf("%s: %w", dir, errKeyDirUnsafe)
	}
	if info.Mode()&fs.ModeSticky != 0 && owner == 0 {
		return nil
	}
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("%s: %w", dir, errKeyDirUnsafe)
	}
	return nil
}
