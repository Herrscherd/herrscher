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
	HealthCompactUnknown   = "cli.health.facts.compact_unknown"
	HealthCompactPartial   = "cli.health.facts.compact_partial"
	HealthCompactReply     = "cli.health.facts.compact_reply"
	HealthCompactError     = "cli.health.facts.compact_error"
	HealthCompactNoNote    = "cli.health.facts.compact_error_no_note"
	HealthCompactKeyOn     = "cli.health.facts.compact_key_present"
	HealthCompactKeyOff    = "cli.health.facts.compact_key_absent"
	HealthNoteBridgeGone   = "cli.health.facts.compact_note.bridge_gone"
	HealthNoteNoFactory    = "cli.health.facts.compact_note.no_compactor_factory"
	HealthNoteNilCompactor = "cli.health.facts.compact_note.nil_compactor"
	HealthNoteNoPlugin     = "cli.health.facts.compact_note.no_compactor_plugin"
	HealthNoteNoHistory    = "cli.health.facts.compact_note.no_history_port"
	HealthNoteHistoryTime  = "cli.health.facts.compact_note.history_timeout"
	HealthNoteHistoryBig   = "cli.health.facts.compact_note.history_too_large"
	HealthNoteUnknown      = "cli.health.facts.compact_note.unknown_failure"
	HealthNoteFellBack     = "cli.health.facts.compact_note.fell_back"
	CompactionJevShare     = "cli.compaction.jev_share"
	CompactionJevBehind    = "cli.compaction.jev_behind"
	CompactionMoney        = "cli.compaction.money"
	CompactionMoneyUnknown = "cli.compaction.money_unknown"
	CompactionMoneyPartial = "cli.compaction.money_partial"
)

// Placeholder names of the compaction texts, as the catalogues write them.
const (
	ParamOutcome   = "Outcome"
	ParamDate      = "Date"
	ParamLabel     = "Label"
	ParamKept      = "Kept"
	ParamTruncated = "Truncated"
	ParamPruned    = "Pruned"
	ParamPercent   = "Percent"
	ParamTokens    = "Tokens"
	ParamSucceeded = "Succeeded"
	ParamBehind    = "Behind"
	ParamMoney     = "Money"
	ParamRuns      = "Runs"
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
		CompactionCalls:        {ParamCount, ParamKept, ParamTruncated, ParamPruned},
		CompactionChars:        {ParamPercent},
		CompactionSaved:        {ParamTokens},
		CompactionRequests:     {ParamCount, ParamTokens},
		CompactionAverage:      {ParamDuration},
		CompactionOutcomeDone:  nil,
		CompactionOutcomeShort: nil,
		CompactionOutcomeFail:  nil,
		CompactionOutcomeDry:   nil,
		CompactionOutcomeSkip:  nil,
		HealthCompactLast:      {ParamOutcome, ParamDate},
		HealthCompactNever:     nil,
		HealthCompactUnknown:   nil,
		HealthCompactPartial:   nil,
		HealthCompactReply:     {ParamOutcome, ParamDate},
		HealthCompactError:     {ParamDate, ParamNote},
		HealthCompactNoNote:    nil,
		HealthCompactKeyOn:     {ParamName},
		HealthCompactKeyOff:    {ParamName},
		HealthNoteBridgeGone:   nil,
		HealthNoteNoFactory:    nil,
		HealthNoteNilCompactor: nil,
		HealthNoteNoPlugin:     nil,
		HealthNoteNoHistory:    nil,
		HealthNoteHistoryTime:  nil,
		HealthNoteHistoryBig:   nil,
		HealthNoteUnknown:      nil,
		HealthNoteFellBack:     nil,
		CompactionJevShare:     {ParamSucceeded, ParamCount, ParamPercent},
		CompactionJevBehind:    {ParamBehind, ParamSessions, ParamPercent},
		CompactionMoney:        {ParamMoney},
		CompactionMoneyUnknown: nil,
		CompactionMoneyPartial: {ParamCount, ParamRuns},
	}
}
