package host

import (
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
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
	got, catalogErr := Catalog(fakePlugins(), contracts.PolicyAll)
	if catalogErr != nil {
		t.Fatal(catalogErr)
	}
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
	got := ResolvePolicy(func(string) string { return "banana" })
	if got != contracts.PolicyAll {
		t.Fatalf("ResolvePolicy(garbage) = %q, want PolicyAll", got)
	}
}

func TestVendorDefaultModelEnvNamesTheVendor(t *testing.T) {
	if got := VendorDefaultModelEnv("claude"); got != "HERRSCHER_DEFAULT_MODEL_CLAUDE" {
		t.Fatalf("claude: %q", got)
	}
	if got := VendorDefaultModelEnv("my-vendor"); got != "HERRSCHER_DEFAULT_MODEL_MY_VENDOR" {
		t.Fatalf("my-vendor: %q", got)
	}
}
