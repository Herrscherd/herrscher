//go:build unix

package drives

import (
	"errors"
	"os"
	"syscall"
)

const (
	sourceOpenFlags  = os.O_RDONLY | syscall.O_NONBLOCK | syscall.O_NOFOLLOW
	stagingOpenFlags = syscall.O_NOFOLLOW
)

func isSymlinkRefusal(err error) bool {
	return errors.Is(err, syscall.ELOOP)
}
