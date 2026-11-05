package manage

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/envx"
	"github.com/Herrscherd/herrscher/core/host"
)

var (
	errUnknownPolicy   = errors.New("unknown route policy")
	errNoGatewayCreds  = errors.New("gateway-only with no gateway credentials: every session would fail to start")
	errIncompleteCreds = errors.New("incomplete gateway credentials")
	errUnknownModel    = errors.New("unknown model")
)

type catalogFn func(contracts.RoutePolicy) ([]host.CatalogEntry, error)

func liveCatalog(models *host.ModelDiscovery) catalogFn {
	return func(p contracts.RoutePolicy) ([]host.CatalogEntry, error) {
		return host.CatalogWith(contracts.Default.Backends(), p, models.Sources())
	}
}

type routeCurrent struct {
	policy   contracts.RoutePolicy
	model    string
	haveCred bool
}

func currentRoute() routeCurrent {
	return routeCurrent{
		policy:   host.ResolvePolicy(envx.Getenv),
		model:    envx.Getenv(host.EnvDefaultModel),
		haveCred: len(host.GatewayEnvPairs()) > 0,
	}
}

func routeStep(sty style, input *bufio.Reader, cat catalogFn, cur routeCurrent, askModel bool) (map[string]string, error) {
	out := make(map[string]string)

	fmt.Fprintf(os.Stderr, "\n  %s  %s\n", sty.wrap(sty.bold+sty.cyan, "routing"), sty.wrap(sty.dim, "where agent turns run"))
	policy, err := choosePolicy(sty, input, cur.policy)
	if err != nil {
		return nil, err
	}
	out[host.EnvRoutePolicy] = string(policy)

	if policy == contracts.PolicyGatewayOnly {
		creds, credsErr := askGatewayCreds(sty, input, cur.haveCred)
		if credsErr != nil {
			return nil, credsErr
		}
		for k, v := range creds {
			out[k] = v
		}
	}

	if !askModel {
		fmt.Fprintf(os.Stderr, "  %s\n", sty.wrap(sty.dim, "default model: run `herrscher init` again after the rebuild to pick one"))
		return out, nil
	}

	entries, err := cat(policy)
	if err != nil {
		return nil, fmt.Errorf("model catalog(): %w", err)
	}
	model, err := chooseDefaultModel(sty, input, entries, cur.model)
	if err != nil {
		return nil, err
	}
	out[host.EnvDefaultModel] = model
	if model == "" && policy == contracts.PolicyGatewayOnly {
		fmt.Fprintf(os.Stderr, "  %s\n", sty.wrap(sty.red, "with no default model, every `session create` must pass --model, which gateway-only requires"))
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

func choosePolicy(sty style, input *bufio.Reader, cur contracts.RoutePolicy) (contracts.RoutePolicy, error) {
	fmt.Fprintf(os.Stderr, "  %s\n", sty.wrap(sty.bold, "route policy"))
	for idx, choice := range policyChoices() {
		name, tag := fmt.Sprintf("%-14s", choice.label), fmt.Sprintf("%-8s", "")
		if choice.policy == cur {
			name = sty.wrap(sty.green, name)
			tag = sty.wrap(sty.dim, fmt.Sprintf("%-8s", "current"))
		}
		fmt.Fprintf(os.Stderr, "    %s %s %s %s\n", sty.wrap(sty.dim, strconv.Itoa(idx+1)), name, tag, sty.wrap(sty.dim, choice.help))
	}

	ans := promptLine(input, "  "+sty.wrap(sty.dim, "›")+" ")
	if ans == "" {
		return cur, nil
	}
	if choice, err := strconv.Atoi(ans); err == nil {
		if choice < 1 || choice > len(policyChoices()) {
			return "", fmt.Errorf("route policy: %w: %d", errChoiceOutOfRange, choice)
		}
		return policyChoices()[choice-1].policy, nil
	}
	for _, choice := range policyChoices() {
		if ans == choice.label {
			return choice.policy, nil
		}
	}
	return "", fmt.Errorf("%w %q", errUnknownPolicy, ans)
}

func askGatewayCreds(sty style, input *bufio.Reader, have bool) (map[string]string, error) {
	note := "both are required"
	if have {
		note = "already set: enter to keep"
	}
	fmt.Fprintf(os.Stderr, "  %s  %s\n", sty.wrap(sty.bold, "gateway · credentials"), sty.wrap(sty.dim, "("+note+"; token entry is hidden)"))

	url := promptLine(input, secretLabel(sty, host.EnvGatewayURL))
	token, err := readSecret(input, secretLabel(sty, host.EnvGatewayToken))
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

func chooseDefaultModel(sty style, input *bufio.Reader, entries []host.CatalogEntry, cur string) (string, error) {
	if len(entries) == 0 {
		fmt.Fprintf(os.Stderr, "  %s\n", sty.wrap(sty.dim, "default model: no compiled backend offers one under this policy"))
		return "", nil
	}
	fmt.Fprintf(os.Stderr, "  %s\n", sty.wrap(sty.bold, "default model"))
	for idx := range entries {
		entry := &entries[idx]
		padded, tag := fmt.Sprintf("%-24s", entry.ID), ""
		if entry.ID == cur {
			padded, tag = sty.wrap(sty.green, padded), sty.wrap(sty.dim, "  current")
		}
		fmt.Fprintf(os.Stderr, "    %s %s %s%s\n", sty.wrap(sty.dim, strconv.Itoa(idx+1)), padded, sty.wrap(sty.dim, entry.Label), tag)
	}
	fmt.Fprintf(os.Stderr, "    %s %s\n", sty.wrap(sty.dim, strconv.Itoa(len(entries)+1)), "none")

	ans := promptLine(input, "  "+sty.wrap(sty.dim, "›")+" ")
	switch ans {
	case "":
		return cur, nil
	case kindNone:
		return "", nil
	}
	if choice, err := strconv.Atoi(ans); err == nil {
		switch {
		case choice >= 1 && choice <= len(entries):
			return entries[choice-1].ID, nil
		case choice == len(entries)+1:
			return "", nil
		}
		return "", fmt.Errorf("default model: %w: %d", errChoiceOutOfRange, choice)
	}
	for idx := range entries {
		if entries[idx].ID == ans {
			return ans, nil
		}
	}
	return "", fmt.Errorf("%w %q (not offered under this policy)", errUnknownModel, ans)
}
