package main

const (
	paramCode    = "Code"
	paramCount   = "Count"
	paramDetail  = "Detail"
	paramLimit   = "Limit"
	paramMoney   = "Money"
	paramName    = "Name"
	paramOffset  = "Offset"
	paramPath    = "Path"
	paramReserve = "Reserve"
	paramRun     = "Run"
	paramSpent   = "Spent"
	paramTokens  = "Tokens"
	paramWant    = "Want"
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
	msgErrDatasetMissing  msgKey = "membench.error.dataset_missing"
	msgErrDatasetDenied   msgKey = "membench.error.dataset_denied"
	msgErrDatasetEmpty    msgKey = "membench.error.dataset_empty"
	msgErrDatasetJSON     msgKey = "membench.error.dataset_json"
	msgErrDatasetDelim    msgKey = "membench.error.dataset_delim"
	msgErrDatasetLarge    msgKey = "membench.error.dataset_large"
	msgErrHerrscher       msgKey = "membench.error.herrscher"
	msgErrHerrscherAbsent msgKey = "membench.error.herrscher_absent"
	msgErrRun             msgKey = "membench.error.run"
	msgErrRunExit         msgKey = "membench.error.run_exit"
	msgErrRunTimeout      msgKey = "membench.error.run_timeout"
	msgErrFlagsSyntax     msgKey = "membench.error.flags_syntax"
	msgErrAuthFileAbsent  msgKey = "membench.error.credentials_absent"
	msgErrAuthFileRead    msgKey = "membench.error.credentials_read"
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
	msgRowPartial         msgKey = "membench.row.partial"
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
		{key: msgErrDatasetRead, params: []string{paramPath}},
		{key: msgErrDatasetMissing, params: []string{paramPath}},
		{key: msgErrDatasetDenied, params: []string{paramPath}},
		{key: msgErrDatasetJSON, params: []string{paramOffset}},
		{key: msgErrDatasetDelim, params: []string{paramWant, paramOffset}},
		{key: msgErrDatasetLarge, params: []string{paramLimit}},
		{key: msgErrHerrscher, params: []string{paramPath}},
		{key: msgErrHerrscherAbsent, params: []string{paramPath}},
		{key: msgErrRun, params: []string{paramRun}},
		{key: msgErrRunExit, params: []string{paramRun, paramCode}},
		{key: msgErrRunTimeout, params: []string{paramRun}},
		{key: msgErrAuthFileAbsent, params: []string{paramPath}},
		{key: msgErrAuthFileRead, params: []string{paramPath}},
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
