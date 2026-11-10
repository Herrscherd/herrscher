package usage

import (
	"math"
	"testing"
)

const priceList = `{
	"sample_spec": {"input_cost_per_token": "doc"},
	"claude-opus-4-1": {"input_cost_per_token": 0.000015, "output_cost_per_token": 0.000075, "cache_read_input_token_cost": 0.0000015, "cache_creation_input_token_cost": 0.00001875, "cache_creation_input_token_cost_above_1hr": 0.00003, "provider_specific_entry": {"fast": 2}},
	"openrouter/openai/gpt-5": {"input_cost_per_token": 0.000002, "output_cost_per_token": 0.00002, "litellm_provider": "openrouter"},
	"openai/gpt-5": {"input_cost_per_token": 0.00000125, "output_cost_per_token": 0.00001, "litellm_provider": "openai"},
	"no-output": {"input_cost_per_token": 0.000001}
}`

func near(got, want float64) bool {
	return math.Abs(got-want) < 1e-9
}

func TestParsePricesKeepsPricedModelsAndPrefersTheFirstPartyAlias(t *testing.T) {
	book, err := ParsePrices([]byte(priceList))
	if err != nil {
		t.Fatal(err)
	}
	gpt, ok := book.Lookup("GPT-5")
	if !ok || !near(gpt.In, 0.00000125) || !near(gpt.CacheRead, gpt.In) {
		t.Fatalf("gpt-5 = %+v, %v, want the openai rates", gpt, ok)
	}
	if _, ok := book.Lookup("claude-opus-4-1[1m]"); !ok {
		t.Fatal("a context suffix hides the model")
	}
	for _, model := range []string{"no-output", "sample_spec", "opus", "<synthetic>", ""} {
		if _, ok := book.Lookup(model); ok {
			t.Errorf("%q is priced", model)
		}
	}
}

func TestPriceSplitsTheCostAndWhatTheCacheSaved(t *testing.T) {
	book, err := ParsePrices([]byte(priceList))
	if err != nil {
		t.Fatal(err)
	}
	records := []Record{
		{Model: "claude-opus-4-1", In: 1000, Out: 100, CacheRead: 10000, CacheCreate: 2000, CacheCreateHour: 500},
		{Model: "claude-opus-4-1", In: 1000, Fast: true},
		{Model: "mystery"},
	}
	book.Price(records)
	split := records[0].Split
	if !near(split.In, 0.015) || !near(split.Out, 0.0075) || !near(split.CacheRead, 0.015) || !near(split.CacheWrite, 1500*0.00001875+500*0.00003) {
		t.Fatalf("split = %+v", split)
	}
	if !near(records[0].Cost, split.Total()) || !near(records[0].Saved, 10000*(0.000015-0.0000015)) {
		t.Fatalf("cost = %v, saved = %v", records[0].Cost, records[0].Saved)
	}
	if !near(records[1].Cost, 0.03) {
		t.Fatalf("fast cost = %v, want the fast multiplier", records[1].Cost)
	}
	if !records[2].Unpriced || records[2].Cost != 0 {
		t.Fatalf("unknown model = %+v", records[2])
	}
}

func TestPriceAppliesTheRatesOfTheLongestContextReached(t *testing.T) {
	book, err := ParsePrices([]byte(`{
		"gemini-2.5-pro": {"input_cost_per_token": 0.00000125, "output_cost_per_token": 0.00001, "cache_read_input_token_cost": 0.000000125,
			"input_cost_per_token_above_200k_tokens": 0.0000025, "output_cost_per_token_above_200k_tokens": 0.000015,
			"input_cost_per_token_above_128k_tokens": 0.000002, "input_cost_per_token_above_200k_tokens_priority": 0.1,
			"input_cost_per_token_above_150k_tokens": 0.2}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	records := []Record{
		{Model: "gemini-2.5-pro", In: 1000, Out: 100, CacheRead: 1000},
		{Model: "gemini-2.5-pro", In: 1000, Out: 100, CacheRead: 1000, Above: 128_000},
		{Model: "gemini-2.5-pro", In: 1000, Out: 100, CacheRead: 1000, Above: 200_000},
	}
	book.Price(records)
	want := []CostSplit{
		{In: 0.00125, Out: 0.001, CacheRead: 0.000125},
		{In: 0.002, Out: 0.001, CacheRead: 0.000125},
		{In: 0.0025, Out: 0.0015, CacheRead: 0.000125},
	}
	for i, record := range records {
		got := record.Split
		if !near(got.In, want[i].In) || !near(got.Out, want[i].Out) || !near(got.CacheRead, want[i].CacheRead) {
			t.Errorf("record %d split = %+v, want %+v", i, got, want[i])
		}
	}
	if !near(records[2].Saved, 1000*(0.0000025-0.000000125)) {
		t.Errorf("saved = %v, want the long context input rate", records[2].Saved)
	}
}
