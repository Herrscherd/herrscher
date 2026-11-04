package host

import (
	"log/slog"
	"sort"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// CatalogEntry is an offered model plus the backend that serves it. The
// vendor is never shown to the user: it lets the host know which plugin to
// instantiate once the model is chosen.
type CatalogEntry struct {
	contracts.ModelSpec

	Vendor string
	Source string
}

// Catalog aggregates the Models of every registered backend and applies the
// route policy. The aggregation works off the Manifests, so it never
// instantiates a single backend, which is what lets the app populate its
// selector before any session exists.
//
// An inconsistent catalog (empty ID, duplicate within one backend, duplicate
// across two backends) is an error, not a warning: the daemon must refuse to
// start rather than silently serve a wrong selector.
func Catalog(plugins []contracts.Plugin, policy contracts.RoutePolicy) ([]CatalogEntry, error) {
	return CatalogWith(plugins, policy, Sources{})
}

// CatalogWith is Catalog with the discovered models and the hidden IDs of the given sources applied.
func CatalogWith(plugins []contracts.Plugin, policy contracts.RoutePolicy, sources Sources) ([]CatalogEntry, error) {
	return buildCatalog(plugins, policy, sources, sources.hidden)
}

func buildCatalog(plugins []contracts.Plugin, policy contracts.RoutePolicy, sources Sources, hidden map[string]bool) ([]CatalogEntry, error) {
	owner := make(map[string]string)
	for idx := range plugins {
		plugin := &plugins[idx]
		kind := plugin.Manifest.Kind
		if err := contracts.ValidateModels(kind, plugin.Manifest.Models); err != nil {
			return nil, err
		}
		if err := claimModelIDs(owner, kind, plugin.Manifest.Models); err != nil {
			return nil, err
		}
	}
	out := []CatalogEntry{}
	for idx := range plugins {
		plugin := &plugins[idx]
		kind := plugin.Manifest.Kind
		static := plugin.Manifest.Models
		merged := mergedModels(kind, static, sources.discovered, owner)
		out = append(out, offeredEntries(kind, static, merged, policy, hidden)...)
	}
	sort.SliceStable(out, func(left, right int) bool {
		if out[left].Vendor != out[right].Vendor {
			return out[left].Vendor < out[right].Vendor
		}
		return out[left].ID < out[right].ID
	})
	return out, nil
}

func claimModelIDs(owner map[string]string, kind string, models []contracts.ModelSpec) error {
	for idx := range models {
		modelID := models[idx].ID
		if prev, clash := owner[modelID]; clash {
			return &duplicateModelError{ID: modelID, First: prev, Second: kind}
		}
		owner[modelID] = kind
	}
	return nil
}

func offeredEntries(kind string, static, merged []contracts.ModelSpec, policy contracts.RoutePolicy, hidden map[string]bool) []CatalogEntry {
	staticIDs := make(map[string]bool, len(static))
	for idx := range static {
		staticIDs[static[idx].ID] = true
	}
	var entries []CatalogEntry
	for _, model := range contracts.FilterModels(merged, policy) {
		if hidden[model.ID] {
			continue
		}
		source := sourceDiscovered
		if staticIDs[model.ID] {
			source = sourceCatalog
		}
		entries = append(entries, CatalogEntry{Vendor: kind, ModelSpec: model, Source: source})
	}
	return entries
}

// LookupModel resolves a model ID to its catalog entry, under the given
// policy. A model the policy excludes is NOT FOUND, not merely hidden: a
// persisted session naming it fails to resume, which is the intended
// behavior when a public build inherits internal state.
func LookupModel(plugins []contracts.Plugin, policy contracts.RoutePolicy, modelID string) (CatalogEntry, error) {
	return LookupModelWith(plugins, policy, Sources{}, modelID)
}

// LookupModelWith is LookupModel over the catalog with the given sources applied.
func LookupModelWith(plugins []contracts.Plugin, policy contracts.RoutePolicy, sources Sources, modelID string) (CatalogEntry, error) {
	entries, err := buildCatalog(plugins, policy, sources, nil)
	if err != nil {
		return CatalogEntry{}, err
	}
	for idx := range entries {
		if entries[idx].ID == modelID {
			return entries[idx], nil
		}
	}
	name := string(policy)
	if name == "" {
		name = "all"
	}
	return CatalogEntry{}, &unknownModelError{Model: modelID, Policy: name}
}

// EnvRoutePolicy selects which models are offered; EnvDefaultModel names the one
// a session gets when it creates without --model. Both are written by
// `herrscher init` and read here, so the wizard and the daemon cannot drift.
const (
	EnvRoutePolicy  = "HERRSCHER_ROUTE_POLICY"
	EnvDefaultModel = "HERRSCHER_DEFAULT_MODEL"
)

// VendorDefaultModelEnv names the variable holding the default model of one vendor.
func VendorDefaultModelEnv(vendor string) string {
	return "HERRSCHER_DEFAULT_MODEL_" + strings.ToUpper(strings.ReplaceAll(vendor, "-", "_"))
}

// ResolvePolicy reads the route policy from the environment. Missing or
// unrecognized, it falls back to PolicyAll, the behavior that predates this
// change. This setting is set by the app when it launches the daemon, not by
// the user: hardening it into a fatal error would break a daemon started by
// hand.
func ResolvePolicy(getenv func(string) string) contracts.RoutePolicy {
	if getenv(EnvRoutePolicy) == string(contracts.PolicyGatewayOnly) {
		return contracts.PolicyGatewayOnly
	}
	return contracts.PolicyAll
}

func mergedModels(kind string, static []contracts.ModelSpec, discovered discoveryCache, owner map[string]string) []contracts.ModelSpec {
	cached := unclaimedBy(kind, discovered.modelsOf(kind), owner)
	if len(cached) == 0 {
		return static
	}
	merged, err := contracts.MergeDiscovered(kind, static, cached)
	if err != nil {
		slog.Warn("cached models rejected, using the static catalog", "vendor", kind, "err", err)
		return static
	}
	claimDiscoveredIDs(owner, kind, merged)
	return merged
}

func unclaimedBy(kind string, models []contracts.ModelSpec, owner map[string]string) []contracts.ModelSpec {
	kept := make([]contracts.ModelSpec, 0, len(models))
	for _, model := range models {
		if other, taken := owner[model.ID]; taken && other != kind {
			slog.Warn("discovered model ID is served by another backend, discarded", "vendor", kind, "model", model.ID, "owner", other)
			continue
		}
		kept = append(kept, model)
	}
	return kept
}

func claimDiscoveredIDs(owner map[string]string, kind string, models []contracts.ModelSpec) {
	for idx := range models {
		if _, taken := owner[models[idx].ID]; !taken {
			owner[models[idx].ID] = kind
		}
	}
}
