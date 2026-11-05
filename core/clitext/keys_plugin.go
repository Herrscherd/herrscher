package clitext

const (
	PluginHostFlagCategory   Key = "cli.plugin_host.flag.category"
	PluginHostFlagNATS       Key = "cli.plugin_host.flag.nats"
	PluginHostFlagInstance   Key = "cli.plugin_host.flag.instance"
	PluginHostUnsupported    Key = "cli.plugin_host.unsupported_category"
	PluginHostNoSkeleton     Key = "cli.plugin_host.no_skeleton"
	PluginHostFailed         Key = "cli.plugin_host.failed"
	PluginHostOrchestrator   Key = "cli.plugin_host.orchestrator"
	PluginHostBackend        Key = "cli.plugin_host.backend"
	PluginHostNoOrchestrator Key = "cli.plugin_host.no_orchestrator"
	PluginHostNoBackend      Key = "cli.plugin_host.no_backend"
)

var pluginKeys = map[Key][]string{
	PluginHostFlagCategory:   nil,
	PluginHostFlagNATS:       nil,
	PluginHostFlagInstance:   nil,
	PluginHostUnsupported:    {"Category"},
	PluginHostNoSkeleton:     {"Category"},
	PluginHostFailed:         {"Err"},
	PluginHostOrchestrator:   {"Err"},
	PluginHostBackend:        {"Err"},
	PluginHostNoOrchestrator: nil,
	PluginHostNoBackend:      nil,
}
