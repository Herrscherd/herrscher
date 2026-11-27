//go:build windows

package filelock

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

const lockedOffset = 1 << 20

// Open opens or creates the lock file at path. It refuses a symbolic link or any other reparse point, as the unix
// open refuses a symbolic link.
func Open(path string) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return nil, err
	}
	if err := refuseReparsePoint(handle); err != nil {
		return nil, errors.Join(err, windows.CloseHandle(handle))
	}
	return os.NewFile(uintptr(handle), path), nil
}

func refuseReparsePoint(handle windows.Handle) error {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return ErrSymlink
	}
	return nil
}

// TryLock takes the exclusive lock of f without waiting. It reports false when
// another process holds it.
func TryLock(f *os.File) (bool, error) {
	err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{Offset: lockedOffset})
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, windows.ERROR_LOCK_VIOLATION):
		return false, nil
	default:
		return false, err
	}
}
