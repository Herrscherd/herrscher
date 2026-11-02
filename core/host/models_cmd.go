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
	for _, e := range entries {
		efforts := e.Efforts
		if efforts == nil {
			efforts = []string{}
		}
		models = append(models, modelJSON{
			ID: e.ID, Label: e.Label, Efforts: efforts,
			Route: string(e.Route), InputPrice: e.InputPrice, Vendor: e.Vendor,
		})
	}
	b, err := json.Marshal(struct {
		Models []modelJSON `json:"models"`
	}{Models: models})
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func modelsListText(entries []CatalogEntry) string {
	if len(entries) == 0 {
		return "no models offered under the current route policy"
	}
	var b strings.Builder
	for _, e := range entries {
		fmt.Fprintf(&b, "%-32s %-24s %-8s %s\n", e.ID, e.Label, e.Route, e.Vendor)
	}
	return strings.TrimRight(b.String(), "\n")
}

// ModelsCommands returns the model catalog verbs.
func ModelsCommands() []contracts.Cmd {
	return []contracts.Cmd{
		contracts.New("models", "list").
			Help("list the models every compiled backend offers, under the active route policy; --json for the app").
			Do(func(ctx context.Context, in contracts.Input) (string, error) {
				entries, err := Catalog(contracts.Default.Backends(), ResolvePolicy(os.Getenv))
				if err != nil {
					return "", err
				}
				if in.JSON {
					return modelsListJSON(entries)
				}
				return modelsListText(entries), nil
			}),
	}
}
