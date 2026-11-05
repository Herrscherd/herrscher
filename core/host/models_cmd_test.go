package host

import (
	"encoding/json"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestModelsListJSONShape(t *testing.T) {
	entries := []CatalogEntry{{
		Vendor: "claude",
		ModelSpec: contracts.ModelSpec{
			ID: "claude-opus-5", Label: "Opus 5", Arg: "claude-opus-5",
			Efforts: []string{"low", "high"}, Route: contracts.RouteNative, InputPrice: 5,
		},
	}}
	out, err := modelsListJSON(entries)
	if err != nil {
		t.Fatalf("modelsListJSON: %v", err)
	}
	var got struct {
		Models []struct {
			ID         string   `json:"id"`
			Label      string   `json:"label"`
			Efforts    []string `json:"efforts"`
			Route      string   `json:"route"`
			InputPrice float64  `json:"inputPrice"`
			Vendor     string   `json:"vendor"`
		} `json:"models"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if len(got.Models) != 1 {
		t.Fatalf("got %d models, want 1", len(got.Models))
	}
	m := got.Models[0]
	if m.ID != "claude-opus-5" || m.Label != "Opus 5" || m.Route != "native" || m.Vendor != "claude" || m.InputPrice != 5 {
		t.Fatalf("unexpected payload: %+v", m)
	}
	if len(m.Efforts) != 2 {
		t.Fatalf("efforts = %v", m.Efforts)
	}
}

func TestModelsListJSONNeverLeaksArg(t *testing.T) {
	entries := []CatalogEntry{{Vendor: "claude", ModelSpec: contracts.ModelSpec{
		ID: "x", Label: "X", Arg: "SECRET-ARG-VALUE", Route: contracts.RouteNative,
	}}}
	out, listErr := modelsListJSON(entries)
	logCleanup(listErr)
	if strings.Contains(out, "SECRET-ARG-VALUE") {
		t.Fatalf("Arg leaked into the JSON payload: %s", out)
	}
}

func TestModelsListJSONEmptyIsAnArrayNotNull(t *testing.T) {
	out, err := modelsListJSON(nil)
	if err != nil {
		t.Fatalf("modelsListJSON(nil): %v", err)
	}
	if !strings.Contains(out, `"models":[]`) {
		t.Fatalf("empty catalog rendered as %s, want an empty array", out)
	}
}

func TestModelsListJSONCarriesPricesWindowAndHidden(t *testing.T) {
	out, err := modelsListJSON([]CatalogEntry{{Vendor: "claude", ModelSpec: contracts.ModelSpec{
		ID: "claude-long", Label: "Long", Arg: "claude-long", Route: contracts.RouteNative,
		InputPrice: 5, OutputPrice: 25, CacheReadPrice: 0.5, CacheWritePrice: 6.25, ContextWindow: 1_000_000, Hidden: true,
	}}})
	if err != nil {
		t.Fatalf("modelsListJSON: %v", err)
	}
	var got struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if len(got.Models) != 1 {
		t.Fatalf("got %d models, want 1", len(got.Models))
	}
	m := got.Models[0]
	if m["outputPrice"] != 25.0 || m["cacheReadPrice"] != 0.5 || m["cacheWritePrice"] != 6.25 || m["contextWindow"] != 1_000_000.0 || m["hidden"] != true {
		t.Fatalf("payload lacks prices, window or hidden flag: %v", m)
	}
}
