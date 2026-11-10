//go:build unix

package runtimedir

import (
	"errors"
	"io/fs"
	"os"
	"syscall"
)

const (
	privateDirMode = 0o700
	groupOtherMask = 0o077
)

// Private creates the current user's private directory if needed and returns
// it, or an UnsafeError when it is a symlink, another user's, or open to its
// group or others. It never changes an existing directory.
func (l Layout) Private() (string, error) {
	dir := l.Dir()
	if err := os.Mkdir(dir, privateDirMode); err != nil && !errors.Is(err, fs.ErrExist) {
		return "", &UnsafeError{Path: dir, Kind: KindOther, Reason: err.Error()}
	}
	if err := verifyPrivate(dir); err != nil {
		return "", err
	}
	return dir, nil
}

func verifyPrivate(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return &UnsafeError{Path: dir, Kind: KindOther, Reason: err.Error()}
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		return &UnsafeError{Path: dir, Kind: KindSymlink, Reason: "is a symbolic link"}
	case !info.IsDir():
		return &UnsafeError{Path: dir, Kind: KindNotDir, Reason: "is not a directory"}
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return &UnsafeError{Path: dir, Kind: KindOther, Reason: "owner cannot be read"}
	}
	if int(stat.Uid) != os.Getuid() {
		return &UnsafeError{Path: dir, Kind: KindForeignOwner, Reason: "is owned by another user"}
	}
	if info.Mode().Perm()&groupOtherMask != 0 {
		return &UnsafeError{Path: dir, Kind: KindOpenAccess, Reason: "is accessible to its group or others"}
	}
	return nil
}
