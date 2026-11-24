package main

const (
	paramCount   = "Count"
	paramDetail  = "Detail"
	paramErr     = "Err"
	paramLimit   = "Limit"
	paramMoney   = "Money"
	paramName    = "Name"
	paramPath    = "Path"
	paramReserve = "Reserve"
	paramRun     = "Run"
	paramSpent   = "Spent"
	paramTokens  = "Tokens"
)

const (
	msgUsage              msgKey = "membench.usage"
	msgFlagHerrscher      msgKey = "membench.flag.herrscher"
	msgFlagModel          msgKey = "membench.flag.model"
	msgFlagBudgetLimit    msgKey = "membench.flag.budget_tokens"
	msgFlagBudgetUSD      msgKey = "membench.flag.budget_usd"
	msgFlagReserveLimit   msgKey = "membench.flag.reserve_limit"
	msgFlagReserveUSD     msgKey = "membench.flag.reserve_usd"
	msgFlagAuthFile       msgKey = "membench.flag.credentials"
	msgFlagKeyEnv         msgKey = "membench.flag.api_key_env"
	msgFlagDataset        msgKey = "membench.flag.dataset"
	msgFlagSamples        msgKey = "membench.flag.samples"
	msgFlagSeed           msgKey = "membench.flag.seed"
	msgFlagSettings       msgKey = "membench.flag.settings"
	msgFlagMaxTurns       msgKey = "membench.flag.max_turns"
	msgFlagFormat         msgKey = "membench.flag.format"
	msgFlagLang           msgKey = "membench.flag.lang"
	msgErrBudgetMissing   msgKey = "membench.error.budget_missing"
	msgErrReserveCeiling  msgKey = "membench.error.reserve_ceiling"
	msgErrFlagValue       msgKey = "membench.error.flag_value"
	msgErrDatasetRead     msgKey = "membench.error.dataset_read"
	msgErrDatasetShape    msgKey = "membench.error.dataset_shape"
	msgErrDatasetLarge    msgKey = "membench.error.dataset_large"
	msgErrHerrscher       msgKey = "membench.error.herrscher"
	msgErrRun             msgKey = "membench.error.run"
	msgErrNoUsage         msgKey = "membench.error.no_usage"
	msgErrNoCost          msgKey = "membench.error.no_cost"
	msgErrPromptLong      msgKey = "membench.error.prompt_long"
	msgSkippedLong        msgKey = "membench.skipped_long"
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
	msgColDeltaVolume     msgKey = "membench.col.delta_tokens"
	msgColMemoryVolume    msgKey = "membench.col.memory_tokens"
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

type keyParams struct {
	key    msgKey
	params []string
}

func paramsOf(key msgKey) []string {
	for _, entry := range paramTable() {
		if entry.key == key {
			return entry.params
		}
	}
	return nil
}

func paramTable() []keyParams {
	return []keyParams{
		{key: msgErrReserveCeiling, params: []string{paramReserve, paramLimit}},
		{key: msgErrFlagValue, params: []string{paramName, paramDetail}},
		{key: msgErrDatasetRead, params: []string{paramPath, paramErr}},
		{key: msgErrDatasetShape, params: []string{paramDetail}},
		{key: msgErrDatasetLarge, params: []string{paramLimit}},
		{key: msgErrHerrscher, params: []string{paramPath, paramErr}},
		{key: msgErrRun, params: []string{paramRun, paramErr}},
		{key: msgErrNoUsage, params: []string{paramRun}},
		{key: msgErrNoCost, params: []string{paramRun}},
		{key: msgErrPromptLong, params: []string{paramRun, paramLimit}},
		{key: msgSkippedLong, params: []string{paramCount}},
		{key: msgStatusFailed, params: []string{paramDetail}},
		{key: msgBudgetStopped, params: []string{paramSpent, paramLimit}},
		{key: msgNotRun, params: []string{paramCount}},
		{key: msgAmount, params: []string{paramTokens, paramMoney}},
	}
}
