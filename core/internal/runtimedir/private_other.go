//go:build !unix

package runtimedir

import (
	"errors"
	"os"
	"strconv"
)

func uidText() string {
	return strconv.Itoa(os.Getuid())
}

// Private reports that no private runtime directory is used on this platform.
func Private() (string, error) {
	return "", errors.ErrUnsupported
}
