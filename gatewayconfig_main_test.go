package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/host"
)

func declaredGatewayKeys(t *testing.T) []string {
	t.Helper()
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
	return keys
}

func envKeepsNoneOf(t *testing.T, keys []string) {
	t.Helper()
	for _, kv := range os.Environ() {
		for _, k := range keys {
			if strings.HasPrefix(kv, k+"=") {
				t.Fatalf("%s survived and rides into every vendor CLI a session spawns", k)
			}
		}
	}
}

func TestEveryCompiledInGatewayKeyIsScrubbedFromTheEnvironment(t *testing.T) {
	keys := declaredGatewayKeys(t)
	for _, k := range keys {
		t.Setenv(k, "s3cret")
	}

	host.CaptureGatewayConfig(contracts.Default.Gateways())

	envKeepsNoneOf(t, keys)
}

func TestTheEnvFileServePassesLeavesNoGatewayKeyBehind(t *testing.T) {
	keys := declaredGatewayKeys(t)
	path := filepath.Join(t.TempDir(), "herrscher.env")
	body := ""
	for _, k := range keys {
		body += k + "=s3cret\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	host.CaptureGatewayConfig(contracts.Default.Gateways())

	if err := loadEnvFile(path); err != nil {
		t.Fatal(err)
	}

	envKeepsNoneOf(t, keys)
}
