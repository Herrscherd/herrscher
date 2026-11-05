package clitext

// Keys of the usage text.
const (
	UsageGroupStart      = "cli.usage.group.start"
	UsageGroupSessions   = "cli.usage.group.sessions"
	UsageGroupDaemon     = "cli.usage.group.daemon"
	UsageGroupSetup      = "cli.usage.group.setup"
	UsageStartTask       = "cli.usage.start.task"
	UsageStartPrint      = "cli.usage.start.print"
	UsageStartTUI        = "cli.usage.start.tui"
	UsageStartVersion    = "cli.usage.start.version"
	UsageStartInit       = "cli.usage.start.init"
	UsageSessionVerbs    = "cli.usage.session.verbs"
	UsageAgentVerbs      = "cli.usage.agent.verbs"
	UsageHostVerbs       = "cli.usage.host.verbs"
	UsageApproveVerbs    = "cli.usage.approve.verbs"
	UsageMemoryVerbs     = "cli.usage.memory.verbs"
	UsageModelsVerbs     = "cli.usage.models.verbs"
	UsageServe           = "cli.usage.serve"
	UsageBridge          = "cli.usage.bridge"
	UsageService         = "cli.usage.service"
	UsageCommands        = "cli.usage.commands"
	UsagePluginVerbs     = "cli.usage.plugin.verbs"
	UsageUpdate          = "cli.usage.update"
	UsageInstall         = "cli.usage.install"
	UsageHelpFooter      = "cli.usage.help.footer"
	UsageEnvOwner        = "cli.usage.env.owner"
	UsageEnvRoute        = "cli.usage.env.route"
	UsageEnvGateway      = "cli.usage.env.gateway"
	UsageEnvDefaultModel = "cli.usage.env.default_model"
	UsageEnvInit         = "cli.usage.env.init"
)

func usageKeys() map[Key][]string {
	return map[Key][]string{
		UsageGroupStart:      nil,
		UsageGroupSessions:   nil,
		UsageGroupDaemon:     nil,
		UsageGroupSetup:      nil,
		UsageStartTask:       nil,
		UsageStartPrint:      nil,
		UsageStartTUI:        nil,
		UsageStartVersion:    nil,
		UsageStartInit:       nil,
		UsageSessionVerbs:    nil,
		UsageAgentVerbs:      nil,
		UsageHostVerbs:       nil,
		UsageApproveVerbs:    nil,
		UsageMemoryVerbs:     nil,
		UsageModelsVerbs:     nil,
		UsageServe:           nil,
		UsageBridge:          nil,
		UsageService:         nil,
		UsageCommands:        nil,
		UsagePluginVerbs:     nil,
		UsageUpdate:          nil,
		UsageInstall:         nil,
		UsageHelpFooter:      nil,
		UsageEnvOwner:        nil,
		UsageEnvRoute:        nil,
		UsageEnvGateway:      nil,
		UsageEnvDefaultModel: nil,
		UsageEnvInit:         nil,
	}
}
