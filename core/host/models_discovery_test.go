package host

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

var errVendorDown = errors.New("vendor down")

func discoveringPlugins(discover contracts.ModelDiscoverer) []contracts.Plugin {
	plugins := fakePlugins()
	plugins[0].Discover = discover
	return plugins
}

func liveAnswer(models ...contracts.ModelSpec) contracts.ModelDiscoverer {
	return func(context.Context, contracts.DiscoveryAccess) ([]contracts.ModelSpec, error) {
		return models, nil
	}
}

func cacheWith(fetchedAt time.Time, models ...cachedModel) discoveryCache {
	return discoveryCache{Vendors: map[string]vendorDiscovery{"alpha": {FetchedAt: fetchedAt, Models: models}}}
}

func TestDiscoveredModelJoinsCatalogWithItsSource(t *testing.T) {
	cache := cacheWith(time.Now(), cachedModel{ID: "a-live", Label: "A live", Arg: "live", Route: "native"})
	got, err := CatalogWith(fakePlugins(), contracts.PolicyAll, Sources{discovered: cache})
	if err != nil {
		t.Fatalf("CatalogWith: %v", err)
	}
	sources := make(map[string]string)
	for _, entry := range got {
		sources[entry.ID] = entry.Source
	}
	if sources["a-live"] != sourceDiscovered {
		t.Fatalf("a-live source = %q, want %q", sources["a-live"], sourceDiscovered)
	}
	if sources["a-native"] != sourceCatalog {
		t.Fatalf("a-native source = %q, want %q", sources["a-native"], sourceCatalog)
	}
}

func TestDiscoveredModelPassesItsArgThrough(t *testing.T) {
	cache := cacheWith(time.Now(), cachedModel{ID: "a-live", Label: "A live", Arg: "live-arg", Route: "native"})
	entry, err := LookupModelWith(fakePlugins(), contracts.PolicyAll, Sources{discovered: cache}, "a-live")
	if err != nil {
		t.Fatalf("LookupModelWith: %v", err)
	}
	if entry.Arg != "live-arg" {
		t.Fatalf("Arg = %q, want live-arg", entry.Arg)
	}
}

func TestHiddenModelIsDroppedFromCatalog(t *testing.T) {
	got, err := CatalogWith(fakePlugins(), contracts.PolicyAll, Sources{hidden: map[string]bool{"a-native": true}})
	if err != nil {
		t.Fatalf("CatalogWith: %v", err)
	}
	for _, entry := range got {
		if entry.ID == "a-native" {
			t.Fatal("hidden model a-native is still listed")
		}
	}
}

func TestPersistedModelGoneAtResumeIsNotFound(t *testing.T) {
	cache := cacheWith(time.Now(), cachedModel{ID: "a-gone", Label: "gone", Arg: "gone", Route: "native"})
	if _, err := LookupModelWith(fakePlugins(), contracts.PolicyAll, Sources{}, "a-gone"); err == nil {
		t.Fatal("LookupModelWith found a-gone without a cache entry")
	}
	if _, err := LookupModelWith(fakePlugins(), contracts.PolicyAll, Sources{discovered: cache}, "a-gone"); err != nil {
		t.Fatalf("LookupModelWith lost a-gone with its cache entry: %v", err)
	}
}

func TestFreshCacheIsNotRefreshedWithoutForce(t *testing.T) {
	called := false
	plugins := discoveringPlugins(func(context.Context, contracts.DiscoveryAccess) ([]contracts.ModelSpec, error) {
		called = true
		return nil, nil
	})
	settings := discoverySettings{ttl: time.Hour, timeout: time.Second}
	cache := cacheWith(time.Now(), cachedModel{ID: "a-live", Label: "A live", Arg: "live", Route: "native"})
	_, outcomes := refreshDiscovery(context.Background(), plugins, cache, settings, time.Now(), false)
	if called || len(outcomes) != 0 {
		t.Fatalf("fresh cache refreshed: called=%v outcomes=%+v", called, outcomes)
	}
}

func TestExpiredCacheIsRefreshed(t *testing.T) {
	plugins := discoveringPlugins(liveAnswer(contracts.ModelSpec{ID: "a-new", Label: "A new", Arg: "new", Route: contracts.RouteNative}))
	settings := discoverySettings{ttl: time.Hour, timeout: time.Second}
	now := time.Now()
	cache := cacheWith(now.Add(-2*time.Hour), cachedModel{ID: "a-old", Label: "A old", Arg: "old", Route: "native"})
	next, outcomes := refreshDiscovery(context.Background(), plugins, cache, settings, now, false)
	if len(outcomes) != 1 || !outcomes[0].Refreshed {
		t.Fatalf("expired cache not refreshed: %+v", outcomes)
	}
	models := next.modelsOf("alpha")
	if len(models) != 1 || models[0].ID != "a-new" {
		t.Fatalf("cache after refresh = %+v, want only a-new", models)
	}
}

func TestFailedDiscoveryKeepsLastKnownList(t *testing.T) {
	plugins := discoveringPlugins(func(context.Context, contracts.DiscoveryAccess) ([]contracts.ModelSpec, error) {
		return nil, errVendorDown
	})
	settings := discoverySettings{ttl: time.Hour, timeout: time.Second}
	cache := cacheWith(time.Now().Add(-2*time.Hour), cachedModel{ID: "a-live", Label: "A live", Arg: "live", Route: "native"})
	next, outcomes := refreshDiscovery(context.Background(), plugins, cache, settings, time.Now(), false)
	if len(outcomes) != 1 || outcomes[0].Refreshed || !outcomes[0].Kept {
		t.Fatalf("failure outcome = %+v, want kept and not refreshed", outcomes)
	}
	if models := next.modelsOf("alpha"); len(models) != 1 || models[0].ID != "a-live" {
		t.Fatalf("failed discovery dropped the cache: %+v", models)
	}
}

func TestSilentVendorDoesNotBlockPastItsDeadline(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	plugins := discoveringPlugins(func(context.Context, contracts.DiscoveryAccess) ([]contracts.ModelSpec, error) {
		<-release
		return nil, nil
	})
	settings := discoverySettings{ttl: time.Hour, timeout: 10 * time.Millisecond}
	cache := cacheWith(time.Now().Add(-2*time.Hour), cachedModel{ID: "a-live", Label: "A live", Arg: "live", Route: "native"})
	started := time.Now()
	next, outcomes := refreshDiscovery(context.Background(), plugins, cache, settings, time.Now(), false)
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("silent vendor blocked refresh for %v", elapsed)
	}
	if len(outcomes) != 1 || !outcomes[0].Kept {
		t.Fatalf("silent vendor outcome = %+v, want kept", outcomes)
	}
	if models := next.modelsOf("alpha"); len(models) != 1 {
		t.Fatalf("silent vendor dropped the cache: %+v", models)
	}
}

func TestDiscoverySettingsFromEnvironment(t *testing.T) {
	env := map[string]string{
		envModelsTTL:      "2h",
		envModelsTimeout:  "3s",
		envModelsDiscover: "claude, codex",
	}
	settings := resolveDiscoverySettings(func(name string) string { return env[name] })
	if settings.ttl != 2*time.Hour || settings.timeout != 3*time.Second {
		t.Fatalf("durations = %v %v", settings.ttl, settings.timeout)
	}
	if !settings.enabled("claude") || !settings.enabled("codex") || settings.enabled("cursor") {
		t.Fatalf("vendor filter = %+v", settings.vendors)
	}
}

func TestDiscoveryDefaultsWhenEnvironmentIsEmpty(t *testing.T) {
	settings := resolveDiscoverySettings(func(string) string { return "" })
	if settings.ttl != defaultModelsTTL || settings.timeout != defaultModelsTimeout {
		t.Fatalf("defaults = %v %v", settings.ttl, settings.timeout)
	}
	if !settings.enabled("anything") {
		t.Fatal("empty HERRSCHER_MODELS_DISCOVER should enable every vendor")
	}
}

func TestNoneDisablesEveryVendor(t *testing.T) {
	settings := resolveDiscoverySettings(func(name string) string {
		if name == envModelsDiscover {
			return "none"
		}
		return ""
	})
	if settings.enabled("claude") {
		t.Fatal("HERRSCHER_MODELS_DISCOVER=none still enables claude")
	}
}

func TestCacheRoundTripsThroughDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models-discovered.json")
	fetched := time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)
	want := cacheWith(fetched, cachedModel{ID: "a-live", Label: "A live", Arg: "live", Efforts: []string{"low", "high"}, Route: "native"})
	if err := saveDiscoveryCache(path, want); err != nil {
		t.Fatalf("saveDiscoveryCache: %v", err)
	}
	got, err := loadDiscoveryCache(path)
	if err != nil {
		t.Fatalf("loadDiscoveryCache: %v", err)
	}
	entry := got.Vendors["alpha"]
	if !entry.FetchedAt.Equal(fetched) || len(entry.Models) != 1 || entry.Models[0].ID != "a-live" || len(entry.Models[0].Efforts) != 2 {
		t.Fatalf("round trip = %+v", entry)
	}
}

func TestMissingCacheFileLoadsEmpty(t *testing.T) {
	got, err := loadDiscoveryCache(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("loadDiscoveryCache on a missing file: %v", err)
	}
	if len(got.Vendors) != 0 {
		t.Fatalf("missing cache loaded %+v", got)
	}
}

func TestLoadSourcesReadsCacheFromStateDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HERRSCHER_STATE_DIR", dir)
	cache := cacheWith(time.Now(), cachedModel{ID: "a-live", Label: "A live", Arg: "live", Route: "native"})
	if err := saveDiscoveryCache(filepath.Join(dir, discoveryCacheName), cache); err != nil {
		t.Fatal(err)
	}
	sources := LoadSources(func(name string) string {
		if name == envModelsHidden {
			return "a-native"
		}
		return ""
	})
	got, err := CatalogWith(fakePlugins(), contracts.PolicyAll, sources)
	if err != nil {
		t.Fatalf("CatalogWith: %v", err)
	}
	ids := make(map[string]bool)
	for _, entry := range got {
		ids[entry.ID] = true
	}
	if !ids["a-live"] || ids["a-native"] {
		t.Fatalf("catalog from state dir = %v, want a-live and no a-native", ids)
	}
}
