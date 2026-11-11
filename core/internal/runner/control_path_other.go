//go:build !unix

package runner

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"

	"github.com/Herrscherd/herrscher/core/internal/runtimedir"
)

// ControlPathFor names the multiplexing socket for a target under the system temp
// directory. Multiplexing is kept on this platform as it was before the private
// runtime directory existed, so the layout is not used here.
func ControlPathFor(_ runtimedir.Layout, target string) (string, error) {
	sum := sha256.Sum256([]byte(target))
	return filepath.Join(os.TempDir(), "hs-ssh-"+hex.EncodeToString(sum[:])[:12]+".sock"), nil
}
