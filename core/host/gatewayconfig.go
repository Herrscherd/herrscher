package host

import (
	"os"
	"sync"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

var capturedConfig struct {
	mu   sync.Mutex
	vals map[string]string
}

func CaptureGatewayConfig(plugins []contracts.Plugin) {
	captureGatewayConfig(plugins, os.Getenv, os.Unsetenv)
}

func captureGatewayConfig(plugins []contracts.Plugin, getenv func(string) string, unsetenv func(string) error) {
	capturedConfig.mu.Lock()
	defer capturedConfig.mu.Unlock()
	if capturedConfig.vals == nil {
		capturedConfig.vals = map[string]string{}
	}
	for _, p := range plugins {
		if p.Gateway == nil {
			continue
		}
		for _, s := range p.Manifest.Config {
			if s.Env == "" {
				continue
			}
			if capturedConfig.vals[s.Env] == "" {
				capturedConfig.vals[s.Env] = getenv(s.Env)
			}
			_ = unsetenv(s.Env)
		}
	}
}

func gatewayConfigGetenv(fallback func(string) string) func(string) string {
	return func(name string) string {
		capturedConfig.mu.Lock()
		v := capturedConfig.vals[name]
		capturedConfig.mu.Unlock()
		if v != "" {
			return v
		}
		return fallback(name)
	}
}
