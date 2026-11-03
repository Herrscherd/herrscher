package manage

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/host"
)

var (
	errUnknownPolicy   = errors.New("unknown route policy")
	errNoGatewayCreds  = errors.New("gateway-only with no gateway credentials: every session would fail to start")
	errIncompleteCreds = errors.New("incomplete gateway credentials")
	errUnknownModel    = errors.New("unknown model")
)

type catalogFn func(contracts.RoutePolicy) ([]host.CatalogEntry, error)

func liveCatalog(p contracts.RoutePolicy) ([]host.CatalogEntry, error) {
	return host.Catalog(contracts.Default.Backends(), p)
}

type routeCurrent struct {
	policy   contracts.RoutePolicy
	model    string
	haveCred bool
}

func currentRoute() routeCurrent {
	return routeCurrent{
		policy:   host.ResolvePolicy(os.Getenv),
		model:    os.Getenv(host.EnvDefaultModel),
		haveCred: len(host.GatewayEnvPairs()) > 0,
	}
}

func routeStep(s style, in *bufio.Reader, cat catalogFn, cur routeCurrent, askModel bool) (map[string]string, error) {
	out := make(map[string]string)

	_, _ = fmt.Fprintf(os.Stderr, "\n  %s  %s\n", s.wrap(s.bold+s.cyan, "routing"), s.wrap(s.dim, "where agent turns run"))
	policy, err := choosePolicy(s, in, cur.policy)
	if err != nil {
		return nil, err
	}
	out[host.EnvRoutePolicy] = string(policy)

	if policy == contracts.PolicyGatewayOnly {
		creds, credsErr := askGatewayCreds(s, in, cur.haveCred)
		if credsErr != nil {
			return nil, credsErr
		}
		for k, v := range creds {
			out[k] = v
		}
	}

	if !askModel {
		_, _ = fmt.Fprintf(os.Stderr, "  %s\n", s.wrap(s.dim, "default model: run `herrscher init` again after the rebuild to pick one"))
		return out, nil
	}

	entries, err := cat(policy)
	if err != nil {
		return nil, fmt.Errorf("model catalog(): %w", err)
	}
	model, err := chooseDefaultModel(s, in, entries, cur.model)
	if err != nil {
		return nil, err
	}
	out[host.EnvDefaultModel] = model
	if model == "" && policy == contracts.PolicyGatewayOnly {
		_, _ = fmt.Fprintf(os.Stderr, "  %s\n", s.wrap(s.red, "with no default model, every `session create` must pass --model, which gateway-only requires"))
	}
	return out, nil
}

func policyChoices() []struct {
	policy contracts.RoutePolicy
	label  string
	help   string
} {
	return []struct {
		policy contracts.RoutePolicy
		label  string
		help   string
	}{
		{contracts.PolicyAll, "all", "native and gateway models (internal build)"},
		{contracts.PolicyGatewayOnly, "gateway-only", "the product gateway only (public build)"},
	}
}

func choosePolicy(s style, in *bufio.Reader, cur contracts.RoutePolicy) (contracts.RoutePolicy, error) {
	_, _ = fmt.Fprintf(os.Stderr, "  %s\n", s.wrap(s.bold, "route policy"))
	for i, c := range policyChoices() {
		name, tag := fmt.Sprintf("%-14s", c.label), fmt.Sprintf("%-8s", "")
		if c.policy == cur {
			name = s.wrap(s.green, name)
			tag = s.wrap(s.dim, fmt.Sprintf("%-8s", "current"))
		}
		_, _ = fmt.Fprintf(os.Stderr, "    %s %s %s %s\n", s.wrap(s.dim, strconv.Itoa(i+1)), name, tag, s.wrap(s.dim, c.help))
	}

	ans := promptLine(in, "  "+s.wrap(s.dim, "›")+" ")
	if ans == "" {
		return cur, nil
	}
	if n, err := strconv.Atoi(ans); err == nil {
		if n < 1 || n > len(policyChoices()) {
			return "", fmt.Errorf("route policy: %w: %d", errChoiceOutOfRange, n)
		}
		return policyChoices()[n-1].policy, nil
	}
	for _, c := range policyChoices() {
		if ans == c.label {
			return c.policy, nil
		}
	}
	return "", fmt.Errorf("%w %q", errUnknownPolicy, ans)
}

func askGatewayCreds(s style, in *bufio.Reader, have bool) (map[string]string, error) {
	note := "both are required"
	if have {
		note = "already set: enter to keep"
	}
	_, _ = fmt.Fprintf(os.Stderr, "  %s  %s\n", s.wrap(s.bold, "gateway · credentials"), s.wrap(s.dim, "("+note+"; token entry is hidden)"))

	url := promptLine(in, secretLabel(s, host.EnvGatewayURL))
	token, err := readSecret(in, secretLabel(s, host.EnvGatewayToken))
	if err != nil {
		return nil, err
	}

	switch {
	case url == "" && token == "":
		if !have {
			return nil, fmt.Errorf("%w: set %s and %s", errNoGatewayCreds, host.EnvGatewayURL, host.EnvGatewayToken)
		}
		return make(map[string]string), nil
	case url == "" || token == "":
		return nil, fmt.Errorf("%w: %s and %s go together; one without the other would run turns on this machine's own account", errIncompleteCreds, host.EnvGatewayURL, host.EnvGatewayToken)
	}
	return map[string]string{host.EnvGatewayURL: url, host.EnvGatewayToken: token}, nil
}

func chooseDefaultModel(s style, in *bufio.Reader, entries []host.CatalogEntry, cur string) (string, error) {
	if len(entries) == 0 {
		_, _ = fmt.Fprintf(os.Stderr, "  %s\n", s.wrap(s.dim, "default model: no compiled backend offers one under this policy"))
		return "", nil
	}
	_, _ = fmt.Fprintf(os.Stderr, "  %s\n", s.wrap(s.bold, "default model"))
	for i, e := range entries {
		id, tag := fmt.Sprintf("%-24s", e.ID), ""
		if e.ID == cur {
			id, tag = s.wrap(s.green, id), s.wrap(s.dim, "  current")
		}
		_, _ = fmt.Fprintf(os.Stderr, "    %s %s %s%s\n", s.wrap(s.dim, strconv.Itoa(i+1)), id, s.wrap(s.dim, e.Label), tag)
	}
	_, _ = fmt.Fprintf(os.Stderr, "    %s %s\n", s.wrap(s.dim, strconv.Itoa(len(entries)+1)), "none")

	ans := promptLine(in, "  "+s.wrap(s.dim, "›")+" ")
	switch ans {
	case "":
		return cur, nil
	case kindNone:
		return "", nil
	}
	if n, err := strconv.Atoi(ans); err == nil {
		switch {
		case n >= 1 && n <= len(entries):
			return entries[n-1].ID, nil
		case n == len(entries)+1:
			return "", nil
		}
		return "", fmt.Errorf("default model: %w: %d", errChoiceOutOfRange, n)
	}
	for _, e := range entries {
		if e.ID == ans {
			return ans, nil
		}
	}
	return "", fmt.Errorf("%w %q (not offered under this policy)", errUnknownModel, ans)
}
