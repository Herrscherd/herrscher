package manage

import (
	"errors"
	"os"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
)

type rowKind int

const (
	rowLine rowKind = iota
	rowAction
	rowChoice
	rowCheck
	rowField
)

type lineTone int

const (
	toneText lineTone = iota
	toneDim
	toneBold
	toneGroup
	toneInfo
	toneOK
	toneBad
	toneWarn
)

type setupRow struct {
	activate func(*setupModel) tea.Cmd
	text     string
	hint     string
	field    string
	tag      string
	kind     rowKind
	tone     lineTone
	on       bool
	dot      bool
}

func line(tone lineTone, text string) setupRow {
	return setupRow{kind: rowLine, tone: tone, text: text}
}

func info(label, value string) setupRow {
	return setupRow{kind: rowLine, tone: toneInfo, text: label, hint: value}
}

func blank() setupRow { return setupRow{kind: rowLine} }

func action(label, hint string, activate func(*setupModel) tea.Cmd) setupRow {
	return setupRow{kind: rowAction, text: label, hint: hint, activate: activate}
}

func choice(on bool, label, hint string, activate func(*setupModel) tea.Cmd) setupRow {
	return setupRow{kind: rowChoice, on: on, text: label, hint: hint, activate: activate}
}

func fieldRow(key, label, hint string) setupRow {
	return setupRow{kind: rowField, field: key, text: label, hint: hint}
}

func (model *setupModel) rows() []setupRow {
	switch model.screen {
	case screenWelcome:
		return model.welcomeRows()
	case screenModules:
		return model.moduleRows()
	case screenDiscord:
		return model.discordRows()
	case screenClaude:
		return model.claudeRows()
	case screenCodex:
		return model.codexRows()
	case screenCursor:
		return model.cursorRows()
	case screenObsidian:
		return model.vaultRows()
	case screenRoute:
		return model.routeRows()
	case screenReview:
		return model.reviewRows()
	case screenInstall:
		return model.installRows()
	case screenHub:
		return model.hubRows()
	}
	return nil
}

func (model *setupModel) continueRow() setupRow {
	return action(model.text(clitext.SetupContinue, nil), "", func(target *setupModel) tea.Cmd { return target.next() })
}

func (model *setupModel) hostLine() string {
	if model.deps.compose() {
		return model.text(clitext.SetupHostSource, clitext.Params{clitext.ParamPath: model.deps.hostDir})
	}
	return model.text(clitext.SetupHostBinary, nil)
}

func (model *setupModel) envLine() string {
	if model.envExists {
		return model.text(clitext.SetupEnvFound, clitext.Params{clitext.ParamPath: model.deps.envPath})
	}
	return model.text(clitext.SetupEnvMissing, clitext.Params{clitext.ParamPath: model.deps.envPath})
}

func (model *setupModel) welcomeRows() []setupRow {
	return []setupRow{
		info(model.text(clitext.SetupLabelHost, nil), model.hostLine()),
		info(model.text(clitext.SetupLabelSettings, nil), model.envLine()),
		info(model.text(clitext.SetupLabelLanguage, nil), model.languageName(model.language())),
		blank(),
		line(toneBold, model.text(clitext.SetupWelcomeQuestion, nil)),
		action(model.text(clitext.SetupQuick, nil), model.text(clitext.SetupQuickHint, nil), func(target *setupModel) tea.Cmd { return target.quick() }),
		action(model.text(clitext.SetupCustom, nil), model.text(clitext.SetupCustomHint, nil), func(target *setupModel) tea.Cmd { return target.goTo(screenModules) }),
		action(model.text(clitext.SetupDashboard, nil), model.text(clitext.SetupDashboardHint, nil), func(target *setupModel) tea.Cmd { return target.openHub() }),
	}
}

func (model *setupModel) moduleRows() []setupRow {
	var rows []setupRow
	for _, group := range moduleGroups() {
		note := clitext.Key(clitext.SetupModulesOne)
		if group.multi {
			note = clitext.SetupModulesMany
		}
		rows = append(rows, setupRow{kind: rowLine, tone: toneGroup, text: model.text(group.title, nil), hint: model.text(note, nil)})
		rows = append(rows, model.groupRows(group)...)
	}
	return append(rows, blank(), line(toneDim, model.text(clitext.SetupModulesCount, clitext.Params{clitext.ParamCount: model.modules.count()})), model.continueRow())
}

func (model *setupModel) groupRows(group moduleGroup) []setupRow {
	kind := rowChoice
	if group.multi {
		kind = rowCheck
	}
	rows := make([]setupRow, 0, len(group.members)+1)
	anyOn := false
	for _, mod := range group.members {
		row := setupRow{kind: kind, on: model.modules[mod], text: mod.spec().kind, hint: model.text(mod.spec().about, mod.spec().params), activate: toggleModule(group, mod)}
		if mod == moduleTerminal {
			row.tag = model.text(clitext.SetupModulesBuiltin, nil)
		}
		anyOn = anyOn || row.on
		rows = append(rows, row)
	}
	if !group.multi {
		rows = append(rows, choice(!anyOn, model.text(clitext.SetupModuleNone, nil), "", func(target *setupModel) tea.Cmd {
			target.modules.unset(group)
			return nil
		}))
	}
	return rows
}

func toggleModule(group moduleGroup, mod setupModule) func(*setupModel) tea.Cmd {
	return func(target *setupModel) tea.Cmd {
		target.modules.toggle(group, mod)
		return nil
	}
}

func (model *setupModel) probeRows(kind probeKind, label string) []setupRow {
	rows := []setupRow{action(label, "", func(target *setupModel) tea.Cmd { return target.runProbe(kind) })}
	result, ok := model.probes[kind]
	if !ok {
		return rows
	}
	switch result.state {
	case probeRunning:
		rows = append(rows, line(toneDim, "  "+model.text(clitext.SetupProbeRunning, nil)))
	case probePassed:
		rows = append(rows, line(toneOK, "  ✓ "+model.text(result.key, result.params)))
	case probeFailed:
		rows = append(rows, line(toneBad, "  ✗ "+model.text(result.key, result.params)))
	case probeIdle:
	}
	return rows
}

func (model *setupModel) discordRows() []setupRow {
	rows := []setupRow{
		line(toneDim, model.text(clitext.SetupBotIntro, clitext.Params{clitext.ParamURL: discordPortal})),
		blank(),
		fieldRow(envDiscordToken, model.text(clitext.SetupBotToken, nil), model.text(clitext.SetupBotTokenHint, nil)),
		fieldRow(envDiscordUser, model.text(clitext.SetupBotUser, nil), model.text(clitext.SetupBotUserHint, nil)),
		blank(),
	}
	rows = append(rows, model.probeRows(probeDiscord, model.text(clitext.SetupBotProbe, nil))...)
	return append(rows, blank(), model.continueRow())
}

func (model *setupModel) claudeRows() []setupRow {
	rows := []setupRow{
		line(toneBold, model.text(clitext.SetupClaudeQuestion, nil)),
		choice(!model.claudeKey, model.text(clitext.SetupClaudeSubscription, nil), model.text(clitext.SetupClaudeSubscriptionHint, nil), setClaudeKey(false)),
		choice(model.claudeKey, model.text(clitext.SetupAPIKey, nil), model.text(clitext.SetupAPIKeyHint, nil), setClaudeKey(true)),
		blank(),
	}
	label := model.text(clitext.SetupLogin, nil)
	if model.claudeKey {
		rows = append(rows, fieldRow(envAnthropicKey, model.text(clitext.SetupAPIKey, nil), "sk-ant-…"))
		label = model.text(clitext.SetupCheckKey, nil)
	}
	rows = append(rows, model.probeRows(probeClaude, label)...)
	return append(rows, blank(), model.continueRow())
}

func setClaudeKey(useKey bool) func(*setupModel) tea.Cmd {
	return func(target *setupModel) tea.Cmd {
		if target.claudeKey == useKey {
			return nil
		}
		target.claudeKey = useKey
		delete(target.probes, probeClaude)
		if !useKey {
			target.draft.set(envAnthropicKey, "")
			return target.statusCheck(probeClaude)
		}
		target.draft.set(envAnthropicKey, target.fields[envAnthropicKey].Value())
		return nil
	}
}

func (model *setupModel) codexRows() []setupRow {
	rows := []setupRow{
		line(toneBold, model.text(clitext.SetupCodexQuestion, nil)),
		choice(!model.codexKey, model.text(clitext.SetupCodexAccount, nil), model.text(clitext.SetupCodexAccountHint, nil), setCodexKey(false)),
		choice(model.codexKey, model.text(clitext.SetupAPIKey, nil), model.text(clitext.SetupAPIKeyHint, nil), setCodexKey(true)),
		blank(),
	}
	label := model.text(clitext.SetupLogin, nil)
	if model.codexKey {
		rows = append(rows, fieldRow(fieldCodexKey, model.text(clitext.SetupAPIKey, nil), "sk-…"))
		label = model.text(clitext.SetupCheckKey, nil)
	}
	rows = append(rows, model.probeRows(probeCodex, label)...)
	return append(rows, blank(), model.continueRow())
}

func setCodexKey(useKey bool) func(*setupModel) tea.Cmd {
	return func(target *setupModel) tea.Cmd {
		if target.codexKey == useKey {
			return nil
		}
		target.codexKey = useKey
		delete(target.probes, probeCodex)
		return target.statusCheck(probeCodex)
	}
}

func (model *setupModel) cursorRows() []setupRow {
	rows := []setupRow{fieldRow(envCursorKey, model.text(clitext.SetupCursorKey, nil), model.text(clitext.SetupCursorKeyHint, nil)), blank()}
	rows = append(rows, model.probeRows(probeCursor, model.text(clitext.SetupCursorProbe, nil))...)
	return append(rows, blank(), model.continueRow())
}

func (model *setupModel) vaultRows() []setupRow {
	rows := []setupRow{fieldRow(envVault, model.text(clitext.SetupVault, nil), model.text(clitext.SetupVaultHint, nil)), blank()}
	rows = append(rows, model.probeRows(probeVault, model.text(clitext.SetupVaultProbe, nil))...)
	return append(rows, blank(), model.continueRow())
}

func (model *setupModel) routeRows() []setupRow {
	policy := contracts.RoutePolicy(model.draft.value(envPolicy))
	rows := []setupRow{
		line(toneGroup, model.text(clitext.SetupRoutePolicy, nil)),
		choice(policy == contracts.PolicyAll, "all", model.text(clitext.ManagePolicyAllHelp, nil), policyChoice(contracts.PolicyAll)),
		choice(policy == contracts.PolicyGatewayOnly, string(contracts.PolicyGatewayOnly), model.text(clitext.ManagePolicyGatewayHelp, nil), policyChoice(contracts.PolicyGatewayOnly)),
	}
	if policy == contracts.PolicyGatewayOnly {
		rows = append(rows,
			fieldRow(envGatewayURL, envGatewayURL, ""),
			fieldRow(envGatewayToken, envGatewayToken, model.text(clitext.ManageGatewayPairRequired, nil)),
		)
	}
	rows = append(rows, blank(), line(toneGroup, model.text(clitext.SetupRouteModel, nil)))
	rows = append(rows, model.modelRows()...)
	rows = append(rows, blank(), line(toneGroup, model.text(clitext.SetupRouteLang, nil)))
	current := model.language()
	for _, lang := range i18n.Languages() {
		rows = append(rows, choice(current == lang, model.languageName(lang), "", languageChoice(lang)))
	}
	if model.notice != nil {
		rows = append(rows, blank(), line(toneBad, model.noticeText()))
	}
	return append(rows, blank(), action(model.text(clitext.SetupContinue, nil), "", func(target *setupModel) tea.Cmd { return target.leaveRoute() }))
}

func (model *setupModel) noticeText() string {
	params := clitext.Params{clitext.ParamURL: envGatewayURL, clitext.ParamToken: envGatewayToken}
	if errors.Is(model.notice, errIncompleteCreds) {
		return model.text(clitext.ManageGatewayPairPartial, params)
	}
	return model.text(clitext.ManageGatewayPairMissing, params)
}

func policyChoice(policy contracts.RoutePolicy) func(*setupModel) tea.Cmd {
	return func(target *setupModel) tea.Cmd { return target.setPolicy(policy) }
}

func languageChoice(lang i18n.Language) func(*setupModel) tea.Cmd {
	return func(target *setupModel) tea.Cmd {
		target.draft.set(envLanguage, lang.String())
		return nil
	}
}

func (model *setupModel) language() i18n.Language {
	if lang, err := i18n.ParseLanguage(model.draft.value(envLanguage)); err == nil {
		return lang
	}
	return clitext.ClientLanguage()
}

func (model *setupModel) languageName(lang i18n.Language) string {
	if lang == i18n.French {
		return model.text(clitext.SetupLangFrench, nil)
	}
	return model.text(clitext.SetupLangEnglish, nil)
}

func (model *setupModel) modelRows() []setupRow {
	if model.route.loading {
		return []setupRow{line(toneDim, model.text(clitext.SetupProbeRunning, nil))}
	}
	current := model.draft.value(envModel)
	rows := []setupRow{choice(current == "", model.text(clitext.SetupRouteNoModel, nil), "", modelChoice(""))}
	if model.route.err != nil {
		rows = append(rows, line(toneWarn, model.text(clitext.SetupRouteCatalogFailed, clitext.Params{clitext.ParamErr: model.route.err.Error()})))
	}
	offered := model.offeredModels()
	for idx := range offered {
		rows = append(rows, choice(current == offered[idx].ID, offered[idx].ID, offered[idx].Label, modelChoice(offered[idx].ID)))
	}
	return rows
}

func modelChoice(modelID string) func(*setupModel) tea.Cmd {
	return func(target *setupModel) tea.Cmd {
		target.draft.set(envModel, modelID)
		return nil
	}
}

func (model *setupModel) reviewRows() []setupRow {
	paths := model.modules.paths(model.extras)
	rows := []setupRow{action(model.text(clitext.SetupReviewPlugins, clitext.Params{clitext.ParamCount: len(paths)}), "", openSection(screenModules))}
	for _, path := range paths {
		rows = append(rows, line(toneOK, "  + "+shortModule(path)))
	}
	if !model.deps.compose() {
		rows = append(rows, line(toneDim, "  "+model.text(clitext.SetupReviewBinary, nil)))
	}
	rows = append(rows, blank(), line(toneGroup, model.text(clitext.SetupReviewSettings, clitext.Params{clitext.ParamPath: model.deps.envPath})))
	rows = append(rows, model.settingRows()...)
	rows = append(rows, blank(), model.checksLine(), blank())
	label := clitext.Key(clitext.SetupReviewSave)
	if model.deps.compose() {
		label = clitext.SetupReviewInstall
	}
	return append(rows, action(model.text(label, nil), "", func(target *setupModel) tea.Cmd { return target.goTo(screenInstall) }))
}

func (model *setupModel) settingRows() []setupRow {
	written := model.draft.written()
	if len(written) == 0 {
		return []setupRow{line(toneDim, "  "+model.text(clitext.SetupReviewNoChange, nil))}
	}
	keys := make([]string, 0, len(written))
	for key := range written {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	rows := make([]setupRow, 0, len(keys))
	for _, key := range keys {
		rows = append(rows, action(key, model.shownValue(key, written[key]), openSection(settingScreen(key))))
	}
	return rows
}

func (model *setupModel) shownValue(key, value string) string {
	switch {
	case value == "":
		return model.text(clitext.SetupReviewUnset, nil)
	case secretKeys()[key]:
		return strings.Repeat("•", min(len(value), maskWidth))
	}
	return value
}

const maskWidth = 24

func settingScreen(key string) setupScreen {
	switch key {
	case envDiscordToken, envDiscordUser:
		return screenDiscord
	case envAnthropicKey:
		return screenClaude
	case envCursorKey:
		return screenCursor
	case envVault:
		return screenObsidian
	}
	return screenRoute
}

func (model *setupModel) checksLine() setupRow {
	if failures := model.failedChecks(); failures > 0 {
		return line(toneWarn, model.text(clitext.SetupReviewWarnings, clitext.Params{clitext.ParamCount: failures}))
	}
	return line(toneOK, model.text(clitext.SetupReviewChecked, nil))
}

func (model *setupModel) installRows() []setupRow {
	rows := make([]setupRow, 0, len(model.install.steps)+outputTail)
	for idx := range model.install.steps {
		rows = append(rows, model.stepRows(&model.install.steps[idx])...)
	}
	if !model.install.finished() {
		return rows
	}
	rows = append(rows, blank())
	switch {
	case model.install.err != nil:
		rows = append(rows, line(toneBad, model.text(clitext.SetupInstallFailed, nil)))
		rows = append(rows, model.restoreRows()...)
	default:
		rows = append(rows,
			line(toneOK, model.text(clitext.SetupInstallDone, nil)),
			line(toneDim, model.text(clitext.SetupInstallStart, clitext.Params{clitext.ParamCommand: serveCommand(invokedBinary(os.Args))})),
		)
	}
	return append(rows, blank(), action(model.text(clitext.SetupInstallHub, nil), "", func(target *setupModel) tea.Cmd { return target.openHub() }))
}

func (model *setupModel) stepRows(step *installStep) []setupRow {
	switch step.state {
	case probeRunning:
		return []setupRow{line(toneDim, "  … "+step.label)}
	case probePassed:
		return []setupRow{line(toneOK, "  ✓ "+step.label)}
	case probeFailed:
		rows := []setupRow{line(toneBad, "  ✗ "+step.label)}
		for _, out := range strings.Split(step.output, "\n") {
			if out != "" {
				rows = append(rows, line(toneDim, "    "+out))
			}
		}
		return rows
	case probeIdle:
	}
	return []setupRow{line(toneDim, "    "+step.label)}
}

func (model *setupModel) restoreRows() []setupRow {
	switch {
	case model.install.restoreErr != nil:
		path := manifestFile
		return []setupRow{line(toneBad, model.text(clitext.ManageRestoreFailed, clitext.Params{clitext.ParamPath: path, clitext.ParamErr: model.install.restoreErr.Error()}))}
	case model.install.restored:
		return []setupRow{line(toneDim, model.text(clitext.SetupInstallRestored, nil))}
	}
	return nil
}

func (model *setupModel) hubRows() []setupRow {
	rows := []setupRow{info(model.text(clitext.SetupLabelHost, nil), model.hostLine()), info(model.text(clitext.SetupLabelSettings, nil), model.envLine()), blank()}
	for _, section := range model.sections() {
		rows = append(rows, setupRow{kind: rowAction, dot: true, tone: section.tone, text: section.name, hint: section.state, activate: openSection(section.screen)})
	}
	return append(rows,
		blank(),
		line(toneDim, model.text(clitext.SetupHubShortcut, clitext.Params{clitext.ParamName: kindDiscord})),
		blank(),
		action(model.text(clitext.SetupHubSave, nil), "", openSection(screenReview)),
		action(model.text(clitext.SetupHubQuit, nil), "", func(*setupModel) tea.Cmd { return tea.Quit }),
	)
}

type hubSection struct {
	name   string
	state  string
	screen setupScreen
	tone   lineTone
}

func (model *setupModel) sections() []hubSection {
	sections := []hubSection{{
		name:   model.text(clitext.SetupSectionModules, nil),
		state:  model.text(clitext.SetupHubModules, clitext.Params{clitext.ParamCount: model.modules.count()}),
		screen: screenModules,
		tone:   toneOK,
	}}
	sections = append(sections,
		model.moduleSection(moduleDiscord, screenDiscord, model.discordState()),
		model.moduleSection(moduleClaude, screenClaude, model.probeState(probeClaude)),
		model.moduleSection(moduleCodex, screenCodex, model.probeState(probeCodex)),
		model.moduleSection(moduleCursor, screenCursor, model.keyState(envCursorKey)),
		model.moduleSection(moduleObsidian, screenObsidian, sectionState{tone: toneOK, text: model.vaultState()}),
		hubSection{name: model.text(clitext.SetupSectionRoute, nil), state: model.routeState(), screen: screenRoute, tone: toneOK},
	)
	return sections
}

type sectionState struct {
	text string
	tone lineTone
}

func (model *setupModel) moduleSection(mod setupModule, screen setupScreen, state sectionState) hubSection {
	section := hubSection{name: mod.spec().kind, screen: screen, state: state.text, tone: state.tone}
	if !model.modules[mod] {
		section.state, section.tone = model.text(clitext.SetupHubUnchecked, nil), toneDim
	}
	return section
}

func (model *setupModel) discordState() sectionState {
	if result, ok := model.probes[probeDiscord]; ok && result.state != probeRunning {
		return model.resultState(result)
	}
	return model.keyState(envDiscordToken)
}

func (model *setupModel) keyState(key string) sectionState {
	if model.draft.value(key) == "" {
		return sectionState{tone: toneBad, text: model.text(clitext.SetupHubMissing, nil)}
	}
	return sectionState{tone: toneOK, text: model.text(clitext.SetupHubConfigured, nil)}
}

func (model *setupModel) probeState(kind probeKind) sectionState {
	result, ok := model.probes[kind]
	if !ok {
		if kind == probeClaude && model.claudeKey {
			return model.keyState(envAnthropicKey)
		}
		return sectionState{tone: toneOK, text: model.text(clitext.SetupHubConfigured, nil)}
	}
	if result.state == probeRunning {
		return sectionState{tone: toneDim, text: model.text(clitext.SetupProbeRunning, nil)}
	}
	return model.resultState(result)
}

func (model *setupModel) resultState(result probeResult) sectionState {
	tone := toneOK
	if result.state == probeFailed {
		tone = toneWarn
	}
	return sectionState{tone: tone, text: model.text(result.key, result.params)}
}

func (model *setupModel) vaultState() string {
	if vault := model.draft.value(envVault); vault != "" {
		return vault
	}
	return vaultPlaceholder
}

func (model *setupModel) routeState() string {
	policy := model.draft.value(envPolicy)
	if policy == "" {
		policy = "all"
	}
	if chosen := model.draft.value(envModel); chosen != "" {
		return chosen + " · " + policy
	}
	return model.text(clitext.SetupRouteNoModel, nil) + " · " + policy
}
