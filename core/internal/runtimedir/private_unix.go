//go:build unix

package runtimedir

import (
	"os"
	"strconv"
	"syscall"
)

const (
	privateDirMode = 0o700
	groupOtherMask = 0o077
)

func uidText() string {
	return strconv.Itoa(os.Getuid())
}

// Private creates the current user's private runtime directory if needed and
// returns it, or an UnsafeError when it is a symlink, another user's, or open to
// its group or others.
func Private() (string, error) {
	dir := privateDir()
	if err := os.MkdirAll(dir, privateDirMode); err != nil {
		return "", &UnsafeError{Path: dir, Reason: err.Error()}
	}
	if err := verifyPrivate(dir); err != nil {
		return "", err
	}
	return dir, nil
}

func verifyPrivate(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return &UnsafeError{Path: dir, Reason: err.Error()}
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		return &UnsafeError{Path: dir, Reason: "is a symbolic link"}
	case !info.IsDir():
		return &UnsafeError{Path: dir, Reason: "is not a directory"}
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return &UnsafeError{Path: dir, Reason: "owner cannot be read"}
	}
	if int(stat.Uid) != os.Getuid() {
		return &UnsafeError{Path: dir, Reason: "is owned by another user"}
	}
	if info.Mode().Perm()&groupOtherMask != 0 {
		return &UnsafeError{Path: dir, Reason: "is accessible to its group or others"}
	}
	return nil
}
