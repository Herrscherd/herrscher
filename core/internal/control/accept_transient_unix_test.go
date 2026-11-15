//go:build !windows

package control

import (
	"syscall"
	"testing"
)

func TestAcceptRetriesEachTransientErrno(t *testing.T) {
	testAcceptRetriesEachTransientError(t, []error{syscall.EMFILE, syscall.ENFILE, syscall.ECONNABORTED, syscall.ENOBUFS, syscall.ENOMEM})
}

func transientAcceptFailure() error {
	return syscall.EMFILE
}
