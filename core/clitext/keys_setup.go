package clitext

// Message keys of the setup command.
const (
	SetupUsage                  = "cli.setup.usage"
	SetupUnknownSection         = "cli.setup.unknown_section"
	SetupNeedsTerminal          = "cli.setup.needs_terminal"
	SetupFailed                 = "cli.setup.failed"
	SetupTitle                  = "cli.setup.title"
	SetupLabelHost              = "cli.setup.label.host"
	SetupLabelSettings          = "cli.setup.label.settings"
	SetupHostBinary             = "cli.setup.host.binary"
	SetupHostSource             = "cli.setup.host.source"
	SetupEnvMissing             = "cli.setup.env.missing"
	SetupEnvFound               = "cli.setup.env.found"
	SetupWelcomeSub             = "cli.setup.welcome.sub"
	SetupWelcomeQuestion        = "cli.setup.welcome.question"
	SetupQuick                  = "cli.setup.welcome.quick"
	SetupQuickHint              = "cli.setup.welcome.quick_hint"
	SetupCustom                 = "cli.setup.welcome.custom"
	SetupCustomHint             = "cli.setup.welcome.custom_hint"
	SetupDashboard              = "cli.setup.welcome.dashboard"
	SetupDashboardHint          = "cli.setup.welcome.dashboard_hint"
	SetupModulesSub             = "cli.setup.modules.sub"
	SetupModulesMany            = "cli.setup.modules.many"
	SetupModulesOne             = "cli.setup.modules.one"
	SetupModulesBuiltin         = "cli.setup.modules.builtin"
	SetupModulesCount           = "cli.setup.modules.count"
	SetupCategoryGateway        = "cli.setup.category.gateway"
	SetupCategoryBackend        = "cli.setup.category.backend"
	SetupCategoryMemory         = "cli.setup.category.memory"
	SetupCategoryOrchestrator   = "cli.setup.category.orchestrator"
	SetupCategoryExtractor      = "cli.setup.category.extractor"
	SetupCategorySkills         = "cli.setup.category.skills"
	SetupCategoryCompactor      = "cli.setup.category.compactor"
	SetupModuleTerminal         = "cli.setup.module.terminal"
	SetupModuleChatBot          = "cli.setup.module.chat_bot"
	SetupModuleTelegram         = "cli.setup.module.telegram"
	SetupModuleClaude           = "cli.setup.module.claude"
	SetupModuleCodex            = "cli.setup.module.codex"
	SetupModuleCursor           = "cli.setup.module.cursor"
	SetupModuleGrok             = "cli.setup.module.grok"
	SetupModuleAntigravity      = "cli.setup.module.antigravity"
	SetupModuleObsidian         = "cli.setup.module.obsidian"
	SetupModuleBasic            = "cli.setup.module.basic"
	SetupModuleLLM              = "cli.setup.module.llm"
	SetupModuleSuperset         = "cli.setup.module.superset"
	SetupModuleJev              = "cli.setup.module.jev"
	SetupModuleNone             = "cli.setup.module.none"
	SetupBotSub                 = "cli.setup.bot.sub"
	SetupBotIntro               = "cli.setup.bot.intro"
	SetupBotToken               = "cli.setup.bot.token"
	SetupBotTokenHint           = "cli.setup.bot.token_hint"
	SetupBotUser                = "cli.setup.bot.user"
	SetupBotUserHint            = "cli.setup.bot.user_hint"
	SetupBotProbe               = "cli.setup.bot.probe"
	SetupBotOK                  = "cli.setup.bot.ok"
	SetupBotRefused             = "cli.setup.bot.refused"
	SetupBotEmpty               = "cli.setup.bot.empty"
	SetupTelegramIntro          = "cli.setup.telegram.intro"
	SetupTelegramUser           = "cli.setup.telegram.user"
	SetupTelegramUserHint       = "cli.setup.telegram.user_hint"
	SetupTelegramUserMissing    = "cli.setup.telegram.user_missing"
	SetupTelegramMalformed      = "cli.setup.telegram.malformed"
	SetupBackendSub             = "cli.setup.backend.sub"
	SetupClaudeQuestion         = "cli.setup.claude.question"
	SetupClaudeSubscription     = "cli.setup.claude.subscription"
	SetupClaudeSubscriptionHint = "cli.setup.claude.subscription_hint"
	SetupAPIKey                 = "cli.setup.api_key"
	SetupAPIKeyHint             = "cli.setup.api_key_hint"
	SetupLogin                  = "cli.setup.login"
	SetupCheckKey               = "cli.setup.check_key"
	SetupLoggedIn               = "cli.setup.logged_in"
	SetupLoggedOut              = "cli.setup.logged_out"
	SetupLoginUnfinished        = "cli.setup.login_unfinished"
	SetupCLIMissing             = "cli.setup.cli_missing"
	SetupKeyEmpty               = "cli.setup.key_empty"
	SetupCodexQuestion          = "cli.setup.codex.question"
	SetupCodexAccount           = "cli.setup.codex.account"
	SetupCodexAccountHint       = "cli.setup.codex.account_hint"
	SetupGrokQuestion           = "cli.setup.grok.question"
	SetupGrokAccount            = "cli.setup.grok.account"
	SetupGrokAccountHint        = "cli.setup.grok.account_hint"
	SetupGrokKeyShadowed        = "cli.setup.grok.key_shadowed"
	SetupCursorKey              = "cli.setup.cursor.key"
	SetupCursorKeyHint          = "cli.setup.cursor.key_hint"
	SetupCursorProbe            = "cli.setup.cursor.probe"
	SetupCursorOK               = "cli.setup.cursor.ok"
	SetupAntigravityIntro       = "cli.setup.antigravity.intro"
	SetupAntigravityHint        = "cli.setup.antigravity.hint"
	SetupAntigravityLogin       = "cli.setup.antigravity.login"
	SetupAntigravityOK          = "cli.setup.antigravity.ok"
	SetupMemorySub              = "cli.setup.memory.sub"
	SetupVault                  = "cli.setup.vault"
	SetupVaultHint              = "cli.setup.vault_hint"
	SetupVaultProbe             = "cli.setup.vault.probe"
	SetupVaultOK                = "cli.setup.vault.ok"
	SetupVaultCreate            = "cli.setup.vault.create"
	SetupVaultEmpty             = "cli.setup.vault.empty"
	SetupVaultBad               = "cli.setup.vault.bad"
	SetupRouteSub               = "cli.setup.route.sub"
	SetupRoutePolicy            = "cli.setup.route.policy"
	SetupRouteModel             = "cli.setup.route.model"
	SetupRouteNoModel           = "cli.setup.route.no_model"
	SetupRoutePolicyAll         = "cli.setup.route.policy_all"
	SetupRouteLang              = "cli.setup.route.lang"
	SetupRouteCatalogFailed     = "cli.setup.route.catalog_failed"
	SetupLangEnglish            = "cli.setup.lang.en"
	SetupLangFrench             = "cli.setup.lang.fr"
	SetupReviewSub              = "cli.setup.review.sub"
	SetupReviewPlugins          = "cli.setup.review.plugins"
	SetupReviewBinary           = "cli.setup.review.binary"
	SetupReviewSettings         = "cli.setup.review.settings"
	SetupReviewNoChange         = "cli.setup.review.no_change"
	SetupReviewUnset            = "cli.setup.review.unset"
	SetupReviewWarnings         = "cli.setup.review.warnings"
	SetupReviewChecked          = "cli.setup.review.checked"
	SetupReviewInstall          = "cli.setup.review.install"
	SetupReviewSave             = "cli.setup.review.save"
	SetupContinue               = "cli.setup.continue"
	SetupInstallSub             = "cli.setup.install.sub"
	SetupStepEnv                = "cli.setup.step.env"
	SetupStepPlugins            = "cli.setup.step.plugins"
	SetupInstallDone            = "cli.setup.install.done"
	SetupInstallStart           = "cli.setup.install.start"
	SetupInstallFailed          = "cli.setup.install.failed"
	SetupInstallRestored        = "cli.setup.install.restored"
	SetupInstallStopping        = "cli.setup.install.stopping"
	SetupInstallNotRestored     = "cli.setup.install.not_restored"
	SetupStepBinary             = "cli.setup.step.binary"
	SetupTrialTitle             = "cli.setup.trial.title"
	SetupTrialStep              = "cli.setup.trial.step"
	SetupTrialFailed            = "cli.setup.trial.failed"
	SetupTrialMissing           = "cli.setup.trial.missing"
	SetupTrialRemedy            = "cli.setup.trial.remedy"
	SetupTrialNotStopped        = "cli.setup.trial.not_stopped"
	SetupHubSub                 = "cli.setup.hub.sub"
	SetupHubModules             = "cli.setup.hub.modules"
	SetupHubConfigured          = "cli.setup.hub.configured"
	SetupHubMissing             = "cli.setup.hub.missing"
	SetupHubUnchecked           = "cli.setup.hub.unchecked"
	SetupHubShortcut            = "cli.setup.hub.shortcut"
	SetupHubSave                = "cli.setup.hub.save"
	SetupHubQuit                = "cli.setup.hub.quit"
	SetupSectionModules         = "cli.setup.section.modules"
	SetupSectionRoute           = "cli.setup.section.route"
	SetupSectionConnections     = "cli.setup.section.connections"
	SetupConnectionsSub         = "cli.setup.connections.sub"
	SetupConnectionsSkip        = "cli.setup.connections.skip"
	SetupGitHubTitle            = "cli.setup.github.title"
	SetupGitHubConnected        = "cli.setup.github.connected"
	SetupGitHubNotConnected     = "cli.setup.github.not_connected"
	SetupGitHubConnect          = "cli.setup.github.connect"
	SetupGitHubInstall          = "cli.setup.github.install"
	SetupGitHubCode             = "cli.setup.github.code"
	SetupGitHubDenied           = "cli.setup.github.denied"
	SetupGitHubExpired          = "cli.setup.github.expired"
	SetupGitHubAppUnavailable   = "cli.setup.github.app_unavailable"
	SetupGitHubFailed           = "cli.setup.github.failed"
	SetupGitHubStatusFailed     = "cli.setup.github.status_failed"
	SetupGitHubForgeRefused     = "cli.setup.github.forge_refused"
	SetupGitHubForgeLimited     = "cli.setup.github.forge_limited"
	SetupGitHubForgeFailed      = "cli.setup.github.forge_failed"
	SetupKeysMove               = "cli.setup.keys.move"
	SetupKeysToggle             = "cli.setup.keys.toggle"
	SetupKeysChoose             = "cli.setup.keys.choose"
	SetupKeysBack               = "cli.setup.keys.back"
	SetupKeysQuit               = "cli.setup.keys.quit"
	SetupProbeRunning           = "cli.setup.probe.running"
	SetupProbeFailed            = "cli.setup.probe.failed"
	SetupKeyAccepted            = "cli.setup.key_accepted"
	SetupKeyRefused             = "cli.setup.key_refused"
	SetupInstallHub             = "cli.setup.install.hub"
	SetupStepUnchanged          = "cli.setup.step.unchanged"
	SetupLabelLanguage          = "cli.setup.label.language"
)

func setupKeys() map[Key][]string {
	return map[Key][]string{
		SetupUsage:                  {ParamName},
		SetupUnknownSection:         {ParamName},
		SetupNeedsTerminal:          nil,
		SetupFailed:                 {ParamErr},
		SetupTitle:                  nil,
		SetupLabelHost:              nil,
		SetupLabelSettings:          nil,
		SetupHostBinary:             nil,
		SetupHostSource:             {ParamPath},
		SetupEnvMissing:             {ParamPath},
		SetupEnvFound:               {ParamPath},
		SetupWelcomeSub:             nil,
		SetupWelcomeQuestion:        nil,
		SetupQuick:                  nil,
		SetupQuickHint:              nil,
		SetupCustom:                 nil,
		SetupCustomHint:             nil,
		SetupDashboard:              nil,
		SetupDashboardHint:          nil,
		SetupModulesSub:             nil,
		SetupModulesMany:            nil,
		SetupModulesOne:             nil,
		SetupModulesBuiltin:         nil,
		SetupModulesCount:           {ParamCount},
		SetupCategoryGateway:        nil,
		SetupCategoryBackend:        nil,
		SetupCategoryMemory:         nil,
		SetupCategoryOrchestrator:   nil,
		SetupCategoryExtractor:      nil,
		SetupCategorySkills:         nil,
		SetupCategoryCompactor:      nil,
		SetupModuleTerminal:         nil,
		SetupModuleChatBot:          {ParamName},
		SetupModuleTelegram:         nil,
		SetupModuleClaude:           nil,
		SetupModuleCodex:            nil,
		SetupModuleCursor:           nil,
		SetupModuleGrok:             nil,
		SetupModuleAntigravity:      nil,
		SetupModuleObsidian:         nil,
		SetupModuleBasic:            nil,
		SetupModuleLLM:              nil,
		SetupModuleSuperset:         nil,
		SetupModuleJev:              nil,
		SetupModuleNone:             nil,
		SetupBotSub:                 nil,
		SetupBotIntro:               {ParamURL},
		SetupBotToken:               nil,
		SetupBotTokenHint:           nil,
		SetupBotUser:                nil,
		SetupBotUserHint:            nil,
		SetupBotProbe:               nil,
		SetupBotOK:                  {ParamName},
		SetupBotRefused:             {ParamName},
		SetupBotEmpty:               {ParamName},
		SetupTelegramIntro:          nil,
		SetupTelegramUser:           nil,
		SetupTelegramUserHint:       nil,
		SetupTelegramUserMissing:    nil,
		SetupTelegramMalformed:      nil,
		SetupBackendSub:             nil,
		SetupClaudeQuestion:         nil,
		SetupClaudeSubscription:     nil,
		SetupClaudeSubscriptionHint: nil,
		SetupAPIKey:                 nil,
		SetupAPIKeyHint:             nil,
		SetupLogin:                  nil,
		SetupCheckKey:               nil,
		SetupLoggedIn:               {ParamName},
		SetupLoggedOut:              nil,
		SetupLoginUnfinished:        nil,
		SetupCLIMissing:             {ParamName},
		SetupKeyEmpty:               nil,
		SetupCodexQuestion:          nil,
		SetupCodexAccount:           nil,
		SetupCodexAccountHint:       nil,
		SetupGrokQuestion:           nil,
		SetupGrokAccount:            nil,
		SetupGrokAccountHint:        nil,
		SetupGrokKeyShadowed:        {ParamName},
		SetupCursorKey:              nil,
		SetupCursorKeyHint:          nil,
		SetupCursorProbe:            nil,
		SetupCursorOK:               nil,
		SetupAntigravityIntro:       nil,
		SetupAntigravityHint:        nil,
		SetupAntigravityLogin:       nil,
		SetupAntigravityOK:          {ParamCount},
		SetupMemorySub:              nil,
		SetupVault:                  nil,
		SetupVaultHint:              nil,
		SetupVaultProbe:             nil,
		SetupVaultOK:                nil,
		SetupVaultCreate:            nil,
		SetupVaultEmpty:             nil,
		SetupVaultBad:               {ParamErr},
		SetupRouteSub:               nil,
		SetupRoutePolicy:            nil,
		SetupRouteModel:             nil,
		SetupRouteNoModel:           nil,
		SetupRoutePolicyAll:         nil,
		SetupRouteLang:              nil,
		SetupRouteCatalogFailed:     {ParamErr},
		SetupLangEnglish:            nil,
		SetupLangFrench:             nil,
		SetupReviewSub:              nil,
		SetupReviewPlugins:          {ParamCount},
		SetupReviewBinary:           nil,
		SetupReviewSettings:         {ParamPath},
		SetupReviewNoChange:         nil,
		SetupReviewUnset:            nil,
		SetupReviewWarnings:         {ParamCount},
		SetupReviewChecked:          nil,
		SetupReviewInstall:          nil,
		SetupReviewSave:             nil,
		SetupContinue:               nil,
		SetupInstallSub:             nil,
		SetupStepEnv:                {ParamPath},
		SetupStepPlugins:            nil,
		SetupInstallDone:            nil,
		SetupInstallStart:           {ParamCommand},
		SetupInstallFailed:          nil,
		SetupInstallRestored:        nil,
		SetupInstallStopping:        nil,
		SetupInstallNotRestored:     nil,
		SetupTrialNotStopped:        nil,
		SetupStepBinary:             {ParamPath},
		SetupTrialTitle:             nil,
		SetupTrialStep:              {ParamName},
		SetupTrialFailed:            nil,
		SetupTrialMissing:           {ParamName},
		SetupTrialRemedy:            {ParamCommand},
		SetupHubSub:                 nil,
		SetupHubModules:             {ParamCount},
		SetupHubConfigured:          nil,
		SetupHubMissing:             nil,
		SetupHubUnchecked:           nil,
		SetupHubShortcut:            {ParamName},
		SetupHubSave:                nil,
		SetupHubQuit:                nil,
		SetupSectionModules:         nil,
		SetupSectionRoute:           nil,
		SetupSectionConnections:     nil,
		SetupConnectionsSub:         nil,
		SetupConnectionsSkip:        nil,
		SetupGitHubTitle:            nil,
		SetupGitHubConnected:        {ParamLogin, ParamSource},
		SetupGitHubNotConnected:     nil,
		SetupGitHubConnect:          nil,
		SetupGitHubInstall:          {ParamURL},
		SetupGitHubCode:             {ParamURL, ParamUserCode},
		SetupGitHubDenied:           nil,
		SetupGitHubExpired:          nil,
		SetupGitHubAppUnavailable:   nil,
		SetupGitHubFailed:           {ParamErr},
		SetupGitHubStatusFailed:     {ParamErr},
		SetupGitHubForgeRefused:     nil,
		SetupGitHubForgeLimited:     nil,
		SetupGitHubForgeFailed:      {ParamErr},
		SetupKeysMove:               nil,
		SetupKeysToggle:             nil,
		SetupKeysChoose:             nil,
		SetupKeysBack:               nil,
		SetupKeysQuit:               nil,
		SetupProbeRunning:           nil,
		SetupProbeFailed:            {ParamErr},
		SetupKeyAccepted:            nil,
		SetupKeyRefused:             nil,
		SetupInstallHub:             nil,
		SetupStepUnchanged:          nil,
		SetupLabelLanguage:          nil,
	}
}
