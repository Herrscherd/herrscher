package main

const (
	paramCount  = "Count"
	paramDetail = "Detail"
	paramErr    = "Err"
	paramLimit  = "Limit"
	paramMoney  = "Money"
	paramName   = "Name"
	paramPath   = "Path"
	paramRun    = "Run"
	paramSpent  = "Spent"
	paramTokens = "Tokens"
)

const (
	msgUsage              msgKey = "membench.usage"
	msgFlagHerrscher      msgKey = "membench.flag.herrscher"
	msgFlagModel          msgKey = "membench.flag.model"
	msgFlagBudgetTokens   msgKey = "membench.flag.budget_tokens"
	msgFlagBudgetUSD      msgKey = "membench.flag.budget_usd"
	msgFlagCredentials    msgKey = "membench.flag.credentials"
	msgFlagAPIKeyEnv      msgKey = "membench.flag.api_key_env"
	msgFlagDataset        msgKey = "membench.flag.dataset"
	msgFlagSamples        msgKey = "membench.flag.samples"
	msgFlagSeed           msgKey = "membench.flag.seed"
	msgFlagSettings       msgKey = "membench.flag.settings"
	msgFlagMaxTurns       msgKey = "membench.flag.max_turns"
	msgFlagFormat         msgKey = "membench.flag.format"
	msgFlagLang           msgKey = "membench.flag.lang"
	msgErrBudgetMissing   msgKey = "membench.error.budget_missing"
	msgErrFlagValue       msgKey = "membench.error.flag_value"
	msgErrDatasetRead     msgKey = "membench.error.dataset_read"
	msgErrDatasetShape    msgKey = "membench.error.dataset_shape"
	msgErrDatasetLarge    msgKey = "membench.error.dataset_large"
	msgErrHerrscher       msgKey = "membench.error.herrscher"
	msgErrRun             msgKey = "membench.error.run"
	msgErrNoUsage         msgKey = "membench.error.no_usage"
	msgErrNoCost          msgKey = "membench.error.no_cost"
	msgReportTitle        msgKey = "membench.report.title"
	msgStatusComplete     msgKey = "membench.status.complete"
	msgStatusPartial      msgKey = "membench.status.partial"
	msgStatusFailed       msgKey = "membench.status.failed"
	msgBudgetStopped      msgKey = "membench.budget_stopped"
	msgNotRun             msgKey = "membench.not_run"
	msgAmount             msgKey = "membench.amount"
	msgNoMeasure          msgKey = "membench.no_measure"
	msgSectionPairs       msgKey = "membench.section.pairs"
	msgSectionLongMemEval msgKey = "membench.section.longmemeval"
	msgColTask            msgKey = "membench.col.task"
	msgColSetting         msgKey = "membench.col.setting"
	msgColOff             msgKey = "membench.col.off"
	msgColOn              msgKey = "membench.col.on"
	msgColDeltaSuccess    msgKey = "membench.col.delta_success"
	msgColDeltaTurns      msgKey = "membench.col.delta_turns"
	msgColDeltaTokens     msgKey = "membench.col.delta_tokens"
	msgColMemoryTokens    msgKey = "membench.col.memory_tokens"
	msgColFalseRecall     msgKey = "membench.col.false_recall"
	msgColCapability      msgKey = "membench.col.capability"
	msgVariantSameAgent   msgKey = "membench.variant.same_agent"
	msgVariantOtherAgent  msgKey = "membench.variant.other_agent"
	msgVariantStale       msgKey = "membench.variant.stale"
	msgVariantNeverGiven  msgKey = "membench.variant.never_given"
	msgCapExtraction      msgKey = "membench.cap.extraction"
	msgCapMultisession    msgKey = "membench.cap.multisession"
	msgCapTemporal        msgKey = "membench.cap.temporal"
	msgCapUpdate          msgKey = "membench.cap.update"
	msgCapAbstention      msgKey = "membench.cap.abstention"
)

func declaredMessages() map[msgKey][]string {
	return map[msgKey][]string{
		msgUsage:              nil,
		msgFlagHerrscher:      nil,
		msgFlagModel:          nil,
		msgFlagBudgetTokens:   nil,
		msgFlagBudgetUSD:      nil,
		msgFlagCredentials:    nil,
		msgFlagAPIKeyEnv:      nil,
		msgFlagDataset:        nil,
		msgFlagSamples:        nil,
		msgFlagSeed:           nil,
		msgFlagSettings:       nil,
		msgFlagMaxTurns:       nil,
		msgFlagFormat:         nil,
		msgFlagLang:           nil,
		msgErrBudgetMissing:   nil,
		msgErrFlagValue:       {paramName, paramDetail},
		msgErrDatasetRead:     {paramPath, paramErr},
		msgErrDatasetShape:    {paramDetail},
		msgErrDatasetLarge:    {paramLimit},
		msgErrHerrscher:       {paramPath, paramErr},
		msgErrRun:             {paramRun, paramErr},
		msgErrNoUsage:         {paramRun},
		msgErrNoCost:          {paramRun},
		msgReportTitle:        nil,
		msgStatusComplete:     nil,
		msgStatusPartial:      nil,
		msgStatusFailed:       {paramDetail},
		msgBudgetStopped:      {paramSpent, paramLimit},
		msgNotRun:             {paramCount},
		msgAmount:             {paramTokens, paramMoney},
		msgNoMeasure:          nil,
		msgSectionPairs:       nil,
		msgSectionLongMemEval: nil,
		msgColTask:            nil,
		msgColSetting:         nil,
		msgColOff:             nil,
		msgColOn:              nil,
		msgColDeltaSuccess:    nil,
		msgColDeltaTurns:      nil,
		msgColDeltaTokens:     nil,
		msgColMemoryTokens:    nil,
		msgColFalseRecall:     nil,
		msgColCapability:      nil,
		msgVariantSameAgent:   nil,
		msgVariantOtherAgent:  nil,
		msgVariantStale:       nil,
		msgVariantNeverGiven:  nil,
		msgCapExtraction:      nil,
		msgCapMultisession:    nil,
		msgCapTemporal:        nil,
		msgCapUpdate:          nil,
		msgCapAbstention:      nil,
	}
}
