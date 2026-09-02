package main

import (
	"os"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/host"
)

func TestEveryCompiledInGatewayKeyIsScrubbedFromTheEnvironment(t *testing.T) {
	var keys []string
	for _, p := range contracts.Default.Gateways() {
		for _, s := range p.Manifest.Config {
			if s.Env != "" {
				keys = append(keys, s.Env)
			}
		}
	}
	if len(keys) == 0 {
		t.Skip("no compiled-in gateway declares an env-bound setting")
	}
	for _, k := range keys {
		t.Setenv(k, "s3cret")
	}

	host.CaptureGatewayConfig(contracts.Default.Gateways())

	for _, kv := range os.Environ() {
		for _, k := range keys {
			if strings.HasPrefix(kv, k+"=") {
				t.Fatalf("%s survived the capture and rides into every vendor CLI a session spawns", k)
			}
		}
	}
}
