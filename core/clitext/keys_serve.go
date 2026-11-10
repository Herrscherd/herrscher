package clitext

// Keys of the skills messages printed while serving.
const (
	SkillsRetireFailed   = "cli.skills.retire_failed"
	SkillRemoved         = "cli.skills.removed"
	SkillRetiredDiverged = "cli.skills.retired_diverged"
	SkillRetiredLinked   = "cli.skills.retired_linked"
	SkillInstalled       = "cli.skills.installed"
	SkillUpdated         = "cli.skills.updated"
	SkillDiverged        = "cli.skills.diverged"
	SkillLinked          = "cli.skills.linked"
	SkillsNotConfigured  = "cli.skills.not_configured"
	SkillsPluginFailed   = "cli.skills.plugin_failed"
	SkillsNoContent      = "cli.skills.no_content"
	SkillsInstallFailed  = "cli.skills.install_failed"
	SkillDuplicate       = "cli.skills.duplicate"
)

// Keys of the flags and of the operator messages of serve.
const (
	ServeFlagConfig         = "cli.serve.flag.config"
	ServeFlagState          = "cli.serve.flag.state"
	ServeFlagCmd            = "cli.serve.flag.cmd"
	ServeFlagHealthAddr     = "cli.serve.flag.health_addr"
	ServeFlagStatusChannel  = "cli.serve.flag.status_channel"
	ServeAttachFailed       = "cli.serve.attach_failed"
	ServeGatewaySkipped     = "cli.serve.gateway_skipped"
	ServeGatewayRetrying    = "cli.serve.gateway_retrying"
	ServeGatewayUnavailable = "cli.serve.gateway_unavailable"
	ServeGatewayCommands    = "cli.serve.gateway_commands"
	ServeNoGateway          = "cli.serve.no_gateway"
	ServeSessionStarted     = "cli.serve.session_started"
	ServeSessionFailed      = "cli.serve.session_failed"
	ServeHubStopped         = "cli.serve.hub_stopped"
)

func serveKeys() map[Key][]string {
	return map[Key][]string{
		ServeFlagConfig:         nil,
		ServeFlagState:          nil,
		ServeFlagCmd:            nil,
		ServeFlagHealthAddr:     nil,
		ServeFlagStatusChannel:  nil,
		ServeAttachFailed:       {ParamErr},
		ServeGatewaySkipped:     {ParamFailure},
		ServeGatewayRetrying:    {ParamFailure, ParamWait},
		ServeGatewayUnavailable: {ParamFailure},
		ServeGatewayCommands:    {ParamErr},
		ServeNoGateway:          nil,
		ServeSessionStarted:     {ParamName},
		ServeSessionFailed:      {ParamName, ParamErr},
		ServeHubStopped:         {ParamErr},
		SkillsRetireFailed:      {ParamErr},
		SkillRemoved:            {ParamName},
		SkillRetiredDiverged:    {ParamName, ParamPath},
		SkillRetiredLinked:      {ParamName, ParamPath},
		SkillInstalled:          {ParamName},
		SkillUpdated:            {ParamName},
		SkillDiverged:           {ParamName, ParamPath},
		SkillLinked:             {ParamName, ParamPath},
		SkillsNotConfigured:     {ParamKind, ParamErr},
		SkillsPluginFailed:      {ParamKind, ParamErr},
		SkillsNoContent:         {ParamKind},
		SkillsInstallFailed:     {ParamKind, ParamErr},
		SkillDuplicate:          {ParamName, ParamKind, ParamOwner},
	}
}
