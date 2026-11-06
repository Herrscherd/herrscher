package host

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Herrscherd/herrscher/core/cli"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/envx"
)

type modelJSON struct {
	ID              string   `json:"id"`
	Label           string   `json:"label"`
	Efforts         []string `json:"efforts"`
	Route           string   `json:"route"`
	InputPrice      float64  `json:"inputPrice"`
	OutputPrice     float64  `json:"outputPrice"`
	CacheReadPrice  float64  `json:"cacheReadPrice"`
	CacheWritePrice float64  `json:"cacheWritePrice"`
	ContextWindow   int      `json:"contextWindow"`
	Hidden          bool     `json:"hidden"`
	Vendor          string   `json:"vendor"`
	Source          string   `json:"source"`
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
			OutputPrice: entry.OutputPrice, CacheReadPrice: entry.CacheReadPrice, CacheWritePrice: entry.CacheWritePrice,
			ContextWindow: entry.ContextWindow, Hidden: entry.Hidden,
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

func modelsListText(lang i18n.Language, entries []CatalogEntry) string {
	if len(entries) == 0 {
		return replyText(lang).Text(clitext.HostModelsNone, nil)
	}
	var builder strings.Builder
	for idx := range entries {
		entry := &entries[idx]
		fmt.Fprintf(&builder, "%-32s %-24s %-8s %-10s %s\n", entry.ID, entry.Label, entry.Route, entry.Source, entry.Vendor)
		if entry.Hidden {
			builder.WriteString(replyText(lang).Text(clitext.HostModelsHidden, nil) + "\n")
		}
	}
	return strings.TrimRight(builder.String(), "\n")
}

func modelsRefreshText(lang i18n.Language, outcomes []discoveryOutcome) string {
	if len(outcomes) == 0 {
		return replyText(lang).Text(clitext.HostModelsRefreshNone, nil)
	}
	var builder strings.Builder
	for _, outcome := range outcomes {
		fmt.Fprintf(&builder, "%-12s %s\n", outcome.Vendor, outcomeSummary(lang, outcome))
	}
	return strings.TrimRight(builder.String(), "\n")
}

func outcomeSummary(lang i18n.Language, outcome discoveryOutcome) string {
	switch {
	case outcome.Refreshed:
		return replyText(lang).Text(clitext.HostModelsDiscovered, clitext.Params{clitext.ParamCount: outcome.Count})
	case outcome.Kept:
		return replyText(lang).Text(clitext.HostModelsDiscoveryKept, clitext.Params{clitext.ParamErr: outcome.Err})
	default:
		return replyText(lang).Text(clitext.HostModelsDiscoveryStatic, clitext.Params{clitext.ParamErr: outcome.Err})
	}
}

func listCatalog(all bool, plugins []contracts.Plugin, policy contracts.RoutePolicy, sources Sources) ([]CatalogEntry, error) {
	if all {
		return CatalogAll(plugins, policy, sources)
	}
	return CatalogWith(plugins, policy, sources)
}

func modelDiscoveryFor(input contracts.Input) *ModelDiscovery {
	statePath := input.Get("state")
	if statePath == "" {
		statePath = DefaultStatePath()
	}
	return ModelDiscoveryFor(statePath)
}

func refreshModels(ctx context.Context, lang i18n.Language, plugins []contracts.Plugin, models *ModelDiscovery) (string, error) {
	path := models.cachePath()
	cache, err := loadDiscoveryCache(path)
	if err != nil {
		return "", err
	}
	next, outcomes := refreshDiscovery(ctx, plugins, cache, resolveDiscoverySettings(), time.Now(), true)
	if anyRefreshed(outcomes) {
		if err := persistRefreshed(path, next, outcomes); err != nil {
			return "", err
		}
	}
	return modelsRefreshText(lang, outcomes), nil
}

// ModelsCommands returns the model catalog verbs.
func ModelsCommands(lang i18n.Language) []contracts.Cmd {
	return []contracts.Cmd{
		contracts.New("models", "list").
			ValueParam("state", "daemon state file whose model cache is read (default: the daemon default)", false).
			Param("all", "also list the hidden models, marked hidden, which still resolve when requested", false).
			Help("list the models every compiled backend offers, under the active route policy, with their source; --all adds the hidden ones; --json for the app").
			Do(func(_ context.Context, input contracts.Input) (contracts.Result, error) {
				entries, err := listCatalog(input.Bool("all"), contracts.Default.Backends(), ResolvePolicy(envx.Getenv), modelDiscoveryFor(input).Sources())
				if err != nil {
					return contracts.Result{}, err
				}
				if input.JSON {
					return cli.Data(modelsListJSON(entries))
				}
				return contracts.DataResult(modelsListText(lang, entries)), nil
			}),
		contracts.New("models", "refresh").
			ValueParam("state", "daemon state file whose model cache is written (default: the daemon default)", false).
			Help("ask every backend for its live model list now and store it; a failed backend keeps its last known list").
			Do(func(ctx context.Context, input contracts.Input) (contracts.Result, error) {
				return cli.Data(refreshModels(ctx, lang, contracts.Default.Backends(), modelDiscoveryFor(input)))
			}),
	}
}
