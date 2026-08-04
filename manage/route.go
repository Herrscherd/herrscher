package manage

import (
	"bufio"
	"fmt"
	"os"
	"strconv"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/host"
)

// catalogFn resolves the models offered under a policy. Injected so the wizard
// can be driven in a test without a compiled-in backend.
type catalogFn func(contracts.RoutePolicy) ([]host.CatalogEntry, error)

// liveCatalog reads the models declared by the backends compiled into THIS
// binary. In config-only mode that is exactly the set the daemon will serve. In
// compose mode it is the set of the stack being REPLACED, which is why the
// caller skips the model question there rather than offering a list that the
// rebuild may invalidate.
func liveCatalog(p contracts.RoutePolicy) ([]host.CatalogEntry, error) {
	return host.Catalog(contracts.Default.Backends(), p)
}

// routeCurrent is what is already configured, so every question can offer the
// operator's own value as its default. The gateway pair is a bool, never the
// value: a configured token is acknowledged, never re-displayed.
type routeCurrent struct {
	policy   contracts.RoutePolicy
	model    string
	haveCred bool
}

// currentRoute reads what this process was started with. The gateway pair has
// already been captured-and-unset by then (see host.CaptureGatewayCreds), so it
// is read back from the capture rather than from the environment.
func currentRoute() routeCurrent {
	return routeCurrent{
		policy:   host.ResolvePolicy(os.Getenv),
		model:    os.Getenv(host.EnvDefaultModel),
		haveCred: len(host.GatewayEnvPairs()) > 0,
	}
}

// routeStep asks where agent turns run: the route policy, the gateway
// credentials it requires, and the model a session gets when it names none. It
// returns the env keys to persist. The gateway pair is absent from that map when
// the operator kept an already-configured one, so it is never rewritten.
//
// askModel is false in compose mode, where the plugin stack is about to be
// rewritten and rebuilt: the models this binary can enumerate belong to the old
// stack, so offering them would invite a choice that the rebuild invalidates.
func routeStep(s style, in *bufio.Reader, cat catalogFn, cur routeCurrent, askModel bool) (map[string]string, error) {
	out := map[string]string{}

	fmt.Fprintf(os.Stderr, "\n  %s  %s\n", s.wrap(s.bold+s.cyan, "routing"), s.wrap(s.dim, "where agent turns run"))
	policy, err := choosePolicy(s, in, cur.policy)
	if err != nil {
		return nil, err
	}
	out[host.EnvRoutePolicy] = string(policy)

	if policy == contracts.PolicyGatewayOnly {
		creds, err := askGatewayCreds(s, in, cur.haveCred)
		if err != nil {
			return nil, err
		}
		for k, v := range creds {
			out[k] = v
		}
	}

	if !askModel {
		fmt.Fprintf(os.Stderr, "  %s\n", s.wrap(s.dim, "default model: run `herrscher init` again after the rebuild to pick one"))
		return out, nil
	}

	entries, err := cat(policy)
	if err != nil {
		return nil, fmt.Errorf("model catalog: %w", err)
	}
	model, err := chooseDefaultModel(s, in, entries, cur.model)
	if err != nil {
		return nil, err
	}
	// An empty value is written on purpose rather than skipped: it is how an
	// operator CLEARS a default they previously set. writeSecretsTo upserts by
	// key, so an absent key would silently keep the old one.
	out[host.EnvDefaultModel] = model
	if model == "" && policy == contracts.PolicyGatewayOnly {
		fmt.Fprintf(os.Stderr, "  %s\n", s.wrap(s.red, "with no default model, every `session create` must pass --model, which gateway-only requires"))
	}
	return out, nil
}

// policyChoices are the two builds, in menu order: the internal one that may
// use this machine's own vendor logins, and the public one that may not.
var policyChoices = []struct {
	policy contracts.RoutePolicy
	label  string
	help   string
}{
	{contracts.PolicyAll, "all", "native and gateway models (internal build)"},
	{contracts.PolicyGatewayOnly, "gateway-only", "the product gateway only (public build)"},
}

// choosePolicy reads the route policy: empty keeps the current one, otherwise a
// 1-based index or the policy name itself.
func choosePolicy(s style, in *bufio.Reader, cur contracts.RoutePolicy) (contracts.RoutePolicy, error) {
	fmt.Fprintf(os.Stderr, "  %s\n", s.wrap(s.bold, "route policy"))
	for i, c := range policyChoices {
		// Pad the plain label, then colour it: an ANSI wrap adds invisible bytes
		// that %-14s would count, shifting every following column.
		name, tag := fmt.Sprintf("%-14s", c.label), fmt.Sprintf("%-8s", "")
		if c.policy == cur {
			name = s.wrap(s.green, name)
			tag = s.wrap(s.dim, fmt.Sprintf("%-8s", "current"))
		}
		fmt.Fprintf(os.Stderr, "    %s %s %s %s\n", s.wrap(s.dim, strconv.Itoa(i+1)), name, tag, s.wrap(s.dim, c.help))
	}

	ans := promptLine(in, "  "+s.wrap(s.dim, "›")+" ")
	if ans == "" {
		return cur, nil
	}
	if n, err := strconv.Atoi(ans); err == nil {
		if n < 1 || n > len(policyChoices) {
			return "", fmt.Errorf("route policy: choice out of range: %d", n)
		}
		return policyChoices[n-1].policy, nil
	}
	for _, c := range policyChoices {
		if ans == c.label {
			return c.policy, nil
		}
	}
	return "", fmt.Errorf("unknown route policy %q", ans)
}

// askGatewayCreds reads the gateway URL and token. Both or neither: a base URL
// without a token would point a vendor CLI at the gateway while it keeps
// authenticating as the machine's own logged-in account, which is the one shape
// the gateway-only policy exists to forbid. When a complete pair is already
// configured, empty answers keep it.
func askGatewayCreds(s style, in *bufio.Reader, have bool) (map[string]string, error) {
	note := "both are required"
	if have {
		note = "already set — enter to keep"
	}
	fmt.Fprintf(os.Stderr, "  %s  %s\n", s.wrap(s.bold, "gateway · credentials"), s.wrap(s.dim, "("+note+"; token entry is hidden)"))

	url := promptLine(in, secretLabel(s, host.EnvGatewayURL))
	token, err := readSecret(in, secretLabel(s, host.EnvGatewayToken))
	if err != nil {
		return nil, err
	}

	switch {
	case url == "" && token == "":
		if !have {
			return nil, fmt.Errorf("gateway-only with no gateway credentials: every session would fail to start — set %s and %s", host.EnvGatewayURL, host.EnvGatewayToken)
		}
		return nil, nil
	case url == "" || token == "":
		return nil, fmt.Errorf("incomplete gateway credentials: %s and %s go together — one without the other would run turns on this machine's own account", host.EnvGatewayURL, host.EnvGatewayToken)
	}
	return map[string]string{host.EnvGatewayURL: url, host.EnvGatewayToken: token}, nil
}

// chooseDefaultModel offers the models the policy allows and returns the one a
// session gets when it names none. The last entry clears the default.
func chooseDefaultModel(s style, in *bufio.Reader, entries []host.CatalogEntry, cur string) (string, error) {
	if len(entries) == 0 {
		fmt.Fprintf(os.Stderr, "  %s\n", s.wrap(s.dim, "default model: no compiled backend offers one under this policy"))
		return "", nil
	}
	fmt.Fprintf(os.Stderr, "  %s\n", s.wrap(s.bold, "default model"))
	for i, e := range entries {
		// Colour the id alone: wrapping the id AND the already-dimmed label in one
		// green span would end at the label's own reset, colouring half a row.
		id, tag := fmt.Sprintf("%-24s", e.ID), ""
		if e.ID == cur {
			id, tag = s.wrap(s.green, id), s.wrap(s.dim, "  current")
		}
		fmt.Fprintf(os.Stderr, "    %s %s %s%s\n", s.wrap(s.dim, strconv.Itoa(i+1)), id, s.wrap(s.dim, e.Label), tag)
	}
	fmt.Fprintf(os.Stderr, "    %s %s\n", s.wrap(s.dim, strconv.Itoa(len(entries)+1)), "none")

	ans := promptLine(in, "  "+s.wrap(s.dim, "›")+" ")
	switch {
	case ans == "":
		return cur, nil
	case ans == "none":
		return "", nil
	}
	if n, err := strconv.Atoi(ans); err == nil {
		switch {
		case n >= 1 && n <= len(entries):
			return entries[n-1].ID, nil
		case n == len(entries)+1:
			return "", nil
		}
		return "", fmt.Errorf("default model: choice out of range: %d", n)
	}
	for _, e := range entries {
		if e.ID == ans {
			return ans, nil
		}
	}
	return "", fmt.Errorf("unknown model %q (not offered under this policy)", ans)
}
