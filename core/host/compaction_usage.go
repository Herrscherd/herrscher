package host

import (
	"log/slog"
	"strconv"
	"time"
	"unicode/utf8"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/usage"
)

const (
	maxStageRunes         = 64
	maxCompactionDuration = 24 * time.Hour
	fellBackNote          = "compaction.fell_back"
	unknownFailureNote    = "compaction.unknown_failure"
)

type stageNotes map[string]string

func (n stageNotes) fail(stage string, err error) {
	if n == nil || err == nil {
		return
	}
	n[stage] = failureClass(err)
}

func failureClass(err error) string {
	if key, known := knownCompactFailure(err); known {
		return key
	}
	return unknownFailureNote
}

func (d *sessionDriver) recordCompaction(stats []contracts.CompactStats, notes stageNotes) {
	if d.sink.Usage == nil || len(stats) == 0 {
		return
	}
	when := time.Now().UTC()
	passage := strconv.FormatInt(when.UnixNano(), 36)
	model := d.modelNow().model
	records := make([]usage.Record, 0, len(stats))
	for index := range stats {
		if _, err := stats[index].Outcome.MarshalText(); err != nil {
			slog.Warn("compaction outcome not recorded", "stage", boundedStage(stats[index].Stage), "err", err)
			continue
		}
		entry := compactionUsage(stats[index])
		entry.Passage = passage
		if entry.Outcome == contracts.CompactFailed {
			entry.Note = failureNote(notes[stats[index].Stage])
		}
		records = append(records, usage.Record{Time: when, Session: d.name, Model: model, Compaction: entry})
	}
	if len(records) > 0 {
		d.sink.Usage(records)
	}
}

func failureNote(class string) string {
	if class == "" {
		return fellBackNote
	}
	return class
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
	return boundedText(stage, maxStageRunes)
}

func boundedText(text string, limit int) string {
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	return string([]rune(text)[:limit])
}
