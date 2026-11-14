//go:build windows

package control

import (
	"testing"

	"golang.org/x/sys/windows"
)

func TestAcceptRetriesEachTransientErrno(t *testing.T) {
	testAcceptRetriesEachTransientError(t, []error{
		windows.ERROR_TOO_MANY_OPEN_FILES,
		windows.ERROR_NO_SYSTEM_RESOURCES,
		windows.ERROR_NOT_ENOUGH_MEMORY,
		windows.ERROR_OUTOFMEMORY,
	})
}

func transientAcceptFailure() error {
	return windows.ERROR_TOO_MANY_OPEN_FILES
}
