package clitext

// Keys of the health command: its usage and flags, the groups and states of its report, and the facts and failures it shows.
const (
	CmdHelpHealth          = "cli.help.cmd.health"
	CmdHelpHealthDeep      = "cli.help.cmd.health.deep"
	FlagHealthDeep         = "cli.flag.health_deep"
	FlagHealthWatch        = "cli.flag.health_watch"
	FlagHealthQuiet        = "cli.flag.health_quiet"
	HealthSummary          = "cli.health.summary"
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
	HealthDaemonFacts      = "cli.health.facts.daemon"
	HealthPoolFacts        = "cli.health.facts.pool"
	HealthDefaultAccount   = "cli.health.default_account"
	HealthWatchRefreshedAt = "cli.health.watch_refreshed_at"
)

func healthKeys() map[Key][]string {
	return map[Key][]string{
		CmdHelpHealth:          nil,
		CmdHelpHealthDeep:      nil,
		FlagHealthDeep:         nil,
		FlagHealthWatch:        nil,
		FlagHealthQuiet:        nil,
		HealthSummary:          {ParamCount, ParamTotal},
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
		HealthDaemonFacts:      {ParamPID, ParamSocket},
		HealthPoolFacts:        {ParamCount, ParamTotal},
		HealthDefaultAccount:   nil,
		HealthWatchRefreshedAt: {ParamTime},
	}
}
