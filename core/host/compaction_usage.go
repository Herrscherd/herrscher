package host

import (
	"time"
	"unicode/utf8"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/usage"
)

const (
	maxStageRunes         = 64
	maxCompactionDuration = 24 * time.Hour
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
	charsBefore := max(stats.CharsBefore, 0)
	return &usage.Compaction{
		Stage:         boundedStage(stats.Stage),
		Outcome:       stats.Outcome,
		Calls:         max(stats.Calls, 0),
		Kept:          max(stats.Kept, 0),
		Truncated:     max(stats.Truncated, 0),
		Pruned:        max(stats.Pruned, 0),
		CharsBefore:   charsBefore,
		CharsAfter:    min(max(stats.CharsAfter, 0), charsBefore),
		TokensRemoved: max(stats.TokensRemoved, 0),
		Requests:      max(stats.Requests, 0),
		RequestTokens: max(stats.RequestTokens, 0),
		Duration:      min(max(stats.Duration, 0), maxCompactionDuration),
	}
}

func boundedStage(stage string) string {
	if utf8.RuneCountInString(stage) <= maxStageRunes {
		return stage
	}
	return string([]rune(stage)[:maxStageRunes])
}
