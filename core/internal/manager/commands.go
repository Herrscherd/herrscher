package manager

import (
	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/clitext"
)

// Commands returns the manager's command set as neutral contracts.Cmd values for
// the CLI registry to dispatch. Each Run closes over the Handler's dependencies,
// so the registry holding these stays agnostic of the gateway, git, or the backend.
func (h *Handler) Commands() []contracts.Cmd {
	return []contracts.Cmd{
		contracts.New("session", "create").
			Help(clitext.CmdHelpSessionCreate).
			Param("name", clitext.CmdParamSessionCreateName, true).
			Param("channel_id", clitext.CmdParamSessionCreateChannelID, false).
			Param("under", clitext.CmdParamSessionCreateUnder, false).
			Param("project", clitext.CmdParamSessionCreateProject, false).
			Param("clone", clitext.CmdParamSessionCreateClone, false).
			Param("cmd", clitext.CmdParamSessionCreateCmd, false).
			Param("backend", clitext.CmdParamSessionCreateBackend, false).
			Param("vendor", clitext.CmdParamSessionCreateVendor, false).
			Param("model", clitext.CmdParamSessionCreateModel, false).
			Param("gateways", clitext.CmdParamSessionCreateGateways, false).
			Param("terminal_only", clitext.CmdParamSessionCreateTerminalOnly, false).
			Param("shared", clitext.CmdParamSessionCreateShared, false).
			Param("agent", clitext.CmdParamSessionCreateAgent, false).
			Param("account", clitext.CmdParamSessionCreateAccount, false).
			Param("host", clitext.CmdParamSessionCreateHost, false).
			Param("approvals", clitext.CmdParamSessionCreateApprovals, false).
			Param("memory_project", clitext.CmdParamSessionCreateMemoryProject, false).
			Param("memory_agent", clitext.CmdParamSessionCreateMemoryAgent, false).
			Param("project_pinned", clitext.CmdParamSessionCreateProjectPinned, false).
			Param("extractor", clitext.CmdParamSessionCreateExtractor, false).
			Param("journal", clitext.CmdParamSessionCreateJournal, false).
			Param("consolidate_every", clitext.CmdParamSessionCreateConsolidateEvery, false).
			Param(langParam, clitext.CmdParamSessionCreateLang, false).
			Param("base", clitext.CmdParamSessionCreateBase, false).
			Param("parent", clitext.CmdParamSessionCreateParent, false).
			Param("cost_cap", clitext.CmdParamSessionCreateCostCap, false).
			Param("token_cap", clitext.CmdParamSessionCreateTokCap, false).
			Param("cohort_cost_cap", clitext.CmdParamSessionCreateCohortCostCap, false).
			Param("cohort_token_cap", clitext.CmdParamSessionCreateCohortTokCap, false).
			Do(h.sessionCreateRun),
		contracts.New("session", "close").
			Help(clitext.CmdHelpSessionClose).
			Param("name", clitext.CmdParamSessionCloseName, true).
			Param("force", clitext.CmdParamSessionCloseForce, false).
			Do(h.sessionCloseRun),
		contracts.New("session", "archive").
			Help(clitext.CmdHelpSessionArchive).
			Param("name", clitext.CmdParamSessionArchiveName, true).
			Do(h.sessionArchiveRun),
		contracts.New("session", "rename").
			Help(clitext.CmdHelpSessionRename).
			Param("name", clitext.CmdParamSessionRenameName, true).
			Param("to", clitext.CmdParamSessionRenameTo, true).
			Do(h.sessionRenameRun),
		contracts.New("session", "list").
			Help(clitext.CmdHelpSessionList).
			Do(h.sessionListRun),
		contracts.New("session", "who").
			Help(clitext.CmdHelpSessionWho).
			Param("name", clitext.CmdParamSessionWhoName, true).
			Do(h.sessionWhoRun),
		contracts.New("session", "log").
			Help(clitext.CmdHelpSessionLog).
			Param("name", clitext.CmdParamSessionLogName, true).
			Param("limit", clitext.CmdParamSessionLogLimit, false).
			Do(h.sessionLogRun),
		contracts.New("session", "resume").
			Help(clitext.CmdHelpSessionResume).
			Param("name", clitext.CmdParamSessionResumeName, true).
			Do(h.sessionResumeRun),
		contracts.New("session", "switch").
			Help(clitext.CmdHelpSessionSwitch).
			Param("name", clitext.CmdParamSessionSwitchName, true).
			Param("vendor", clitext.CmdParamSessionSwitchVendor, true).
			Param("cmd", clitext.CmdParamSessionSwitchCmd, true).
			Param("model", clitext.CmdParamSessionSwitchModel, false).
			Param("handoff", clitext.CmdParamSessionSwitchHandoff, false).
			Do(h.sessionSwitchRun),
		contracts.New("session", "set-budget").
			Help(clitext.CmdHelpSessionSetBudget).
			Param("name", clitext.CmdParamSessionSetBudgetName, true).
			Param("cost_cap", clitext.CmdParamSessionSetBudgetCostCap, false).
			Param("token_cap", clitext.CmdParamSessionSetBudgetTokCap, false).
			Param("cohort_cost_cap", clitext.CmdParamSessionSetBudgetCohortCostCap, false).
			Param("cohort_token_cap", clitext.CmdParamSessionSetBudgetCohortTokCap, false).
			Param("scope", clitext.CmdParamSessionSetBudgetScope, false).
			Do(h.sessionSetBudgetRun),
		contracts.New("session", "set-mode").
			Help(clitext.CmdHelpSessionSetMode).
			Param("name", clitext.CmdParamSessionSetModeName, true).
			Param("mode", clitext.CmdParamSessionSetModeMode, true).
			Do(h.sessionSetModeRun),
		contracts.New("session", "get-mode").
			Help(clitext.CmdHelpSessionGetMode).
			Param("name", clitext.CmdParamSessionGetModeName, true).
			Do(h.sessionGetModeRun),
		contracts.New("agent", "create").
			Help(clitext.CmdHelpAgentCreate).
			Param("name", clitext.CmdParamAgentCreateName, true).
			Param("soul", clitext.CmdParamAgentCreateSoul, false).
			Param("mcp", clitext.CmdParamAgentCreateMcp, false).
			Param("backend", clitext.CmdParamAgentCreateBackend, false).
			Param("cmd", clitext.CmdParamAgentCreateCmd, false).
			Param("host", clitext.CmdParamAgentCreateHost, false).
			Param("tags", clitext.CmdParamAgentCreateTags, false).
			Do(h.agentCreateRun),
		contracts.New("agent", "list").
			Help(clitext.CmdHelpAgentList).
			Do(h.agentListRun),
		contracts.New("agent", "show").
			Help(clitext.CmdHelpAgentShow).
			Param("name", clitext.CmdParamAgentShowName, true).
			Do(h.agentShowRun),
		contracts.New("agent", "set-soul").
			Help(clitext.CmdHelpAgentSetSoul).
			Param("name", clitext.CmdParamAgentSetSoulName, true).
			Param("soul", clitext.CmdParamAgentSetSoulSoul, true).
			Do(h.agentSetSoulRun),
		contracts.New("set", "home").
			Help(clitext.CmdHelpSetHome).
			Param("channel", clitext.CmdParamSetHomeChannel, true).
			Do(h.setHomeRun),
		contracts.New("set", "source").
			Help(clitext.CmdHelpSetSource).
			Param("path", clitext.CmdParamSetSourcePath, true).
			Do(h.setSourceRun),
		contracts.New("service", "restart").
			Help(clitext.CmdHelpServiceRestart).
			Do(h.serviceRestartRun),
		contracts.New("service", "update").
			Help(clitext.CmdHelpServiceUpdate).
			Param("no_pull", clitext.CmdParamServiceUpdateNoPull, false).
			Do(h.serviceUpdateRun),
	}
}
