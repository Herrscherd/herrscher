package host

import (
	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/envx"
)

func CaptureGatewayConfig(plugins []contracts.Plugin) (reveal func()) {
	return envx.Hide(gatewayConfigKeys(plugins))
}

func gatewayConfigKeys(plugins []contracts.Plugin) []string {
	var keys []string
	for _, p := range plugins {
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
