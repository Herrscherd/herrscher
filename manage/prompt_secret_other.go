//go:build !linux

package manage

import "bufio"

func readSecret(in *bufio.Reader, msg string) (string, error) {
	return promptLine(in, msg), nil
}
