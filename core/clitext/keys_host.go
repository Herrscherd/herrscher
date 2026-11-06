package clitext

// Keys of the replies that host commands return to the operator.
const (
	HostHomeSet                 = "cli.host.home.set"
	HostSourceSet               = "cli.host.source.set"
	HostLangSet                 = "cli.host.lang.set"
	HostLangReset               = "cli.host.lang.reset"
	HostSessionLangSet          = "cli.host.session.lang.set"
	HostSessionLangReset        = "cli.host.session.lang.reset"
	HostAgentCreated            = "cli.host.agent.created"
	HostAgentListEmpty          = "cli.host.agent.list_empty"
	HostAgentSoulUpdated        = "cli.host.agent.soul_updated"
	HostServiceRestarting       = "cli.host.service.restarting"
	HostServiceRebuilt          = "cli.host.service.rebuilt"
	HostSessionStartedPost      = "cli.host.session.started_post"
	HostCheckpointNone          = "cli.host.checkpoint.none"
	HostCheckpointUndone        = "cli.host.checkpoint.undone"
	HostMemoryRestored          = "cli.host.memory.restored"
	HostMemoryUnlinked          = "cli.host.memory.unlinked"
	HostScheduleNone            = "cli.host.schedule.none"
	HostScheduleRemoved         = "cli.host.schedule.removed"
	HostScheduleFired           = "cli.host.schedule.fired"
	HostSchedulePaused          = "cli.host.schedule.paused"
	HostScheduleResumed         = "cli.host.schedule.resumed"
	HostApproveNone             = "cli.host.approve.none"
	HostApproveAllowed          = "cli.host.approve.allowed"
	HostApproveDenied           = "cli.host.approve.denied"
	HostApproveRuleRemoved      = "cli.host.approve.rule_removed"
	HostApproveRuleAdded        = "cli.host.approve.rule_added"
	HostApproveRuleNone         = "cli.host.approve.rule_none"
	HostModelsNone              = "cli.host.models.none"
	HostModelsHidden            = "cli.host.models.hidden"
	HostModelsRefreshNone       = "cli.host.models.refresh_none"
	HostModelsDiscovered        = "cli.host.models.discovered"
	HostModelsDiscoveryKept     = "cli.host.models.discovery_kept"
	HostRoleNone                = "cli.host.role.none"
	HostRoleFirstGrant          = "cli.host.role.first_grant"
	HostRoleGrantable           = "cli.host.role.grantable"
	HostRoleAgentNote           = "cli.host.role.agent_note"
	HostRoleGranted             = "cli.host.role.granted"
	HostRoleRevokedLast         = "cli.host.role.revoked_last"
	HostRoleRevoked             = "cli.host.role.revoked"
	HostRoleHolds               = "cli.host.role.holds"
	HostModelsDiscoveryStatic   = "cli.host.models.discovery_static"
	HostMemoryForgotten         = "cli.host.memory.forgotten"
	HostMemoryRecorded          = "cli.host.memory.recorded"
	HostMemorySkillsNone        = "cli.host.memory.skills_none"
	HostMemorySkillLastUsed     = "cli.host.memory.skill_last_used"
	HostMemorySkillRevoked      = "cli.host.memory.skill_revoked"
	HostMemorySkillApproved     = "cli.host.memory.skill_approved"
	HostMemorySearchNone        = "cli.host.memory.search_none"
	HostApproveModeSet          = "cli.host.approve.mode_set"
	HostApproveSmartNoBackend   = "cli.host.approve.smart_no_backend"
	HostApproveBypassNoHook     = "cli.host.approve.bypass_no_hook"
	HostCheckpointRewound       = "cli.host.checkpoint.rewound"
	HostScheduleEvery           = "cli.host.schedule.every"
	HostScheduleCron            = "cli.host.schedule.cron"
	HostHandoffSession          = "cli.host.handoff.session"
	HostHandoffSessionNamed     = "cli.host.handoff.session_named"
	HostHandoffVendorDefault    = "cli.host.handoff.vendor_default"
	HostHandoffPointer          = "cli.host.handoff.pointer"
	HostHandoffReport           = "cli.host.handoff.report"
	HostErrMissingChannel       = "cli.host.err.missing_channel"
	HostErrMissingName          = "cli.host.err.missing_name"
	HostErrMissingPath          = "cli.host.err.missing_path"
	HostErrUnsupportedLang      = "cli.host.err.unsupported_lang"
	HostErrSessionLimit         = "cli.host.err.session_limit"
	HostFaultUnsupportedLang    = "cli.host.fault.unsupported_lang"
	HostServiceRestartFailed    = "cli.host.service.restart_failed"
	HostServiceUpdateFailed     = "cli.host.service.update_failed"
	HostServiceRebuildFailed    = "cli.host.service.rebuilt_restart_failed"
	HostSessionBudgetPersist    = "cli.host.session.budget_persist"
	HostErrMissingSoul          = "cli.host.err.missing_soul"
	HostErrMissingTo            = "cli.host.err.missing_to"
	HostErrConflictingChannel   = "cli.host.err.conflicting_channel"
	HostErrConflictingPlacement = "cli.host.err.conflicting_placement"
	HostErrCloneUnsupported     = "cli.host.err.clone_unsupported"
	HostErrCloneFailed          = "cli.host.err.clone_failed"
	HostErrNoHostRegistered     = "cli.host.err.no_host"
	HostErrAccountVendor        = "cli.host.err.account_vendor"
	HostErrAccountRemote        = "cli.host.err.account_remote"
	HostErrAccountHost          = "cli.host.err.account_host"
	HostScheduleScheduled       = "cli.host.schedule.scheduled"
	HostScheduleNoSession       = "cli.host.schedule.no_session"
	HostScheduleLastRun         = "cli.host.schedule.last_run"
	HostScheduleNextRun         = "cli.host.schedule.next_run"
	HostScheduleNever           = "cli.host.schedule.never"
	HostScheduleDue             = "cli.host.schedule.due"
	HostScheduleUnknown         = "cli.host.schedule.unknown"
	HostScheduleLive            = "cli.host.schedule.live"
	HostScheduleStatusPaused    = "cli.host.schedule.status_paused"
	HostErrWorktree             = "cli.host.err.worktree"
	HostErrProvisionAgent       = "cli.host.err.provision_agent"
	HostErrCreateChannel        = "cli.host.err.create_channel"
	HostErrCreateForumPost      = "cli.host.err.create_forum_post"
	HostErrPersist              = "cli.host.err.persist"
	HostErrInspectChannel       = "cli.host.err.inspect_channel"
	HostErrReadSoul             = "cli.host.err.read_soul"
	HostErrAccountOfAgent       = "cli.host.err.account_of_agent"
	HostAgentListHeader         = "cli.host.agent.list_header"
)

func hostKeys() map[Key][]string {
	return map[Key][]string{
		HostHomeSet:                 {ParamChannel, ParamKind},
		HostSourceSet:               {ParamPath},
		HostLangSet:                 {ParamLang},
		HostLangReset:               {ParamLang},
		HostSessionLangSet:          {ParamName, ParamLang},
		HostSessionLangReset:        {ParamName, ParamLang},
		HostErrUnsupportedLang:      {ParamLang},
		HostErrSessionLimit:         {ParamCount},
		HostServiceRestartFailed:    {ParamErr},
		HostServiceUpdateFailed:     {ParamErr},
		HostServiceRebuildFailed:    {ParamVersion, ParamErr},
		HostSessionBudgetPersist:    {ParamName, ParamErr},
		HostAgentCreated:            {ParamName, ParamHome},
		HostAgentListEmpty:          nil,
		HostAgentSoulUpdated:        {ParamName},
		HostServiceRestarting:       nil,
		HostServiceRebuilt:          {ParamVersion},
		HostSessionStartedPost:      {ParamName},
		HostCheckpointNone:          nil,
		HostCheckpointUndone:        {ParamName},
		HostMemoryRestored:          {ParamKey},
		HostMemoryUnlinked:          {ParamFrom, ParamTo},
		HostScheduleNone:            nil,
		HostScheduleRemoved:         {ParamName},
		HostScheduleFired:           {ParamName},
		HostSchedulePaused:          {ParamName},
		HostScheduleResumed:         {ParamName},
		HostApproveNone:             nil,
		HostApproveAllowed:          {ParamID},
		HostApproveDenied:           {ParamID},
		HostApproveRuleRemoved:      {ParamRule},
		HostApproveRuleAdded:        {ParamRule},
		HostApproveRuleNone:         nil,
		HostModelsNone:              nil,
		HostModelsHidden:            nil,
		HostModelsRefreshNone:       nil,
		HostModelsDiscovered:        {ParamCount},
		HostModelsDiscoveryKept:     {ParamErr},
		HostRoleNone:                nil,
		HostRoleFirstGrant:          nil,
		HostRoleGrantable:           {ParamRoles},
		HostRoleAgentNote:           nil,
		HostRoleGranted:             {ParamPrincipal, ParamRole},
		HostRoleRevokedLast:         {ParamPrincipal},
		HostRoleRevoked:             {ParamPrincipal},
		HostRoleHolds:               {ParamPrincipal, ParamRole},
		HostModelsDiscoveryStatic:   {ParamErr},
		HostMemoryForgotten:         {ParamKey},
		HostMemoryRecorded:          {ParamKey},
		HostMemorySkillsNone:        nil,
		HostMemorySkillLastUsed:     {ParamWhen},
		HostMemorySkillRevoked:      {ParamKey},
		HostMemorySkillApproved:     {ParamKey},
		HostMemorySearchNone:        nil,
		HostApproveModeSet:          {ParamName, ParamMode},
		HostApproveSmartNoBackend:   {ParamName, ParamMode},
		HostApproveBypassNoHook:     {ParamName, ParamMode},
		HostCheckpointRewound:       {ParamName, ParamPosition, ParamWhat},
		HostScheduleEvery:           {ParamEvery},
		HostScheduleCron:            {ParamCron},
		HostHandoffSession:          nil,
		HostHandoffSessionNamed:     {ParamName},
		HostHandoffVendorDefault:    nil,
		HostHandoffPointer:          {ParamWhere, ParamTo},
		HostHandoffReport:           {ParamTarget, ParamSource, ParamAccount, ParamReply, ParamBrief},
		HostErrMissingChannel:       nil,
		HostErrMissingName:          nil,
		HostErrMissingPath:          nil,
		HostErrMissingSoul:          nil,
		HostErrMissingTo:            nil,
		HostErrConflictingChannel:   nil,
		HostErrConflictingPlacement: nil,
		HostErrCloneUnsupported:     {ParamHost},
		HostErrCloneFailed:          {ParamErr},
		HostErrNoHostRegistered:     {ParamHost},
		HostErrAccountVendor:        {ParamAccount},
		HostErrAccountRemote:        {ParamAccount},
		HostErrAccountHost:          {ParamAccount, ParamHost},
		HostScheduleScheduled:       {ParamName, ParamCadence},
		HostScheduleNoSession:       {ParamName},
		HostScheduleLastRun:         {ParamWhen},
		HostScheduleNextRun:         {ParamWhen},
		HostScheduleNever:           nil,
		HostScheduleDue:             nil,
		HostScheduleUnknown:         nil,
		HostScheduleLive:            nil,
		HostScheduleStatusPaused:    nil,
		HostErrWorktree:             {ParamErr},
		HostErrProvisionAgent:       {ParamAgent, ParamErr},
		HostErrCreateChannel:        {ParamErr},
		HostErrCreateForumPost:      {ParamErr},
		HostErrPersist:              {ParamErr},
		HostErrInspectChannel:       {ParamErr},
		HostErrReadSoul:             {ParamErr},
		HostErrAccountOfAgent:       {ParamAgent, ParamErr},
		HostAgentListHeader:         nil,
	}
}
