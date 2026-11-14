//go:build windows

package control

import "golang.org/x/sys/windows"

func transientAcceptErrors() []error {
	return []error{
		windows.ERROR_TOO_MANY_OPEN_FILES,
		windows.ERROR_NO_SYSTEM_RESOURCES,
		windows.ERROR_NOT_ENOUGH_MEMORY,
		windows.ERROR_OUTOFMEMORY,
	}
}
