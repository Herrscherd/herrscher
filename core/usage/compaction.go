package usage

import (
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const jevStage = "jev"

// Compaction is what one stage of a compaction did, as the host records it once the
// stack has run, failures included. The stages of one run share its Passage, and a
// failed stage carries the note of its error.
type Compaction struct {
	Stage         string                   `json:"stage"`
	Passage       string                   `json:"passage,omitempty"`
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
	Note          string                   `json:"note,omitempty"`
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
	Saved         float64                          `json:"saved,omitempty"`
	UnpricedRuns  int                              `json:"unpricedRuns,omitempty"`
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

// JevUsage is how the Jev stage fared over a period. Runs counts the attempts of the
// stage, the outcomes done, short and failed, and Succeeded those that ended done or
// short. Sessions counts the sessions with an attempt, and Behind those where the backend
// stage ran after such an attempt in the same passage, a backend skipped leaving no mark.
type JevUsage struct {
	Runs      int `json:"runs"`
	Succeeded int `json:"succeeded"`
	Sessions  int `json:"sessions"`
	Behind    int `json:"behind"`
}

// SucceededShare is the part of the Jev runs that ended done or short, in percent.
func (j JevUsage) SucceededShare() float64 {
	return share(j.Succeeded, j.Runs)
}

// BehindShare is the part of the sessions with a Jev run where the backend stage had to
// run after it, in percent.
func (j JevUsage) BehindShare() float64 {
	return share(j.Behind, j.Sessions)
}

func share(part, whole int) float64 {
	if whole == 0 {
		return 0
	}
	return float64(part) / float64(whole) * percentScale
}

// LastCompaction is the most recent run of a stage in the ledger.
type LastCompaction struct {
	At      time.Time
	Outcome contracts.CompactOutcome
	Note    string
}

// LastCompactions returns the latest run of each stage among the records, the records of
// the other kinds ignored.
func LastCompactions(records []Record) map[string]LastCompaction {
	return latestRuns(records, anyOutcome)
}

// LastSucceeded returns the latest run of each stage that ended done or short.
func LastSucceeded(records []Record) map[string]LastCompaction {
	return latestRuns(records, succeeded)
}

// LastFailed returns the latest failed run of each stage, with the note of its error.
func LastFailed(records []Record) map[string]LastCompaction {
	return latestRuns(records, failed)
}

func anyOutcome(contracts.CompactOutcome) bool {
	return true
}

func succeeded(outcome contracts.CompactOutcome) bool {
	return outcome == contracts.CompactDone || outcome == contracts.CompactShort
}

func failed(outcome contracts.CompactOutcome) bool {
	return outcome == contracts.CompactFailed
}

func attempted(outcome contracts.CompactOutcome) bool {
	return succeeded(outcome) || failed(outcome)
}

func latestRuns(records []Record, keep func(contracts.CompactOutcome) bool) map[string]LastCompaction {
	last := make(map[string]LastCompaction)
	for index := range records {
		entry := records[index].Compaction
		if entry == nil || !keep(entry.Outcome) {
			continue
		}
		if known, ok := last[entry.Stage]; ok && known.At.After(records[index].Time) {
			continue
		}
		last[entry.Stage] = LastCompaction{At: records[index].Time, Outcome: entry.Outcome, Note: entry.Note}
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
		stages[entry.Stage] = stage.add(&records[index])
	}
	out := make([]StageUsage, 0, len(order))
	for _, name := range order {
		out = append(out, stages[name])
	}
	return out
}

func (s StageUsage) add(record *Record) StageUsage {
	entry := record.Compaction
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
	s.Saved += record.Saved
	if record.Unpriced {
		s.UnpricedRuns++
	}
	return s
}

type passage struct {
	session string
	id      string
}

type jevTally struct {
	jev      JevUsage
	sessions map[string]bool
	behind   map[string]bool
	ran      map[passage]bool
}

func jevUsages(records []Record, since time.Time, key AccountKey) JevUsage {
	kept := make([]*Record, 0, len(records))
	for index := range records {
		record := &records[index]
		if record.Compaction != nil && !record.Time.Before(since) && key.Matches(record.Vendor, record.Account) {
			kept = append(kept, record)
		}
	}
	tally := jevTally{sessions: make(map[string]bool), behind: make(map[string]bool), ran: make(map[passage]bool)}
	for _, record := range kept {
		if record.Compaction.Stage == jevStage {
			tally.addJevRun(record)
		}
	}
	for _, record := range kept {
		if record.Compaction.Stage == contracts.CompactStageBackend {
			tally.addBackend(record)
		}
	}
	jev := tally.jev
	jev.Sessions = len(tally.sessions)
	jev.Behind = len(tally.behind)
	return jev
}

func (t *jevTally) addJevRun(record *Record) {
	outcome := record.Compaction.Outcome
	if !attempted(outcome) {
		return
	}
	t.jev.Runs++
	if succeeded(outcome) {
		t.jev.Succeeded++
	}
	t.sessions[record.Session] = true
	if id := record.Compaction.Passage; id != "" {
		t.ran[passage{session: record.Session, id: id}] = true
	}
}

func (t *jevTally) addBackend(record *Record) {
	if record.Compaction.Outcome == contracts.CompactSkipped {
		return
	}
	where := passage{session: record.Session, id: record.Compaction.Passage}
	if where.id != "" && t.ran[where] {
		t.behind[record.Session] = true
	}
}
