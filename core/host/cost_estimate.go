package host

import contracts "github.com/Herrscherd/herrscher-contracts"

const tokensPerPriceUnit = 1_000_000

type modelRates struct {
	input, output, cacheRead, cacheWrite float64
}

func ratesOf(spec contracts.ModelSpec) modelRates {
	return modelRates{input: spec.InputPrice, output: spec.OutputPrice, cacheRead: spec.CacheReadPrice, cacheWrite: spec.CacheWritePrice}
}

type priceableTokens struct {
	uncachedInput, output, cacheRead, cacheWrite int
}

func estimateTurnCost(reported float64, tokens priceableTokens, rates modelRates) float64 {
	if reported > 0 {
		return reported
	}
	priced := [...]struct {
		count int
		price float64
	}{
		{tokens.uncachedInput, rates.input},
		{tokens.output, rates.output},
		{tokens.cacheRead, rates.cacheRead},
		{tokens.cacheWrite, rates.cacheWrite},
	}
	total := 0.0
	for _, category := range priced {
		if category.count <= 0 {
			continue
		}
		if category.price <= 0 {
			return 0
		}
		total += float64(category.count) * category.price
	}
	return total / tokensPerPriceUnit
}
