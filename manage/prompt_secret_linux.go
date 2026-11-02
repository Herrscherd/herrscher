//go:build linux

package manage

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

func readSecret(in *bufio.Reader, msg string) (string, error) {
	fmt.Fprint(os.Stderr, msg)
	fd := os.Stdin.Fd()

	var old syscall.Termios
	if err := ioctlTermios(fd, syscall.TCGETS, &old); err != nil {
		return promptLine(in, ""), nil
	}
	raw := old
	raw.Lflag &^= syscall.ECHO
	if err := ioctlTermios(fd, syscall.TCSETS, &raw); err != nil {
		return "", err
	}

	line, err := in.ReadString('\n')
	if restoreErr := ioctlTermios(fd, syscall.TCSETS, &old); restoreErr != nil {
		slog.Warn("restore terminal echo", "err", restoreErr)
	}
	fmt.Fprintln(os.Stderr)
	return strings.TrimSpace(line), err
}

func ioctlTermios(fd uintptr, request uintptr, t *syscall.Termios) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, request, uintptr(unsafe.Pointer(t))); errno != 0 {
		return errno
	}
	return nil
}
