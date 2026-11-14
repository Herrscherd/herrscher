package control

import (
	"errors"
	"syscall"
)

var transientAcceptErrnos = []syscall.Errno{
	syscall.EMFILE,
	syscall.ENFILE,
	syscall.ECONNABORTED,
	syscall.ENOBUFS,
	syscall.ENOMEM,
}

func isTransientAcceptError(err error) bool {
	for _, errno := range transientAcceptErrnos {
		if errors.Is(err, errno) {
			return true
		}
	}
	return false
}
