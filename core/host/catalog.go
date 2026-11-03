package host

import (
	"sort"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// CatalogEntry is an offered model plus the backend that serves it. The
// vendor is never shown to the user: it lets the host know which plugin to
// instantiate once the model is chosen.
type CatalogEntry struct {
	contracts.ModelSpec

	Vendor string
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
	out := []CatalogEntry{}
	owner := make(map[string]string)
	for idx := range plugins {
		plugin := plugins[idx]
		kind := plugin.Manifest.Kind
		if err := contracts.ValidateModels(kind, plugin.Manifest.Models); err != nil {
			return nil, err
		}
		for _, model := range plugin.Manifest.Models {
			if prev, clash := owner[model.ID]; clash {
				return nil, &duplicateModelError{ID: model.ID, First: prev, Second: kind}
			}
			owner[model.ID] = kind
		}
		for _, m := range contracts.FilterModels(plugin.Manifest.Models, policy) {
			out = append(out, CatalogEntry{Vendor: kind, ModelSpec: m})
		}
	}
	sort.SliceStable(out, func(left, right int) bool {
		if out[left].Vendor != out[right].Vendor {
			return out[left].Vendor < out[right].Vendor
		}
		return out[left].ID < out[right].ID
	})
	return out, nil
}

// LookupModel resolves a model ID to its catalog entry, under the given
// policy. A model the policy excludes is NOT FOUND, not merely hidden: a
// persisted session naming it fails to resume, which is the intended
// behavior when a public build inherits internal state.
func LookupModel(plugins []contracts.Plugin, policy contracts.RoutePolicy, modelID string) (CatalogEntry, error) {
	entries, err := Catalog(plugins, policy)
	if err != nil {
		return CatalogEntry{}, err
	}
	for _, entry := range entries {
		if entry.ID == modelID {
			return entry, nil
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
