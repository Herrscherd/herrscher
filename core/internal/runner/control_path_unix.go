//go:build unix

package runner

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/Herrscherd/herrscher/core/internal/runtimedir"
)

// ControlPathFor names the multiplexing socket for a target, in the user's
// private runtime directory, which it creates first so ssh can bind there. It is
// named by a digest of the target because the target text can be longer than a
// unix socket path allows. An error means ssh must run without multiplexing.
func ControlPathFor(target string) (string, error) {
	return controlPathIn(runtimedir.Production(), target)
}

func controlPathIn(layout runtimedir.Layout, target string) (string, error) {
	if _, err := layout.Private(); err != nil {
		return "", fmt.Errorf("ssh control socket: %w", err)
	}
	return layout.Socket("herrscher-ssh-" + targetDigest(target)), nil
}

func targetDigest(target string) string {
	sum := sha256.Sum256([]byte(target))
	return hex.EncodeToString(sum[:])[:12]
}
