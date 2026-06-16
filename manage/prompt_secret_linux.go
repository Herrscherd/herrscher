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

// readSecret prompts on stderr and reads one line with terminal echo disabled,
// so a typed or pasted token never appears on screen. It reads through the
// shared bufio.Reader (no input lost between prompts) and falls back to a
// visible read when stdin is not a terminal.
func readSecret(in *bufio.Reader, msg string) (string, error) {
	fmt.Fprint(os.Stderr, msg)
	fd := os.Stdin.Fd()

	var old syscall.Termios
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TCGETS, uintptr(unsafe.Pointer(&old))); errno != 0 {
		return promptLine(in, ""), nil // not a tty: read visibly
	}
	raw := old
	raw.Lflag &^= syscall.ECHO
	syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TCSETS, uintptr(unsafe.Pointer(&raw)))

	line, err := in.ReadString('\n')
	syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TCSETS, uintptr(unsafe.Pointer(&old)))
	fmt.Fprintln(os.Stderr) // the suppressed newline
	return strings.TrimSpace(line), err
}
