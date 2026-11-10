// Package runtimedir gives every local socket and lock path a home that no other
// user can pre-create: a short name in the shared temp dir when it fits, and a
// private per-user directory otherwise.
package runtimedir

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
)

// MaxUnixSocketPath is the longest socket path, in bytes, that sun_path holds
// with its terminating NUL on the supported platforms.
const MaxUnixSocketPath = 103

const (
	socketSuffix      = ".sock"
	privateSubdir     = "herrscher"
	maxPrivateBytes   = 40
	keyBytes          = 24
	sharedRuntimeRoot = "/tmp"
)

// UnsafeError reports a private runtime directory that another user could have
// planted, and names the directory and what is wrong with it.
type UnsafeError struct {
	Path   string
	Reason string
}

// Error renders the path and the reason it cannot be used.
func (e *UnsafeError) Error() string {
	return e.Path + ": " + e.Reason
}

// Key is a short, stable digest of name, safe to embed in a file name.
func Key(name string) string {
	sum := sha256.Sum256([]byte(name))
	return hex.EncodeToString(sum[:])[:keyBytes]
}

// Socket is the unix socket path for stem. It stays in the shared temp dir when
// it fits there, and otherwise goes in the private runtime directory under a
// digest of stem, so no other user can pre-create it.
func Socket(stem string) string {
	plain := filepath.Join(os.TempDir(), stem+socketSuffix)
	if len(plain) <= MaxUnixSocketPath {
		return plain
	}
	return filepath.Join(privateDir(), "hs-"+Key(stem)+socketSuffix)
}

// Holds reports whether path lives in the private runtime directory.
func Holds(path string) bool {
	return filepath.Dir(path) == privateDir()
}

func privateDir() string {
	if base := os.Getenv("XDG_RUNTIME_DIR"); base != "" {
		if dir := filepath.Join(base, privateSubdir); len(dir) <= maxPrivateBytes {
			return dir
		}
	}
	return filepath.Join(sharedRuntimeRoot, privateSubdir+"-"+uidText())
}

// Check verifies the private runtime directory when path lives in it, and
// accepts any other path.
func Check(path string) error {
	if !Holds(path) {
		return nil
	}
	_, err := Private()
	return err
}
