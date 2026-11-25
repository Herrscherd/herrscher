package host

import (
	"time"

	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/usage"
)

func (s *usageSource) injections(records []usage.Record, since time.Time) map[string]usage.Injection {
	injected := make(map[string]usage.Injection)
	for index := range records {
		name := records[index].Session
		if records[index].Compaction != nil {
			continue
		}
		if _, seen := injected[name]; seen {
			continue
		}
		injected[name] = injectionOf(state.ReadTranscript(state.TranscriptPath(s.partDir, name), 0), since)
	}
	return injected
}

func injectionOf(entries []state.TranscriptEntry, since time.Time) usage.Injection {
	var injection usage.Injection
	for index := range entries {
		entry := entries[index]
		if entry.Role != "assistant" || len(entry.Injected) == 0 {
			continue
		}
		at, err := time.Parse(time.RFC3339, entry.Ts)
		if err != nil || at.Before(since) {
			continue
		}
		injection.Turns++
		for _, part := range entry.Injected {
			injection.Tokens += int64(part.Tokens)
		}
	}
	return injection
}
