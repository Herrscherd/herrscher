package host

import (
	"testing"

	"github.com/Herrscherd/herrscher-contracts"
)

func fakePlugins() []contracts.Plugin {
	return []contracts.Plugin{
		{Manifest: contracts.Manifest{Kind: "alpha", Category: contracts.CategoryBackend, Models: []contracts.ModelSpec{
			{ID: "a-native", Label: "A native", Arg: "an", Route: contracts.RouteNative},
			{ID: "a-gw", Label: "A gateway", Arg: "ag", Route: contracts.RouteGateway},
		}}},
		{Manifest: contracts.Manifest{Kind: "beta", Category: contracts.CategoryBackend, Models: []contracts.ModelSpec{
			{ID: "b-native", Label: "B native", Arg: "bn", Route: contracts.RouteNative},
		}}},
	}
}

func TestCatalogAggregatesAcrossBackends(t *testing.T) {
	got, err := Catalog(fakePlugins(), contracts.PolicyAll)
	if err != nil {
		t.Fatalf("Catalog: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("Catalog returned %d entries, want 3: %+v", len(got), got)
	}
}

func TestCatalogTagsTheServingVendor(t *testing.T) {
	got, _ := Catalog(fakePlugins(), contracts.PolicyAll)
	for _, e := range got {
		if e.ID == "b-native" && e.Vendor != "beta" {
			t.Fatalf("model b-native is tagged vendor %q, want beta", e.Vendor)
		}
	}
}

func TestCatalogGatewayOnlyDropsNative(t *testing.T) {
	got, err := Catalog(fakePlugins(), contracts.PolicyGatewayOnly)
	if err != nil {
		t.Fatalf("Catalog: %v", err)
	}
	if len(got) != 1 || got[0].ID != "a-gw" {
		t.Fatalf("gateway-only catalog = %+v, want only a-gw", got)
	}
}

func TestCatalogRejectsDuplicateIDsAcrossBackends(t *testing.T) {
	// Two backends declaring the same ID would make LookupModel non-deterministic.
	// The daemon must refuse to start rather than pick one at random.
	plugins := append(fakePlugins(), contracts.Plugin{Manifest: contracts.Manifest{
		Kind: "gamma", Category: contracts.CategoryBackend,
		Models: []contracts.ModelSpec{{ID: "a-native", Label: "clash", Arg: "x", Route: contracts.RouteNative}},
	}})
	if _, err := Catalog(plugins, contracts.PolicyAll); err == nil {
		t.Fatal("Catalog accepted the same model ID from two backends")
	}
}

func TestCatalogRejectsInvalidBackendCatalog(t *testing.T) {
	plugins := []contracts.Plugin{{Manifest: contracts.Manifest{
		Kind: "broken", Category: contracts.CategoryBackend,
		Models: []contracts.ModelSpec{{ID: "", Label: "x", Arg: "x", Route: contracts.RouteNative}},
	}}}
	if _, err := Catalog(plugins, contracts.PolicyAll); err == nil {
		t.Fatal("Catalog accepted a backend with an invalid model")
	}
}

func TestLookupModelFindsAndTags(t *testing.T) {
	e, err := LookupModel(fakePlugins(), contracts.PolicyAll, "a-gw")
	if err != nil {
		t.Fatalf("LookupModel: %v", err)
	}
	if e.Vendor != "alpha" || e.Route != contracts.RouteGateway {
		t.Fatalf("LookupModel = %+v", e)
	}
}

func TestLookupModelRespectsPolicy(t *testing.T) {
	// Under gateway-only, a native model isn't merely hidden: it's not found.
	// A persisted session naming it can therefore not resume.
	if _, err := LookupModel(fakePlugins(), contracts.PolicyGatewayOnly, "a-native"); err == nil {
		t.Fatal("LookupModel resolved a native model under gateway-only")
	}
}

func TestLookupModelUnknownIsAnError(t *testing.T) {
	if _, err := LookupModel(fakePlugins(), contracts.PolicyAll, "nope"); err == nil {
		t.Fatal("LookupModel resolved an unknown model ID")
	}
}

func TestResolvePolicyDefaultsToAll(t *testing.T) {
	got := ResolvePolicy(func(string) string { return "" })
	if got != contracts.PolicyAll {
		t.Fatalf("ResolvePolicy with no env = %q, want PolicyAll", got)
	}
}

func TestResolvePolicyReadsEnv(t *testing.T) {
	got := ResolvePolicy(func(k string) string {
		if k == "HERRSCHER_ROUTE_POLICY" {
			return "gateway-only"
		}
		return ""
	})
	if got != contracts.PolicyGatewayOnly {
		t.Fatalf("ResolvePolicy = %q, want gateway-only", got)
	}
}

func TestResolvePolicyIgnoresGarbage(t *testing.T) {
	// An unknown value must fall back to the historical behavior, not block the
	// daemon: this setting is set by the app, not the user.
	got := ResolvePolicy(func(string) string { return "banana" })
	if got != contracts.PolicyAll {
		t.Fatalf("ResolvePolicy(garbage) = %q, want PolicyAll", got)
	}
}
