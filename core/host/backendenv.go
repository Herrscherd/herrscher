package host

import (
	"fmt"
	"os"
	"sync"

	"github.com/Herrscherd/herrscher-contracts"
)

// The pair the app hands the daemon, from the Neublox account token. These are
// PRODUCT credentials on a SHARED paid account — they must never reach a
// vendor CLI child, whose agent has a shell and is prompt-injectable.
const (
	EnvGatewayURL   = "NEUBLOX_GATEWAY_URL"
	EnvGatewayToken = "NEUBLOX_GATEWAY_TOKEN"
)

// capturedGateway holds the pair read once at startup, after which the two
// variables are removed from this process's environment.
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
	// Unset unconditionally: a re-entrant call must still scrub a pair that
	// reappeared (an .env reload, a test) rather than leave it for a child.
	_ = unsetenv(EnvGatewayURL)
	_ = unsetenv(EnvGatewayToken)
}

// GatewayEnvPairs returns the captured pair as KEY=VALUE entries, for the
// environment of a TRUSTED child only — `herrscher bridge`, which is this same
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

// gatewayEnvFor translates a credential pair into environment variables,
// according to the protocol the vendor's CLI speaks.
//
// The creds argument is of type GatewayCreds, which cannot exist half-filled:
// this function therefore cannot produce an orphaned base URL. A vendor that
// cannot be redirected returns an empty map — not a partial one.
func gatewayEnvFor(vendor string, creds contracts.GatewayCreds, modelArg string) map[string]string {
	switch vendor {
	case "claude":
		// Anthropic protocol. ANTHROPIC_AUTH_TOKEN takes precedence over the
		// subscription OAuth in the claude CLI's precedence order, which
		// guarantees the session runs on our account, not the user's.
		return map[string]string{
			contracts.EnvAnthropicBaseURL:   creds.BaseURL(),
			contracts.EnvAnthropicAuthToken: creds.Token(),
			// Belt and braces only: an explicit --model flag beats ANTHROPIC_MODEL
			// in the claude CLI's precedence order, and BuildBackendFor always
			// passes the same Arg as the "model" plugin setting, which is the flag.
			// This is the default for a spawn that somehow loses the setting.
			"ANTHROPIC_MODEL": modelArg,
		}
	case "codex":
		// OpenAI protocol. The codex CLI additionally requires a provider
		// declared in TOML with wire_api = "responses"; these variables aren't
		// sufficient alone, but they carry the token that provider references.
		return map[string]string{
			contracts.EnvOpenAIBaseURL: creds.BaseURL(),
			contracts.EnvNeubloxToken:  creds.Token(),
		}
	default:
		// cursor and any future vendor that cannot be redirected.
		return nil
	}
}

// loadGatewayCreds resolves the pair. Once CaptureGatewayCreds has run — the
// production path, at process startup — the variables are gone from the
// environment and the captured value is the only source. getenv stays as a
// seam for tests (and for a process that never captured). A lone half is an
// error, by construction of NewGatewayCreds.
func loadGatewayCreds(getenv func(string) string) (contracts.GatewayCreds, error) {
	capturedGateway.mu.Lock()
	captured, url, token := capturedGateway.captured, capturedGateway.url, capturedGateway.token
	capturedGateway.mu.Unlock()
	if captured {
		return contracts.NewGatewayCreds(url, token)
	}
	return contracts.NewGatewayCreds(getenv(EnvGatewayURL), getenv(EnvGatewayToken))
}

// spawnEnvFor computes the variables to inject for a catalog entry.
//
// Native route: no variables, the daemon's environment passes through
// unchanged — this is the non-regression for the internal build.
//
// Gateway route: credentials are required. In their absence the function
// FAILS, it does not degrade. A bare spawn on a machine where the user has a
// claude.ai login would silently run on their subscription, which Anthropic
// forbids — the silence is precisely the danger.
func spawnEnvFor(e CatalogEntry, getenv func(string) string) (map[string]string, error) {
	if e.Route != contracts.RouteGateway {
		return nil, nil
	}
	creds, err := loadGatewayCreds(getenv)
	if err != nil {
		return nil, fmt.Errorf("model %q needs the Neublox gateway: %w", e.ID, err)
	}
	env := gatewayEnvFor(e.Vendor, creds, e.Arg)
	if len(env) == 0 {
		return nil, fmt.Errorf("model %q is routed through the gateway but backend %q cannot be redirected", e.ID, e.Vendor)
	}
	return env, nil
}
