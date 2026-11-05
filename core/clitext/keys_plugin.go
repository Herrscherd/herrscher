package clitext

// Keys of the plugin host messages.
const (
	PluginHostFlagCategory   = "cli.plugin_host.flag.category"
	PluginHostFlagNATS       = "cli.plugin_host.flag.nats"
	PluginHostFlagInstance   = "cli.plugin_host.flag.instance"
	PluginHostUnsupported    = "cli.plugin_host.unsupported_category"
	PluginHostNoSkeleton     = "cli.plugin_host.no_skeleton"
	PluginHostFailed         = "cli.plugin_host.failed"
	PluginHostOrchestrator   = "cli.plugin_host.orchestrator"
	PluginHostBackend        = "cli.plugin_host.backend"
	PluginHostNoOrchestrator = "cli.plugin_host.no_orchestrator"
	PluginHostNoBackend      = "cli.plugin_host.no_backend"
)

func pluginKeys() map[Key][]string {
	return map[Key][]string{
		PluginHostFlagCategory:   nil,
		PluginHostFlagNATS:       nil,
		PluginHostFlagInstance:   nil,
		PluginHostUnsupported:    {ParamCategory},
		PluginHostNoSkeleton:     {ParamCategory},
		PluginHostFailed:         {ParamErr},
		PluginHostOrchestrator:   {ParamErr},
		PluginHostBackend:        {ParamErr},
		PluginHostNoOrchestrator: nil,
		PluginHostNoBackend:      nil,
	}
}
