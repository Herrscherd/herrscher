package host

import (
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/usage"
)

func (d *sessionDriver) recordCompaction(stats []contracts.CompactStats) {
	if d.sink.Usage == nil || len(stats) == 0 {
		return
	}
	when := time.Now().UTC()
	records := make([]usage.Record, 0, len(stats))
	for index := range stats {
		if _, err := stats[index].Outcome.MarshalText(); err != nil {
			continue
		}
		records = append(records, usage.Record{Time: when, Session: d.name, Compaction: compactionUsage(stats[index])})
	}
	if len(records) > 0 {
		d.sink.Usage(records)
	}
}

func compactionUsage(stats contracts.CompactStats) *usage.Compaction {
	return &usage.Compaction{
		Stage:         stats.Stage,
		Outcome:       stats.Outcome,
		Calls:         stats.Calls,
		Kept:          stats.Kept,
		Truncated:     stats.Truncated,
		Pruned:        stats.Pruned,
		CharsBefore:   stats.CharsBefore,
		CharsAfter:    stats.CharsAfter,
		TokensRemoved: stats.TokensRemoved,
		Requests:      stats.Requests,
		RequestTokens: stats.RequestTokens,
		Duration:      stats.Duration,
	}
}
