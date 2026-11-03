package host

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

type modelJSON struct {
	ID         string   `json:"id"`
	Label      string   `json:"label"`
	Efforts    []string `json:"efforts"`
	Route      string   `json:"route"`
	InputPrice float64  `json:"inputPrice"`
	Vendor     string   `json:"vendor"`
}

func modelsListJSON(entries []CatalogEntry) (string, error) {
	models := make([]modelJSON, 0, len(entries))
	for _, entry := range entries {
		efforts := entry.Efforts
		if efforts == nil {
			efforts = []string{}
		}
		models = append(models, modelJSON{
			ID: entry.ID, Label: entry.Label, Efforts: efforts,
			Route: string(entry.Route), InputPrice: entry.InputPrice, Vendor: entry.Vendor,
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
	for _, e := range entries {
		fmt.Fprintf(&builder, "%-32s %-24s %-8s %s\n", e.ID, e.Label, e.Route, e.Vendor)
	}
	return strings.TrimRight(builder.String(), "\n")
}

// ModelsCommands returns the model catalog verbs.
func ModelsCommands() []contracts.Cmd {
	return []contracts.Cmd{
		contracts.New("models", "list").
			Help("list the models every compiled backend offers, under the active route policy; --json for the app").
			Do(func(_ context.Context, input contracts.Input) (string, error) {
				entries, err := Catalog(contracts.Default.Backends(), ResolvePolicy(os.Getenv))
				if err != nil {
					return "", err
				}
				if input.JSON {
					return modelsListJSON(entries)
				}
				return modelsListText(entries), nil
			}),
	}
}
