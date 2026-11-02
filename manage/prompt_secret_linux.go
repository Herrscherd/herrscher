//go:build linux

package manage

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

func readSecret(in *bufio.Reader, msg string) (string, error) {
	fmt.Fprint(os.Stderr, msg)
	fd := os.Stdin.Fd()

	var old syscall.Termios
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TCGETS, uintptr(unsafe.Pointer(&old))); errno != 0 {
		return promptLine(in, ""), nil
	}
	raw := old
	raw.Lflag &^= syscall.ECHO
	syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TCSETS, uintptr(unsafe.Pointer(&raw)))

	line, err := in.ReadString('\n')
	syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TCSETS, uintptr(unsafe.Pointer(&old)))
	fmt.Fprintln(os.Stderr)
	return strings.TrimSpace(line), err
}
