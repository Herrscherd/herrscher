package host

import (
	"fmt"
	"sort"

	"github.com/Herrscherd/herrscher-contracts"
)

// CatalogEntry is an offered model plus the backend that serves it. The
// vendor is never shown to the user: it lets the host know which plugin to
// instantiate once the model is chosen.
type CatalogEntry struct {
	Vendor string
	contracts.ModelSpec
}

// Catalog aggregates the Models of every registered backend and applies the
// route policy. The aggregation works off the Manifests, so it never
// instantiates a single backend — that's what lets the app populate its
// selector before any session exists.
//
// An inconsistent catalog (empty ID, duplicate within one backend, duplicate
// across two backends) is an error, not a warning: the daemon must refuse to
// start rather than silently serve a wrong selector.
func Catalog(plugins []contracts.Plugin, policy contracts.RoutePolicy) ([]CatalogEntry, error) {
	out := []CatalogEntry{}
	owner := map[string]string{}
	for _, p := range plugins {
		kind := p.Manifest.Kind
		if err := contracts.ValidateModels(kind, p.Manifest.Models); err != nil {
			return nil, err
		}
		for _, m := range p.Manifest.Models {
			if prev, clash := owner[m.ID]; clash {
				return nil, fmt.Errorf("model ID %q is declared by both %q and %q", m.ID, prev, kind)
			}
			owner[m.ID] = kind
		}
		for _, m := range contracts.FilterModels(p.Manifest.Models, policy) {
			out = append(out, CatalogEntry{Vendor: kind, ModelSpec: m})
		}
	}
	// Stable order: the app renders this list as-is, and an order that shifts
	// between restarts would move entries out from under the cursor.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Vendor != out[j].Vendor {
			return out[i].Vendor < out[j].Vendor
		}
		return out[i].ID < out[j].ID
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
	for _, e := range entries {
		if e.ID == modelID {
			return e, nil
		}
	}
	return CatalogEntry{}, fmt.Errorf("unknown model %q (not offered under route policy %q)", modelID, policy)
}

// ResolvePolicy reads the route policy from the environment. Missing or
// unrecognized, it falls back to PolicyAll — the behavior that predates this
// change. This setting is set by the app when it launches the daemon, not by
// the user: hardening it into a fatal error would break a daemon started by
// hand.
func ResolvePolicy(getenv func(string) string) contracts.RoutePolicy {
	if getenv("HERRSCHER_ROUTE_POLICY") == string(contracts.PolicyGatewayOnly) {
		return contracts.PolicyGatewayOnly
	}
	return contracts.PolicyAll
}
