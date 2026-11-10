package control

import (
	"crypto/sha256"
	"encoding/hex"
	"path"

	"github.com/Herrscherd/herrscher/core/internal/runtimedir"
)

// MaxUnixSocketPath is runtimedir.MaxUnixSocketPath, kept for the callers that
// bound their own socket names.
const MaxUnixSocketPath = runtimedir.MaxUnixSocketPath

const socketSuffix = ".sock"

// BoundedSocketPath joins dir and stem into a unix socket path of at most
// MaxUnixSocketPath bytes. When the plain path is too long, the stem is cut and
// suffixed with a digest of the full stem, so distinct stems stay distinct. A
// directory too long to leave room falls back to remoteSocketDir.
func BoundedSocketPath(dir, stem string) string {
	dir = path.Clean(dir)
	full := path.Join(dir, stem+socketSuffix)
	if len(full) <= MaxUnixSocketPath {
		return full
	}
	sum := sha256.Sum256([]byte(stem))
	digest := hex.EncodeToString(sum[:])
	tail := "-" + digest[:12] + socketSuffix
	room := MaxUnixSocketPath - len(dir) - len("/") - len(tail)
	if room <= 0 {
		return path.Join(remoteSocketDir, "hs-"+digest[:24]+socketSuffix)
	}
	head := stem
	if len(head) > room {
		head = head[:room]
	}
	return path.Join(dir, head+tail)
}
