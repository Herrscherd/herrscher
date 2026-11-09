package clitext

import contracts "github.com/Herrscherd/herrscher-contracts"

// Keys of the compaction usage view and of the compaction lines of the health report.
const (
	CompactionEmpty        = "cli.compaction.empty"
	CompactionRuns         = "cli.compaction.runs"
	CompactionOutcomeCount = "cli.compaction.outcome_count"
	CompactionCalls        = "cli.compaction.calls"
	CompactionChars        = "cli.compaction.chars"
	CompactionSaved        = "cli.compaction.saved"
	CompactionRequests     = "cli.compaction.requests"
	CompactionAverage      = "cli.compaction.average"
	CompactionOutcomeDone  = "cli.compaction.outcome.done"
	CompactionOutcomeShort = "cli.compaction.outcome.short"
	CompactionOutcomeFail  = "cli.compaction.outcome.failed"
	CompactionOutcomeDry   = "cli.compaction.outcome.dry_run"
	CompactionOutcomeSkip  = "cli.compaction.outcome.skipped"
	HealthCompactLast      = "cli.health.facts.compact_last"
	HealthCompactNever     = "cli.health.facts.compact_never"
)

// Placeholder names of the compaction texts, as the catalogues write them.
const (
	ParamOutcome   = "Outcome"
	ParamDate      = "Date"
	ParamLabel     = "Label"
	ParamCalls     = "Calls"
	ParamKept      = "Kept"
	ParamTruncated = "Truncated"
	ParamPruned    = "Pruned"
	ParamPercent   = "Percent"
	ParamTokens    = "Tokens"
	ParamRequests  = "Requests"
)

// CompactOutcomeKey names the text of a compaction outcome, the zero value naming none.
func CompactOutcomeKey(outcome contracts.CompactOutcome) (Key, bool) {
	key, ok := map[contracts.CompactOutcome]Key{
		contracts.CompactDone:    CompactionOutcomeDone,
		contracts.CompactShort:   CompactionOutcomeShort,
		contracts.CompactFailed:  CompactionOutcomeFail,
		contracts.CompactDryRun:  CompactionOutcomeDry,
		contracts.CompactSkipped: CompactionOutcomeSkip,
	}[outcome]
	return key, ok
}

func compactionKeys() map[Key][]string {
	return map[Key][]string{
		CompactionEmpty:        nil,
		CompactionRuns:         {ParamCount},
		CompactionOutcomeCount: {ParamLabel, ParamCount},
		CompactionCalls:        {ParamCalls, ParamKept, ParamTruncated, ParamPruned},
		CompactionChars:        {ParamPercent},
		CompactionSaved:        {ParamTokens},
		CompactionRequests:     {ParamRequests, ParamTokens},
		CompactionAverage:      {ParamDuration},
		CompactionOutcomeDone:  nil,
		CompactionOutcomeShort: nil,
		CompactionOutcomeFail:  nil,
		CompactionOutcomeDry:   nil,
		CompactionOutcomeSkip:  nil,
		HealthCompactLast:      {ParamOutcome, ParamDate},
		HealthCompactNever:     nil,
	}
}
