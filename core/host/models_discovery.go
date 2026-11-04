package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/envx"
)

const (
	envModelsTTL      = "HERRSCHER_MODELS_TTL"
	envModelsTimeout  = "HERRSCHER_MODELS_TIMEOUT"
	envModelsDiscover = "HERRSCHER_MODELS_DISCOVER"
	envModelsHidden   = "HERRSCHER_MODELS_HIDDEN"

	discoveryCacheName   = "models-discovered.json"
	defaultModelsTTL     = 24 * time.Hour
	defaultModelsTimeout = 15 * time.Second
	discoveryGrace       = 2 * time.Second
	cacheFileMode        = 0o600
	noneVendors          = "none"

	sourceCatalog    = "catalog"
	sourceDiscovered = "discovered"
)

var errDiscoveryNoAnswer = errors.New("no answer before the discovery deadline")

type discoverySettings struct {
	ttl     time.Duration
	timeout time.Duration
	vendors map[string]bool
	policy  contracts.RoutePolicy
}

type discoveryCache struct {
	Vendors map[string]vendorDiscovery `json:"vendors"`
}

type vendorDiscovery struct {
	FetchedAt time.Time     `json:"fetchedAt"`
	Models    []cachedModel `json:"models"`
}

type cachedModel struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Arg     string   `json:"arg"`
	Efforts []string `json:"efforts,omitempty"`
	Route   string   `json:"route"`
}

// Sources carries the discovered model lists and the hidden model IDs.
type Sources struct {
	discovered discoveryCache
	hidden     map[string]bool
}

type discoveryOutcome struct {
	Vendor    string
	Count     int
	Refreshed bool
	Kept      bool
	Err       error
}

type discoveryJob struct {
	kind   string
	plugin *contracts.Plugin
}

type discoveryAnswer struct {
	models []contracts.ModelSpec
	err    error
}

// LoadSources reads the discovered cache and the hidden IDs from the environment.
func LoadSources() Sources {
	sources := Sources{hidden: splitSet(envx.Getenv(envModelsHidden))}
	path, err := discoveryCachePath()
	if err != nil {
		slog.Warn("model cache disabled", "err", err)
		return sources
	}
	cache, err := loadDiscoveryCache(path)
	if err != nil {
		slog.Warn("model cache unreadable, using the static catalog", "path", path, "err", err)
	}
	if _, credsErr := loadGatewayCreds(envx.Getenv); credsErr != nil {
		cache = withoutGatewayRoutes(cache)
	}
	sources.discovered = cache
	return sources
}

func withoutGatewayRoutes(cache discoveryCache) discoveryCache {
	kept := discoveryCache{Vendors: make(map[string]vendorDiscovery, len(cache.Vendors))}
	for vendor, entry := range cache.Vendors {
		models := make([]cachedModel, 0, len(entry.Models))
		for _, model := range entry.Models {
			if contracts.Route(model.Route) != contracts.RouteGateway {
				models = append(models, model)
			}
		}
		entry.Models = models
		kept.Vendors[vendor] = entry
	}
	return kept
}

// StartBackgroundDiscovery refreshes the stale vendor lists in the background.
func StartBackgroundDiscovery(ctx context.Context, plugins []contracts.Plugin) {
	go func() {
		path, err := discoveryCachePath()
		if err != nil {
			slog.Warn("model discovery disabled", "err", err)
			return
		}
		cache, err := loadDiscoveryCache(path)
		if err != nil {
			slog.Warn("model cache unreadable, refreshing from the backends", "path", path, "err", err)
		}
		next, outcomes := refreshDiscovery(ctx, plugins, cache, resolveDiscoverySettings(), time.Now(), false)
		if !anyRefreshed(outcomes) {
			return
		}
		if err := persistRefreshed(path, next, outcomes); err != nil {
			slog.Warn("model cache not saved", "path", path, "err", err)
		}
	}()
}

func resolveDiscoverySettings() discoverySettings {
	return discoverySettings{
		ttl:     durationSetting(envModelsTTL, defaultModelsTTL),
		timeout: durationSetting(envModelsTimeout, defaultModelsTimeout),
		vendors: vendorSetting(envx.Getenv(envModelsDiscover)),
		policy:  ResolvePolicy(envx.Getenv),
	}
}

func (s discoverySettings) enabled(vendor string) bool {
	return s.vendors == nil || s.vendors[vendor]
}

func durationSetting(name string, fallback time.Duration) time.Duration {
	text := strings.TrimSpace(envx.Getenv(name))
	if text == "" {
		return fallback
	}
	value, err := time.ParseDuration(text)
	if err != nil || value <= 0 {
		slog.Warn("ignoring invalid model setting", "setting", name, "value", text)
		return fallback
	}
	return value
}

func vendorSetting(raw string) map[string]bool {
	raw = strings.TrimSpace(raw)
	switch raw {
	case "":
		return nil
	case noneVendors:
		return make(map[string]bool)
	default:
		return splitSet(raw)
	}
}

func splitSet(raw string) map[string]bool {
	set := make(map[string]bool)
	for _, item := range strings.Split(raw, ",") {
		if item = strings.TrimSpace(item); item != "" {
			set[item] = true
		}
	}
	return set
}

func discoveryCachePath() (string, error) {
	if dir := envx.Get("STATE_DIR"); dir != "" {
		return filepath.Join(dir, discoveryCacheName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home for the model cache: %w", err)
	}
	return filepath.Join(home, ".config", "herrscher", discoveryCacheName), nil
}

func loadDiscoveryCache(path string) (discoveryCache, error) {
	empty := discoveryCache{Vendors: make(map[string]vendorDiscovery)}
	data, err := os.ReadFile(filepath.Clean(path))
	if errors.Is(err, fs.ErrNotExist) {
		return empty, nil
	}
	if err != nil {
		return empty, fmt.Errorf("read model cache: %w", err)
	}
	var cache discoveryCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return empty, fmt.Errorf("decode model cache: %w", err)
	}
	if cache.Vendors == nil {
		cache.Vendors = make(map[string]vendorDiscovery)
	}
	return cache, nil
}

func saveDiscoveryCache(path string, cache discoveryCache) error {
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return fmt.Errorf("encode model cache: %w", err)
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("open model cache dir: %w", err)
	}
	defer func() { logCleanup(root.Close()) }()
	name := filepath.Base(path)
	tempName := fmt.Sprintf(".%s-%d.tmp", name, time.Now().UnixNano())
	temp, err := root.OpenFile(tempName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, cacheFileMode)
	if err != nil {
		return fmt.Errorf("create model cache temp file: %w", err)
	}
	if err := writeAndClose(temp, data); err != nil {
		logCleanup(root.Remove(tempName))
		return fmt.Errorf("write model cache: %w", err)
	}
	if err := root.Rename(tempName, name); err != nil {
		logCleanup(root.Remove(tempName))
		return fmt.Errorf("replace model cache: %w", err)
	}
	return nil
}

func persistRefreshed(path string, next discoveryCache, outcomes []discoveryOutcome) error {
	latest, err := loadDiscoveryCache(path)
	if err != nil {
		slog.Warn("model cache unreadable, writing only the refreshed vendors", "path", path, "err", err)
	}
	for _, outcome := range outcomes {
		if outcome.Refreshed {
			latest.Vendors[outcome.Vendor] = next.Vendors[outcome.Vendor]
		}
	}
	return saveDiscoveryCache(path, latest)
}

func writeAndClose(file *os.File, data []byte) error {
	_, writeErr := file.Write(data)
	return errors.Join(writeErr, file.Close())
}

func (c discoveryCache) stale(vendor string, now time.Time, ttl time.Duration) bool {
	entry, ok := c.Vendors[vendor]
	if !ok {
		return true
	}
	age := now.Sub(entry.FetchedAt)
	return age < 0 || age > ttl
}

func (c discoveryCache) modelsOf(vendor string) []contracts.ModelSpec {
	entry, ok := c.Vendors[vendor]
	if !ok {
		return nil
	}
	specs := make([]contracts.ModelSpec, 0, len(entry.Models))
	for _, model := range entry.Models {
		specs = append(specs, contracts.ModelSpec{
			ID: model.ID, Label: model.Label, Arg: model.Arg,
			Efforts: model.Efforts, Route: contracts.Route(model.Route),
		})
	}
	return specs
}

func refreshDiscovery(ctx context.Context, plugins []contracts.Plugin, cache discoveryCache, settings discoverySettings, now time.Time, force bool) (discoveryCache, []discoveryOutcome) {
	creds, credsErr := loadGatewayCreds(envx.Getenv)
	if credsErr != nil {
		slog.Warn("gateway credentials unusable for model discovery, listing without them", "err", credsErr)
	}
	jobs := discoveryJobs(plugins, cache, settings, now, force)
	access := contracts.DiscoveryAccess{Gateway: creds, Getenv: envx.Getenv}
	answers := askVendors(ctx, jobs, settings.timeout, access)
	next := cloneCache(cache)
	outcomes := make([]discoveryOutcome, 0, len(jobs))
	for _, job := range jobs {
		answer, answered := answers[job.kind]
		if !answered {
			answer = discoveryAnswer{err: errDiscoveryNoAnswer}
		}
		outcomes = append(outcomes, applyAnswer(&next, job, answer, now))
	}
	sort.Slice(outcomes, func(left, right int) bool { return outcomes[left].Vendor < outcomes[right].Vendor })
	return next, outcomes
}

func discoveryJobs(plugins []contracts.Plugin, cache discoveryCache, settings discoverySettings, now time.Time, force bool) []discoveryJob {
	if settings.policy == contracts.PolicyGatewayOnly {
		return nil
	}
	var jobs []discoveryJob
	for idx := range plugins {
		plugin := &plugins[idx]
		kind := plugin.Manifest.Kind
		if plugin.Discover == nil || !settings.enabled(kind) {
			continue
		}
		if !force && !cache.stale(kind, now, settings.ttl) {
			continue
		}
		jobs = append(jobs, discoveryJob{kind: kind, plugin: plugin})
	}
	return jobs
}

func askVendors(ctx context.Context, jobs []discoveryJob, timeout time.Duration, access contracts.DiscoveryAccess) map[string]discoveryAnswer {
	type reply struct {
		kind   string
		answer discoveryAnswer
	}
	replies := make(chan reply, len(jobs))
	for _, job := range jobs {
		go func(job discoveryJob) {
			vendorCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			models, err := job.plugin.Discover(vendorCtx, access)
			replies <- reply{kind: job.kind, answer: discoveryAnswer{models: models, err: err}}
		}(job)
	}
	answers := make(map[string]discoveryAnswer, len(jobs))
	deadline := time.NewTimer(timeout + discoveryGrace)
	defer deadline.Stop()
	for range jobs {
		select {
		case received := <-replies:
			answers[received.kind] = received.answer
		case <-deadline.C:
			return answers
		}
	}
	return answers
}

func applyAnswer(next *discoveryCache, job discoveryJob, answer discoveryAnswer, now time.Time) discoveryOutcome {
	outcome := discoveryOutcome{Vendor: job.kind}
	err := answer.err
	if err == nil {
		_, err = contracts.MergeDiscovered(job.kind, job.plugin.Manifest.Models, answer.models)
	}
	if err != nil {
		_, hadCache := next.Vendors[job.kind]
		outcome.Err, outcome.Kept = err, hadCache
		outcome.Count = len(next.modelsOf(job.kind))
		slog.Warn("model discovery failed, keeping the last known list", "vendor", job.kind, "kept", hadCache, "err", err)
		return outcome
	}
	next.Vendors[job.kind] = vendorDiscovery{FetchedAt: now, Models: toCached(answer.models)}
	outcome.Count, outcome.Refreshed = len(answer.models), true
	return outcome
}

func toCached(models []contracts.ModelSpec) []cachedModel {
	cached := make([]cachedModel, 0, len(models))
	for _, model := range models {
		cached = append(cached, cachedModel{
			ID: model.ID, Label: model.Label, Arg: model.Arg,
			Efforts: model.Efforts, Route: string(model.Route),
		})
	}
	return cached
}

func cloneCache(cache discoveryCache) discoveryCache {
	next := discoveryCache{Vendors: make(map[string]vendorDiscovery, len(cache.Vendors))}
	for vendor, entry := range cache.Vendors {
		next.Vendors[vendor] = entry
	}
	return next
}

func anyRefreshed(outcomes []discoveryOutcome) bool {
	for _, outcome := range outcomes {
		if outcome.Refreshed {
			return true
		}
	}
	return false
}
