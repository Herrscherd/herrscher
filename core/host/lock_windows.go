//go:build windows

package host

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

const lockedOffset = 1 << 20

func lockInstance(instanceID string) (func(), error) {
	name := `Local\herrscher-instance-` + instanceID
	mutexName, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, wrapHost(err, "lock.state", nil)
	}
	handle, err := windows.CreateMutex(nil, false, mutexName)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		logCleanup(windows.CloseHandle(handle))
		return nil, wrapHost(errSocketTaken, "daemon.socket_taken", map[string]any{pathParam: name})
	}
	if err != nil {
		return nil, wrapHost(err, "lock.state", nil)
	}
	return func() { logCleanup(windows.CloseHandle(handle)) }, nil
}

func tryLock(f *os.File) (bool, error) {
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
