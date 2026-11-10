// Package runtimedir gives each user one directory for the local sockets and
// locks of herrscher: /tmp/herrscher-<uid>, created 0700 and verified before use,
// so no other user can pre-create or substitute an entry in it. A socket keeps a
// readable name when its full path fits the unix socket limit and is named by a
// digest otherwise.
package runtimedir

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path"
	"strconv"
	"strings"
)

// MaxUnixSocketPath is the longest socket path, in bytes, that sun_path holds
// with its terminating NUL on the supported platforms.
const MaxUnixSocketPath = 103

const (
	productionRoot = "/tmp"
	dirPrefix      = "herrscher-"
	plainPrefix    = "herrscher-"
	hashedPrefix   = "hs-"
	instanceLock   = "herrscher-instance-"
	socketSuffix   = ".sock"
	lockSuffix     = ".lock"
	keyBytes       = 24
)

// Layout is where a user's herrscher entries live. Production always uses the
// shared temp root; a test sets Root to a short directory of its own.
type Layout struct {
	Root string
}

// Production is the layout of a running herrscher.
func Production() Layout {
	return Layout{Root: productionRoot}
}

// DirOf is the private directory of the user uid under root.
func DirOf(root string, uid int) string {
	return path.Join(root, dirPrefix+strconv.Itoa(uid))
}

// Dir is the private directory of the current user.
func (l Layout) Dir() string {
	return DirOf(l.Root, os.Getuid())
}

// Socket is the unix socket path for stem in the private directory.
func (l Layout) Socket(stem string) string {
	return Name(l.Dir(), stem)
}

// Lock is the instance lock path for instanceID in the private directory.
func (l Layout) Lock(instanceID string) string {
	return path.Join(l.Dir(), instanceLock+Key(instanceID)+lockSuffix)
}

// Name is the socket path for stem inside dir. A stem that starts with
// herrscher- keeps its name when the path fits MaxUnixSocketPath; any other stem,
// and any longer path, takes the hashed name, which starts with hs- and so never
// meets a readable name.
func Name(dir, stem string) string {
	plain := path.Join(dir, stem+socketSuffix)
	if strings.HasPrefix(stem, plainPrefix) && len(plain) <= MaxUnixSocketPath {
		return plain
	}
	return path.Join(dir, hashedPrefix+Key(stem)+socketSuffix)
}

// Key is a short, stable digest of name, safe to embed in a file name.
func Key(name string) string {
	sum := sha256.Sum256([]byte(name))
	return hex.EncodeToString(sum[:])[:keyBytes]
}

// Check verifies the private directory when path lives in it, and accepts any
// other path.
func (l Layout) Check(p string) error {
	if path.Dir(p) != l.Dir() {
		return nil
	}
	_, err := l.Private()
	return err
}

// Kind classifies why a private directory cannot be used.
type Kind string

// The kinds of Kind, one per reason a private directory is refused.
const (
	KindSymlink      Kind = "symlink"
	KindNotDir       Kind = "not_dir"
	KindForeignOwner Kind = "foreign_owner"
	KindOpenAccess   Kind = "open_access"
	KindOther        Kind = "other"
)

// UnsafeError reports a private runtime directory that cannot be used, with the
// directory, the kind of problem, and the detail in English.
type UnsafeError struct {
	Path   string
	Kind   Kind
	Reason string
}

// Error renders the path and the reason it cannot be used.
func (e *UnsafeError) Error() string {
	return e.Path + ": " + e.Reason
}
