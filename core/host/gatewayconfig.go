package host

import (
	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/envx"
)

// CaptureGatewayConfig hides the gateway configuration keys of plugins from the environment and returns the function that restores them.
func CaptureGatewayConfig(plugins []contracts.Plugin) func() {
	return envx.Hide(gatewayConfigKeys(plugins))
}

func gatewayConfigKeys(plugins []contracts.Plugin) []string {
	var keys []string
	for idx := range plugins {
		p := plugins[idx]
		if p.Gateway == nil {
			continue
		}
		for _, s := range p.Manifest.Config {
			if s.Env != "" {
				keys = append(keys, s.Env)
			}
		}
	}
	return keys
}
