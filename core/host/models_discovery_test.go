package host

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/cli"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/envx"
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
	t.Setenv(envModelsTTL, "2h")
	t.Setenv(envModelsTimeout, "3s")
	t.Setenv(envModelsDiscover, "claude, codex")
	settings := resolveDiscoverySettings()
	if settings.ttl != 2*time.Hour || settings.timeout != 3*time.Second {
		t.Fatalf("durations = %v %v", settings.ttl, settings.timeout)
	}
	if !settings.enabled("claude") || !settings.enabled("codex") || settings.enabled("cursor") {
		t.Fatalf("vendor filter = %+v", settings.vendors)
	}
}

func TestDiscoveryDefaultsWhenEnvironmentIsEmpty(t *testing.T) {
	t.Setenv(envModelsTTL, "")
	t.Setenv(envModelsTimeout, "")
	t.Setenv(envModelsDiscover, "")
	settings := resolveDiscoverySettings()
	if settings.ttl != defaultModelsTTL || settings.timeout != defaultModelsTimeout {
		t.Fatalf("defaults = %v %v", settings.ttl, settings.timeout)
	}
	if !settings.enabled("anything") {
		t.Fatal("empty HERRSCHER_MODELS_DISCOVER should enable every vendor")
	}
}

func TestNoneDisablesEveryVendor(t *testing.T) {
	t.Setenv(envModelsDiscover, "none")
	settings := resolveDiscoverySettings()
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

func TestSourcesReadTheCacheBesideTheStateFile(t *testing.T) {
	dir := t.TempDir()
	cache := cacheWith(time.Now(), cachedModel{ID: "a-live", Label: "A live", Arg: "live", Route: "native"})
	if err := saveDiscoveryCache(filepath.Join(dir, discoveryCacheName), cache); err != nil {
		t.Fatal(err)
	}
	t.Setenv(envModelsHidden, "a-native")
	sources := ModelDiscoveryFor(filepath.Join(dir, "state.json")).Sources()
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

func TestHiddenModelStillResumes(t *testing.T) {
	sources := Sources{hidden: map[string]bool{"a-native": true}}
	if _, err := LookupModelWith(fakePlugins(), contracts.PolicyAll, sources, "a-native"); err != nil {
		t.Fatalf("hidden model a-native no longer resumes: %v", err)
	}
}

func TestDiscoveredIDServedByOtherBackendIsDiscarded(t *testing.T) {
	cache := cacheWith(
		time.Now(),
		cachedModel{ID: "b-native", Label: "clash", Arg: "clash", Route: "native"},
		cachedModel{ID: "a-live", Label: "A live", Arg: "live", Route: "native"},
	)
	got, err := CatalogWith(fakePlugins(), contracts.PolicyAll, Sources{discovered: cache})
	if err != nil {
		t.Fatalf("CatalogWith rejected the whole catalog for one clashing ID: %v", err)
	}
	counts := make(map[string]int)
	for _, entry := range got {
		counts[entry.ID]++
		if entry.ID == "b-native" && entry.Vendor != "beta" {
			t.Fatalf("b-native served by %q, want beta", entry.Vendor)
		}
	}
	if counts["b-native"] != 1 || counts["a-live"] != 1 {
		t.Fatalf("catalog counts = %v, want b-native once and a-live once", counts)
	}
}

func TestFutureFetchedAtIsStale(t *testing.T) {
	plugins := discoveringPlugins(liveAnswer(contracts.ModelSpec{ID: "a-new", Label: "A new", Arg: "new", Route: contracts.RouteNative}))
	settings := discoverySettings{ttl: time.Hour, timeout: time.Second}
	now := time.Now()
	cache := cacheWith(now.Add(time.Hour), cachedModel{ID: "a-old", Label: "A old", Arg: "old", Route: "native"})
	_, outcomes := refreshDiscovery(context.Background(), plugins, cache, settings, now, false)
	if len(outcomes) != 1 || !outcomes[0].Refreshed {
		t.Fatalf("cache dated in the future was not refreshed: %+v", outcomes)
	}
}

func TestGatewayOnlyNeverCallsNativeDiscovery(t *testing.T) {
	t.Setenv(EnvGatewayURL, "")
	t.Setenv(EnvGatewayToken, "")
	called := false
	plugins := discoveringPlugins(func(context.Context, contracts.DiscoveryAccess) ([]contracts.ModelSpec, error) {
		called = true
		return nil, nil
	})
	settings := discoverySettings{ttl: time.Hour, timeout: time.Second, policy: contracts.PolicyGatewayOnly}
	_, outcomes := refreshDiscovery(context.Background(), plugins, cacheWith(time.Now().Add(-2*time.Hour)), settings, time.Now(), true)
	if called || len(outcomes) != 0 {
		t.Fatalf("gateway-only policy called native discovery: called=%v outcomes=%+v", called, outcomes)
	}
}

func TestInvalidAnswerKeepsPreviousCache(t *testing.T) {
	plugins := discoveringPlugins(liveAnswer(contracts.ModelSpec{ID: "", Label: "broken", Arg: "broken", Route: contracts.RouteNative}))
	settings := discoverySettings{ttl: time.Hour, timeout: time.Second}
	cache := cacheWith(time.Now().Add(-2*time.Hour), cachedModel{ID: "a-live", Label: "A live", Arg: "live", Route: "native"})
	next, outcomes := refreshDiscovery(context.Background(), plugins, cache, settings, time.Now(), false)
	if len(outcomes) != 1 || outcomes[0].Refreshed || !outcomes[0].Kept {
		t.Fatalf("invalid answer outcome = %+v, want kept and not refreshed", outcomes)
	}
	if models := next.modelsOf("alpha"); len(models) != 1 || models[0].ID != "a-live" {
		t.Fatalf("invalid answer replaced the cache: %+v", models)
	}
}

func TestPersistRefreshedKeepsVendorsWrittenByOthers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models-discovered.json")
	now := time.Now()
	onDisk := discoveryCache{Vendors: map[string]vendorDiscovery{
		"beta": {FetchedAt: now, Models: []cachedModel{{ID: "b-live", Label: "B live", Arg: "live", Route: "native"}}},
	}}
	if err := saveDiscoveryCache(path, onDisk); err != nil {
		t.Fatal(err)
	}
	next := discoveryCache{Vendors: map[string]vendorDiscovery{
		"alpha": {FetchedAt: now, Models: []cachedModel{{ID: "a-new", Label: "A new", Arg: "new", Route: "native"}}},
	}}
	if err := persistRefreshed(path, next, []discoveryOutcome{{Vendor: "alpha", Refreshed: true}}); err != nil {
		t.Fatalf("persistRefreshed: %v", err)
	}
	got, err := loadDiscoveryCache(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.modelsOf("alpha")) != 1 || len(got.modelsOf("beta")) != 1 {
		t.Fatalf("cache after persist = %+v, want alpha and beta both kept", got.Vendors)
	}
}

func TestGatewayRoutesDroppedWithoutCredentials(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("HERRSCHER_STATE_DIR", stateDir)
	t.Setenv(EnvGatewayURL, "")
	t.Setenv(EnvGatewayToken, "")
	cache := cacheWith(
		time.Now(),
		cachedModel{ID: "a-via-gw", Label: "via gw", Arg: "gw", Route: "gateway"},
		cachedModel{ID: "a-live", Label: "A live", Arg: "live", Route: "native"},
	)
	if err := saveDiscoveryCache(filepath.Join(stateDir, discoveryCacheName), cache); err != nil {
		t.Fatal(err)
	}
	if ids := catalogIDs(t); ids["a-via-gw"] || !ids["a-live"] {
		t.Fatalf("without gateway credentials, catalog = %v, want a-live and no a-via-gw", ids)
	}
	t.Setenv(EnvGatewayURL, "https://gateway.example")
	t.Setenv(EnvGatewayToken, "token")
	if ids := catalogIDs(t); !ids["a-via-gw"] {
		t.Fatalf("with gateway credentials, catalog = %v, want a-via-gw", ids)
	}
}

func catalogIDs(t *testing.T) map[string]bool {
	t.Helper()
	got, err := CatalogWith(fakePlugins(), contracts.PolicyAll, DefaultModelDiscovery().Sources())
	if err != nil {
		t.Fatalf("CatalogWith: %v", err)
	}
	ids := make(map[string]bool)
	for idx := range got {
		ids[got[idx].ID] = true
	}
	return ids
}

func TestDiscoveryAccessCarriesTheHostRoutePolicy(t *testing.T) {
	t.Setenv(EnvGatewayURL, "https://gateway.example")
	t.Setenv(EnvGatewayToken, "token")
	var seen contracts.RoutePolicy
	plugins := discoveringPlugins(func(_ context.Context, access contracts.DiscoveryAccess) ([]contracts.ModelSpec, error) {
		seen = access.Policy
		return nil, nil
	})
	settings := discoverySettings{ttl: time.Hour, timeout: time.Second, policy: contracts.PolicyGatewayOnly}
	refreshDiscovery(context.Background(), plugins, cacheWith(time.Now().Add(-2*time.Hour)), settings, time.Now(), true)
	if seen != contracts.PolicyGatewayOnly {
		t.Fatalf("discovery access policy = %q, want the host gateway-only policy", seen)
	}
}

func TestGatewayOnlyDiscoversThroughTheGatewayWhenCredentialsExist(t *testing.T) {
	t.Setenv(EnvGatewayURL, "https://gateway.example")
	t.Setenv(EnvGatewayToken, "token")
	var viaGateway string
	plugins := discoveringPlugins(func(_ context.Context, access contracts.DiscoveryAccess) ([]contracts.ModelSpec, error) {
		viaGateway = access.Gateway.BaseURL()
		return nil, nil
	})
	settings := discoverySettings{ttl: time.Hour, timeout: time.Second, policy: contracts.PolicyGatewayOnly}
	_, outcomes := refreshDiscovery(context.Background(), plugins, cacheWith(time.Now().Add(-2*time.Hour)), settings, time.Now(), true)
	if len(outcomes) != 1 || viaGateway != "https://gateway.example" {
		t.Fatalf("gateway-only with credentials: outcomes=%+v gateway=%q, want one discovery through the gateway", outcomes, viaGateway)
	}
}

func TestSaveDiscoveryCacheCreatesItsDirectoryPrivately(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state", "nested")
	if err := saveDiscoveryCache(filepath.Join(dir, discoveryCacheName), cacheWith(time.Now())); err != nil {
		t.Fatalf("saveDiscoveryCache into a missing directory: %v", err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("cache directory mode = %o, want 700", perm)
	}
}

func TestModelDiscoveryPutsTheCacheBesideTheDaemonState(t *testing.T) {
	t.Setenv("HERRSCHER_STATE_DIR", "")
	state := filepath.Join(t.TempDir(), "elsewhere", "state.json")
	path := ModelDiscoveryFor(state).cachePath()
	if want := filepath.Join(filepath.Dir(state), discoveryCacheName); path != want {
		t.Fatalf("cache path = %s, want %s beside the --state file", path, want)
	}
}

func TestModelDiscoveryForLeavesTheEnvironmentAlone(t *testing.T) {
	t.Setenv("HERRSCHER_STATE_DIR", "")
	_ = ModelDiscoveryFor(filepath.Join(t.TempDir(), "state.json"))
	if inherited := os.Getenv("HERRSCHER_STATE_DIR"); inherited != "" {
		t.Fatalf("ModelDiscoveryFor exported HERRSCHER_STATE_DIR=%q to child processes", inherited)
	}
}

func modelsVerb(t *testing.T, verb string) contracts.Cmd {
	t.Helper()
	for _, cmd := range ModelsCommands(fixedLanguage(i18n.English)) {
		if len(cmd.Path) == 2 && cmd.Path[1] == verb {
			return cmd
		}
	}
	t.Fatalf("no models %s verb", verb)
	return contracts.Cmd{}
}

func TestModelsListFollowsTheStateFlag(t *testing.T) {
	saved := contracts.Default
	t.Cleanup(func() { contracts.Default = saved })
	contracts.Default = contracts.Registry{}
	for _, plugin := range fakePlugins() {
		contracts.Default.Register(plugin)
	}
	t.Setenv("HERRSCHER_STATE_DIR", t.TempDir())
	dir := t.TempDir()
	cache := cacheWith(time.Now(), cachedModel{ID: "alpha-state-live", Label: "state live", Arg: "sl", Route: "native"})
	if err := saveDiscoveryCache(discoveryCacheIn(dir), cache); err != nil {
		t.Fatal(err)
	}
	out, err := cli.Plain(modelsVerb(t, "list").Run(context.Background(), contracts.Input{Args: map[string]string{"state": filepath.Join(dir, "state.json")}}))
	if err != nil {
		t.Fatalf("models list --state: %v", err)
	}
	if !strings.Contains(out, "alpha-state-live") {
		t.Fatalf("models list --state ignored the state file's cache:\n%s", out)
	}
}

func TestClashWarnsOncePerCacheAcrossLoads(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	path := discoveryCacheIn(t.TempDir())
	if err := saveDiscoveryCache(path, cacheWith(time.Now(), cachedModel{ID: "b-native", Label: "clash", Arg: "clash", Route: "native"})); err != nil {
		t.Fatal(err)
	}
	models := ModelDiscoveryFor(filepath.Join(filepath.Dir(path), "state.json"))
	for range 5 {
		if _, err := CatalogWith(fakePlugins(), contracts.PolicyAll, models.Sources()); err != nil {
			t.Fatalf("CatalogWith: %v", err)
		}
	}
	if count := strings.Count(logs.String(), "discovered model ID is served by another backend"); count != 1 {
		t.Fatalf("clash warnings over 5 loads of one cache = %d, want 1\n%s", count, logs.String())
	}
}

func TestClashWarnsOncePerIDPerCacheLoad(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	sources := Sources{discovered: cacheWith(time.Now(), cachedModel{ID: "b-native", Label: "clash", Arg: "clash", Route: "native"}), notices: newClashNotices()}
	for range 5 {
		if _, err := CatalogWith(fakePlugins(), contracts.PolicyAll, sources); err != nil {
			t.Fatalf("CatalogWith: %v", err)
		}
	}
	if count := strings.Count(logs.String(), "discovered model ID is served by another backend"); count != 1 {
		t.Fatalf("clash warnings over 5 catalog builds = %d, want 1\n%s", count, logs.String())
	}
}

func TestChooseVendorHonoursAHiddenRoutePolicy(t *testing.T) {
	t.Setenv("HERRSCHER_STATE_DIR", t.TempDir())
	t.Setenv(EnvRoutePolicy, string(contracts.PolicyGatewayOnly))
	restore := envx.Hide([]string{EnvRoutePolicy})
	defer restore()
	if _, _, err := chooseVendor("", "", "a-native", fakePlugins(), nil); err == nil {
		t.Fatal("a native model resolved under a gateway-only policy that is only hidden from the process environment")
	}
}

func TestConcurrentPersistKeepsEveryWriterVendor(t *testing.T) {
	dir := t.TempDir()
	for round := range 200 {
		path := filepath.Join(dir, fmt.Sprintf("models-%d.json", round))
		var group sync.WaitGroup
		errs := make(chan error, 2)
		start := make(chan struct{})
		for _, vendor := range []string{"alpha", "beta"} {
			group.Add(1)
			go func(vendor string) {
				defer group.Done()
				<-start
				errs <- persistRefreshed(path, oneVendorCache(vendor), []discoveryOutcome{{Vendor: vendor, Refreshed: true}})
			}(vendor)
		}
		close(start)
		group.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatalf("round %d: persistRefreshed: %v", round, err)
			}
		}
		got, err := loadDiscoveryCache(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.modelsOf("alpha")) != 1 || len(got.modelsOf("beta")) != 1 {
			t.Fatalf("round %d: two concurrent writers left %+v, want alpha and beta both kept", round, got.Vendors)
		}
	}
}

func oneVendorCache(vendor string) discoveryCache {
	return discoveryCache{Vendors: map[string]vendorDiscovery{
		vendor: {FetchedAt: time.Now(), Models: []cachedModel{{ID: vendor + "-live", Label: vendor, Arg: "live", Route: "native"}}},
	}}
}

func TestGatewayOnlyDropsNativeAnswersOfABackendThatIgnoresThePolicy(t *testing.T) {
	t.Setenv(EnvGatewayURL, "https://gateway.example")
	t.Setenv(EnvGatewayToken, "token")
	plugins := discoveringPlugins(liveAnswer(
		contracts.ModelSpec{ID: "leaked-native", Label: "Leaked", Arg: "leaked", Route: contracts.RouteNative},
		contracts.ModelSpec{ID: "via-gateway", Label: "Via", Arg: "via", Route: contracts.RouteGateway},
	))
	settings := discoverySettings{ttl: time.Hour, timeout: time.Second, policy: contracts.PolicyGatewayOnly}
	next, _ := refreshDiscovery(context.Background(), plugins, cacheWith(time.Now().Add(-2*time.Hour)), settings, time.Now(), true)
	ids := make(map[string]bool)
	for _, model := range next.Vendors["alpha"].Models {
		ids[model.ID] = true
	}
	if ids["leaked-native"] || !ids["via-gateway"] {
		t.Fatalf("gateway-only cache = %v, want via-gateway and no leaked-native", ids)
	}
}

func TestBridgeModelDiscoveryReadsTheCacheBesideItsStateFile(t *testing.T) {
	stateDir := t.TempDir()
	cache := cacheWith(time.Now(), cachedModel{ID: "a-live", Label: "A live", Arg: "live", Route: "native"})
	if err := saveDiscoveryCache(filepath.Join(stateDir, discoveryCacheName), cache); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERRSCHER_STATE_DIR", t.TempDir())
	sources := BridgeModelDiscovery(filepath.Join(stateDir, "state.json")).Sources()
	got, err := CatalogWith(fakePlugins(), contracts.PolicyAll, sources)
	if err != nil {
		t.Fatalf("CatalogWith: %v", err)
	}
	for _, entry := range got {
		if entry.ID == "a-live" {
			return
		}
	}
	t.Fatal("bridge catalog lacks a-live: it did not read the cache beside --state")
}

func pluginsWithRetiredModel() []contracts.Plugin {
	return []contracts.Plugin{{Manifest: contracts.Manifest{Kind: "alpha", Category: contracts.CategoryBackend, Models: []contracts.ModelSpec{
		{ID: "a-visible", Label: "visible", Arg: "av", Route: contracts.RouteNative},
		{ID: "a-retired", Label: "retired", Arg: "ar", Route: contracts.RouteNative, Hidden: true},
	}}}}
}

func TestModelHiddenBySpecLeavesSelectorButResolves(t *testing.T) {
	plugins := pluginsWithRetiredModel()
	listed, err := CatalogWith(plugins, contracts.PolicyAll, Sources{})
	if err != nil {
		t.Fatalf("CatalogWith: %v", err)
	}
	for _, entry := range listed {
		if entry.ID == "a-retired" {
			t.Fatal("spec-hidden model a-retired is still offered by the selector")
		}
	}
	entry, err := LookupModelWith(plugins, contracts.PolicyAll, Sources{}, "a-retired")
	if err != nil {
		t.Fatalf("hidden model a-retired no longer resolves: %v", err)
	}
	if !entry.Hidden {
		t.Fatal("resolved entry of a-retired is not marked hidden")
	}
}

func TestResolvingAHiddenModelWarnsWithoutSwitching(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	entry, err := LookupModelWith(pluginsWithRetiredModel(), contracts.PolicyAll, Sources{hidden: map[string]bool{"a-visible": true}}, "a-visible")
	if err != nil {
		t.Fatalf("LookupModelWith: %v", err)
	}
	if entry.ID != "a-visible" {
		t.Fatalf("resolved %q, want the requested a-visible", entry.ID)
	}
	if !strings.Contains(logs.String(), "hidden from the selector") || !strings.Contains(logs.String(), "a-visible") {
		t.Fatalf("no warning for a resolved hidden model, logs: %s", logs.String())
	}
}

func TestResolvingAVisibleModelDoesNotWarn(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	if _, err := LookupModelWith(pluginsWithRetiredModel(), contracts.PolicyAll, Sources{}, "a-visible"); err != nil {
		t.Fatalf("LookupModelWith: %v", err)
	}
	if strings.Contains(logs.String(), "hidden from the selector") {
		t.Fatalf("visible model produced a hidden warning: %s", logs.String())
	}
}

func TestCacheWrittenBeforeWindowAndHiddenLoads(t *testing.T) {
	path := discoveryCacheIn(t.TempDir())
	legacy := `{"vendors":{"alpha":{"fetchedAt":"2026-11-04T09:00:00Z","models":[{"id":"a-live","label":"A live","arg":"live","route":"native"}]}}}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := loadDiscoveryCache(path)
	if err != nil {
		t.Fatalf("loadDiscoveryCache of a cache without window or hidden: %v", err)
	}
	models := got.modelsOf("alpha")
	if len(models) != 1 || models[0].ID != "a-live" || models[0].ContextWindow != 0 || models[0].Hidden {
		t.Fatalf("legacy cache entry = %+v", models)
	}
}

func TestCacheCarriesContextWindowAndHidden(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models-discovered.json")
	want := cacheWith(time.Now(), cachedModel{ID: "a-live", Label: "A live", Arg: "live", Route: "native", ContextWindow: 400_000, Hidden: true})
	if err := saveDiscoveryCache(path, want); err != nil {
		t.Fatalf("saveDiscoveryCache: %v", err)
	}
	got, err := loadDiscoveryCache(path)
	if err != nil {
		t.Fatalf("loadDiscoveryCache: %v", err)
	}
	models := got.modelsOf("alpha")
	if len(models) != 1 || models[0].ContextWindow != 400_000 || !models[0].Hidden {
		t.Fatalf("round trip lost the window or the hidden flag: %+v", models)
	}
}

func TestToCachedKeepsContextWindowAndHidden(t *testing.T) {
	cached := toCached([]contracts.ModelSpec{{ID: "a-live", Route: contracts.RouteNative, ContextWindow: 400_000, Hidden: true}})
	if len(cached) != 1 || cached[0].ContextWindow != 400_000 || !cached[0].Hidden {
		t.Fatalf("toCached dropped the window or the hidden flag: %+v", cached)
	}
}

func TestModelsListTextFollowsTheLanguage(t *testing.T) {
	if got := modelsListText(i18n.French, nil); got != "aucun modèle proposé avec la politique de routage actuelle" {
		t.Fatalf("modelsListText(fr, none) = %q", got)
	}
	if got := modelsListText(i18n.English, nil); got != "no models offered under the current route policy" {
		t.Fatalf("modelsListText(en, none) = %q", got)
	}
}
