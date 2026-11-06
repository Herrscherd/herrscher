package clitext

// Keys of the messages of attached sessions and of the session verbs.
const (
	AttachNotAvailable            = "cli.attach.not_available"
	AttachNoSession               = "cli.attach.no_session"
	SessionListEmpty              = "cli.session.list.empty"
	SessionListHeader             = "cli.session.list.header"
	SessionWhoEmpty               = "cli.session.who.empty"
	SessionWhoHeader              = "cli.session.who.header"
	SessionLogEmpty               = "cli.session.log.empty"
	SessionClosed                 = "cli.session.closed"
	SessionCloseBranchKept        = "cli.session.close.branch_kept"
	SessionArchived               = "cli.session.archived"
	SessionRenamed                = "cli.session.renamed"
	SessionRenameSameName         = "cli.session.rename.same_name"
	SessionChannelLeftOpen        = "cli.session.channel.left_open"
	SessionTranscriptLeft         = "cli.session.rename.transcript_left"
	SessionParticipantsLeft       = "cli.session.rename.participants_left"
	SessionScheduleOwnsSession    = "cli.session.rename.schedule_owns"
	SessionBudgetUpdated          = "cli.session.budget.updated"
	SessionSwitchedNoResume       = "cli.session.switch.no_resume"
	SessionSwitchUnconfirmed      = "cli.session.switch.unconfirmed"
	SessionSwitchedResumed        = "cli.session.switch.resumed"
	SessionSwitchRestartRestored  = "cli.session.switch.restart_restored"
	SessionSwitchRestartLost      = "cli.session.switch.restart_lost"
	SessionApprovalsUnenforced    = "cli.session.approvals.unenforced"
	SessionApprovalsUnenforcedNow = "cli.session.approvals.unenforced_now"
	SessionApprovalsNoChannel     = "cli.session.approvals.no_channel"
	SessionApprovalsShared        = "cli.session.approvals.shared"
	SessionApprovalsRemoteNoAgent = "cli.session.approvals.remote_no_agent"
	SessionApprovalsHookFailed    = "cli.session.approvals.hook_failed"
	SessionBannerReady            = "cli.session.banner.ready"
	SessionBannerProjectCwd       = "cli.session.banner.project_cwd"
	SessionBannerProject          = "cli.session.banner.project"
	SessionBannerWorktree         = "cli.session.banner.worktree"
	SessionBannerShared           = "cli.session.banner.shared"
	SessionBannerNotGit           = "cli.session.banner.not_git"
	SessionBannerCommand          = "cli.session.banner.command"
	SessionBannerModel            = "cli.session.banner.model"
	SessionRunning                = "cli.session.running"
	SessionResumed                = "cli.session.resumed"
	SessionModeSet                = "cli.session.mode.set"
	SessionModeUnenforced         = "cli.session.mode.unenforced"
	SessionModeRecreateNotice     = "cli.session.mode.bypass_recreate"
	SessionPicked                 = "cli.session.picked"
	SessionInterrupted            = "cli.session.interrupted"
	SessionCompactMergedKept      = "cli.session.compact.merged_kept"
	SessionCompactMergedReplaced  = "cli.session.compact.merged_replaced"
	SessionAccountNotice          = "cli.session.account.notice"
	SessionAccountNoticeFresh     = "cli.session.account.notice_fresh"
	SessionAccountVendorDefault   = "cli.session.account.vendor_default"
	SessionAccountNative          = "cli.session.account.native"
	SessionAccountThis            = "cli.session.account.this"
)

func sessionKeys() map[Key][]string {
	return map[Key][]string{
		AttachNotAvailable:            {ParamVerb},
		AttachNoSession:               nil,
		SessionListEmpty:              nil,
		SessionListHeader:             nil,
		SessionWhoEmpty:               nil,
		SessionWhoHeader:              {ParamName},
		SessionLogEmpty:               nil,
		SessionClosed:                 {ParamName},
		SessionCloseBranchKept:        {ParamBranch},
		SessionArchived:               {ParamName},
		SessionRenamed:                {ParamName, ParamTo},
		SessionRenameSameName:         {ParamName},
		SessionChannelLeftOpen:        {ParamErr},
		SessionTranscriptLeft:         {ParamName, ParamErr},
		SessionParticipantsLeft:       {ParamName, ParamErr},
		SessionScheduleOwnsSession:    {ParamSchedule, ParamName},
		SessionBudgetUpdated:          {ParamCount},
		SessionSwitchedNoResume:       {ParamName, ParamVendor, ParamNote},
		SessionSwitchUnconfirmed:      {ParamName, ParamVendor, ParamHandoff, ParamNote},
		SessionSwitchedResumed:        {ParamName, ParamVendor, ParamHandoff, ParamNote},
		SessionSwitchRestartRestored:  {ParamName, ParamVendor, ParamErr},
		SessionSwitchRestartLost:      {ParamName, ParamErr, ParamRollbackErr},
		SessionApprovalsUnenforced:    {ParamVendor, ParamWhy, ParamName},
		SessionApprovalsUnenforcedNow: {ParamVendor, ParamWhy, ParamName},
		SessionApprovalsNoChannel:     nil,
		SessionApprovalsShared:        {ParamName},
		SessionApprovalsRemoteNoAgent: {ParamName},
		SessionApprovalsHookFailed:    {ParamErr, ParamName},
		SessionBannerReady:            {ParamName},
		SessionBannerProjectCwd:       nil,
		SessionBannerProject:          {ParamName, ParamRepo},
		SessionBannerWorktree:         {ParamPath, ParamBranch},
		SessionBannerShared:           nil,
		SessionBannerNotGit:           nil,
		SessionBannerCommand:          {ParamCommand},
		SessionBannerModel:            {ParamModel},
		SessionRunning:                {ParamChannel, ParamBanner, ParamApprovals},
		SessionResumed:                {ParamName},
		SessionModeSet:                {ParamName, ParamMode},
		SessionModeUnenforced:         {ParamVendor, ParamName},
		SessionModeRecreateNotice:     nil,
		SessionPicked:                 {ParamName},
		SessionInterrupted:            {ParamName},
		SessionCompactMergedKept:      {ParamName},
		SessionCompactMergedReplaced:  {ParamName},
		SessionAccountNotice:          {ParamName, ParamAccount},
		SessionAccountNoticeFresh:     {ParamName, ParamAccount},
		SessionAccountVendorDefault:   nil,
		SessionAccountNative:          nil,
		SessionAccountThis:            nil,
	}
}
