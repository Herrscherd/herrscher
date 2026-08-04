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

	fmt.Fprintf(os.Stderr, "\n  %s  %s\n", s.wrap(s.bold+s.cyan, "routage"), s.wrap(s.dim, "où tournent les tours d'agent"))
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
		fmt.Fprintf(os.Stderr, "  %s\n", s.wrap(s.dim, "modèle par défaut : relancez `herrscher init` après la reconstruction pour le choisir"))
		return out, nil
	}

	entries, err := cat(policy)
	if err != nil {
		return nil, fmt.Errorf("catalogue de modèles: %w", err)
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
		fmt.Fprintf(os.Stderr, "  %s\n", s.wrap(s.red, "sans modèle par défaut, chaque `session create` devra passer --model (obligatoire sous gateway-only)"))
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
	{contracts.PolicyAll, "all", "les modèles locaux et passerelle (build interne)"},
	{contracts.PolicyGatewayOnly, "gateway-only", "uniquement la passerelle du produit (build public)"},
}

// choosePolicy reads the route policy: empty keeps the current one, otherwise a
// 1-based index or the policy name itself.
func choosePolicy(s style, in *bufio.Reader, cur contracts.RoutePolicy) (contracts.RoutePolicy, error) {
	fmt.Fprintf(os.Stderr, "  %s\n", s.wrap(s.bold, "politique de route"))
	for i, c := range policyChoices {
		// Pad the plain label, then colour it: an ANSI wrap adds invisible bytes
		// that %-14s would count, shifting every following column.
		name, tag := fmt.Sprintf("%-14s", c.label), fmt.Sprintf("%-8s", "")
		if c.policy == cur {
			name = s.wrap(s.green, name)
			tag = s.wrap(s.dim, fmt.Sprintf("%-8s", "actuel"))
		}
		fmt.Fprintf(os.Stderr, "    %s %s %s %s\n", s.wrap(s.dim, strconv.Itoa(i+1)), name, tag, s.wrap(s.dim, c.help))
	}

	ans := promptLine(in, "  "+s.wrap(s.dim, "›")+" ")
	if ans == "" {
		return cur, nil
	}
	if n, err := strconv.Atoi(ans); err == nil {
		if n < 1 || n > len(policyChoices) {
			return "", fmt.Errorf("politique de route: choix hors plage: %d", n)
		}
		return policyChoices[n-1].policy, nil
	}
	for _, c := range policyChoices {
		if ans == c.label {
			return c.policy, nil
		}
	}
	return "", fmt.Errorf("politique de route inconnue %q", ans)
}

// askGatewayCreds reads the gateway URL and token. Both or neither: a base URL
// without a token would point a vendor CLI at the gateway while it keeps
// authenticating as the machine's own logged-in account, which is the one shape
// the gateway-only policy exists to forbid. When a complete pair is already
// configured, empty answers keep it.
func askGatewayCreds(s style, in *bufio.Reader, have bool) (map[string]string, error) {
	note := "les deux sont requis"
	if have {
		note = "déjà configurés — entrée pour garder"
	}
	fmt.Fprintf(os.Stderr, "  %s  %s\n", s.wrap(s.bold, "passerelle · identifiants"), s.wrap(s.dim, "("+note+", le jeton est masqué)"))

	url := promptLine(in, secretLabel(s, host.EnvGatewayURL))
	token, err := readSecret(in, secretLabel(s, host.EnvGatewayToken))
	if err != nil {
		return nil, err
	}

	switch {
	case url == "" && token == "":
		if !have {
			return nil, fmt.Errorf("gateway-only sans identifiants de passerelle: toute session échouerait au démarrage — renseignez %s et %s", host.EnvGatewayURL, host.EnvGatewayToken)
		}
		return nil, nil
	case url == "" || token == "":
		return nil, fmt.Errorf("identifiants de passerelle incomplets: %s et %s vont ensemble — l'un sans l'autre ferait tourner les tours sur le compte de cette machine", host.EnvGatewayURL, host.EnvGatewayToken)
	}
	return map[string]string{host.EnvGatewayURL: url, host.EnvGatewayToken: token}, nil
}

// chooseDefaultModel offers the models the policy allows and returns the one a
// session gets when it names none. The last entry clears the default.
func chooseDefaultModel(s style, in *bufio.Reader, entries []host.CatalogEntry, cur string) (string, error) {
	if len(entries) == 0 {
		fmt.Fprintf(os.Stderr, "  %s\n", s.wrap(s.dim, "modèle par défaut : aucun backend compilé n'offre de modèle sous cette politique"))
		return "", nil
	}
	fmt.Fprintf(os.Stderr, "  %s\n", s.wrap(s.bold, "modèle par défaut"))
	for i, e := range entries {
		label := e.ID
		if e.Label != "" {
			label = fmt.Sprintf("%-24s %s", e.ID, s.wrap(s.dim, e.Label))
		}
		tag := ""
		if e.ID == cur {
			label, tag = s.wrap(s.green, label), s.wrap(s.dim, "  actuel")
		}
		fmt.Fprintf(os.Stderr, "    %s %s%s\n", s.wrap(s.dim, strconv.Itoa(i+1)), label, tag)
	}
	fmt.Fprintf(os.Stderr, "    %s %s\n", s.wrap(s.dim, strconv.Itoa(len(entries)+1)), "aucun")

	ans := promptLine(in, "  "+s.wrap(s.dim, "›")+" ")
	switch {
	case ans == "":
		return cur, nil
	case ans == "aucun", ans == "none":
		return "", nil
	}
	if n, err := strconv.Atoi(ans); err == nil {
		switch {
		case n >= 1 && n <= len(entries):
			return entries[n-1].ID, nil
		case n == len(entries)+1:
			return "", nil
		}
		return "", fmt.Errorf("modèle par défaut: choix hors plage: %d", n)
	}
	for _, e := range entries {
		if e.ID == ans {
			return ans, nil
		}
	}
	return "", fmt.Errorf("modèle inconnu %q (non offert sous cette politique)", ans)
}
