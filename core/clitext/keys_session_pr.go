package clitext

// Help keys of the session pr verbs.
const (
	CmdHelpSessionPROpen     = "cli.help.cmd.session.pr.open"
	CmdHelpSessionPRStatus   = "cli.help.cmd.session.pr.status"
	CmdHelpSessionPRChecks   = "cli.help.cmd.session.pr.checks"
	CmdHelpSessionPRComments = "cli.help.cmd.session.pr.comments"
	CmdParamSessionPRName    = "cli.help.param.session.pr.name"
	CmdParamSessionPRTitle   = "cli.help.param.session.pr.title"
	CmdParamSessionPRBody    = "cli.help.param.session.pr.body"
	CmdParamSessionPRBase    = "cli.help.param.session.pr.base"
	CmdParamSessionPRDraft   = "cli.help.param.session.pr.draft"

	SessionPROpened        = "cli.session.pr.opened"
	SessionPRStatus        = "cli.session.pr.status"
	SessionPRCheck         = "cli.session.pr.check"
	SessionPRChecksEmpty   = "cli.session.pr.checks_empty"
	SessionPRThread        = "cli.session.pr.thread"
	SessionPRComment       = "cli.session.pr.comment"
	SessionPRResolved      = "cli.session.pr.resolved"
	SessionPRUnresolved    = "cli.session.pr.unresolved"
	SessionPRCommentsEmpty = "cli.session.pr.comments_empty"
	SessionPRNone          = "cli.session.pr.none"
	SessionPRRecorded      = "cli.session.pr.recorded"
	SessionPRNoDir         = "cli.session.pr.no_dir"
	SessionPRUnpushed      = "cli.session.pr.unpushed"
	SessionPRNoBase        = "cli.session.pr.no_base"
	SessionPRWrongHost     = "cli.session.pr.wrong_host"
	SessionPRNotConnected  = "cli.session.pr.not_connected"
	SessionPRUnauthorized  = "cli.session.pr.unauthorized"
	SessionPRRateLimited   = "cli.session.pr.rate_limited"
	SessionPRForgeMissing  = "cli.session.pr.forge_missing"
	SessionPRPagesExceeded = "cli.session.pr.pages_exceeded"

	ParamNumber     = "Number"
	ParamTitle      = "Title"
	ParamHead       = "Head"
	ParamBase       = "Base"
	ParamMerge      = "Merge"
	ParamAuthor     = "Author"
	ParamBody       = "Body"
	ParamResolution = "Resolution"
	ParamReset      = "Reset"
	ParamPages      = "Pages"
)

func sessionPRKeys() map[Key][]string {
	return map[Key][]string{
		CmdHelpSessionPROpen:     nil,
		CmdHelpSessionPRStatus:   nil,
		CmdHelpSessionPRChecks:   nil,
		CmdHelpSessionPRComments: nil,
		CmdParamSessionPRName:    nil,
		CmdParamSessionPRTitle:   nil,
		CmdParamSessionPRBody:    nil,
		CmdParamSessionPRBase:    nil,
		CmdParamSessionPRDraft:   nil,

		SessionPROpened:        {ParamName, ParamNumber, ParamURL},
		SessionPRStatus:        {ParamNumber, ParamState, ParamTitle, ParamURL, ParamHead, ParamBase, ParamMerge},
		SessionPRCheck:         {ParamStatus, ParamName, ParamURL},
		SessionPRChecksEmpty:   {ParamNumber},
		SessionPRThread:        {ParamPath, ParamLine, ParamResolution},
		SessionPRComment:       {ParamAuthor, ParamBody},
		SessionPRResolved:      nil,
		SessionPRUnresolved:    nil,
		SessionPRCommentsEmpty: {ParamNumber},
		SessionPRNone:          {ParamName},
		SessionPRRecorded:      {ParamName, ParamNumber},
		SessionPRNoDir:         {ParamName},
		SessionPRUnpushed:      {ParamBranch},
		SessionPRNoBase:        nil,
		SessionPRWrongHost:     nil,
		SessionPRNotConnected:  nil,
		SessionPRUnauthorized:  nil,
		SessionPRRateLimited:   {ParamReset},
		SessionPRForgeMissing:  nil,
		SessionPRPagesExceeded: {ParamPages},
	}
}
