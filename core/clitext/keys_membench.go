package clitext

// ParamSpent and ParamRun name the placeholders of the membench messages.
const (
	ParamSpent = "Spent"
	ParamRun   = "Run"
)

// The membench keys of the catalogue; membenchKeys lists the parameters each one takes.
const (
	MembenchUsage              = "cli.membench.usage"
	MembenchFlagHerrscher      = "cli.membench.flag.herrscher"
	MembenchFlagModel          = "cli.membench.flag.model"
	MembenchFlagBudgetTokens   = "cli.membench.flag.budget_tokens"
	MembenchFlagBudgetUSD      = "cli.membench.flag.budget_usd"
	MembenchFlagCredentials    = "cli.membench.flag.credentials"
	MembenchFlagDataset        = "cli.membench.flag.dataset"
	MembenchFlagSamples        = "cli.membench.flag.samples"
	MembenchFlagSeed           = "cli.membench.flag.seed"
	MembenchFlagSettings       = "cli.membench.flag.settings"
	MembenchFlagMaxTurns       = "cli.membench.flag.max_turns"
	MembenchFlagFormat         = "cli.membench.flag.format"
	MembenchFlagLang           = "cli.membench.flag.lang"
	MembenchErrBudgetMissing   = "cli.membench.error.budget_missing"
	MembenchErrFlagValue       = "cli.membench.error.flag_value"
	MembenchErrDatasetRead     = "cli.membench.error.dataset_read"
	MembenchErrDatasetShape    = "cli.membench.error.dataset_shape"
	MembenchErrHerrscher       = "cli.membench.error.herrscher"
	MembenchErrRun             = "cli.membench.error.run"
	MembenchReportTitle        = "cli.membench.report.title"
	MembenchStatusComplete     = "cli.membench.status.complete"
	MembenchStatusPartial      = "cli.membench.status.partial"
	MembenchBudgetStopped      = "cli.membench.budget_stopped"
	MembenchNotRun             = "cli.membench.not_run"
	MembenchAmount             = "cli.membench.amount"
	MembenchNoMeasure          = "cli.membench.no_measure"
	MembenchSectionPairs       = "cli.membench.section.pairs"
	MembenchSectionLongMemEval = "cli.membench.section.longmemeval"
	MembenchColTask            = "cli.membench.col.task"
	MembenchColSetting         = "cli.membench.col.setting"
	MembenchColOff             = "cli.membench.col.off"
	MembenchColOn              = "cli.membench.col.on"
	MembenchColDeltaSuccess    = "cli.membench.col.delta_success"
	MembenchColDeltaTurns      = "cli.membench.col.delta_turns"
	MembenchColDeltaTokens     = "cli.membench.col.delta_tokens"
	MembenchColMemoryTokens    = "cli.membench.col.memory_tokens"
	MembenchColFalseRecall     = "cli.membench.col.false_recall"
	MembenchColCapability      = "cli.membench.col.capability"
	MembenchColRuns            = "cli.membench.col.runs"
	MembenchColRate            = "cli.membench.col.rate"
	MembenchVariantSameAgent   = "cli.membench.variant.same_agent"
	MembenchVariantOtherAgent  = "cli.membench.variant.other_agent"
	MembenchVariantStale       = "cli.membench.variant.stale"
	MembenchVariantNeverGiven  = "cli.membench.variant.never_given"
	MembenchCapExtraction      = "cli.membench.cap.extraction"
	MembenchCapMultisession    = "cli.membench.cap.multisession"
	MembenchCapTemporal        = "cli.membench.cap.temporal"
	MembenchCapUpdate          = "cli.membench.cap.update"
	MembenchCapAbstention      = "cli.membench.cap.abstention"
	MembenchLongMemEvalCount   = "cli.membench.longmemeval.count"
)

func membenchKeys() map[Key][]string {
	return map[Key][]string{
		MembenchUsage:              nil,
		MembenchFlagHerrscher:      nil,
		MembenchFlagModel:          nil,
		MembenchFlagBudgetTokens:   nil,
		MembenchFlagBudgetUSD:      nil,
		MembenchFlagCredentials:    nil,
		MembenchFlagDataset:        nil,
		MembenchFlagSamples:        nil,
		MembenchFlagSeed:           nil,
		MembenchFlagSettings:       nil,
		MembenchFlagMaxTurns:       nil,
		MembenchFlagFormat:         nil,
		MembenchFlagLang:           nil,
		MembenchErrBudgetMissing:   nil,
		MembenchErrFlagValue:       {ParamName, ParamDetail},
		MembenchErrDatasetRead:     {ParamPath, ParamErr},
		MembenchErrDatasetShape:    {ParamDetail},
		MembenchErrHerrscher:       {ParamPath, ParamErr},
		MembenchErrRun:             {ParamRun, ParamErr},
		MembenchReportTitle:        nil,
		MembenchStatusComplete:     nil,
		MembenchStatusPartial:      nil,
		MembenchBudgetStopped:      {ParamSpent, ParamLimit},
		MembenchNotRun:             {ParamCount},
		MembenchAmount:             {ParamTokens, ParamMoney},
		MembenchNoMeasure:          nil,
		MembenchSectionPairs:       nil,
		MembenchSectionLongMemEval: nil,
		MembenchColTask:            nil,
		MembenchColSetting:         nil,
		MembenchColOff:             nil,
		MembenchColOn:              nil,
		MembenchColDeltaSuccess:    nil,
		MembenchColDeltaTurns:      nil,
		MembenchColDeltaTokens:     nil,
		MembenchColMemoryTokens:    nil,
		MembenchColFalseRecall:     nil,
		MembenchColCapability:      nil,
		MembenchColRuns:            nil,
		MembenchColRate:            nil,
		MembenchVariantSameAgent:   nil,
		MembenchVariantOtherAgent:  nil,
		MembenchVariantStale:       nil,
		MembenchVariantNeverGiven:  nil,
		MembenchCapExtraction:      nil,
		MembenchCapMultisession:    nil,
		MembenchCapTemporal:        nil,
		MembenchCapUpdate:          nil,
		MembenchCapAbstention:      nil,
		MembenchLongMemEvalCount:   {ParamCount},
	}
}
