//go:build !windows

package control

import "syscall"

func transientAcceptErrors() []error {
	return []error{
		syscall.EMFILE,
		syscall.ENFILE,
		syscall.ECONNABORTED,
		syscall.ENOBUFS,
		syscall.ENOMEM,
	}
}
