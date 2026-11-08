package clitext

// Keys of the usage command: the limits of the accounts, the spend and the tokens of a period.
const (
	CmdHelpUsage        = "cli.help.cmd.usage"
	CmdHelpUsageRange   = "cli.help.cmd.usage.range"
	CmdHelpUsageAccount = "cli.help.cmd.usage.account"
	SpendDaemonDown     = "cli.spend.daemon_down"
	SpendRangeUnknown   = "cli.spend.range_unknown"
	SpendModeUnknown    = "cli.spend.mode_unknown"
	SpendAccountInvalid = "cli.spend.account_invalid"
	QuotaColAccount     = "cli.quota.col.account"
	QuotaColWindow      = "cli.quota.col.window"
	QuotaColRemaining   = "cli.quota.col.remaining"
	QuotaColReset       = "cli.quota.col.reset"
	QuotaWindowFiveHour = "cli.quota.window.five_hour"
	QuotaWindowWeekly   = "cli.quota.window.weekly"
	QuotaWindowMonthly  = "cli.quota.window.monthly"
	QuotaWindowScoped   = "cli.quota.window.scoped"
	QuotaAccountNative  = "cli.quota.account.native"
	QuotaUnavailable    = "cli.quota.unavailable"
	QuotaFailed         = "cli.quota.failed"
	QuotaEmpty          = "cli.quota.empty"
	QuotaExhausted      = "cli.quota.exhausted"
	QuotaResetNow       = "cli.quota.reset.now"
	QuotaResetUnknown   = "cli.quota.reset.unknown"
	QuotaResetDays      = "cli.quota.reset.days"
	QuotaResetHours     = "cli.quota.reset.hours"
	QuotaResetMinutes   = "cli.quota.reset.minutes"
	QuotaPaceFaster     = "cli.quota.pace.faster"
	QuotaPaceSlower     = "cli.quota.pace.slower"
	SpendEmpty          = "cli.spend.empty"
	SpendSessions       = "cli.spend.sessions"
	SpendRange24h       = "cli.spend.range.24h"
	SpendRange7d        = "cli.spend.range.7d"
	SpendRange30d       = "cli.spend.range.30d"
	SpendRange90d       = "cli.spend.range.90d"
	SpendEstimate       = "cli.spend.estimate"
	SpendPeak           = "cli.spend.peak"
	SpendColAccount     = "cli.spend.col.account"
	SpendColModel       = "cli.spend.col.model"
	SpendColCost        = "cli.spend.col.cost"
	SpendColShare       = "cli.spend.col.share"
	SpendColVolume      = "cli.spend.col.tokens"
	SpendModelUnknown   = "cli.spend.model.unknown"
	SpendVolumeTotal    = "cli.spend.tokens.total"
	SpendVolumeCache    = "cli.spend.tokens.cache"
	SpendUnitKilo       = "cli.spend.unit.kilo"
	SpendUnitMega       = "cli.spend.unit.mega"
	SpendUnitGiga       = "cli.spend.unit.giga"
)

// Placeholder names of the quota and spend texts, as the catalogues write them.
const (
	ParamWindow = "Window"
	ParamScope  = "Scope"
	ParamDelta  = "Delta"
	ParamDay    = "Day"
	ParamCost   = "Cost"
	ParamIn     = "In"
	ParamOut    = "Out"
	ParamRead   = "Read"
	ParamCreate = "Create"
	ParamRatio  = "Ratio"
)

func spendKeys() map[Key][]string {
	return map[Key][]string{
		CmdHelpUsage:        nil,
		CmdHelpUsageRange:   nil,
		CmdHelpUsageAccount: nil,
		SpendDaemonDown:     {ParamSocket},
		SpendRangeUnknown:   {ParamName},
		SpendModeUnknown:    {ParamName},
		SpendAccountInvalid: {ParamName},
		QuotaColAccount:     nil,
		QuotaColWindow:      nil,
		QuotaColRemaining:   nil,
		QuotaColReset:       nil,
		QuotaWindowFiveHour: nil,
		QuotaWindowWeekly:   nil,
		QuotaWindowMonthly:  nil,
		QuotaWindowScoped:   {ParamWindow, ParamScope},
		QuotaAccountNative:  nil,
		QuotaUnavailable:    nil,
		QuotaFailed:         nil,
		QuotaEmpty:          nil,
		QuotaExhausted:      nil,
		QuotaResetNow:       nil,
		QuotaResetUnknown:   nil,
		QuotaResetDays:      {ParamDays, ParamHours},
		QuotaResetHours:     {ParamHours, ParamMinutes},
		QuotaResetMinutes:   {ParamMinutes},
		QuotaPaceFaster:     {ParamDelta},
		QuotaPaceSlower:     {ParamDelta},
		SpendEmpty:          nil,
		SpendSessions:       {ParamCount},
		SpendRange24h:       nil,
		SpendRange7d:        nil,
		SpendRange30d:       nil,
		SpendRange90d:       nil,
		SpendEstimate:       nil,
		SpendPeak:           {ParamCost, ParamDay},
		SpendColAccount:     nil,
		SpendColModel:       nil,
		SpendColCost:        nil,
		SpendColShare:       nil,
		SpendColVolume:      nil,
		SpendModelUnknown:   nil,
		SpendVolumeTotal:    {ParamTotal, ParamIn, ParamOut},
		SpendVolumeCache:    {ParamRead, ParamCreate, ParamRatio},
		SpendUnitKilo:       nil,
		SpendUnitMega:       nil,
		SpendUnitGiga:       nil,
	}
}
