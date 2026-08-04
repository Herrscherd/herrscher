package host

import (
	"fmt"

	"github.com/Herrscherd/herrscher-contracts"
)

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
			"ANTHROPIC_MODEL":               modelArg,
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

// loadGatewayCreds reads the pair from the daemon's environment. The app sets
// it when launching herrscher, from the Neublox account token. A lone half is
// an error, by construction of NewGatewayCreds.
func loadGatewayCreds(getenv func(string) string) (contracts.GatewayCreds, error) {
	return contracts.NewGatewayCreds(getenv("NEUBLOX_GATEWAY_URL"), getenv("NEUBLOX_GATEWAY_TOKEN"))
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
