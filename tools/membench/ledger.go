package main

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/Herrscherd/herrscher/core/usage"
)

const usageSubdir = "usage"

type tally struct {
	turns  int
	input  int64
	tokens int64
	usd    float64
}

func (t tally) minus(earlier tally) tally {
	return tally{
		turns:  t.turns - earlier.turns,
		input:  t.input - earlier.input,
		tokens: t.tokens - earlier.tokens,
		usd:    t.usd - earlier.usd,
	}
}

func (t tally) spend() spend {
	return spend{tokens: t.tokens, usd: t.usd}
}

func readTally(stateDir string) (tally, error) {
	ledger := usage.Ledger{Dir: filepath.Join(stateDir, usageSubdir), Now: time.Now}
	records, err := ledger.Records(usage.Range24h)
	var total tally
	for i := range records {
		record := records[i]
		total.turns++
		total.input += record.In
		total.tokens += record.In + record.Out + record.CacheRead + record.CacheCreate
		total.usd += record.Cost
	}
	if err != nil {
		return total, fmt.Errorf("membench usage: %w", err)
	}
	return total, nil
}
