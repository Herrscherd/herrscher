package usage

import (
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// Compaction is what one stage of a compaction did, as the host records it once the
// stack has run, failures included.
type Compaction struct {
	Stage         string                   `json:"stage"`
	Outcome       contracts.CompactOutcome `json:"outcome"`
	Calls         int                      `json:"calls"`
	Kept          int                      `json:"kept"`
	Truncated     int                      `json:"truncated"`
	Pruned        int                      `json:"pruned"`
	CharsBefore   int                      `json:"charsBefore"`
	CharsAfter    int                      `json:"charsAfter"`
	TokensRemoved int                      `json:"tokensRemoved"`
	Requests      int                      `json:"requests"`
	RequestTokens int                      `json:"requestTokens"`
	Duration      time.Duration            `json:"duration"`
}

// StageUsage is what the compactions of one stage did over a period, summed over its runs.
type StageUsage struct {
	Stage         string                           `json:"stage"`
	Runs          int                              `json:"runs"`
	Outcomes      map[contracts.CompactOutcome]int `json:"outcomes,omitempty"`
	Calls         int                              `json:"calls"`
	Kept          int                              `json:"kept"`
	Truncated     int                              `json:"truncated"`
	Pruned        int                              `json:"pruned"`
	CharsBefore   int64                            `json:"charsBefore"`
	CharsAfter    int64                            `json:"charsAfter"`
	TokensRemoved int64                            `json:"tokensRemoved"`
	Requests      int64                            `json:"requests"`
	RequestTokens int64                            `json:"requestTokens"`
	Duration      time.Duration                    `json:"duration"`
}

// CharsRemoved is the share of the characters the stage found that it removed, in percent.
func (s StageUsage) CharsRemoved() float64 {
	if s.CharsBefore == 0 {
		return 0
	}
	return float64(s.CharsBefore-s.CharsAfter) / float64(s.CharsBefore) * percentScale
}

// AverageDuration is the mean time one run of the stage took.
func (s StageUsage) AverageDuration() time.Duration {
	if s.Runs == 0 {
		return 0
	}
	return s.Duration / time.Duration(s.Runs)
}

// LastCompaction is the most recent run of a stage in the ledger.
type LastCompaction struct {
	At      time.Time
	Outcome contracts.CompactOutcome
}

// LastCompactions returns the latest run of each stage among the records, the records of
// the other kinds ignored.
func LastCompactions(records []Record) map[string]LastCompaction {
	last := make(map[string]LastCompaction)
	for index := range records {
		entry := records[index].Compaction
		if entry == nil {
			continue
		}
		if known, ok := last[entry.Stage]; ok && known.At.After(records[index].Time) {
			continue
		}
		last[entry.Stage] = LastCompaction{At: records[index].Time, Outcome: entry.Outcome}
	}
	return last
}

func stageUsages(records []Record, since time.Time, key AccountKey) []StageUsage {
	var order []string
	stages := make(map[string]StageUsage)
	for index := range records {
		entry := records[index].Compaction
		if entry == nil || records[index].Time.Before(since) || !key.Matches(records[index].Vendor, records[index].Account) {
			continue
		}
		stage, ok := stages[entry.Stage]
		if !ok {
			stage = StageUsage{Stage: entry.Stage, Outcomes: make(map[contracts.CompactOutcome]int)}
			order = append(order, entry.Stage)
		}
		stages[entry.Stage] = stage.add(entry)
	}
	out := make([]StageUsage, 0, len(order))
	for _, name := range order {
		out = append(out, stages[name])
	}
	return out
}

func (s StageUsage) add(entry *Compaction) StageUsage {
	s.Runs++
	s.Outcomes[entry.Outcome]++
	s.Calls += entry.Calls
	s.Kept += entry.Kept
	s.Truncated += entry.Truncated
	s.Pruned += entry.Pruned
	s.CharsBefore += int64(entry.CharsBefore)
	s.CharsAfter += int64(entry.CharsAfter)
	s.TokensRemoved += int64(entry.TokensRemoved)
	s.Requests += int64(entry.Requests)
	s.RequestTokens += int64(entry.RequestTokens)
	s.Duration += entry.Duration
	return s
}
