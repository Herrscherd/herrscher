package usage

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// PriceBook is the rate of every model of the LiteLLM price list, by lowercase model
// name, a bare name standing for its provider-prefixed entry.
type PriceBook struct {
	rates map[string]Rates
}

// Rates is what one token of each type costs, in dollars, and the multiplier the fast
// speed applies to every rate.
type Rates struct {
	In             float64
	Out            float64
	CacheRead      float64
	CacheWrite     float64
	CacheWriteHour float64
	Fast           float64
}

// CostSplit is a cost split by token type.
type CostSplit struct {
	In         float64 `json:"in,omitempty"`
	Out        float64 `json:"out,omitempty"`
	CacheRead  float64 `json:"cacheRead,omitempty"`
	CacheWrite float64 `json:"cacheWrite,omitempty"`
}

// Total is the cost of every token type.
func (c CostSplit) Total() float64 {
	return c.In + c.Out + c.CacheRead + c.CacheWrite
}

func (c CostSplit) plus(other CostSplit) CostSplit {
	return CostSplit{In: c.In + other.In, Out: c.Out + other.Out, CacheRead: c.CacheRead + other.CacheRead, CacheWrite: c.CacheWrite + other.CacheWrite}
}

type priceEntry struct {
	Input          *float64                   `json:"input_cost_per_token"`
	Output         *float64                   `json:"output_cost_per_token"`
	CacheRead      *float64                   `json:"cache_read_input_token_cost"`
	CacheWrite     *float64                   `json:"cache_creation_input_token_cost"`
	CacheWriteHour *float64                   `json:"cache_creation_input_token_cost_above_1hr"`
	Provider       string                     `json:"litellm_provider"`
	Specific       map[string]json.RawMessage `json:"provider_specific_entry"`
}

func firstPartyProviders() []string {
	return []string{"anthropic", "openai"}
}

func unpriceableModels() []string {
	return []string{"<synthetic>", "synthetic", "opus", "sonnet", "haiku", "fable"}
}

// ParsePrices reads the LiteLLM price list, keeping the models that have both an input
// and an output rate.
func ParsePrices(raw []byte) (PriceBook, error) {
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return PriceBook{}, fmt.Errorf("usage prices: %w", err)
	}
	rates := make(map[string]Rates)
	aliases := make(map[string][]aliasCandidate)
	for _, key := range slices.Sorted(maps.Keys(entries)) {
		var entry priceEntry
		if json.Unmarshal(entries[key], &entry) != nil || entry.Input == nil || entry.Output == nil {
			continue
		}
		name := modelKey(key)
		rates[name] = entry.rates()
		if _, bare, ok := strings.Cut(name, "/"); ok {
			aliases[lastSegment(bare)] = append(aliases[lastSegment(bare)], aliasCandidate{provider: entry.Provider, rates: rates[name]})
		}
	}
	for bare, candidates := range aliases {
		if _, ok := rates[bare]; ok {
			continue
		}
		rates[bare] = preferredAlias(candidates)
	}
	return PriceBook{rates: rates}, nil
}

type aliasCandidate struct {
	provider string
	rates    Rates
}

func preferredAlias(candidates []aliasCandidate) Rates {
	for _, candidate := range candidates {
		if slices.Contains(firstPartyProviders(), candidate.provider) {
			return candidate.rates
		}
	}
	return candidates[0].rates
}

func (e priceEntry) rates() Rates {
	rates := Rates{In: *e.Input, Out: *e.Output, CacheRead: *e.Input, CacheWrite: *e.Input, Fast: 1}
	if e.CacheRead != nil {
		rates.CacheRead = *e.CacheRead
	}
	if e.CacheWrite != nil {
		rates.CacheWrite = *e.CacheWrite
	}
	rates.CacheWriteHour = rates.CacheWrite
	if e.CacheWriteHour != nil {
		rates.CacheWriteHour = *e.CacheWriteHour
	}
	var fast float64
	if json.Unmarshal(e.Specific["fast"], &fast) == nil && fast > 0 {
		rates.Fast = fast
	}
	return rates
}

// Lookup finds the rates of a model, by its exact name first, then by its bare name.
func (b PriceBook) Lookup(model string) (Rates, bool) {
	name := modelKey(model)
	if name == "" || slices.Contains(unpriceableModels(), name) {
		return Rates{}, false
	}
	if rates, ok := b.rates[name]; ok {
		return rates, true
	}
	rates, ok := b.rates[lastSegment(name)]
	return rates, ok
}

// Empty tells whether the book knows no model at all.
func (b PriceBook) Empty() bool {
	return len(b.rates) == 0
}

// Price sets the cost of every record from the rates of its model, and marks the
// records whose model has no rate.
func (b PriceBook) Price(records []Record) {
	for index := range records {
		record := &records[index]
		rates, ok := b.Lookup(record.Model)
		if !ok {
			record.Unpriced = true
			continue
		}
		record.Split, record.Saved = rates.cost(*record)
		record.Cost = record.Split.Total()
	}
}

// PriceCompactions sets the saving of every compaction record: the tokens it removed are
// no longer read back from the cache on the turns that follow, so they are counted once
// at the cache read rate of its model. A record whose model has no cache read rate is
// marked unpriced instead, and its saving left at zero.
func (b PriceBook) PriceCompactions(records []Record) {
	for index := range records {
		record := &records[index]
		if record.Compaction == nil {
			continue
		}
		rates, ok := b.Lookup(record.Model)
		record.Unpriced = !ok || rates.CacheRead == 0
		record.Saved = 0
		if !record.Unpriced {
			record.Saved = float64(record.Compaction.TokensRemoved) * rates.CacheRead
		}
	}
}

func (r Rates) cost(record Record) (CostSplit, float64) {
	speed := 1.0
	if record.Fast {
		speed = r.Fast
	}
	hour := min(record.CacheCreateHour, record.CacheCreate)
	split := CostSplit{
		In:         float64(record.In) * r.In * speed,
		Out:        float64(record.Out) * r.Out * speed,
		CacheRead:  float64(record.CacheRead) * r.CacheRead * speed,
		CacheWrite: (float64(record.CacheCreate-hour)*r.CacheWrite + float64(hour)*r.CacheWriteHour) * speed,
	}
	saved := float64(record.CacheRead) * max(r.In-r.CacheRead, 0) * speed
	return split, saved
}

func modelKey(model string) string {
	name := strings.ToLower(strings.TrimSpace(model))
	if open := strings.IndexByte(name, '['); open > 0 && strings.HasSuffix(name, "]") {
		name = name[:open]
	}
	return name
}

func lastSegment(name string) string {
	return name[strings.LastIndexByte(name, '/')+1:]
}
