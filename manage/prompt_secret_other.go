//go:build !linux

package manage

import "bufio"

// readSecret falls back to a visible line read on platforms without the termios
// echo toggle; the token is shown as typed.
func readSecret(in *bufio.Reader, msg string) (string, error) {
	return promptLine(in, msg), nil
}
