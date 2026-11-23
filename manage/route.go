package manage

import (
	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/host"
)

var (
	errNoGatewayCreds  = clitext.SentinelError("gateway-only with no gateway credentials: every session would fail to start")
	errIncompleteCreds = clitext.SentinelError("incomplete gateway credentials: the URL and the token go together")
)

type catalogFn func(contracts.RoutePolicy) ([]host.CatalogEntry, error)

func liveCatalog(models *host.ModelDiscovery) catalogFn {
	return func(p contracts.RoutePolicy) ([]host.CatalogEntry, error) {
		return host.CatalogWith(contracts.Default.Backends(), p, models.Sources())
	}
}
