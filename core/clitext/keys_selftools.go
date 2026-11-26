package clitext

// Keys of the verbs a session reaches through its self tools: the configuration file, the setup check and the forge issues.
const (
	CmdHelpConfigGet       = "cli.help.cmd.config.get"
	CmdParamConfigGetKey   = "cli.help.param.config.get.key"
	CmdHelpConfigSet       = "cli.help.cmd.config.set"
	CmdParamConfigSetKey   = "cli.help.param.config.set.key"
	CmdParamConfigSetValue = "cli.help.param.config.set.value"
	CmdHelpConfigUnset     = "cli.help.cmd.config.unset"
	CmdParamConfigUnsetKey = "cli.help.param.config.unset.key"
	HostConfigSet          = "cli.host.config.set"
	HostConfigUnset        = "cli.host.config.unset"
	HostConfigBackup       = "cli.host.config.backup"
	ManageFlagSetupProbe   = "cli.manage.flag.setup_probe"
	ManageFlagJSON         = "cli.manage.flag.json"

	CmdHelpSessionIssueList    = "cli.help.cmd.session.issue.list"
	CmdHelpSessionIssueShow    = "cli.help.cmd.session.issue.show"
	CmdParamSessionIssueName   = "cli.help.param.session.issue.name"
	CmdParamSessionIssueState  = "cli.help.param.session.issue.state"
	CmdParamSessionIssueLabels = "cli.help.param.session.issue.labels"
	CmdParamSessionIssueNumber = "cli.help.param.session.issue.number"
	SessionIssueNone           = "cli.session.issue.none"
	SessionIssueBadNumber      = "cli.session.issue.bad_number"
	SessionIssueBadState       = "cli.session.issue.bad_state"
	CmdHelpSessionIssueOpen    = "cli.help.cmd.session.issue.open"
	CmdHelpSessionIssueComment = "cli.help.cmd.session.issue.comment"
	CmdParamSessionIssueTitle  = "cli.help.param.session.issue.title"
	CmdParamSessionIssueBody   = "cli.help.param.session.issue.body"
	SessionIssueOpened         = "cli.session.issue.opened"
	SessionIssueCommented      = "cli.session.issue.commented"
	SessionIssueNoWriter       = "cli.session.issue.no_writer"
	CmdHelpSetupCheck          = "cli.help.cmd.setup.check"
	CmdHelpConnectStatus       = "cli.help.cmd.connect.status"
	CmdHelpPluginList          = "cli.help.cmd.plugin.list"
	CmdHelpPluginAdd           = "cli.help.cmd.plugin.add"
	CmdHelpPluginRemove        = "cli.help.cmd.plugin.rm"
	CmdParamPluginModule       = "cli.help.param.plugin.module"
	CmdParamPluginNoBuild      = "cli.help.param.plugin.no_build"
	CmdHelpEnvGet              = "cli.help.cmd.env.get"
	CmdHelpEnvSet              = "cli.help.cmd.env.set"
	CmdHelpEnvUnset            = "cli.help.cmd.env.unset"
	CmdParamEnvKey             = "cli.help.param.env.key"
	CmdParamEnvValue           = "cli.help.param.env.value"
	HostEnvSet                 = "cli.host.env.set"
	HostEnvUnset               = "cli.host.env.unset"
	CmdHelpSessionPRReply      = "cli.help.cmd.session.pr.reply"
	CmdParamSessionPRThread    = "cli.help.param.session.pr.thread"
	CmdParamSessionPRReplyBody = "cli.help.param.session.pr.reply_body"
	SessionPRReplied           = "cli.session.pr.replied"
	HealthSelfToolsFacts       = "cli.health.facts.self_tools"
	HealthSelfToolsOff         = "cli.health.facts.self_tools_off"
	CmdParamSessionIssueLimit  = "cli.help.param.session.issue.limit"
	SessionIssueTruncated      = "cli.session.issue.truncated"
	SessionIssueBadLimit       = "cli.session.issue.bad_limit"
)

// ParamSize is the parameter that carries a size in bytes.
const ParamSize = "Size"

func selfToolsKeys() map[Key][]string {
	return map[Key][]string{
		CmdHelpConfigGet:       nil,
		CmdParamConfigGetKey:   nil,
		CmdHelpConfigSet:       nil,
		CmdParamConfigSetKey:   nil,
		CmdParamConfigSetValue: nil,
		CmdHelpConfigUnset:     nil,
		CmdParamConfigUnsetKey: nil,
		HostConfigSet:          {ParamKey, ParamPath},
		HostConfigUnset:        {ParamKey, ParamPath},
		HostConfigBackup:       {ParamPath},
		ManageFlagSetupProbe:   nil,
		ManageFlagJSON:         nil,

		CmdHelpSessionIssueList:    nil,
		CmdHelpSessionIssueShow:    nil,
		CmdParamSessionIssueName:   nil,
		CmdParamSessionIssueState:  nil,
		CmdParamSessionIssueLabels: nil,
		CmdParamSessionIssueNumber: nil,
		SessionIssueNone:           {ParamRepo},
		SessionIssueBadNumber:      {ParamNumber},
		SessionIssueBadState:       {ParamState},
		CmdHelpSessionIssueOpen:    nil,
		CmdHelpSessionIssueComment: nil,
		CmdParamSessionIssueTitle:  nil,
		CmdParamSessionIssueBody:   nil,
		SessionIssueOpened:         {ParamNumber, ParamURL},
		SessionIssueCommented:      {ParamNumber},
		SessionIssueNoWriter:       nil,
		CmdHelpSetupCheck:          nil,
		CmdHelpConnectStatus:       nil,
		CmdHelpPluginList:          nil,
		CmdHelpPluginAdd:           nil,
		CmdHelpPluginRemove:        nil,
		CmdParamPluginModule:       nil,
		CmdParamPluginNoBuild:      nil,
		CmdHelpEnvGet:              nil,
		CmdHelpEnvSet:              nil,
		CmdHelpEnvUnset:            nil,
		CmdParamEnvKey:             nil,
		CmdParamEnvValue:           nil,
		HostEnvSet:                 {ParamKey, ParamPath},
		HostEnvUnset:               {ParamKey, ParamPath},
		CmdHelpSessionPRReply:      nil,
		CmdParamSessionPRThread:    nil,
		CmdParamSessionPRReplyBody: nil,
		SessionPRReplied:           {ParamNumber},
		HealthSelfToolsFacts:       {ParamMode, ParamCount, ParamSize},
		HealthSelfToolsOff:         nil,
		CmdParamSessionIssueLimit:  nil,
		SessionIssueTruncated:      {ParamCount},
		SessionIssueBadLimit:       {ParamCount},
	}
}
