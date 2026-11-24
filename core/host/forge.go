package host

import (
	"context"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/forgecreds"
	"github.com/Herrscherd/herrscher/core/forgeport"
)

// GitHubOpener returns the function that opens the GitHub forge of a host with
// the given credentials, so one credential store serves every forge operation of
// the process.
func GitHubOpener(credentials *forgecreds.Credentials) func(ctx context.Context, forgeHost contracts.ForgeHost) (contracts.Forge, error) {
	return func(ctx context.Context, forgeHost contracts.ForgeHost) (contracts.Forge, error) {
		return forgeport.Open(ctx, forgeport.KindGitHub, forgeHost, credentials)
	}
}
