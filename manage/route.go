package manage

import (
	"bufio"
	"fmt"
	"os"
	"strconv"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/envx"
	"github.com/Herrscherd/herrscher/core/host"
)

var (
	errUnknownPolicy   = clitext.SentinelError("unknown route policy")
	errNoGatewayCreds  = clitext.SentinelError("gateway-only with no gateway credentials: every session would fail to start")
	errIncompleteCreds = clitext.SentinelError("incomplete gateway credentials: the URL and the token go together")
	errUnknownModel    = clitext.SentinelError("unknown model (not offered under this policy)")
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

func routeStep(messages *clitext.Messages, sty style, input *bufio.Reader, cat catalogFn, cur routeCurrent, askModel bool) (map[string]string, error) {
	out := make(map[string]string)

	fmt.Fprintf(os.Stderr, "\n  %s  %s\n", sty.wrap(sty.bold+sty.cyan, msg(messages, clitext.ManageRoutingHeader, nil)), sty.wrap(sty.dim, msg(messages, clitext.ManageRoutingNote, nil)))
	policy, err := choosePolicy(messages, sty, input, cur.policy)
	if err != nil {
		return nil, err
	}
	out[host.EnvRoutePolicy] = string(policy)

	if policy == contracts.PolicyGatewayOnly {
		creds, credsErr := askGatewayCreds(messages, sty, input, cur.haveCred)
		if credsErr != nil {
			return nil, credsErr
		}
		for k, v := range creds {
			out[k] = v
		}
	}

	if !askModel {
		fmt.Fprintf(os.Stderr, "  %s\n", sty.wrap(sty.dim, msg(messages, clitext.ManageDefaultModelLater, nil)))
		return out, nil
	}

	entries, err := cat(policy)
	if err != nil {
		return nil, failure(messages, err, clitext.ManageCatalogFailed, clitext.Params{clitext.ParamErr: err.Error()})
	}
	model, err := chooseDefaultModel(messages, sty, input, entries, cur.model)
	if err != nil {
		return nil, err
	}
	out[host.EnvDefaultModel] = model
	if model == "" && policy == contracts.PolicyGatewayOnly {
		fmt.Fprintf(os.Stderr, "  %s\n", sty.wrap(sty.red, msg(messages, clitext.ManageGatewayNoModel, nil)))
	}
	return out, nil
}

func policyChoices(messages *clitext.Messages) []struct {
	policy contracts.RoutePolicy
	label  string
	help   string
} {
	return []struct {
		policy contracts.RoutePolicy
		label  string
		help   string
	}{
		{contracts.PolicyAll, "all", msg(messages, clitext.ManagePolicyAllHelp, nil)},
		{contracts.PolicyGatewayOnly, "gateway-only", msg(messages, clitext.ManagePolicyGatewayHelp, nil)},
	}
}

func choosePolicy(messages *clitext.Messages, sty style, input *bufio.Reader, cur contracts.RoutePolicy) (contracts.RoutePolicy, error) {
	fmt.Fprintf(os.Stderr, "  %s\n", sty.wrap(sty.bold, msg(messages, clitext.ManageRoutePolicyHeader, nil)))
	translator := messages.For(clitext.ClientLanguage())
	for idx, choice := range policyChoices(messages) {
		name, tag := fmt.Sprintf("%-14s", choice.label), fmt.Sprintf("%-8s", "")
		if choice.policy == cur {
			name = sty.wrap(sty.green, name)
			tag = sty.wrap(sty.dim, fmt.Sprintf("%-8s", translator.Text(clitext.ManageCurrentTag, nil)))
		}
		fmt.Fprintf(os.Stderr, "    %s %s %s %s\n", sty.wrap(sty.dim, strconv.Itoa(idx+1)), name, tag, sty.wrap(sty.dim, choice.help))
	}
	logRenderErr(translator)

	ans := promptLine(input, "  "+sty.wrap(sty.dim, "›")+" ")
	if ans == "" {
		return cur, nil
	}
	if choice, err := strconv.Atoi(ans); err == nil {
		if choice < 1 || choice > len(policyChoices(messages)) {
			return "", failure(messages, errChoiceOutOfRange, clitext.ManageChoiceOutOfRange, clitext.Params{clitext.ParamName: msg(messages, clitext.ManageRoutePolicyHeader, nil), clitext.ParamChoice: choice})
		}
		return policyChoices(messages)[choice-1].policy, nil
	}
	for _, choice := range policyChoices(messages) {
		if ans == choice.label {
			return choice.policy, nil
		}
	}
	return "", failure(messages, errUnknownPolicy, clitext.ManageUnknownPolicy, clitext.Params{clitext.ParamPolicy: strconv.Quote(ans)})
}

func askGatewayCreds(messages *clitext.Messages, sty style, input *bufio.Reader, have bool) (map[string]string, error) {
	note := msg(messages, clitext.ManageGatewayPairRequired, nil)
	if have {
		note = msg(messages, clitext.ManageGatewayPairKeep, nil)
	}
	fmt.Fprintf(os.Stderr, "  %s  %s\n", sty.wrap(sty.bold, msg(messages, clitext.ManageGatewayPairHeader, nil)), sty.wrap(sty.dim, note))

	url := promptLine(input, secretLabel(sty, host.EnvGatewayURL))
	token, err := readSecret(input, secretLabel(sty, host.EnvGatewayToken))
	if err != nil {
		return nil, err
	}

	switch {
	case url == "" && token == "":
		if !have {
			return nil, failure(messages, errNoGatewayCreds, clitext.ManageGatewayPairMissing, clitext.Params{clitext.ParamURL: host.EnvGatewayURL, clitext.ParamToken: host.EnvGatewayToken})
		}
		return make(map[string]string), nil
	case url == "" || token == "":
		return nil, failure(messages, errIncompleteCreds, clitext.ManageGatewayPairPartial, clitext.Params{clitext.ParamURL: host.EnvGatewayURL, clitext.ParamToken: host.EnvGatewayToken})
	}
	return map[string]string{host.EnvGatewayURL: url, host.EnvGatewayToken: token}, nil
}

func chooseDefaultModel(messages *clitext.Messages, sty style, input *bufio.Reader, entries []host.CatalogEntry, cur string) (string, error) {
	if len(entries) == 0 {
		fmt.Fprintf(os.Stderr, "  %s\n", sty.wrap(sty.dim, msg(messages, clitext.ManageNoModelOffered, nil)))
		return "", nil
	}
	fmt.Fprintf(os.Stderr, "  %s\n", sty.wrap(sty.bold, msg(messages, clitext.ManageDefaultModelHeader, nil)))
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
		return "", failure(messages, errChoiceOutOfRange, clitext.ManageChoiceOutOfRange, clitext.Params{clitext.ParamName: msg(messages, clitext.ManageDefaultModelHeader, nil), clitext.ParamChoice: choice})
	}
	for idx := range entries {
		if entries[idx].ID == ans {
			return ans, nil
		}
	}
	return "", failure(messages, errUnknownModel, clitext.ManageUnknownModel, clitext.Params{clitext.ParamModel: strconv.Quote(ans)})
}
