package host

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/envx"
)

type modelJSON struct {
	ID         string   `json:"id"`
	Label      string   `json:"label"`
	Efforts    []string `json:"efforts"`
	Route      string   `json:"route"`
	InputPrice float64  `json:"inputPrice"`
	Vendor     string   `json:"vendor"`
	Source     string   `json:"source"`
}

func modelsListJSON(entries []CatalogEntry) (string, error) {
	models := make([]modelJSON, 0, len(entries))
	for idx := range entries {
		entry := &entries[idx]
		efforts := entry.Efforts
		if efforts == nil {
			efforts = []string{}
		}
		models = append(models, modelJSON{
			ID: entry.ID, Label: entry.Label, Efforts: efforts,
			Route: string(entry.Route), InputPrice: entry.InputPrice,
			Vendor: entry.Vendor, Source: entry.Source,
		})
	}
	payload, err := json.Marshal(struct {
		Models []modelJSON `json:"models"`
	}{Models: models})
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

func modelsListText(entries []CatalogEntry) string {
	if len(entries) == 0 {
		return "no models offered under the current route policy"
	}
	var builder strings.Builder
	for idx := range entries {
		e := &entries[idx]
		fmt.Fprintf(&builder, "%-32s %-24s %-8s %-10s %s\n", e.ID, e.Label, e.Route, e.Source, e.Vendor)
	}
	return strings.TrimRight(builder.String(), "\n")
}

func modelsRefreshText(outcomes []discoveryOutcome) string {
	if len(outcomes) == 0 {
		return "no vendor is due for discovery (see HERRSCHER_MODELS_DISCOVER and HERRSCHER_ROUTE_POLICY)"
	}
	var builder strings.Builder
	for _, outcome := range outcomes {
		fmt.Fprintf(&builder, "%-12s %s\n", outcome.Vendor, outcomeSummary(outcome))
	}
	return strings.TrimRight(builder.String(), "\n")
}

func outcomeSummary(outcome discoveryOutcome) string {
	switch {
	case outcome.Refreshed:
		return fmt.Sprintf("%d model(s) discovered", outcome.Count)
	case outcome.Kept:
		return fmt.Sprintf("discovery failed (%v), kept the last known list", outcome.Err)
	default:
		return fmt.Sprintf("discovery failed (%v), static catalog only", outcome.Err)
	}
}

func refreshModels(ctx context.Context) (string, error) {
	path, err := discoveryCachePath()
	if err != nil {
		return "", err
	}
	cache, err := loadDiscoveryCache(path)
	if err != nil {
		return "", err
	}
	next, outcomes := refreshDiscovery(ctx, contracts.Default.Backends(), cache, resolveDiscoverySettings(), time.Now(), true)
	if anyRefreshed(outcomes) {
		if err := persistRefreshed(path, next, outcomes); err != nil {
			return "", err
		}
	}
	return modelsRefreshText(outcomes), nil
}

// ModelsCommands returns the model catalog verbs.
func ModelsCommands() []contracts.Cmd {
	return []contracts.Cmd{
		contracts.New("models", "list").
			Help("list the models every compiled backend offers, under the active route policy, with their source; --json for the app").
			Do(func(_ context.Context, input contracts.Input) (string, error) {
				entries, err := CatalogWith(contracts.Default.Backends(), ResolvePolicy(envx.Getenv), LoadSources())
				if err != nil {
					return "", err
				}
				if input.JSON {
					return modelsListJSON(entries)
				}
				return modelsListText(entries), nil
			}),
		contracts.New("models", "refresh").
			Help("ask every backend for its live model list now and store it; a failed backend keeps its last known list").
			Do(func(ctx context.Context, _ contracts.Input) (string, error) {
				return refreshModels(ctx)
			}),
	}
}
