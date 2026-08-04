package manage

import (
	"bufio"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/host"
)

// fakeCatalog offers two gateway models and one native one, filtered by policy
// the way the real catalog is.
func fakeCatalog(p contracts.RoutePolicy) ([]host.CatalogEntry, error) {
	all := []host.CatalogEntry{
		{Vendor: "claude", ModelSpec: contracts.ModelSpec{ID: "gw-opus", Route: contracts.RouteGateway}},
		{Vendor: "claude", ModelSpec: contracts.ModelSpec{ID: "gw-sonnet", Route: contracts.RouteGateway}},
		{Vendor: "claude", ModelSpec: contracts.ModelSpec{ID: "local-opus", Route: contracts.RouteNative}},
	}
	out := []host.CatalogEntry{}
	for _, e := range all {
		if p.Allows(e.Route) {
			out = append(out, e)
		}
	}
	return out, nil
}

// answers drives the wizard from a script of typed lines.
func answers(lines ...string) *bufio.Reader {
	return bufio.NewReader(strings.NewReader(strings.Join(lines, "\n") + "\n"))
}

func TestRouteStepDefaultsToTheInternalBuild(t *testing.T) {
	// Two empty lines: keep the current policy, keep the current model.
	got, err := routeStep(style{}, answers("", ""), fakeCatalog, routeCurrent{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if got[host.EnvRoutePolicy] != string(contracts.PolicyAll) {
		t.Fatalf("policy = %q, want all", got[host.EnvRoutePolicy])
	}
	if _, asked := got[host.EnvGatewayToken]; asked {
		t.Fatal("asked for gateway credentials on the internal build")
	}
}

// The whole point of the step: gateway-only without credentials is a build that
// cannot start a single session, so it must not be persistable.
func TestRouteStepRefusesGatewayOnlyWithNoCredentials(t *testing.T) {
	_, err := routeStep(style{}, answers("gateway-only", "", ""), fakeCatalog, routeCurrent{}, true)
	if err == nil {
		t.Fatal("gateway-only accepted with no credentials")
	}
	if !strings.Contains(err.Error(), host.EnvGatewayToken) {
		t.Fatalf("error does not name the missing key: %v", err)
	}
}

// A base URL without a token points the CLI at the gateway while it keeps
// authenticating as this machine's own account — the exact shape gateway-only
// exists to forbid, so a half pair is refused rather than written.
func TestRouteStepRefusesAHalfGatewayPair(t *testing.T) {
	for _, tc := range []struct{ name, url, token string }{
		{"url only", "https://gw.example", ""},
		{"token only", "", "secret"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := routeStep(style{}, answers("2", tc.url, tc.token, ""), fakeCatalog, routeCurrent{}, true)
			if err == nil {
				t.Fatal("a half gateway pair was accepted")
			}
			if !strings.Contains(err.Error(), "incomplete") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestRouteStepWritesTheGatewayPairAndModel(t *testing.T) {
	got, err := routeStep(style{}, answers("2", "https://gw.example", "secret", "1"), fakeCatalog, routeCurrent{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if got[host.EnvRoutePolicy] != string(contracts.PolicyGatewayOnly) {
		t.Fatalf("policy = %q", got[host.EnvRoutePolicy])
	}
	if got[host.EnvGatewayURL] != "https://gw.example" || got[host.EnvGatewayToken] != "secret" {
		t.Fatalf("gateway pair not persisted: %v", got)
	}
	// The native model must not be offered here, so index 1 is a gateway one.
	if got[host.EnvDefaultModel] != "gw-opus" {
		t.Fatalf("default model = %q, want the first GATEWAY model", got[host.EnvDefaultModel])
	}
}

// An already-configured pair is kept on an empty answer, and never re-displayed.
func TestRouteStepKeepsAnExistingGatewayPair(t *testing.T) {
	got, err := routeStep(style{}, answers("2", "", "", ""), fakeCatalog, routeCurrent{haveCred: true}, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, rewritten := got[host.EnvGatewayToken]; rewritten {
		t.Fatal("rewrote a gateway pair the operator asked to keep")
	}
}

// Compose mode is about to rebuild the binary, so the models this one can
// enumerate belong to the stack being replaced.
func TestRouteStepSkipsTheModelQuestionInComposeMode(t *testing.T) {
	got, err := routeStep(style{}, answers(""), fakeCatalog, routeCurrent{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, asked := got[host.EnvDefaultModel]; asked {
		t.Fatal("offered a default model from a stack that is about to be replaced")
	}
}

// Clearing is distinct from keeping: it must write an empty value, since
// writeSecretsTo upserts by key and an absent key keeps the old default.
func TestRouteStepClearsTheDefaultModel(t *testing.T) {
	got, err := routeStep(style{}, answers("1", "none"), fakeCatalog, routeCurrent{model: "local-opus"}, true)
	if err != nil {
		t.Fatal(err)
	}
	v, present := got[host.EnvDefaultModel]
	if !present || v != "" {
		t.Fatalf("default model = %q present=%v, want an explicit empty value", v, present)
	}
}

// The wizard must render the catalog in the order it is given — the same
// (vendor, id) order `models list` and the app selector show. Re-sorting here
// would make "pick 2" mean two different models on two surfaces.
func TestRouteStepKeepsTheCatalogOrder(t *testing.T) {
	// Vendor-grouped, so a sort by id alone would reorder these.
	grouped := func(contracts.RoutePolicy) ([]host.CatalogEntry, error) {
		return []host.CatalogEntry{
			{Vendor: "claude", ModelSpec: contracts.ModelSpec{ID: "z-claude"}},
			{Vendor: "codex", ModelSpec: contracts.ModelSpec{ID: "a-codex"}},
		}, nil
	}
	got, err := routeStep(style{}, answers("1", "2"), grouped, routeCurrent{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if got[host.EnvDefaultModel] != "a-codex" {
		t.Fatalf("entry 2 = %q, want the catalog's second entry", got[host.EnvDefaultModel])
	}
}

func TestRouteStepRejectsAnUnknownModel(t *testing.T) {
	if _, err := routeStep(style{}, answers("1", "nope"), fakeCatalog, routeCurrent{}, true); err == nil {
		t.Fatal("an unknown model id was accepted")
	}
}

func TestRouteStepRejectsAnOutOfRangePolicy(t *testing.T) {
	if _, err := routeStep(style{}, answers("9"), fakeCatalog, routeCurrent{}, true); err == nil {
		t.Fatal("an out-of-range policy choice was accepted")
	}
}
