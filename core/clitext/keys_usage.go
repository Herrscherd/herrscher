package clitext

const (
	UsageTagline         Key = "cli.usage.tagline"
	UsageGroupStart      Key = "cli.usage.group.start"
	UsageGroupSessions   Key = "cli.usage.group.sessions"
	UsageGroupDaemon     Key = "cli.usage.group.daemon"
	UsageGroupSetup      Key = "cli.usage.group.setup"
	UsageStartTask       Key = "cli.usage.start.task"
	UsageStartPrint      Key = "cli.usage.start.print"
	UsageStartTUI        Key = "cli.usage.start.tui"
	UsageStartVersion    Key = "cli.usage.start.version"
	UsageStartInit       Key = "cli.usage.start.init"
	UsageSessionVerbs    Key = "cli.usage.session.verbs"
	UsageAgentVerbs      Key = "cli.usage.agent.verbs"
	UsageHostVerbs       Key = "cli.usage.host.verbs"
	UsageApproveVerbs    Key = "cli.usage.approve.verbs"
	UsageMemoryVerbs     Key = "cli.usage.memory.verbs"
	UsageModelsVerbs     Key = "cli.usage.models.verbs"
	UsageServe           Key = "cli.usage.serve"
	UsageBridge          Key = "cli.usage.bridge"
	UsageService         Key = "cli.usage.service"
	UsageCommands        Key = "cli.usage.commands"
	UsagePluginVerbs     Key = "cli.usage.plugin.verbs"
	UsageUpdate          Key = "cli.usage.update"
	UsageInstall         Key = "cli.usage.install"
	UsageHelpFooter      Key = "cli.usage.help.footer"
	UsageEnvDiscord      Key = "cli.usage.env.discord"
	UsageEnvOwner        Key = "cli.usage.env.owner"
	UsageEnvRoute        Key = "cli.usage.env.route"
	UsageEnvGateway      Key = "cli.usage.env.gateway"
	UsageEnvDefaultModel Key = "cli.usage.env.default_model"
	UsageEnvInit         Key = "cli.usage.env.init"
)

var usageKeys = map[Key][]string{
	UsageTagline:         nil,
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
	UsageEnvDiscord:      nil,
	UsageEnvOwner:        nil,
	UsageEnvRoute:        nil,
	UsageEnvGateway:      nil,
	UsageEnvDefaultModel: nil,
	UsageEnvInit:         nil,
}
