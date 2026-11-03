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
	if !stdinIsTerminal(fd) {
		return promptLine(in, ""), nil
	}

	old, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return "", err
	}
	raw := *old
	raw.Lflag &^= unix.ECHO
	if setErr := unix.IoctlSetTermios(fd, unix.TCSETS, &raw); setErr != nil {
		return "", setErr
	}

	line, err := in.ReadString('\n')
	if restoreErr := unix.IoctlSetTermios(fd, unix.TCSETS, old); restoreErr != nil {
		slog.Warn("restore terminal echo", "err", restoreErr)
	}
	fmt.Fprintln(os.Stderr)
	return strings.TrimSpace(line), err
}

func stdinIsTerminal(fd int) bool {
	_, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	return err == nil
}
