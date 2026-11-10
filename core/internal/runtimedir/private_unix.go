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

// Private creates the directory if needed and returns it, or an UnsafeError when
// it is a symlink, another user's, not a directory, or open to its group or
// others. It never changes an existing directory.
func (l Layout) Private() (string, error) {
	if err := os.Mkdir(l.dir, privateDirMode); err != nil && !errors.Is(err, fs.ErrExist) {
		return "", &UnsafeError{Path: l.dir, Kind: KindOther, Reason: err.Error()}
	}
	if err := verifyPrivate(l.dir); err != nil {
		return "", err
	}
	return l.dir, nil
}

func verifyPrivate(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return &UnsafeError{Path: dir, Kind: KindOther, Reason: err.Error()}
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return &UnsafeError{Path: dir, Kind: KindSymlink, Reason: "is a symbolic link"}
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return &UnsafeError{Path: dir, Kind: KindOther, Reason: "owner cannot be read"}
	}
	if int(stat.Uid) != os.Getuid() {
		return &UnsafeError{Path: dir, Kind: KindForeignOwner, Reason: "was created by another account"}
	}
	if !info.IsDir() {
		return &UnsafeError{Path: dir, Kind: KindNotDir, Reason: "is not a directory"}
	}
	if info.Mode().Perm()&groupOtherMask != 0 {
		return &UnsafeError{Path: dir, Kind: KindOpenAccess, Reason: "is accessible to its group or others"}
	}
	return nil
}

func usableRunUserDir(dir string, uid int) bool {
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == uid && info.Mode().Perm() == privateDirMode
}
