package host

import (
	"fmt"
	"os"
	"sync"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// The pair the app hands the daemon, from the Neublox account token. These are
// PRODUCT credentials on a SHARED paid account. They must never reach a
// vendor CLI child, whose agent has a shell and is prompt-injectable.
const (
	EnvGatewayURL   = "NEUBLOX_GATEWAY_URL"
	EnvGatewayToken = "NEUBLOX_GATEWAY_TOKEN"
)

var capturedGateway struct {
	mu       sync.Mutex
	url      string
	token    string
	captured bool
}

// CaptureGatewayCreds reads the gateway pair from the process environment,
// stores it in-process, and UNSETS both variables.
//
// Without this the pair propagates daemon → bridge → vendor CLI unconditionally
// on EVERY route, because the backends spawn with MergeEnv(os.Environ(), env).
// A coding agent inside any session could then read the product's shared paid
// credential out of its own environment, from a session never routed to the
// gateway at all.
//
// Idempotent: a second call keeps the first capture, so a process that captures
// at more than one entry point (main + runBridge) does not lose the value.
func CaptureGatewayCreds() {
	captureGatewayCreds(os.Getenv, os.Unsetenv)
}

func captureGatewayCreds(getenv func(string) string, unsetenv func(string) error) {
	capturedGateway.mu.Lock()
	defer capturedGateway.mu.Unlock()
	if !capturedGateway.captured {
		capturedGateway.url, capturedGateway.token = getenv(EnvGatewayURL), getenv(EnvGatewayToken)
		capturedGateway.captured = true
	}
	logCleanup(unsetenv(EnvGatewayURL))
	logCleanup(unsetenv(EnvGatewayToken))
}

// GatewayEnvPairs returns the captured pair as KEY=VALUE entries, for the
// environment of a TRUSTED child only: `herrscher bridge`, which is this same
// binary and captures-and-unsets at its own startup before building a backend.
// Never for a vendor CLI, and never on argv: /proc/<pid>/cmdline is world
// readable. Empty when nothing was captured.
func GatewayEnvPairs() []string {
	capturedGateway.mu.Lock()
	defer capturedGateway.mu.Unlock()
	if capturedGateway.url == "" || capturedGateway.token == "" {
		return nil
	}
	return []string{
		EnvGatewayURL + "=" + capturedGateway.url,
		EnvGatewayToken + "=" + capturedGateway.token,
	}
}

func gatewayEnvFor(vendor string, creds contracts.GatewayCreds, modelArg string) map[string]string {
	switch vendor {
	case "claude":
		return map[string]string{
			contracts.EnvAnthropicBaseURL: creds.BaseURL(),
			contracts.EnvAnthropicAPIKey:  creds.Token(),
			"ANTHROPIC_MODEL":             modelArg,
		}
	case "codex":
		return map[string]string{
			contracts.EnvOpenAIBaseURL: creds.BaseURL(),
			contracts.EnvNeubloxToken:  creds.Token(),
		}
	default:
		return nil
	}
}

func loadGatewayCreds(getenv func(string) string) (contracts.GatewayCreds, error) {
	capturedGateway.mu.Lock()
	captured, url, token := capturedGateway.captured, capturedGateway.url, capturedGateway.token
	capturedGateway.mu.Unlock()
	if captured {
		return contracts.NewGatewayCreds(url, token)
	}
	return contracts.NewGatewayCreds(getenv(EnvGatewayURL), getenv(EnvGatewayToken))
}

func spawnEnvFor(e CatalogEntry, getenv func(string) string) (map[string]string, error) {
	if e.Route != contracts.RouteGateway {
		return map[string]string{}, nil
	}
	creds, err := loadGatewayCreds(getenv)
	if err != nil {
		return nil, fmt.Errorf("model %q needs the Neublox gateway: %w", e.ID, err)
	}
	env := gatewayEnvFor(e.Vendor, creds, e.Arg)
	if len(env) == 0 {
		return nil, &modelNotRedirectableError{Model: e.ID, Backend: e.Vendor}
	}
	return env, nil
}
