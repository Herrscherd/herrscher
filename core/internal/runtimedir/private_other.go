//go:build !unix

package runtimedir

import "errors"

// Private reports that no private runtime directory is used on this platform.
func (l Layout) Private() (string, error) {
	return "", errors.ErrUnsupported
}
