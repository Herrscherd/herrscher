//go:build unix

package keyproxy

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

func openKeyFile(path string) (*os.File, error) {
	return os.OpenFile(filepath.Clean(path), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
}

func checkKeyDirectory(dir string, info fs.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("%s: %w", dir, errKeyDirUnsafe)
	}
	owner := int64(stat.Uid)
	if owner != 0 && owner != int64(os.Getuid()) {
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
