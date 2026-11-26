package manage

import (
	"context"
	"errors"
	"os/exec"
	"time"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/host"
)

const (
	vaultPlaceholder = "~/.herrscher/memory"
	keyEnter         = "enter"
	keySpace         = " "
)

type setupScreen int

const (
	screenWelcome setupScreen = iota
	screenModules
	screenDiscord
	screenTelegram
	screenClaude
	screenCodex
	screenCursor
	screenGrok
	screenAntigravity
	screenObsidian
	screenRoute
	screenReview
	screenInstall
	screenConnections
	screenHub
)

func flowOrder() []setupScreen {
	return []setupScreen{screenModules, screenDiscord, screenTelegram, screenClaude, screenCodex, screenCursor, screenGrok, screenAntigravity, screenObsidian, screenRoute, screenReview}
}

type setupDeps struct {
	prober      setupProber
	github      gitHubLink
	login       func(context.Context, probeKind) tea.Cmd
	catalog     catalogFn
	run         commandRunner
	envPath     string
	hostDir     string
	manifest    []byte
	binary      string
	abandonWait time.Duration
	trialWait   time.Duration
}

func (deps setupDeps) compose() bool { return deps.manifest != nil }

type routeModels struct {
	err     error
	policy  contracts.RoutePolicy
	entries []host.CatalogEntry
	loading bool
}

type probeDoneMsg struct {
	input  string
	result probeResult
	kind   probeKind
}

type loginDoneMsg struct {
	err  error
	kind probeKind
}

type catalogMsg struct {
	err     error
	policy  contracts.RoutePolicy
	entries []host.CatalogEntry
}

type setupModel struct {
	ctx       context.Context
	messages  *clitext.Messages
	modules   moduleSet
	fields    map[string]textinput.Model
	probes    map[probeKind]probeResult
	notice    error
	deps      setupDeps
	draft     settingsDraft
	extras    []string
	route     routeModels
	github    gitHubState
	install   installRun
	screen    setupScreen
	base      setupScreen
	detour    setupScreen
	cursor    int
	claudeKey bool
	codexKey  bool
	grokKey   bool
	envExists bool
}

type setupStart struct {
	ctx       context.Context
	getenv    func(string) string
	screen    setupScreen
	envExists bool
}

func newSetupModel(messages *clitext.Messages, deps setupDeps, start setupStart) *setupModel {
	settings := readSettings(start.getenv)
	model := &setupModel{
		ctx:       start.ctx,
		messages:  messages,
		deps:      deps,
		draft:     newDraft(settings),
		probes:    make(map[probeKind]probeResult),
		screen:    start.screen,
		base:      screenWelcome,
		detour:    screenWelcome,
		claudeKey: settings[envAnthropicKey] != "",
		grokKey:   settings[envXAIKey] != "",
		envExists: start.envExists,
	}
	model.modules, model.extras = model.startModules(settings)
	model.fields = model.newFields()
	if start.screen != screenWelcome {
		model.base = screenHub
	}
	return model
}

func (model *setupModel) startModules(settings map[string]string) (moduleSet, []string) {
	if model.deps.compose() {
		if paths, err := listPlugins(model.messages, string(model.deps.manifest)); err == nil {
			return modulesFromPaths(paths)
		}
	}
	set := defaultModules()
	if settings[envCursorKey] != "" {
		set[moduleCursor] = true
	}
	if settings[envXAIKey] != "" {
		set[moduleGrok] = true
	}
	return set, nil
}

func (model *setupModel) newFields() map[string]textinput.Model {
	fields := make(map[string]textinput.Model)
	for _, key := range []string{envDiscordToken, envDiscordUser, envTelegramBot, envTelegramUser, envAnthropicKey, fieldCodexKey, envCursorKey, envXAIKey, envVault, envGatewayURL, envGatewayToken} {
		input := textinput.New()
		input.Prompt = ""
		input.SetValue(model.draft.value(key))
		input.Cursor.SetMode(cursor.CursorStatic)
		if secretKeys()[key] {
			input.EchoMode = textinput.EchoPassword
			input.EchoCharacter = '•'
		}
		if key == envVault {
			input.Placeholder = vaultPlaceholder
		}
		fields[key] = input
	}
	return fields
}

func (model *setupModel) text(key clitext.Key, params clitext.Params) string {
	return msg(model.messages, key, params)
}

func (model *setupModel) Init() tea.Cmd {
	model.syncFocus()
	return model.enter(model.screen)
}

func (model *setupModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := message.(type) {
	case tea.KeyMsg:
		cmd := model.key(typed)
		return model, cmd
	case probeDoneMsg:
		if typed.input == model.probeInput(typed.kind) {
			model.probes[typed.kind] = typed.result
		}
	case loginDoneMsg:
		cmd := model.loginDone(typed)
		return model, cmd
	case catalogMsg:
		if typed.policy == model.route.policy {
			model.route = routeModels{policy: typed.policy, entries: typed.entries, err: typed.err}
		}
	case stepDoneMsg:
		cmd := model.stepDone(typed)
		return model, cmd
	case trialDoneMsg:
		cmd := model.trialDone(typed)
		return model, cmd
	case gitHubStatusMsg:
		model.gitHubStatusDone(typed)
	case gitHubCodeMsg:
		cmd := model.gitHubCodeShown(typed)
		return model, cmd
	case gitHubConnectDoneMsg:
		cmd := model.gitHubConnectDone(typed)
		return model, cmd
	}
	return model, nil
}

func (model *setupModel) key(press tea.KeyMsg) tea.Cmd {
	switch press.String() {
	case "ctrl+c":
		if model.install.running() || model.install.trialsRunning() {
			model.abortInstall()
			return nil
		}
		return tea.Quit
	case "up", "shift+tab":
		model.move(-1)
		return nil
	case "down", "tab":
		model.move(1)
		return nil
	case "esc":
		return model.back()
	}
	row, ok := model.current()
	if !ok {
		return nil
	}
	if row.kind == rowField {
		if press.Type == tea.KeyEnter {
			model.move(1)
			return nil
		}
		return model.typeInto(row.field, press)
	}
	switch press.String() {
	case keyEnter:
		if model.screen == screenModules && row.kind != rowAction {
			return model.next()
		}
		return row.activate(model)
	case keySpace:
		return row.activate(model)
	}
	return nil
}

func (model *setupModel) selectable() []setupRow {
	var rows []setupRow
	for _, row := range model.rows() {
		if row.kind != rowLine {
			rows = append(rows, row)
		}
	}
	return rows
}

func (model *setupModel) current() (setupRow, bool) {
	rows := model.selectable()
	if len(rows) == 0 {
		return setupRow{}, false
	}
	model.cursor = min(model.cursor, len(rows)-1)
	return rows[model.cursor], true
}

func (model *setupModel) move(delta int) {
	total := len(model.selectable())
	if total == 0 {
		return
	}
	model.cursor = (model.cursor + delta + total) % total
	model.syncFocus()
}

func (model *setupModel) syncFocus() {
	row, ok := model.current()
	for key := range model.fields {
		input := model.fields[key]
		if ok && row.kind == rowField && row.field == key {
			input.Focus()
		} else {
			input.Blur()
		}
		model.fields[key] = input
	}
}

func (model *setupModel) typeInto(key string, press tea.KeyMsg) tea.Cmd {
	input, cmd := model.fields[key].Update(press)
	model.fields[key] = input
	model.draft.set(key, input.Value())
	model.notice = nil
	if kind, ok := fieldProbes()[key]; ok {
		delete(model.probes, kind)
	}
	return cmd
}

func fieldProbes() map[string]probeKind {
	return map[string]probeKind{
		envDiscordToken: probeDiscord,
		envTelegramBot:  probeTelegram,
		envAnthropicKey: probeClaude,
		fieldCodexKey:   probeCodex,
		envCursorKey:    probeCursor,
		envXAIKey:       probeGrok,
		envVault:        probeVault,
	}
}

func (model *setupModel) goTo(screen setupScreen) tea.Cmd {
	model.leaveScreen(screen)
	model.screen = screen
	model.cursor = 0
	model.notice = nil
	model.syncFocus()
	return model.enter(screen)
}

func (model *setupModel) shown(screen setupScreen) bool {
	switch screen {
	case screenDiscord:
		return model.modules[moduleDiscord]
	case screenTelegram:
		return model.modules[moduleTelegram]
	case screenClaude:
		return model.modules[moduleClaude]
	case screenCodex:
		return model.modules[moduleCodex]
	case screenCursor:
		return model.modules[moduleCursor]
	case screenGrok:
		return model.modules[moduleGrok]
	case screenAntigravity:
		return model.modules[moduleAntigravity]
	case screenObsidian:
		return model.modules[moduleObsidian]
	case screenWelcome, screenModules, screenRoute, screenReview, screenInstall, screenConnections, screenHub:
		return true
	}
	return false
}

func (model *setupModel) returnTarget() (setupScreen, bool) {
	if model.detour != screenWelcome {
		target := model.detour
		model.detour = screenWelcome
		return target, true
	}
	if model.base == screenHub && model.screen != screenHub {
		return screenHub, true
	}
	return screenWelcome, false
}

func (model *setupModel) next() tea.Cmd {
	if target, ok := model.returnTarget(); ok {
		return model.goTo(target)
	}
	order := flowOrder()
	for idx, screen := range order {
		if screen != model.screen {
			continue
		}
		for _, later := range order[idx+1:] {
			if model.shown(later) {
				return model.goTo(later)
			}
		}
	}
	return nil
}

func (model *setupModel) quick() tea.Cmd {
	model.leaveScreen(screenModules)
	model.screen = screenModules
	return model.next()
}

func (model *setupModel) back() tea.Cmd {
	switch model.screen {
	case screenWelcome, screenHub:
		return nil
	case screenInstall:
		if model.install.finished() && !model.install.trialsRunning() {
			return model.openHub()
		}
		return nil
	case screenModules, screenDiscord, screenTelegram, screenClaude, screenCodex, screenCursor, screenGrok, screenAntigravity, screenObsidian, screenRoute, screenReview, screenConnections:
	}
	if target, ok := model.returnTarget(); ok {
		return model.goTo(target)
	}
	order := flowOrder()
	for idx := len(order) - 1; idx >= 0; idx-- {
		if order[idx] != model.screen {
			continue
		}
		for earlier := idx - 1; earlier >= 0; earlier-- {
			if model.shown(order[earlier]) {
				return model.goTo(order[earlier])
			}
		}
	}
	return model.goTo(screenWelcome)
}

func (model *setupModel) openHub() tea.Cmd {
	model.base = screenHub
	model.detour = screenWelcome
	return model.goTo(screenHub)
}

func openSection(screen setupScreen) func(*setupModel) tea.Cmd {
	return func(model *setupModel) tea.Cmd {
		if model.screen != screenHub {
			model.detour = model.screen
		}
		return model.goTo(screen)
	}
}

func (model *setupModel) enter(screen setupScreen) tea.Cmd {
	switch screen {
	case screenClaude:
		return model.statusCheck(probeClaude)
	case screenCodex:
		return model.statusCheck(probeCodex)
	case screenGrok:
		return model.statusCheck(probeGrok)
	case screenAntigravity:
		return model.statusCheck(probeAntigravity)
	case screenRoute:
		return model.loadCatalog()
	case screenHub:
		return tea.Batch(model.statusCheck(probeClaude), model.statusCheck(probeCodex), model.statusCheck(probeGrok), model.statusCheck(probeAntigravity), model.githubStatus())
	case screenInstall:
		return model.startInstall()
	case screenConnections:
		return model.githubStatus()
	case screenWelcome, screenModules, screenDiscord, screenTelegram, screenCursor, screenObsidian, screenReview:
	}
	return nil
}

func (model *setupModel) statusCheck(kind probeKind) tea.Cmd {
	switch kind {
	case probeClaude:
		if !model.modules[moduleClaude] || model.claudeKey {
			return nil
		}
	case probeCodex:
		if !model.modules[moduleCodex] || model.codexKey {
			return nil
		}
	case probeGrok:
		if !model.modules[moduleGrok] || model.grokKey {
			return nil
		}
	case probeAntigravity:
		if !model.modules[moduleAntigravity] {
			return nil
		}
	case probeDiscord, probeTelegram, probeCursor, probeVault:
		return nil
	}
	return model.vendorStatus(kind, false)
}

func (model *setupModel) probeInput(kind probeKind) string {
	switch kind {
	case probeDiscord:
		return model.draft.value(envDiscordToken)
	case probeTelegram:
		return model.draft.value(envTelegramBot)
	case probeClaude:
		if model.claudeKey {
			return model.draft.value(envAnthropicKey)
		}
	case probeCodex:
		if model.codexKey {
			return model.draft.value(fieldCodexKey)
		}
	case probeCursor:
		return model.draft.value(envCursorKey)
	case probeGrok:
		if model.grokKey {
			return model.draft.value(envXAIKey)
		}
	case probeVault:
		return model.draft.value(envVault)
	case probeAntigravity:
	}
	return ""
}

func (model *setupModel) vendorStatus(kind probeKind, afterLogin bool) tea.Cmd {
	prober := model.deps.prober
	switch kind {
	case probeCodex:
		return model.probe(kind, func(ctx context.Context) probeResult { return prober.codex(ctx, afterLogin) })
	case probeGrok:
		return model.probe(kind, func(ctx context.Context) probeResult { return prober.grok(ctx, afterLogin) })
	case probeAntigravity:
		return model.probe(kind, func(ctx context.Context) probeResult { return prober.antigravity(ctx, afterLogin) })
	case probeDiscord, probeTelegram, probeClaude, probeCursor, probeVault:
	}
	return model.probe(kind, func(ctx context.Context) probeResult { return prober.claude(ctx, afterLogin) })
}

func (model *setupModel) probe(kind probeKind, check func(context.Context) probeResult) tea.Cmd {
	model.probes[kind] = probeResult{state: probeRunning}
	input, parent := model.probeInput(kind), model.ctx
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(parent, probeTimeout)
		defer cancel()
		return probeDoneMsg{kind: kind, input: input, result: check(ctx)}
	}
}

func (model *setupModel) runProbe(kind probeKind) tea.Cmd {
	prober := model.deps.prober
	switch kind {
	case probeDiscord:
		token := model.draft.value(envDiscordToken)
		return model.probe(kind, func(ctx context.Context) probeResult { return prober.discord(ctx, token) })
	case probeTelegram:
		token := model.draft.value(envTelegramBot)
		return model.probe(kind, func(ctx context.Context) probeResult { return prober.telegram(ctx, token) })
	case probeClaude:
		if !model.claudeKey {
			return model.login(kind)
		}
		key := model.draft.value(envAnthropicKey)
		return model.probe(kind, func(ctx context.Context) probeResult { return prober.anthropicKey(ctx, key) })
	case probeCodex:
		if !model.codexKey {
			return model.login(kind)
		}
		key := model.draft.value(fieldCodexKey)
		return model.probe(kind, func(ctx context.Context) probeResult { return prober.codexKeyLogin(ctx, key) })
	case probeCursor:
		key := model.draft.value(envCursorKey)
		return model.probe(kind, func(context.Context) probeResult { return prober.cursor(key) })
	case probeGrok:
		if !model.grokKey {
			return model.login(kind)
		}
		key := model.draft.value(envXAIKey)
		return model.probe(kind, func(ctx context.Context) probeResult { return prober.xaiKey(ctx, key) })
	case probeAntigravity:
		return model.login(kind)
	case probeVault:
		path := expandHome(model.draft.value(envVault))
		return model.probe(kind, func(context.Context) probeResult { return vaultCheck(path) })
	}
	return nil
}

func (model *setupModel) login(kind probeKind) tea.Cmd {
	model.probes[kind] = probeResult{state: probeRunning}
	return model.deps.login(model.ctx, kind)
}

func (model *setupModel) loginDone(done loginDoneMsg) tea.Cmd {
	if errors.Is(done.err, exec.ErrNotFound) {
		name := loginCLI(done.kind)
		model.probes[done.kind] = failed(clitext.SetupCLIMissing, clitext.Params{clitext.ParamName: name})
		return nil
	}
	return model.vendorStatus(done.kind, true)
}

func loginCLI(kind probeKind) string {
	return loginCmd(context.Background(), kind).Args[0]
}

func loginCmd(ctx context.Context, kind probeKind) *exec.Cmd {
	switch kind {
	case probeCodex:
		return exec.CommandContext(ctx, kindCodex, verbLogin)
	case probeGrok:
		return exec.CommandContext(ctx, grokCLI, verbLogin)
	case probeAntigravity:
		return exec.CommandContext(ctx, antigravityCLI)
	case probeDiscord, probeTelegram, probeClaude, probeCursor, probeVault:
	}
	return exec.CommandContext(ctx, kindClaude, "auth", verbLogin)
}

func execLogin(ctx context.Context, kind probeKind) tea.Cmd {
	cmd := loginCmd(ctx, kind)
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return loginDoneMsg{kind: kind, err: err} })
}

func (model *setupModel) loadCatalog() tea.Cmd {
	policy := contracts.RoutePolicy(model.draft.value(envPolicy))
	model.route = routeModels{policy: policy, loading: true}
	fetch := model.deps.catalog
	if fetch == nil {
		model.route.loading = false
		return nil
	}
	return func() tea.Msg {
		entries, err := fetch(policy)
		return catalogMsg{policy: policy, entries: entries, err: err}
	}
}

func (model *setupModel) setPolicy(policy contracts.RoutePolicy) tea.Cmd {
	model.draft.set(envPolicy, string(policy))
	model.notice = nil
	return model.loadCatalog()
}

func (model *setupModel) offeredModels() []host.CatalogEntry {
	kinds := model.modules.backendKinds()
	var offered []host.CatalogEntry
	for idx := range model.route.entries {
		if kinds[model.route.entries[idx].Vendor] {
			offered = append(offered, model.route.entries[idx])
		}
	}
	return offered
}

func (model *setupModel) leaveRoute() tea.Cmd {
	policy := contracts.RoutePolicy(model.draft.value(envPolicy))
	if err := gatewayPairError(policy, model.draft.value(envGatewayURL), model.draft.value(envGatewayToken)); err != nil {
		model.notice = err
		return nil
	}
	return model.next()
}

func gatewayPairError(policy contracts.RoutePolicy, url, token string) error {
	if policy != contracts.PolicyGatewayOnly {
		return nil
	}
	switch {
	case url == "" && token == "":
		return errNoGatewayCreds
	case url == "" || token == "":
		return errIncompleteCreds
	}
	return nil
}

func (model *setupModel) failedChecks() int {
	total := 0
	for kind, result := range model.probes {
		if result.state == probeFailed && model.shown(probeScreen(kind)) {
			total++
		}
	}
	return total
}

func probeScreen(kind probeKind) setupScreen {
	switch kind {
	case probeDiscord:
		return screenDiscord
	case probeTelegram:
		return screenTelegram
	case probeClaude:
		return screenClaude
	case probeCodex:
		return screenCodex
	case probeCursor:
		return screenCursor
	case probeGrok:
		return screenGrok
	case probeAntigravity:
		return screenAntigravity
	case probeVault:
		return screenObsidian
	}
	return screenHub
}
