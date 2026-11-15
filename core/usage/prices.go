package usage

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// PriceBook is the rate of every model of the LiteLLM price list, by lowercase model
// name, a bare name standing for its provider-prefixed entry.
type PriceBook struct {
	rates map[string]Rates
}

// Rates is what one token of each type costs, in dollars, the multiplier the fast speed
// applies to every rate, and the rates of the requests whose context goes past a
// threshold, by threshold.
type Rates struct {
	In             float64
	Out            float64
	CacheRead      float64
	CacheWrite     float64
	CacheWriteHour float64
	Fast           float64
	Long           map[int64]TokenRates
}

// TokenRates is what one token of each type costs, in dollars.
type TokenRates struct {
	In             float64
	Out            float64
	CacheRead      float64
	CacheWrite     float64
	CacheWriteHour float64
}

const (
	longContextInfix  = "_above_"
	longContextSuffix = "k_tokens"
	tokensPerK        = 1000
)

func contextThresholds() []int64 {
	return []int64{32_000, 100_000, 128_000, 200_000, 256_000, 272_000, 512_000}
}

func longContextFields() map[string]func(*TokenRates, float64) {
	return map[string]func(*TokenRates, float64){
		"input_cost_per_token":                      func(r *TokenRates, v float64) { r.In = v },
		"output_cost_per_token":                     func(r *TokenRates, v float64) { r.Out = v },
		"cache_read_input_token_cost":               func(r *TokenRates, v float64) { r.CacheRead = v },
		"cache_creation_input_token_cost":           func(r *TokenRates, v float64) { r.CacheWrite = v },
		"cache_creation_input_token_cost_above_1hr": func(r *TokenRates, v float64) { r.CacheWriteHour = v },
	}
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
		rates[name] = entry.rates(entries[key])
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

func (e priceEntry) rates(raw json.RawMessage) Rates {
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
	rates.Long = longContextRates(raw, rates.tokens())
	return rates
}

func longContextRates(raw json.RawMessage, base TokenRates) map[int64]TokenRates {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil
	}
	setters := longContextFields()
	var long map[int64]TokenRates
	for name, value := range fields {
		field, threshold, ok := longContextField(name)
		set, known := setters[field]
		var rate float64
		if !ok || !known || json.Unmarshal(value, &rate) != nil {
			continue
		}
		if long == nil {
			long = make(map[int64]TokenRates)
		}
		tier, seen := long[threshold]
		if !seen {
			tier = base
		}
		set(&tier, rate)
		long[threshold] = tier
	}
	return long
}

func longContextField(name string) (string, int64, bool) {
	cut := strings.LastIndex(name, longContextInfix)
	if cut < 0 || !strings.HasSuffix(name, longContextSuffix) {
		return "", 0, false
	}
	thousands, err := strconv.ParseInt(strings.TrimSuffix(name[cut+len(longContextInfix):], longContextSuffix), 10, 64)
	threshold := thousands * tokensPerK
	if err != nil || !slices.Contains(contextThresholds(), threshold) {
		return "", 0, false
	}
	return name[:cut], threshold, true
}

func (r Rates) tokens() TokenRates {
	return TokenRates{In: r.In, Out: r.Out, CacheRead: r.CacheRead, CacheWrite: r.CacheWrite, CacheWriteHour: r.CacheWriteHour}
}

func (r Rates) at(above int64) TokenRates {
	tokens := r.tokens()
	var reached int64
	for threshold, long := range r.Long {
		if threshold <= above && threshold > reached {
			tokens, reached = long, threshold
		}
	}
	return tokens
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
// at the cache read rate of its model, which falls back to the input rate when the list
// gives none. A record whose model is not in the list, or is priced at zero, is marked
// unpriced instead, and its saving left at zero.
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
	rates := r.at(record.LongTier)
	hour := min(record.CacheCreateHour, record.CacheCreate)
	split := CostSplit{
		In:         float64(record.In) * rates.In * speed,
		Out:        float64(record.Out) * rates.Out * speed,
		CacheRead:  float64(record.CacheRead) * rates.CacheRead * speed,
		CacheWrite: (float64(record.CacheCreate-hour)*rates.CacheWrite + float64(hour)*rates.CacheWriteHour) * speed,
	}
	saved := float64(record.CacheRead) * max(rates.In-rates.CacheRead, 0) * speed
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
