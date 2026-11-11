//go:build !windows

// Package sockfile binds unix socket files in a way that never removes a path.
package sockfile

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"os"
	"path/filepath"
)

const hexDigitsPerByte = 2

// Listen binds a unix socket that answers at path with the given mode. The
// socket is bound under a fresh name of the same length in the same directory
// and then renamed onto path, so a dead socket file left there by a crash is
// replaced in one step and never removed by path, and a socket is never
// visible with a looser mode than mode. The listener does not unlink its file
// when closed: the file stays in place, dead, until a later Listen replaces it
// or its owner removes it.
func Listen(ctx context.Context, path string, mode os.FileMode) (net.Listener, error) {
	staged, err := stagedPath(path)
	if err != nil {
		return nil, err
	}
	listener, err := new(net.ListenConfig).Listen(ctx, "unix", staged)
	if err != nil {
		return nil, err
	}
	if ul, ok := listener.(*net.UnixListener); ok {
		ul.SetUnlinkOnClose(false)
	}
	if err := publish(staged, path, mode); err != nil {
		return nil, errors.Join(err, listener.Close(), os.Remove(staged))
	}
	return listener, nil
}

func publish(staged, path string, mode os.FileMode) error {
	if err := os.Chmod(staged, mode); err != nil {
		return err
	}
	return os.Rename(staged, path)
}

func stagedPath(path string) (string, error) {
	dir, base := filepath.Split(path)
	name, err := randomName(len(base))
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

func randomName(length int) (string, error) {
	buf := make([]byte, (length+hexDigitsPerByte-1)/hexDigitsPerByte)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf)[:length], nil
}
