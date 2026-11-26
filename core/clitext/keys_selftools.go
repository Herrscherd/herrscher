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
)

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
	}
}
