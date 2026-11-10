// Package runtimedir gives each user one directory for the local sockets and
// locks of herrscher. Resolve picks it deterministically: <run user>/<uid>/herrscher
// when systemd has created a private per-user runtime directory for the uid, and
// /tmp/herrscher-<uid> otherwise, which is where macOS and hosts without a user
// session put it. The directory is created 0700 and verified before use, so no
// other user can pre-create or substitute an entry in it. A socket keeps a
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
	runUserDirName = "herrscher"
	dirPrefix      = "herrscher-"
	plainPrefix    = "herrscher-"
	hashedPrefix   = "hs-"
	instanceLock   = "herrscher-instance-"
	socketSuffix   = ".sock"
	lockSuffix     = ".lock"
	keyBytes       = 24
	hashedNameLen  = len("/") + len(hashedPrefix) + keyBytes + len(socketSuffix)
)

// Roots are the two parents a per-user directory can live under: RunUser holds
// the systemd runtime directory of each uid, Tmp the fallback herrscher-<uid>.
type Roots struct {
	RunUser string
	Tmp     string
}

// ProductionRoots are the parents used by a running herrscher.
func ProductionRoots() Roots {
	return Roots{RunUser: "/run/user", Tmp: "/tmp"}
}

// Layout is the private directory where one user's herrscher entries live.
// The zero value is not usable; a Layout comes from Production, Resolve or At.
type Layout struct {
	dir string
}

// RootError reports a directory or root that cannot host the local entries.
type RootError struct {
	Path   string
	Reason string
}

// Error renders the rejected path and why it was rejected.
func (e *RootError) Error() string {
	return e.Path + ": " + e.Reason
}

// Production is the layout of the user running this process.
func Production() Layout {
	return resolve(os.Getuid(), ProductionRoots())
}

// Resolve is the layout for uid under roots. It rejects a root that is empty or
// relative, and a directory too long to hold a hashed socket name.
func Resolve(uid int, roots Roots) (Layout, error) {
	for _, root := range []string{roots.RunUser, roots.Tmp} {
		if err := checkAbsolute(root); err != nil {
			return Layout{}, err
		}
	}
	return checkedLayout(resolve(uid, roots))
}

// At is the layout whose directory is dir itself, for an explicit location such
// as a test directory or the daemon started with a runtime directory flag.
func At(dir string) (Layout, error) {
	if err := checkAbsolute(dir); err != nil {
		return Layout{}, err
	}
	return checkedLayout(Layout{dir: dir})
}

func resolve(uid int, roots Roots) Layout {
	uidText := strconv.Itoa(uid)
	if usableRunUserDir(path.Join(roots.RunUser, uidText), uid) {
		return Layout{dir: path.Join(roots.RunUser, uidText, runUserDirName)}
	}
	return Layout{dir: DirOf(roots.Tmp, uid)}
}

func checkAbsolute(root string) error {
	switch {
	case root == "":
		return &RootError{Path: root, Reason: "is empty"}
	case !path.IsAbs(root):
		return &RootError{Path: root, Reason: "is not an absolute path"}
	}
	return nil
}

func checkedLayout(layout Layout) (Layout, error) {
	if len(layout.dir)+hashedNameLen > MaxUnixSocketPath {
		return Layout{}, &RootError{Path: layout.dir, Reason: "is too long for a unix socket name"}
	}
	return layout, nil
}

// DirOf is the private directory of the user uid under the temp root.
func DirOf(root string, uid int) string {
	return path.Join(root, dirPrefix+strconv.Itoa(uid))
}

// Dir is the private directory this layout names.
func (l Layout) Dir() string {
	return l.dir
}

// Socket is the unix socket path for stem in the private directory.
func (l Layout) Socket(stem string) string {
	return Name(l.dir, stem)
}

// Lock is the instance lock path for instanceID in the private directory.
func (l Layout) Lock(instanceID string) string {
	return path.Join(l.dir, instanceLock+Key(instanceID)+lockSuffix)
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

// Check verifies the private directory when p lives in it, and accepts any
// other path.
func (l Layout) Check(p string) error {
	if path.Dir(p) != l.dir {
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
