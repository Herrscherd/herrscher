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

	SessionPROpened            = "cli.session.pr.opened"
	SessionPROpenedUnrecorded  = "cli.session.pr.opened_unrecorded"
	SessionPRRefreshUnrecorded = "cli.session.pr.refresh_unrecorded"
	SessionPROpening           = "cli.session.pr.opening"
	SessionPRStatus            = "cli.session.pr.status"
	SessionPRCheck             = "cli.session.pr.check"
	SessionPRChecksEmpty       = "cli.session.pr.checks_empty"
	SessionPRThread            = "cli.session.pr.thread"
	SessionPRComment           = "cli.session.pr.comment"
	SessionPRResolved          = "cli.session.pr.resolved"
	SessionPRUnresolved        = "cli.session.pr.unresolved"
	SessionPRCommentsEmpty     = "cli.session.pr.comments_empty"
	SessionPRNone              = "cli.session.pr.none"
	SessionPRRecorded          = "cli.session.pr.recorded"
	SessionPRRemote            = "cli.session.pr.remote"
	SessionPRNoDir             = "cli.session.pr.no_dir"
	SessionPRNoOrigin          = "cli.session.pr.no_origin"
	SessionPROriginRead        = "cli.session.pr.origin_read"
	SessionPRUnpushed          = "cli.session.pr.unpushed"
	SessionPRNoBase            = "cli.session.pr.no_base"
	SessionPRWrongHost         = "cli.session.pr.wrong_host"
	SessionPRNotConnected      = "cli.session.pr.not_connected"
	SessionPRUnauthorized      = "cli.session.pr.unauthorized"
	SessionPRRateLimited       = "cli.session.pr.rate_limited"
	SessionPRTimedOut          = "cli.session.pr.timed_out"
	SessionPRForgeMissing      = "cli.session.pr.forge_missing"
	SessionPRPagesExceeded     = "cli.session.pr.pages_exceeded"
	SessionPRFailed            = "cli.session.pr.failed"
	SessionPRUnknownValue      = "cli.session.pr.unknown_value"

	SessionPRStateDraft        = "cli.session.pr.state.draft"
	SessionPRStateUnknown      = "cli.session.pr.state.unknown"
	SessionPRStateOpen         = "cli.session.pr.state.open"
	SessionPRStateClosed       = "cli.session.pr.state.closed"
	SessionPRStateMerged       = "cli.session.pr.state.merged"
	SessionPRMergeUnknown      = "cli.session.pr.merge.unknown"
	SessionPRMergeClean        = "cli.session.pr.merge.clean"
	SessionPRMergeConflicting  = "cli.session.pr.merge.conflicting"
	SessionPRMergeBlocked      = "cli.session.pr.merge.blocked"
	SessionPRMergeBehind       = "cli.session.pr.merge.behind"
	SessionPRMergeDraft        = "cli.session.pr.merge.draft"
	SessionPRCheckQueued       = "cli.session.pr.check_status.queued"
	SessionPRCheckInProgress   = "cli.session.pr.check_status.in_progress"
	SessionPRCheckCompleted    = "cli.session.pr.check_status.completed"
	SessionPRConclusionNone    = "cli.session.pr.conclusion.none"
	SessionPRConclusionSuccess = "cli.session.pr.conclusion.success"
	SessionPRConclusionFailure = "cli.session.pr.conclusion.failure"
	SessionPRConclusionNeutral = "cli.session.pr.conclusion.neutral"
	SessionPRConclusionCancel  = "cli.session.pr.conclusion.cancelled"
	SessionPRConclusionSkipped = "cli.session.pr.conclusion.skipped"
	SessionPRConclusionTimeout = "cli.session.pr.conclusion.timed_out"
	SessionPRConclusionAction  = "cli.session.pr.conclusion.action_required"
	SessionPRConclusionOther   = "cli.session.pr.conclusion.other"

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
	ParamValue      = "Value"
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

		SessionPROpened:            {ParamName, ParamNumber, ParamURL},
		SessionPROpenedUnrecorded:  {ParamName, ParamNumber, ParamURL, ParamErr},
		SessionPRRefreshUnrecorded: {ParamName, ParamNumber},
		SessionPROpening:           {ParamName},
		SessionPRStatus:            {ParamNumber, ParamState, ParamTitle, ParamURL, ParamHead, ParamBase, ParamMerge},
		SessionPRCheck:             {ParamStatus, ParamName, ParamURL},
		SessionPRChecksEmpty:       {ParamNumber},
		SessionPRThread:            {ParamPath, ParamLine, ParamResolution},
		SessionPRComment:           {ParamAuthor, ParamBody},
		SessionPRResolved:          nil,
		SessionPRUnresolved:        nil,
		SessionPRCommentsEmpty:     {ParamNumber},
		SessionPRNone:              {ParamName},
		SessionPRRecorded:          {ParamName, ParamNumber},
		SessionPRRemote:            {ParamName},
		SessionPRNoDir:             {ParamName},
		SessionPRNoOrigin:          {ParamPath},
		SessionPROriginRead:        {ParamPath, ParamErr},
		SessionPRUnpushed:          {ParamBranch},
		SessionPRNoBase:            nil,
		SessionPRWrongHost:         nil,
		SessionPRNotConnected:      nil,
		SessionPRUnauthorized:      nil,
		SessionPRRateLimited:       {ParamReset},
		SessionPRTimedOut:          nil,
		SessionPRForgeMissing:      nil,
		SessionPRPagesExceeded:     {ParamPages},
		SessionPRFailed:            {ParamErr},
		SessionPRUnknownValue:      {ParamValue},

		SessionPRStateDraft:        nil,
		SessionPRStateUnknown:      nil,
		SessionPRStateOpen:         nil,
		SessionPRStateClosed:       nil,
		SessionPRStateMerged:       nil,
		SessionPRMergeUnknown:      nil,
		SessionPRMergeClean:        nil,
		SessionPRMergeConflicting:  nil,
		SessionPRMergeBlocked:      nil,
		SessionPRMergeBehind:       nil,
		SessionPRMergeDraft:        nil,
		SessionPRCheckQueued:       nil,
		SessionPRCheckInProgress:   nil,
		SessionPRCheckCompleted:    nil,
		SessionPRConclusionNone:    nil,
		SessionPRConclusionSuccess: nil,
		SessionPRConclusionFailure: nil,
		SessionPRConclusionNeutral: nil,
		SessionPRConclusionCancel:  nil,
		SessionPRConclusionSkipped: nil,
		SessionPRConclusionTimeout: nil,
		SessionPRConclusionAction:  nil,
		SessionPRConclusionOther:   nil,
	}
}
