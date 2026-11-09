package clitext

// Keys of the health text.
const (
	CmdHelpHealth          = "cli.help.cmd.health"
	CmdHelpHealthDeep      = "cli.help.cmd.health.deep"
	HealthSummaryDown      = "cli.health.summary.down"
	HealthSummaryDegraded  = "cli.health.summary.degraded"
	HealthSummaryOK        = "cli.health.summary.ok"
	HealthDetailHint       = "cli.health.detail_hint"
	HealthBanner           = "cli.health.banner"
	HealthBannerTitle      = "cli.health.banner_title"
	HealthProxyFacts       = "cli.health.facts.proxy"
	HealthPoolLastStatus   = "cli.health.facts.last_status"
	HealthPoolCooling      = "cli.health.facts.cooling"
	HealthHeader           = "cli.health.header"
	HealthGroupCore        = "cli.health.group.core"
	HealthGroupGateways    = "cli.health.group.gateways"
	HealthGroupBackends    = "cli.health.group.backends"
	HealthGroupCompaction  = "cli.health.group.compaction"
	HealthGroupMemory      = "cli.health.group.memory"
	HealthGroupKeys        = "cli.health.group.keys"
	HealthStateOK          = "cli.health.state.ok"
	HealthStateDegraded    = "cli.health.state.degraded"
	HealthStateDown        = "cli.health.state.down"
	HealthStateOff         = "cli.health.state.off"
	HealthDaemonDown       = "cli.health.daemon_down"
	HealthUnknownService   = "cli.health.unknown_service"
	HealthPoolFacts        = "cli.health.facts.pool"
	HealthWatchRefreshedAt = "cli.health.watch_refreshed_at"
	HealthSpanDays         = "cli.health.span.days"
	HealthSpanHours        = "cli.health.span.hours"
	HealthSpanMinutes      = "cli.health.span.minutes"
	HealthSpanSeconds      = "cli.health.span.seconds"
	HealthSpanMillis       = "cli.health.span.millis"
	HealthNameDaemon       = "cli.health.name.daemon"
	HealthNameProxy        = "cli.health.name.proxy"
	HealthDetailState      = "cli.health.detail.state"
	HealthDetailCause      = "cli.health.detail.cause"
	HealthDetailRemedy     = "cli.health.detail.remedy"
)

func healthKeys() map[Key][]string {
	return map[Key][]string{
		CmdHelpHealth:          nil,
		CmdHelpHealthDeep:      nil,
		HealthSummaryDown:      {ParamCount},
		HealthSummaryDegraded:  {ParamCount},
		HealthSummaryOK:        {ParamCount},
		HealthDetailHint:       nil,
		HealthBanner:           {ParamUptime, ParamPID, ParamElapsed},
		HealthBannerTitle:      {ParamVersion},
		HealthProxyFacts:       {ParamListen, ParamCount},
		HealthPoolLastStatus:   {ParamStatus},
		HealthPoolCooling:      {ParamDuration},
		HealthHeader:           nil,
		HealthGroupCore:        nil,
		HealthGroupGateways:    nil,
		HealthGroupBackends:    nil,
		HealthGroupCompaction:  nil,
		HealthGroupMemory:      nil,
		HealthGroupKeys:        nil,
		HealthStateOK:          nil,
		HealthStateDegraded:    nil,
		HealthStateDown:        nil,
		HealthStateOff:         nil,
		HealthDaemonDown:       {ParamSocket},
		HealthUnknownService:   {ParamName},
		HealthPoolFacts:        {ParamCount, ParamTotal},
		HealthWatchRefreshedAt: {ParamTime},
		HealthSpanDays:         {ParamDays, ParamHours},
		HealthSpanHours:        {ParamHours, ParamMinutes},
		HealthSpanMinutes:      {ParamMinutes},
		HealthSpanSeconds:      {ParamSeconds},
		HealthSpanMillis:       {ParamDuration},
		HealthNameDaemon:       nil,
		HealthNameProxy:        nil,
		HealthDetailState:      nil,
		HealthDetailCause:      nil,
		HealthDetailRemedy:     nil,
	}
}
