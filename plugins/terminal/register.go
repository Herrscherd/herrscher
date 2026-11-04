package terminal

import contracts "github.com/Herrscherd/herrscher-contracts"

func init() {
	contracts.Register(contracts.Plugin{
		Manifest: contracts.Manifest{
			Kind:         Kind,
			Category:     contracts.CategoryGateway,
			Capabilities: contracts.Capabilities{Replies: true, SelectMenus: true},
			Config:       settings(),
		},
		Gateway: newGatewaySet,
	})
}
