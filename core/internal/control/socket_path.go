package control

import (
	"crypto/sha256"
	"encoding/hex"
	"path"
)

const MaxUnixSocketPath = 103

const socketSuffix = ".sock"

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
		return path.Join(dir, "hs-"+digest[:24]+socketSuffix)
	}
	head := stem
	if len(head) > room {
		head = head[:room]
	}
	return path.Join(dir, head+tail)
}
