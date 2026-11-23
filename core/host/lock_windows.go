//go:build windows

package host

import (
	"errors"

	"golang.org/x/sys/windows"

	"github.com/Herrscherd/herrscher/core/internal/runtimedir"
)

func lockInstance(messages *hostMessages, _ runtimedir.Layout, instanceID string) (func(), error) {
	name := `Local\herrscher-instance-` + instanceID
	mutexName, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, wrapHost(messages, err, "lock.state", nil)
	}
	handle, err := windows.CreateMutex(nil, false, mutexName)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		logCleanup(windows.CloseHandle(handle))
		return nil, wrapHost(messages, errInstanceLocked, "daemon.instance_locked", map[string]any{pathParam: name})
	}
	if err != nil {
		return nil, wrapHost(messages, err, "lock.state", nil)
	}
	return func() { logCleanup(windows.CloseHandle(handle)) }, nil
}
