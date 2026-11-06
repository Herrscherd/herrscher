package clitext

// CmdHelp and CmdParam name the help texts of the commands and their params.
const (
	CmdHelpAgentCreate                    = "cli.help.cmd.agent.create"
	CmdHelpAgentList                      = "cli.help.cmd.agent.list"
	CmdHelpAgentSetSoul                   = "cli.help.cmd.agent.set_soul"
	CmdHelpAgentShow                      = "cli.help.cmd.agent.show"
	CmdHelpApproveAllow                   = "cli.help.cmd.approve.allow"
	CmdHelpApproveAsk                     = "cli.help.cmd.approve.ask"
	CmdHelpApproveDeny                    = "cli.help.cmd.approve.deny"
	CmdHelpApproveList                    = "cli.help.cmd.approve.list"
	CmdHelpApproveMode                    = "cli.help.cmd.approve.mode"
	CmdHelpApproveRule                    = "cli.help.cmd.approve.rule"
	CmdHelpCommands                       = "cli.help.cmd.commands"
	CmdHelpCommandsWarning                = "cli.help.commands_warning"
	CmdHelpHostAdd                        = "cli.help.cmd.host.add"
	CmdHelpHostCheck                      = "cli.help.cmd.host.check"
	CmdHelpHostList                       = "cli.help.cmd.host.list"
	CmdHelpHostProvision                  = "cli.help.cmd.host.provision"
	CmdHelpHostRm                         = "cli.help.cmd.host.rm"
	CmdHelpMemoryForget                   = "cli.help.cmd.memory.forget"
	CmdHelpMemoryLocate                   = "cli.help.cmd.memory.locate"
	CmdHelpMemoryRecord                   = "cli.help.cmd.memory.record"
	CmdHelpMemoryRestore                  = "cli.help.cmd.memory.restore"
	CmdHelpMemorySearch                   = "cli.help.cmd.memory.search"
	CmdHelpMemoryUnlink                   = "cli.help.cmd.memory.unlink"
	CmdHelpModelsList                     = "cli.help.cmd.models.list"
	CmdHelpModelsRefresh                  = "cli.help.cmd.models.refresh"
	CmdHelpRoleGrant                      = "cli.help.cmd.role.grant"
	CmdHelpRoleList                       = "cli.help.cmd.role.list"
	CmdHelpRoleRevoke                     = "cli.help.cmd.role.revoke"
	CmdHelpRoleShow                       = "cli.help.cmd.role.show"
	CmdHelpScheduleAdd                    = "cli.help.cmd.schedule.add"
	CmdHelpScheduleList                   = "cli.help.cmd.schedule.list"
	CmdHelpSchedulePause                  = "cli.help.cmd.schedule.pause"
	CmdHelpScheduleResume                 = "cli.help.cmd.schedule.resume"
	CmdHelpScheduleRm                     = "cli.help.cmd.schedule.rm"
	CmdHelpScheduleRun                    = "cli.help.cmd.schedule.run"
	CmdHelpServiceRestart                 = "cli.help.cmd.service.restart"
	CmdHelpServiceUpdate                  = "cli.help.cmd.service.update"
	CmdHelpSessionAccounts                = "cli.help.cmd.session.accounts"
	CmdHelpSessionArchive                 = "cli.help.cmd.session.archive"
	CmdHelpSessionCheckpoints             = "cli.help.cmd.session.checkpoints"
	CmdHelpSessionClose                   = "cli.help.cmd.session.close"
	CmdHelpSessionCompact                 = "cli.help.cmd.session.compact"
	CmdHelpSessionCreate                  = "cli.help.cmd.session.create"
	CmdHelpSessionFork                    = "cli.help.cmd.session.fork"
	CmdHelpSessionGetMode                 = "cli.help.cmd.session.get_mode"
	CmdHelpSessionHandoff                 = "cli.help.cmd.session.handoff"
	CmdHelpSessionInfo                    = "cli.help.cmd.session.info"
	CmdHelpSessionInterrupt               = "cli.help.cmd.session.interrupt"
	CmdHelpSessionList                    = "cli.help.cmd.session.list"
	CmdHelpSessionLog                     = "cli.help.cmd.session.log"
	CmdHelpSessionOverview                = "cli.help.cmd.session.overview"
	CmdHelpSessionPick                    = "cli.help.cmd.session.pick"
	CmdHelpSessionRename                  = "cli.help.cmd.session.rename"
	CmdHelpSessionResume                  = "cli.help.cmd.session.resume"
	CmdHelpSessionRewind                  = "cli.help.cmd.session.rewind"
	CmdHelpSessionScrollback              = "cli.help.cmd.session.scrollback"
	CmdHelpSessionSeed                    = "cli.help.cmd.session.seed"
	CmdHelpSessionSeen                    = "cli.help.cmd.session.seen"
	CmdHelpSessionSend                    = "cli.help.cmd.session.send"
	CmdHelpSessionSetAccount              = "cli.help.cmd.session.set_account"
	CmdHelpSessionSetBudget               = "cli.help.cmd.session.set_budget"
	CmdHelpSessionSetCompact              = "cli.help.cmd.session.set_compact"
	CmdHelpSessionSetMode                 = "cli.help.cmd.session.set_mode"
	CmdHelpSessionShell                   = "cli.help.cmd.session.shell"
	CmdHelpSessionSwitch                  = "cli.help.cmd.session.switch"
	CmdHelpSessionUndo                    = "cli.help.cmd.session.undo"
	CmdHelpSessionWho                     = "cli.help.cmd.session.who"
	CmdHelpSetHome                        = "cli.help.cmd.set.home"
	CmdHelpSetSource                      = "cli.help.cmd.set.source"
	CmdHelpSetLang                        = "cli.help.cmd.set.lang"
	CmdHelpSessionLang                    = "cli.help.cmd.session.lang"
	CmdHelpSkillApprove                   = "cli.help.cmd.skill.approve"
	CmdHelpSkillList                      = "cli.help.cmd.skill.list"
	CmdHelpIdentity                       = "cli.help.cmd.whoami"
	CmdParamAgentCreateBackend            = "cli.help.param.agent.create.backend"
	CmdParamAgentCreateCmd                = "cli.help.param.agent.create.cmd"
	CmdParamAgentCreateHost               = "cli.help.param.agent.create.host"
	CmdParamAgentCreateMcp                = "cli.help.param.agent.create.mcp"
	CmdParamAgentCreateName               = "cli.help.param.agent.create.name"
	CmdParamAgentCreateSoul               = "cli.help.param.agent.create.soul"
	CmdParamAgentCreateTags               = "cli.help.param.agent.create.tags"
	CmdParamAgentSetSoulName              = "cli.help.param.agent.set_soul.name"
	CmdParamAgentSetSoulSoul              = "cli.help.param.agent.set_soul.soul"
	CmdParamAgentShowName                 = "cli.help.param.agent.show.name"
	CmdParamApproveAllowAlways            = "cli.help.param.approve.allow.always"
	CmdParamApproveAllowID                = "cli.help.param.approve.allow.id"
	CmdParamApproveAllowSession           = "cli.help.param.approve.allow.session"
	CmdParamApproveAskSession             = "cli.help.param.approve.ask.session"
	CmdParamApproveAskSubject             = "cli.help.param.approve.ask.subject"
	CmdParamApproveAskTool                = "cli.help.param.approve.ask.tool"
	CmdParamApproveDenyID                 = "cli.help.param.approve.deny.id"
	CmdParamApproveDenyReason             = "cli.help.param.approve.deny.reason"
	CmdParamApproveModeMode               = "cli.help.param.approve.mode.mode"
	CmdParamApproveModeSession            = "cli.help.param.approve.mode.session"
	CmdParamApproveRuleRm                 = "cli.help.param.approve.rule.rm"
	CmdParamApproveRuleRule               = "cli.help.param.approve.rule.rule"
	CmdParamHostAddName                   = "cli.help.param.host.add.name"
	CmdParamHostAddSSH                    = "cli.help.param.host.add.ssh"
	CmdParamHostAddWorkspace              = "cli.help.param.host.add.workspace"
	CmdParamHostCheckName                 = "cli.help.param.host.check.name"
	CmdParamHostProvisionName             = "cli.help.param.host.provision.name"
	CmdParamHostRmName                    = "cli.help.param.host.rm.name"
	CmdParamMemoryForgetKey               = "cli.help.param.memory.forget.key"
	CmdParamMemoryLocateKey               = "cli.help.param.memory.locate.key"
	CmdParamMemoryRecordBody              = "cli.help.param.memory.record.body"
	CmdParamMemoryRecordKey               = "cli.help.param.memory.record.key"
	CmdParamMemoryRecordKind              = "cli.help.param.memory.record.kind"
	CmdParamMemoryRecordTitle             = "cli.help.param.memory.record.title"
	CmdParamMemoryRestoreForce            = "cli.help.param.memory.restore.force"
	CmdParamMemoryRestoreKey              = "cli.help.param.memory.restore.key"
	CmdParamMemorySearchLimit             = "cli.help.param.memory.search.limit"
	CmdParamMemorySearchRaw               = "cli.help.param.memory.search.raw"
	CmdParamMemorySearchText              = "cli.help.param.memory.search.text"
	CmdParamMemoryUnlinkFrom              = "cli.help.param.memory.unlink.from"
	CmdParamMemoryUnlinkTo                = "cli.help.param.memory.unlink.to"
	CmdParamModelsListAll                 = "cli.help.param.models.list.all"
	CmdParamModelsListState               = "cli.help.param.models.list.state"
	CmdParamModelsRefreshState            = "cli.help.param.models.refresh.state"
	CmdParamRoleGrantLabel                = "cli.help.param.role.grant.label"
	CmdParamRoleGrantPrincipal            = "cli.help.param.role.grant.principal"
	CmdParamRoleGrantRole                 = "cli.help.param.role.grant.role"
	CmdParamRoleRevokePrincipal           = "cli.help.param.role.revoke.principal"
	CmdParamRoleShowPrincipal             = "cli.help.param.role.show.principal"
	CmdParamScheduleAddAgent              = "cli.help.param.schedule.add.agent"
	CmdParamScheduleAddCron               = "cli.help.param.schedule.add.cron"
	CmdParamScheduleAddEvery              = "cli.help.param.schedule.add.every"
	CmdParamScheduleAddGrace              = "cli.help.param.schedule.add.grace"
	CmdParamScheduleAddName               = "cli.help.param.schedule.add.name"
	CmdParamScheduleAddProject            = "cli.help.param.schedule.add.project"
	CmdParamScheduleAddSession            = "cli.help.param.schedule.add.session"
	CmdParamScheduleAddTask               = "cli.help.param.schedule.add.task"
	CmdParamScheduleName                  = "cli.help.param.schedule.name"
	CmdParamScheduleRmName                = "cli.help.param.schedule.rm.name"
	CmdParamScheduleRunName               = "cli.help.param.schedule.run.name"
	CmdParamServiceUpdateNoPull           = "cli.help.param.service.update.no_pull"
	CmdParamSessionAccountsName           = "cli.help.param.session.accounts.name"
	CmdParamSessionArchiveName            = "cli.help.param.session.archive.name"
	CmdParamSessionCheckpointsName        = "cli.help.param.session.checkpoints.name"
	CmdParamSessionCloseForce             = "cli.help.param.session.close.force"
	CmdParamSessionCloseName              = "cli.help.param.session.close.name"
	CmdParamSessionCompactInstruction     = "cli.help.param.session.compact.instruction"
	CmdParamSessionCompactName            = "cli.help.param.session.compact.name"
	CmdParamSessionCompactOrigin          = "cli.help.param.session.compact.origin"
	CmdParamSessionCreateAccount          = "cli.help.param.session.create.account"
	CmdParamSessionCreateAgent            = "cli.help.param.session.create.agent"
	CmdParamSessionCreateApprovals        = "cli.help.param.session.create.approvals"
	CmdParamSessionCreateBackend          = "cli.help.param.session.create.backend"
	CmdParamSessionCreateBase             = "cli.help.param.session.create.base"
	CmdParamSessionCreateChannelID        = "cli.help.param.session.create.channel_id"
	CmdParamSessionCreateClone            = "cli.help.param.session.create.clone"
	CmdParamSessionCreateCmd              = "cli.help.param.session.create.cmd"
	CmdParamSessionCreateCohortCostCap    = "cli.help.param.session.create.cohort_cost_cap"
	CmdParamSessionCreateCohortTokCap     = "cli.help.param.session.create.cohort_token_cap"
	CmdParamSessionCreateConsolidateEvery = "cli.help.param.session.create.consolidate_every"
	CmdParamSessionCreateCostCap          = "cli.help.param.session.create.cost_cap"
	CmdParamSessionCreateExtractor        = "cli.help.param.session.create.extractor"
	CmdParamSessionCreateGateways         = "cli.help.param.session.create.gateways"
	CmdParamSessionCreateHost             = "cli.help.param.session.create.host"
	CmdParamSessionCreateJournal          = "cli.help.param.session.create.journal"
	CmdParamSessionCreateLang             = "cli.help.param.session.create.lang"
	CmdParamSessionCreateMemoryAgent      = "cli.help.param.session.create.memory_agent"
	CmdParamSessionCreateMemoryProject    = "cli.help.param.session.create.memory_project"
	CmdParamSessionCreateModel            = "cli.help.param.session.create.model"
	CmdParamSessionCreateName             = "cli.help.param.session.create.name"
	CmdParamSessionCreateParent           = "cli.help.param.session.create.parent"
	CmdParamSessionCreateProject          = "cli.help.param.session.create.project"
	CmdParamSessionCreateProjectPinned    = "cli.help.param.session.create.project_pinned"
	CmdParamSessionCreateShared           = "cli.help.param.session.create.shared"
	CmdParamSessionCreateTerminalOnly     = "cli.help.param.session.create.terminal_only"
	CmdParamSessionCreateTokCap           = "cli.help.param.session.create.token_cap"
	CmdParamSessionCreateUnder            = "cli.help.param.session.create.under"
	CmdParamSessionCreateVendor           = "cli.help.param.session.create.vendor"
	CmdParamSessionForkAs                 = "cli.help.param.session.fork.as"
	CmdParamSessionForkName               = "cli.help.param.session.fork.name"
	CmdParamSessionForkTo                 = "cli.help.param.session.fork.to"
	CmdParamSessionGetModeName            = "cli.help.param.session.get_mode.name"
	CmdParamSessionHandoffAccount         = "cli.help.param.session.handoff.account"
	CmdParamSessionHandoffChannelID       = "cli.help.param.session.handoff.channel_id"
	CmdParamSessionHandoffModel           = "cli.help.param.session.handoff.model"
	CmdParamSessionHandoffName            = "cli.help.param.session.handoff.name"
	CmdParamSessionHandoffProject         = "cli.help.param.session.handoff.project"
	CmdParamSessionHandoffTo              = "cli.help.param.session.handoff.to"
	CmdParamSessionHandoffUnder           = "cli.help.param.session.handoff.under"
	CmdParamSessionHandoffVendor          = "cli.help.param.session.handoff.vendor"
	CmdParamSessionInterruptName          = "cli.help.param.session.interrupt.name"
	CmdParamSessionLogLimit               = "cli.help.param.session.log.limit"
	CmdParamSessionLogName                = "cli.help.param.session.log.name"
	CmdParamSessionPickName               = "cli.help.param.session.pick.name"
	CmdParamSessionPickValue              = "cli.help.param.session.pick.value"
	CmdParamSessionRenameName             = "cli.help.param.session.rename.name"
	CmdParamSessionRenameTo               = "cli.help.param.session.rename.to"
	CmdParamSessionResumeName             = "cli.help.param.session.resume.name"
	CmdParamSessionRewindName             = "cli.help.param.session.rewind.name"
	CmdParamSessionRewindTo               = "cli.help.param.session.rewind.to"
	CmdParamSessionRewindWhat             = "cli.help.param.session.rewind.what"
	CmdParamSessionScrollbackName         = "cli.help.param.session.scrollback.name"
	CmdParamSessionSeedName               = "cli.help.param.session.seed.name"
	CmdParamSessionSeedTask               = "cli.help.param.session.seed.task"
	CmdParamSessionSeedTimeout            = "cli.help.param.session.seed.timeout"
	CmdParamSessionSeedTurnID             = "cli.help.param.session.seed.turn_id"
	CmdParamSessionSeenName               = "cli.help.param.session.seen.name"
	CmdParamSessionSendAttachments        = "cli.help.param.session.send.attachments"
	CmdParamSessionSendAuthor             = "cli.help.param.session.send.author"
	CmdParamSessionSendName               = "cli.help.param.session.send.name"
	CmdParamSessionSendOrigin             = "cli.help.param.session.send.origin"
	CmdParamSessionSendText               = "cli.help.param.session.send.text"
	CmdParamSessionSetAccountFresh        = "cli.help.param.session.set_account.fresh"
	CmdParamSessionSetAccountName         = "cli.help.param.session.set_account.name"
	CmdParamSessionSetAccountProfile      = "cli.help.param.session.set_account.profile"
	CmdParamSessionSetBudgetCohortCostCap = "cli.help.param.session.set_budget.cohort_cost_cap"
	CmdParamSessionSetBudgetCohortTokCap  = "cli.help.param.session.set_budget.cohort_token_cap"
	CmdParamSessionSetBudgetCostCap       = "cli.help.param.session.set_budget.cost_cap"
	CmdParamSessionSetBudgetName          = "cli.help.param.session.set_budget.name"
	CmdParamSessionSetBudgetScope         = "cli.help.param.session.set_budget.scope"
	CmdParamSessionSetBudgetTokCap        = "cli.help.param.session.set_budget.token_cap"
	CmdParamSessionSetCompactName         = "cli.help.param.session.set_compact.name"
	CmdParamSessionSetCompactPercent      = "cli.help.param.session.set_compact.percent"
	CmdParamSessionSetModeMode            = "cli.help.param.session.set_mode.mode"
	CmdParamSessionSetModeName            = "cli.help.param.session.set_mode.name"
	CmdParamSessionShellCmd               = "cli.help.param.session.shell.cmd"
	CmdParamSessionShellName              = "cli.help.param.session.shell.name"
	CmdParamSessionSwitchCmd              = "cli.help.param.session.switch.cmd"
	CmdParamSessionSwitchHandoff          = "cli.help.param.session.switch.handoff"
	CmdParamSessionSwitchModel            = "cli.help.param.session.switch.model"
	CmdParamSessionSwitchName             = "cli.help.param.session.switch.name"
	CmdParamSessionSwitchVendor           = "cli.help.param.session.switch.vendor"
	CmdParamSessionUndoName               = "cli.help.param.session.undo.name"
	CmdParamSessionWhoName                = "cli.help.param.session.who.name"
	CmdParamSetHomeChannel                = "cli.help.param.set.home.channel"
	CmdParamSetSourcePath                 = "cli.help.param.set.source.path"
	CmdParamSetLangLang                   = "cli.help.param.set.lang.lang"
	CmdParamSessionLangLang               = "cli.help.param.session.lang.lang"
	CmdParamSessionLangName               = "cli.help.param.session.lang.name"
	CmdParamSkillApproveKey               = "cli.help.param.skill.approve.key"
	CmdParamSkillApproveRevoke            = "cli.help.param.skill.approve.revoke"
	CmdParamSkillListLimit                = "cli.help.param.skill.list.limit"
)

func helpKeys() map[Key][]string {
	return map[Key][]string{
		CmdHelpAgentCreate:                    nil,
		CmdHelpAgentList:                      nil,
		CmdHelpAgentSetSoul:                   nil,
		CmdHelpAgentShow:                      nil,
		CmdHelpApproveAllow:                   nil,
		CmdHelpApproveAsk:                     nil,
		CmdHelpApproveDeny:                    nil,
		CmdHelpApproveList:                    nil,
		CmdHelpApproveMode:                    nil,
		CmdHelpApproveRule:                    nil,
		CmdHelpCommands:                       nil,
		CmdHelpCommandsWarning:                {ParamErr},
		CmdHelpHostAdd:                        nil,
		CmdHelpHostCheck:                      nil,
		CmdHelpHostList:                       nil,
		CmdHelpHostProvision:                  nil,
		CmdHelpHostRm:                         nil,
		CmdHelpMemoryForget:                   nil,
		CmdHelpMemoryLocate:                   nil,
		CmdHelpMemoryRecord:                   nil,
		CmdHelpMemoryRestore:                  nil,
		CmdHelpMemorySearch:                   nil,
		CmdHelpMemoryUnlink:                   nil,
		CmdHelpModelsList:                     nil,
		CmdHelpModelsRefresh:                  nil,
		CmdHelpRoleGrant:                      nil,
		CmdHelpRoleList:                       nil,
		CmdHelpRoleRevoke:                     nil,
		CmdHelpRoleShow:                       nil,
		CmdHelpScheduleAdd:                    nil,
		CmdHelpScheduleList:                   nil,
		CmdHelpSchedulePause:                  nil,
		CmdHelpScheduleResume:                 nil,
		CmdHelpScheduleRm:                     nil,
		CmdHelpScheduleRun:                    nil,
		CmdHelpServiceRestart:                 nil,
		CmdHelpServiceUpdate:                  nil,
		CmdHelpSessionAccounts:                nil,
		CmdHelpSessionArchive:                 nil,
		CmdHelpSessionCheckpoints:             nil,
		CmdHelpSessionClose:                   nil,
		CmdHelpSessionCompact:                 nil,
		CmdHelpSessionCreate:                  nil,
		CmdHelpSessionFork:                    nil,
		CmdHelpSessionGetMode:                 nil,
		CmdHelpSessionHandoff:                 nil,
		CmdHelpSessionInfo:                    nil,
		CmdHelpSessionInterrupt:               nil,
		CmdHelpSessionList:                    nil,
		CmdHelpSessionLog:                     nil,
		CmdHelpSessionOverview:                nil,
		CmdHelpSessionPick:                    nil,
		CmdHelpSessionRename:                  nil,
		CmdHelpSessionResume:                  nil,
		CmdHelpSessionRewind:                  nil,
		CmdHelpSessionScrollback:              nil,
		CmdHelpSessionSeed:                    nil,
		CmdHelpSessionSeen:                    nil,
		CmdHelpSessionSend:                    nil,
		CmdHelpSessionSetAccount:              nil,
		CmdHelpSessionSetBudget:               nil,
		CmdHelpSessionSetCompact:              nil,
		CmdHelpSessionSetMode:                 nil,
		CmdHelpSessionShell:                   nil,
		CmdHelpSessionSwitch:                  nil,
		CmdHelpSessionUndo:                    nil,
		CmdHelpSessionWho:                     nil,
		CmdHelpSetHome:                        nil,
		CmdHelpSetSource:                      nil,
		CmdHelpSetLang:                        nil,
		CmdHelpSessionLang:                    nil,
		CmdHelpSkillApprove:                   nil,
		CmdHelpSkillList:                      nil,
		CmdHelpIdentity:                       nil,
		CmdParamAgentCreateBackend:            nil,
		CmdParamAgentCreateCmd:                nil,
		CmdParamAgentCreateHost:               nil,
		CmdParamAgentCreateMcp:                nil,
		CmdParamAgentCreateName:               nil,
		CmdParamAgentCreateSoul:               nil,
		CmdParamAgentCreateTags:               nil,
		CmdParamAgentSetSoulName:              nil,
		CmdParamAgentSetSoulSoul:              nil,
		CmdParamAgentShowName:                 nil,
		CmdParamApproveAllowAlways:            nil,
		CmdParamApproveAllowID:                nil,
		CmdParamApproveAllowSession:           nil,
		CmdParamApproveAskSession:             nil,
		CmdParamApproveAskSubject:             nil,
		CmdParamApproveAskTool:                nil,
		CmdParamApproveDenyID:                 nil,
		CmdParamApproveDenyReason:             nil,
		CmdParamApproveModeMode:               nil,
		CmdParamApproveModeSession:            nil,
		CmdParamApproveRuleRm:                 nil,
		CmdParamApproveRuleRule:               nil,
		CmdParamHostAddName:                   nil,
		CmdParamHostAddSSH:                    nil,
		CmdParamHostAddWorkspace:              nil,
		CmdParamHostCheckName:                 nil,
		CmdParamHostProvisionName:             nil,
		CmdParamHostRmName:                    nil,
		CmdParamMemoryForgetKey:               nil,
		CmdParamMemoryLocateKey:               nil,
		CmdParamMemoryRecordBody:              nil,
		CmdParamMemoryRecordKey:               nil,
		CmdParamMemoryRecordKind:              nil,
		CmdParamMemoryRecordTitle:             nil,
		CmdParamMemoryRestoreForce:            nil,
		CmdParamMemoryRestoreKey:              nil,
		CmdParamMemorySearchLimit:             nil,
		CmdParamMemorySearchRaw:               nil,
		CmdParamMemorySearchText:              nil,
		CmdParamMemoryUnlinkFrom:              nil,
		CmdParamMemoryUnlinkTo:                nil,
		CmdParamModelsListAll:                 nil,
		CmdParamModelsListState:               nil,
		CmdParamModelsRefreshState:            nil,
		CmdParamRoleGrantLabel:                nil,
		CmdParamRoleGrantPrincipal:            nil,
		CmdParamRoleGrantRole:                 {"Roles"},
		CmdParamRoleRevokePrincipal:           nil,
		CmdParamRoleShowPrincipal:             nil,
		CmdParamScheduleAddAgent:              nil,
		CmdParamScheduleAddCron:               nil,
		CmdParamScheduleAddEvery:              nil,
		CmdParamScheduleAddGrace:              nil,
		CmdParamScheduleAddName:               nil,
		CmdParamScheduleAddProject:            nil,
		CmdParamScheduleAddSession:            nil,
		CmdParamScheduleAddTask:               nil,
		CmdParamScheduleName:                  nil,
		CmdParamScheduleRmName:                nil,
		CmdParamScheduleRunName:               nil,
		CmdParamServiceUpdateNoPull:           nil,
		CmdParamSessionAccountsName:           nil,
		CmdParamSessionArchiveName:            nil,
		CmdParamSessionCheckpointsName:        nil,
		CmdParamSessionCloseForce:             nil,
		CmdParamSessionCloseName:              nil,
		CmdParamSessionCompactInstruction:     nil,
		CmdParamSessionCompactName:            nil,
		CmdParamSessionCompactOrigin:          nil,
		CmdParamSessionCreateAccount:          nil,
		CmdParamSessionCreateAgent:            nil,
		CmdParamSessionCreateApprovals:        nil,
		CmdParamSessionCreateBackend:          nil,
		CmdParamSessionCreateBase:             nil,
		CmdParamSessionCreateChannelID:        nil,
		CmdParamSessionCreateClone:            nil,
		CmdParamSessionCreateCmd:              nil,
		CmdParamSessionCreateCohortCostCap:    nil,
		CmdParamSessionCreateCohortTokCap:     nil,
		CmdParamSessionCreateConsolidateEvery: nil,
		CmdParamSessionCreateCostCap:          nil,
		CmdParamSessionCreateExtractor:        nil,
		CmdParamSessionCreateGateways:         nil,
		CmdParamSessionCreateHost:             nil,
		CmdParamSessionCreateJournal:          nil,
		CmdParamSessionCreateLang:             nil,
		CmdParamSessionCreateMemoryAgent:      nil,
		CmdParamSessionCreateMemoryProject:    nil,
		CmdParamSessionCreateModel:            nil,
		CmdParamSessionCreateName:             nil,
		CmdParamSessionCreateParent:           nil,
		CmdParamSessionCreateProject:          nil,
		CmdParamSessionCreateProjectPinned:    nil,
		CmdParamSessionCreateShared:           nil,
		CmdParamSessionCreateTerminalOnly:     nil,
		CmdParamSessionCreateTokCap:           nil,
		CmdParamSessionCreateUnder:            nil,
		CmdParamSessionCreateVendor:           nil,
		CmdParamSessionForkAs:                 nil,
		CmdParamSessionForkName:               nil,
		CmdParamSessionForkTo:                 nil,
		CmdParamSessionGetModeName:            nil,
		CmdParamSessionHandoffAccount:         nil,
		CmdParamSessionHandoffChannelID:       nil,
		CmdParamSessionHandoffModel:           nil,
		CmdParamSessionHandoffName:            nil,
		CmdParamSessionHandoffProject:         nil,
		CmdParamSessionHandoffTo:              nil,
		CmdParamSessionHandoffUnder:           nil,
		CmdParamSessionHandoffVendor:          nil,
		CmdParamSessionInterruptName:          nil,
		CmdParamSessionLogLimit:               nil,
		CmdParamSessionLogName:                nil,
		CmdParamSessionPickName:               nil,
		CmdParamSessionPickValue:              nil,
		CmdParamSessionRenameName:             nil,
		CmdParamSessionRenameTo:               nil,
		CmdParamSessionResumeName:             nil,
		CmdParamSessionRewindName:             nil,
		CmdParamSessionRewindTo:               nil,
		CmdParamSessionRewindWhat:             nil,
		CmdParamSessionScrollbackName:         nil,
		CmdParamSessionSeedName:               nil,
		CmdParamSessionSeedTask:               nil,
		CmdParamSessionSeedTimeout:            nil,
		CmdParamSessionSeedTurnID:             nil,
		CmdParamSessionSeenName:               nil,
		CmdParamSessionSendAttachments:        nil,
		CmdParamSessionSendAuthor:             nil,
		CmdParamSessionSendName:               nil,
		CmdParamSessionSendOrigin:             nil,
		CmdParamSessionSendText:               nil,
		CmdParamSessionSetAccountFresh:        nil,
		CmdParamSessionSetAccountName:         nil,
		CmdParamSessionSetAccountProfile:      nil,
		CmdParamSessionSetBudgetCohortCostCap: nil,
		CmdParamSessionSetBudgetCohortTokCap:  nil,
		CmdParamSessionSetBudgetCostCap:       nil,
		CmdParamSessionSetBudgetName:          nil,
		CmdParamSessionSetBudgetScope:         nil,
		CmdParamSessionSetBudgetTokCap:        nil,
		CmdParamSessionSetCompactName:         nil,
		CmdParamSessionSetCompactPercent:      nil,
		CmdParamSessionSetModeMode:            nil,
		CmdParamSessionSetModeName:            nil,
		CmdParamSessionShellCmd:               nil,
		CmdParamSessionShellName:              nil,
		CmdParamSessionSwitchCmd:              nil,
		CmdParamSessionSwitchHandoff:          nil,
		CmdParamSessionSwitchModel:            nil,
		CmdParamSessionSwitchName:             nil,
		CmdParamSessionSwitchVendor:           nil,
		CmdParamSessionUndoName:               nil,
		CmdParamSessionWhoName:                nil,
		CmdParamSetHomeChannel:                nil,
		CmdParamSetSourcePath:                 nil,
		CmdParamSetLangLang:                   nil,
		CmdParamSessionLangLang:               nil,
		CmdParamSessionLangName:               nil,
		CmdParamSkillApproveKey:               nil,
		CmdParamSkillApproveRevoke:            nil,
		CmdParamSkillListLimit:                nil,
	}
}
