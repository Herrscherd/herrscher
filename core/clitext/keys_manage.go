package clitext

// Parameter names of the manage messages.
const (
	ParamModule  = "Module"
	ParamCurrent = "Current"
	ParamWant    = "Want"
	ParamHave    = "Have"
	ParamPolicy  = "Policy"
	ParamChoice  = "Choice"
	ParamURL     = "URL"
	ParamToken   = "Token"
	ParamFile    = "File"
	ParamLine    = "Line"
	ParamMarker  = "Marker"
	ParamAction  = "Action"
)

// Message keys of the manage CLI.
const (
	ManagePluginUsage               = "cli.manage.plugin.usage"
	ManagePluginUnknownSubcommand   = "cli.manage.plugin.unknown_subcommand"
	ManagePluginEditUsage           = "cli.manage.plugin.edit_usage"
	ManagePluginPinUsage            = "cli.manage.plugin.pin_usage"
	ManagePluginAdded               = "cli.manage.plugin.added"
	ManagePluginRemoved             = "cli.manage.plugin.removed"
	ManagePluginNoChange            = "cli.manage.plugin.no_change"
	ManagePluginMoved               = "cli.manage.plugin.moved"
	ManagePluginSkippedBuild        = "cli.manage.plugin.skipped_build"
	ManagePluginVersionNotInstalled = "cli.manage.plugin.version_not_installed"
	ManagePluginPinned              = "cli.manage.plugin.pinned"
	ManagePluginUnpinned            = "cli.manage.plugin.unpinned"
	ManagePinnedTag                 = "cli.manage.pinned_tag"
	ManageStateAdded                = "cli.manage.state.added"
	ManageStateRemoved              = "cli.manage.state.removed"
	ManageAborted                   = "cli.manage.aborted"
	ManageAbortedPinned             = "cli.manage.aborted_pinned"
	ManageFindingMovesBack          = "cli.manage.finding.moves_back"
	ManageFindingContracts          = "cli.manage.finding.contracts"
	ManageNoHostModule              = "cli.manage.no_host_module"
	ManageNotCompiledIn             = "cli.manage.not_compiled_in"
	ManageReadFailed                = "cli.manage.read_failed"
	ManageWriteFailed               = "cli.manage.write_failed"
	ManageSaveFailed                = "cli.manage.save_failed"
	ManageRestoreFailed             = "cli.manage.restore_failed"
	ManageCommandFailed             = "cli.manage.command_failed"
	ManageChoiceOutOfRange          = "cli.manage.choice_out_of_range"
	ManageUnknownKind               = "cli.manage.unknown_kind"
	ManageUnknownKindListed         = "cli.manage.unknown_kind.listed"
	ManageInvalidModulePath         = "cli.manage.invalid_module_path"
	ManageEmptyStack                = "cli.manage.empty_stack"
	ManageDefaultTag                = "cli.manage.default_tag"
	ManageUnknownPolicy             = "cli.manage.unknown_policy"
	ManageUnknownModel              = "cli.manage.unknown_model"
	ManageUnknownAction             = "cli.manage.unknown_action"
	ManageGatewayPairMissing        = "cli.manage.gateway_pair_missing"
	ManageGatewayPairPartial        = "cli.manage.gateway_pair_partial"
	ManagePinsLine                  = "cli.manage.pins.line"
	ManageEndMarkerNotFound         = "cli.manage.marker.end_not_found"
	ManageRegionNotFound            = "cli.manage.marker.region_not_found"
	ManageDuplicateMarker           = "cli.manage.marker.duplicate"
	ManageMarkerOrder               = "cli.manage.marker.order"
	ManageSkippedPinned             = "cli.manage.skipped_pinned"
	ManageNoPlugins                 = "cli.manage.no_plugins"
	ManageAllPinned                 = "cli.manage.all_pinned"
	ManageUpdatedNoBuild            = "cli.manage.updated_no_build"
	ManageUpdated                   = "cli.manage.updated"
	ManageFlagHost                  = "cli.manage.flag.host"
	ManageFlagPluginNoBuild         = "cli.manage.flag.plugin_no_build"
	ManageFlagUpdateNoBuild         = "cli.manage.flag.update_no_build"
	ManageFlagInitNoBuild           = "cli.manage.flag.init_no_build"
	ManageFlagYes                   = "cli.manage.flag.yes"
	ManageFlagList                  = "cli.manage.flag.list"
	ManageFlagInitYes               = "cli.manage.flag.init_yes"
	ManageFlagCategory              = "cli.manage.flag.category"
	ManageFlagWith                  = "cli.manage.flag.with"
	ManageInitWroteNoBuild          = "cli.manage.init.wrote_no_build"
	ManageWroteSettings             = "cli.manage.wrote_settings"
	ManageBuildRestored             = "cli.manage.build_restored"
	ManageSeedFailed                = "cli.manage.seed_failed"
	ManageSeeded                    = "cli.manage.seeded"
	ManageEnvWriteFailed            = "cli.manage.env_write_failed"
	ManageCatalogFailed             = "cli.manage.catalog_failed"
	ManageDefaultModelLater         = "cli.manage.route.default_model_later"
	ManageNoModelOffered            = "cli.manage.route.no_model_offered"
	ManageGatewayNoModel            = "cli.manage.route.gateway_no_model"
	ManageRoutingHeader             = "cli.manage.route.routing_header"
	ManageRoutingNote               = "cli.manage.route.routing_note"
	ManageRoutePolicyHeader         = "cli.manage.route.policy_header"
	ManagePolicyAllHelp             = "cli.manage.route.policy_all"
	ManagePolicyGatewayHelp         = "cli.manage.route.policy_gateway_only"
	ManageDefaultModelHeader        = "cli.manage.route.default_model_header"
	ManageGatewayPairHeader         = "cli.manage.gateway_pair.header"
	ManageGatewayPairRequired       = "cli.manage.gateway_pair.required"
	ManageGatewayPairKeep           = "cli.manage.gateway_pair.keep"
	ManageEnvHeader                 = "cli.manage.env.header"
	ManageTokenHidden               = "cli.manage.token_hidden"
	ManageWizardCompose             = "cli.manage.wizard.compose"
	ManageWizardConfigure           = "cli.manage.wizard.configure"
	ManageBeforeWriting             = "cli.manage.decider.before_writing"
	ManageNothingKnown              = "cli.manage.decider.nothing_known"
	ManageBuildFailed               = "cli.manage.decider.build_failed"
	ManagePromptProceed             = "cli.manage.prompt.proceed"
	ManagePromptRestore             = "cli.manage.prompt.restore"
	ManageHintYesDefault            = "cli.manage.prompt.hint_yes_default"
	ManageHintNoDefault             = "cli.manage.prompt.hint_no_default"
	ManageNothingEntered            = "cli.manage.nothing_entered"
	ManageInstalledNote             = "cli.manage.installed_note"
	ManageStartWith                 = "cli.manage.start_with"
	ManageStackHeader               = "cli.manage.stack_header"
	ManageCurrentTag                = "cli.manage.current_tag"
)

func manageKeys() map[Key][]string {
	return map[Key][]string{
		ManagePluginUsage:               nil,
		ManagePluginUnknownSubcommand:   {ParamName},
		ManagePluginEditUsage:           {ParamVerb},
		ManagePluginPinUsage:            nil,
		ManagePluginAdded:               {ParamModule, ParamPath},
		ManagePluginRemoved:             {ParamModule, ParamPath},
		ManagePluginNoChange:            {ParamModule, ParamState},
		ManagePluginMoved:               {ParamModule, ParamPath, ParamVersion},
		ManagePluginSkippedBuild:        nil,
		ManagePluginVersionNotInstalled: {ParamModule, ParamVersion},
		ManagePluginPinned:              {ParamModule},
		ManagePluginUnpinned:            {ParamModule},
		ManagePinnedTag:                 nil,
		ManageStateAdded:                nil,
		ManageStateRemoved:              nil,
		ManageAborted:                   nil,
		ManageAbortedPinned:             nil,
		ManageFindingMovesBack:          {ParamModule, ParamCurrent, ParamVersion},
		ManageFindingContracts:          {ParamModule, ParamVersion, ParamWant, ParamHave},
		ManageNoHostModule:              nil,
		ManageNotCompiledIn:             {ParamModule},
		ManageReadFailed:                {ParamPath, ParamErr},
		ManageWriteFailed:               {ParamPath, ParamErr},
		ManageSaveFailed:                {ParamPath, ParamErr},
		ManageRestoreFailed:             {ParamPath, ParamErr},
		ManageCommandFailed:             {ParamName, ParamErr},
		ManageChoiceOutOfRange:          {ParamName, ParamChoice},
		ManageUnknownKind:               {ParamCategory, ParamKind},
		ManageUnknownKindListed:         {ParamCategory, ParamKind},
		ManageInvalidModulePath:         {ParamModule},
		ManageEmptyStack:                nil,
		ManageDefaultTag:                nil,
		ManageUnknownPolicy:             {ParamPolicy},
		ManageUnknownModel:              {ParamModel},
		ManageUnknownAction:             {ParamAction},
		ManageGatewayPairMissing:        {ParamURL, ParamToken},
		ManageGatewayPairPartial:        {ParamURL, ParamToken},
		ManagePinsLine:                  {ParamFile, ParamLine, ParamModule},
		ManageEndMarkerNotFound:         {ParamMarker},
		ManageRegionNotFound:            nil,
		ManageDuplicateMarker:           {ParamMarker},
		ManageMarkerOrder:               nil,
		ManageSkippedPinned:             {ParamModule, ParamVersion},
		ManageNoPlugins:                 nil,
		ManageAllPinned:                 nil,
		ManageUpdatedNoBuild:            nil,
		ManageUpdated:                   nil,
		ManageFlagHost:                  nil,
		ManageFlagPluginNoBuild:         nil,
		ManageFlagUpdateNoBuild:         nil,
		ManageFlagInitNoBuild:           nil,
		ManageFlagYes:                   nil,
		ManageFlagList:                  nil,
		ManageFlagInitYes:               nil,
		ManageFlagCategory:              {ParamCategory},
		ManageFlagWith:                  nil,
		ManageInitWroteNoBuild:          nil,
		ManageWroteSettings:             {ParamCount, ParamPath},
		ManageBuildRestored:             {ParamPath},
		ManageSeedFailed:                {ParamPath, ParamErr},
		ManageSeeded:                    {ParamPath},
		ManageEnvWriteFailed:            {ParamErr},
		ManageCatalogFailed:             {ParamErr},
		ManageDefaultModelLater:         nil,
		ManageNoModelOffered:            nil,
		ManageGatewayNoModel:            nil,
		ManageRoutingHeader:             nil,
		ManageRoutingNote:               nil,
		ManageRoutePolicyHeader:         nil,
		ManagePolicyAllHelp:             nil,
		ManagePolicyGatewayHelp:         nil,
		ManageDefaultModelHeader:        nil,
		ManageGatewayPairHeader:         nil,
		ManageGatewayPairRequired:       nil,
		ManageGatewayPairKeep:           nil,
		ManageEnvHeader:                 {ParamKind},
		ManageTokenHidden:               nil,
		ManageWizardCompose:             nil,
		ManageWizardConfigure:           nil,
		ManageBeforeWriting:             nil,
		ManageNothingKnown:              nil,
		ManageBuildFailed:               nil,
		ManagePromptProceed:             nil,
		ManagePromptRestore:             nil,
		ManageHintYesDefault:            nil,
		ManageHintNoDefault:             nil,
		ManageNothingEntered:            {ParamPath},
		ManageInstalledNote:             nil,
		ManageStartWith:                 {ParamCommand},
		ManageStackHeader:               nil,
		ManageCurrentTag:                nil,
	}
}
