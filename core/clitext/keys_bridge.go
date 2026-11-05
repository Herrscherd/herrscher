package clitext

const (
	BridgeFlagSession          = "cli.bridge.flag.session"
	BridgeFlagProject          = "cli.bridge.flag.project"
	BridgeFlagAgent            = "cli.bridge.flag.agent"
	BridgeFlagProjectPinned    = "cli.bridge.flag.project_pinned"
	BridgeFlagVerbose          = "cli.bridge.flag.verbose"
	BridgeFlagHubSocket        = "cli.bridge.flag.hub_socket"
	BridgeFlagAgentsRoot       = "cli.bridge.flag.agents_root"
	BridgeFlagExtractor        = "cli.bridge.flag.extractor"
	BridgeFlagJournal          = "cli.bridge.flag.journal"
	BridgeFlagConsolidateEvery = "cli.bridge.flag.consolidate_every"
	BridgeFlagState            = "cli.bridge.flag.state"
	BridgeFlagEnvStdin         = "cli.bridge.flag.env_stdin"
	BridgeFlagCmd              = "cli.bridge.flag.cmd"
	BridgeFlagBackend          = "cli.bridge.flag.backend"
	BridgeFlagVendor           = "cli.bridge.flag.vendor"
	BridgeFlagResume           = "cli.bridge.flag.resume"
	BridgeFlagModel            = "cli.bridge.flag.model"
	BridgeFlagAccount          = "cli.bridge.flag.account"
	BridgeFlagStream           = "cli.bridge.flag.stream"
)

func bridgeKeys() map[Key][]string {
	return map[Key][]string{
		BridgeFlagSession:          nil,
		BridgeFlagProject:          nil,
		BridgeFlagAgent:            nil,
		BridgeFlagProjectPinned:    nil,
		BridgeFlagVerbose:          nil,
		BridgeFlagHubSocket:        nil,
		BridgeFlagAgentsRoot:       nil,
		BridgeFlagExtractor:        nil,
		BridgeFlagJournal:          nil,
		BridgeFlagConsolidateEvery: nil,
		BridgeFlagState:            nil,
		BridgeFlagEnvStdin:         nil,
		BridgeFlagCmd:              nil,
		BridgeFlagBackend:          nil,
		BridgeFlagVendor:           nil,
		BridgeFlagResume:           nil,
		BridgeFlagModel:            nil,
		BridgeFlagAccount:          nil,
		BridgeFlagStream:           nil,
	}
}
