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
		plugin := plugins[idx]
		if plugin.Gateway == nil {
			continue
		}
		for _, field := range plugin.Manifest.Config {
			if field.Env != "" {
				keys = append(keys, field.Env)
			}
		}
	}
	return keys
}
