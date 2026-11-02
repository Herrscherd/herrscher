//go:build linux

package manage

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

func readSecret(in *bufio.Reader, msg string) (string, error) {
	fmt.Fprint(os.Stderr, msg)
	fd := int(os.Stdin.Fd())

	old, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return promptLine(in, ""), nil
	}
	raw := *old
	raw.Lflag &^= unix.ECHO
	if err = unix.IoctlSetTermios(fd, unix.TCSETS, &raw); err != nil {
		return "", err
	}

	line, err := in.ReadString('\n')
	if restoreErr := unix.IoctlSetTermios(fd, unix.TCSETS, old); restoreErr != nil {
		slog.Warn("restore terminal echo", "err", restoreErr)
	}
	fmt.Fprintln(os.Stderr)
	return strings.TrimSpace(line), err
}
