// Package forgeport opens the forge plugins registered in the contracts
// registry, so that the host and the terminal UI reach a forge through the
// contracts port and never through its command line.
package forgeport

import (
	"context"
	"errors"
	"fmt"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// KindGitHub is the kind of the GitHub forge plugin.
const KindGitHub = "github"

// GitHubHost is the host of github.com, the only forge host whose token is sent.
const GitHubHost = "github.com"

// ErrNoPlugin matches the error Open returns when no forge plugin of the
// requested kind is registered in the build.
var ErrNoPlugin = errors.New("no forge plugin of this kind is built in")

// Open builds the live forge of kind for host, with creds as the port that
// supplies its token. It returns an error matching [ErrNoPlugin] when no forge
// plugin of kind is registered.
func Open(ctx context.Context, kind string, host contracts.ForgeHost, creds contracts.ForgeCredentials) (contracts.Forge, error) {
	plugins := contracts.Default.Forges()
	for index := range plugins {
		if plugins[index].Manifest.Kind == kind && plugins[index].Forge != nil {
			return plugins[index].Forge(ctx, contracts.PluginConfig{}, host, creds)
		}
	}
	return nil, fmt.Errorf("%w: %q", ErrNoPlugin, kind)
}
