package manage

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/host"
)

type fakeShell struct {
	replies map[string]fakeReply
	effect  func(setupCommand)
	calls   []setupCommand
	mu      sync.Mutex
}

type fakeReply struct {
	err error
	out string
}

func newShell() *fakeShell {
	return &fakeShell{replies: map[string]fakeReply{
		"claude auth status --json":                       {out: `{"loggedIn":true,"authMethod":"claude.ai","subscriptionType":"max"}`},
		"codex login status":                              {out: "Logged in using ChatGPT\n"},
		"claude -p " + trialPrompt:                        {out: "OK\n"},
		"codex exec --skip-git-repo-check " + trialPrompt: {out: "OK\n"},
		cursorCLI + " -p " + trialPrompt:                  {out: "OK\n"},
		grokCLI + " models":                               {out: "You are logged in with grok.com.\n\nDefault model: grok-4.7\n"},
	}}
}

func (shell *fakeShell) run(_ context.Context, cmd setupCommand) ([]byte, error) {
	shell.mu.Lock()
	defer shell.mu.Unlock()
	shell.calls = append(shell.calls, cmd)
	if shell.effect != nil {
		shell.effect(cmd)
	}
	reply := shell.replies[strings.Join(append([]string{cmd.name}, cmd.args...), " ")]
	if reply.err == nil && cmd.name == "go" && len(cmd.args) > 2 && cmd.args[0] == "build" && cmd.args[1] == "-o" {
		return []byte(reply.out), os.WriteFile(cmd.args[2], []byte(freshBuild), modeFile)
	}
	return []byte(reply.out), reply.err
}

const freshBuild = "fresh build"

func (shell *fakeShell) goCommands() []string {
	var lines []string
	for _, line := range shell.commands() {
		if strings.HasPrefix(line, "go ") {
			lines = append(lines, line)
		}
	}
	return lines
}

func (shell *fakeShell) commands() []string {
	shell.mu.Lock()
	defer shell.mu.Unlock()
	var lines []string
	for _, call := range shell.calls {
		lines = append(lines, strings.Join(append([]string{call.name}, call.args...), " "))
	}
	return lines
}

func discordServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bot good-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if _, err := w.Write([]byte(`{"username":"herrscher-bot"}`)); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func telegramServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bot123:good/getMe" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if _, err := w.Write([]byte(`{"ok":true,"result":{"id":7,"username":"herrscher_bot"}}`)); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func anthropicServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("X-Api-Key") != "sk-ant-good" || r.Header.Get("Anthropic-Version") != anthropicVersion {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if _, err := w.Write([]byte(`{"data":[]}`)); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func testProber(t *testing.T, shell *fakeShell) setupProber {
	t.Helper()
	return setupProber{
		run:          shell.run,
		lookPath:     func(string) (string, error) { return "/usr/bin/cursor-agent", nil },
		client:       http.DefaultClient,
		discordURL:   discordServer(t).URL,
		telegramURL:  telegramServer(t).URL,
		anthropicURL: anthropicServer(t).URL,
		xaiURL:       xaiServer(t).URL,
	}
}

func xaiServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/api-key" || r.Header.Get("Authorization") != "Bearer xai-good" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if _, err := w.Write([]byte(`{"api_key_blocked":false}`)); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func fakeCatalog(contracts.RoutePolicy) ([]host.CatalogEntry, error) {
	return []host.CatalogEntry{
		{ModelSpec: contracts.ModelSpec{ID: "claude-opus", Label: "Opus"}, Vendor: kindClaude},
		{ModelSpec: contracts.ModelSpec{ID: "gpt-codex", Label: "Codex"}, Vendor: kindCodex},
		{ModelSpec: contracts.ModelSpec{ID: "grok-build", Label: "Grok"}, Vendor: kindGrok},
		{ModelSpec: contracts.ModelSpec{ID: "gemini-agy", Label: "Gemini"}, Vendor: kindAntigravity},
	}, nil
}

type setupHarness struct {
	model *setupModel
	shell *fakeShell
	env   string
}

func newHarness(t *testing.T, manifest []byte, screen setupScreen, env map[string]string) *setupHarness {
	t.Helper()
	return newHarnessWithGitHub(t, manifest, screen, env, &fakeGitHub{})
}

func newHarnessWithGitHub(t *testing.T, manifest []byte, screen setupScreen, env map[string]string, github gitHubLink) *setupHarness {
	t.Helper()
	shell := newShell()
	dir := t.TempDir()
	deps := setupDeps{
		prober: testProber(t, shell),
		github: github,
		login: func(_ context.Context, kind probeKind) tea.Cmd {
			return func() tea.Msg { return loginDoneMsg{kind: kind} }
		},
		catalog:     fakeCatalog,
		run:         shell.run,
		envPath:     filepath.Join(dir, ".env"),
		hostDir:     dir,
		manifest:    manifest,
		binary:      filepath.Join(dir, binaryName),
		abandonWait: time.Minute,
		trialWait:   time.Minute,
	}
	if manifest != nil {
		if err := os.WriteFile(filepath.Join(dir, manifestFile), manifest, modeFile); err != nil {
			t.Fatal(err)
		}
	}
	getenv := func(key string) string { return env[key] }
	harness := &setupHarness{model: newSetupModel(testMessages(), deps, setupStart{ctx: t.Context(), getenv: getenv, screen: screen}), shell: shell, env: deps.envPath}
	harness.drive(harness.model.Init())
	return harness
}

func (harness *setupHarness) drive(cmd tea.Cmd) {
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		if next == nil {
			continue
		}
		switch message := next().(type) {
		case nil, tea.QuitMsg:
		case tea.BatchMsg:
			queue = append(queue, message...)
		default:
			_, follow := harness.model.Update(message)
			queue = append(queue, follow)
		}
	}
}

func (harness *setupHarness) press(press tea.KeyMsg) {
	_, cmd := harness.model.Update(press)
	harness.drive(cmd)
}

func (harness *setupHarness) enter() { harness.press(tea.KeyMsg{Type: tea.KeyEnter}) }

func (harness *setupHarness) typeText(text string) {
	harness.press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)})
}

func (harness *setupHarness) text(key clitext.Key) string {
	return msg(testMessages(), key, nil)
}

func (harness *setupHarness) focus(t *testing.T, match func(setupRow) bool) {
	t.Helper()
	for idx, row := range harness.model.selectable() {
		if match(row) {
			harness.model.cursor = idx
			harness.model.syncFocus()
			return
		}
	}
	t.Fatalf("screen %d has no such row", harness.model.screen)
}

func (harness *setupHarness) choose(t *testing.T, label string) {
	t.Helper()
	harness.focus(t, func(row setupRow) bool { return row.text == label })
	harness.enter()
}

func (harness *setupHarness) fill(t *testing.T, field, value string) {
	t.Helper()
	harness.focus(t, func(row setupRow) bool { return row.kind == rowField && row.field == field })
	harness.typeText(value)
}

func (harness *setupHarness) proceed(t *testing.T) {
	t.Helper()
	harness.choose(t, harness.text(clitext.SetupContinue))
}

func (harness *setupHarness) expectScreen(t *testing.T, want setupScreen) {
	t.Helper()
	if harness.model.screen != want {
		t.Fatalf("screen = %d, want %d", harness.model.screen, want)
	}
}

func (harness *setupHarness) envFile(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(harness.env)
	if err != nil {
		t.Fatalf("read env: %v", err)
	}
	return string(data)
}

func TestQuickSetupOnBinaryHostChecksEverythingAndWritesTheSettings(t *testing.T) {
	harness := newHarness(t, nil, screenWelcome, nil)
	harness.choose(t, harness.text(clitext.SetupQuick))
	harness.expectScreen(t, screenDiscord)

	harness.fill(t, envDiscordToken, "good-token")
	harness.fill(t, envDiscordUser, "42")
	harness.choose(t, harness.text(clitext.SetupBotProbe))
	if got := harness.model.probes[probeDiscord]; got.state != probePassed || got.params[clitext.ParamName] != "herrscher-bot" {
		t.Fatalf("discord probe = %+v, want passed as herrscher-bot", got)
	}
	harness.proceed(t)

	harness.expectScreen(t, screenClaude)
	if got := harness.model.probes[probeClaude]; got.state != probePassed || got.params[clitext.ParamName] != "max" {
		t.Fatalf("claude status = %+v, want signed in on max", got)
	}
	harness.proceed(t)

	harness.expectScreen(t, screenObsidian)
	vault := t.TempDir()
	harness.fill(t, envVault, vault)
	harness.choose(t, harness.text(clitext.SetupVaultProbe))
	if got := harness.model.probes[probeVault]; got.state != probePassed {
		t.Fatalf("vault probe = %+v, want passed", got)
	}
	harness.proceed(t)

	harness.expectScreen(t, screenRoute)
	harness.choose(t, "claude-opus")
	harness.choose(t, harness.text(clitext.SetupLangFrench))
	harness.proceed(t)

	harness.expectScreen(t, screenReview)
	harness.choose(t, harness.text(clitext.SetupReviewSave))
	harness.expectScreen(t, screenInstall)
	if !harness.model.install.done {
		t.Fatalf("install not done: %+v", harness.model.install)
	}
	env := harness.envFile(t)
	for _, want := range []string{"DISCORD_BOT_TOKEN=good-token", "DISCORD_USER_ID=42", "OBSIDIAN_VAULT=" + vault, envModel + "=claude-opus", envLanguage + "=fr"} {
		if !strings.Contains(env, want) {
			t.Errorf("env file = %q, want line %q", env, want)
		}
	}
	if len(harness.model.draft.edits) != 0 {
		t.Fatalf("edits left after a save: %v", harness.model.draft.edits)
	}
}

func TestDiscordProbeReportsARefusedToken(t *testing.T) {
	harness := newHarness(t, nil, screenDiscord, nil)
	harness.fill(t, envDiscordToken, "wrong")
	harness.choose(t, harness.text(clitext.SetupBotProbe))
	if got := harness.model.probes[probeDiscord]; got.state != probeFailed || got.key != clitext.SetupBotRefused {
		t.Fatalf("probe = %+v, want refused", got)
	}
	harness.fill(t, envDiscordToken, "x")
	if _, stale := harness.model.probes[probeDiscord]; stale {
		t.Fatal("editing the token kept the old probe result")
	}
}

func TestClaudeSignInRunsTheLoginThenRechecks(t *testing.T) {
	harness := newHarness(t, nil, screenClaude, nil)
	harness.shell.replies["claude auth status --json"] = fakeReply{out: `{"loggedIn":false}`}
	harness.drive(harness.model.statusCheck(probeClaude))
	if got := harness.model.probes[probeClaude]; got.key != clitext.SetupLoggedOut {
		t.Fatalf("status = %+v, want logged out", got)
	}
	harness.choose(t, harness.text(clitext.SetupLogin))
	if got := harness.model.probes[probeClaude]; got.key != clitext.SetupLoginUnfinished {
		t.Fatalf("after a login that did not sign in, probe = %+v, want unfinished", got)
	}
	harness.shell.replies["claude auth status --json"] = fakeReply{out: `{"loggedIn":true,"authMethod":"console"}`}
	harness.choose(t, harness.text(clitext.SetupLogin))
	if got := harness.model.probes[probeClaude]; got.state != probePassed || got.params[clitext.ParamName] != "console" {
		t.Fatalf("after login, probe = %+v, want signed in", got)
	}
}

func TestLoginWithoutTheCLISaysItIsMissing(t *testing.T) {
	harness := newHarness(t, nil, screenClaude, nil)
	harness.model.deps.login = func(_ context.Context, kind probeKind) tea.Cmd {
		return func() tea.Msg { return loginDoneMsg{kind: kind, err: exec.ErrNotFound} }
	}
	harness.choose(t, harness.text(clitext.SetupLogin))
	if got := harness.model.probes[probeClaude]; got.key != clitext.SetupCLIMissing || got.params[clitext.ParamName] != kindClaude {
		t.Fatalf("probe = %+v, want the claude CLI missing", got)
	}
}

func TestClaudeAPIKeyIsCheckedAndWritten(t *testing.T) {
	harness := newHarness(t, nil, screenClaude, nil)
	harness.choose(t, harness.text(clitext.SetupAPIKey))
	harness.fill(t, envAnthropicKey, "sk-ant-bad")
	harness.choose(t, harness.text(clitext.SetupCheckKey))
	if got := harness.model.probes[probeClaude]; got.key != clitext.SetupKeyRefused {
		t.Fatalf("probe = %+v, want refused", got)
	}
	harness.focus(t, func(row setupRow) bool { return row.field == envAnthropicKey })
	harness.press(tea.KeyMsg{Type: tea.KeyCtrlU})
	harness.typeText("sk-ant-good")
	harness.choose(t, harness.text(clitext.SetupCheckKey))
	if got := harness.model.probes[probeClaude]; got.key != clitext.SetupKeyAccepted {
		t.Fatalf("probe = %+v, want accepted", got)
	}
	if got := harness.model.draft.written()[envAnthropicKey]; got != "sk-ant-good" {
		t.Fatalf("written key = %q", got)
	}
	harness.choose(t, harness.text(clitext.SetupClaudeSubscription))
	if got, ok := harness.model.draft.written()[envAnthropicKey]; ok {
		t.Fatalf("back on the subscription the key is still written: %q", got)
	}
}

func TestCodexAPIKeyGoesToTheCLINotTheSettings(t *testing.T) {
	modules := defaultModules()
	modules[moduleCodex] = true
	harness := newHarness(t, nil, screenCodex, nil)
	harness.model.modules = modules
	harness.choose(t, harness.text(clitext.SetupAPIKey))
	harness.shell.replies["codex login status"] = fakeReply{out: "Logged in using an API key - sk-proj-***\n"}
	harness.fill(t, fieldCodexKey, "sk-proj-secret")
	harness.choose(t, harness.text(clitext.SetupCheckKey))
	if got := harness.model.probes[probeCodex]; got.state != probePassed || got.params[clitext.ParamName] != "an API key" {
		t.Fatalf("probe = %+v, want signed in with an API key", got)
	}
	var login *setupCommand
	for idx := range harness.shell.calls {
		if slices.Equal(harness.shell.calls[idx].args, []string{"login", "--with-api-key"}) {
			login = &harness.shell.calls[idx]
		}
	}
	if login == nil || login.stdin != "sk-proj-secret\n" {
		t.Fatalf("codex login calls = %v, want the key piped on stdin", harness.shell.commands())
	}
	if _, leaked := harness.model.draft.written()[fieldCodexKey]; leaked {
		t.Fatal("the codex key would be written to the settings file")
	}
}

func TestGatewayOnlyRouteNeedsBothCredentials(t *testing.T) {
	harness := newHarness(t, nil, screenRoute, nil)
	harness.choose(t, string(contracts.PolicyGatewayOnly))
	harness.proceed(t)
	harness.expectScreen(t, screenRoute)
	if !errors.Is(harness.model.notice, errNoGatewayCreds) {
		t.Fatalf("notice = %v, want missing credentials", harness.model.notice)
	}
	harness.fill(t, envGatewayURL, "https://gw.example")
	harness.proceed(t)
	if !errors.Is(harness.model.notice, errIncompleteCreds) {
		t.Fatalf("notice = %v, want incomplete credentials", harness.model.notice)
	}
	harness.fill(t, envGatewayToken, "secret")
	harness.proceed(t)
	harness.expectScreen(t, screenHub)
}

func TestRouteOffersOnlyModelsOfCheckedBackends(t *testing.T) {
	harness := newHarness(t, nil, screenRoute, nil)
	var offered []string
	for _, entry := range harness.model.offeredModels() {
		offered = append(offered, entry.ID)
	}
	if !slices.Equal(offered, []string{"claude-opus"}) {
		t.Fatalf("offered = %v, want only the claude model", offered)
	}
}

func TestHubSectionReturnsToTheHub(t *testing.T) {
	harness := newHarness(t, nil, screenHub, map[string]string{envDiscordToken: "good-token"})
	harness.choose(t, kindDiscord)
	harness.expectScreen(t, screenDiscord)
	harness.proceed(t)
	harness.expectScreen(t, screenHub)
	harness.choose(t, kindDiscord)
	harness.press(tea.KeyMsg{Type: tea.KeyEsc})
	harness.expectScreen(t, screenHub)
}

func TestReviewRowOpensItsSectionAndComesBack(t *testing.T) {
	harness := newHarness(t, nil, screenDiscord, nil)
	harness.fill(t, envDiscordUser, "42")
	harness.drive(harness.model.goTo(screenReview))
	harness.choose(t, envDiscordUser)
	harness.expectScreen(t, screenDiscord)
	harness.proceed(t)
	harness.expectScreen(t, screenReview)
}

func TestModulesScreenTogglesAndSkipsUncheckedSections(t *testing.T) {
	harness := newHarness(t, nil, screenWelcome, nil)
	harness.choose(t, harness.text(clitext.SetupCustom))
	harness.expectScreen(t, screenModules)
	harness.focus(t, func(row setupRow) bool { return row.text == kindDiscord })
	harness.press(tea.KeyMsg{Type: tea.KeySpace})
	harness.focus(t, func(row setupRow) bool { return row.text == kindTerminal })
	harness.press(tea.KeyMsg{Type: tea.KeySpace})
	if harness.model.modules[moduleDiscord] || !harness.model.modules[moduleTerminal] {
		t.Fatalf("modules = %v, want discord off and terminal kept", harness.model.modules)
	}
	harness.proceed(t)
	harness.expectScreen(t, screenClaude)
}

func TestTheTelegramModuleAsksForItsTokenAndChecksIt(t *testing.T) {
	harness := newHarness(t, nil, screenWelcome, nil)
	harness.choose(t, harness.text(clitext.SetupCustom))
	harness.focus(t, func(row setupRow) bool { return row.text == kindTelegram })
	harness.press(tea.KeyMsg{Type: tea.KeySpace})
	harness.proceed(t)
	harness.expectScreen(t, screenDiscord)
	harness.proceed(t)
	harness.expectScreen(t, screenTelegram)

	harness.fill(t, envTelegramBot, "123:good")
	harness.fill(t, envTelegramUser, "42")
	harness.choose(t, harness.text(clitext.SetupBotProbe))
	if got := harness.model.probes[probeTelegram]; got.state != probePassed || got.params[clitext.ParamName] != "@herrscher_bot" {
		t.Fatalf("telegram probe = %+v, want passed as @herrscher_bot", got)
	}
}

func TestATelegramTokenTheBotAPIRefusesIsReported(t *testing.T) {
	harness := newHarness(t, nil, screenTelegram, map[string]string{envTelegramBot: "123:bad"})
	harness.choose(t, harness.text(clitext.SetupBotProbe))
	if got := harness.model.probes[probeTelegram]; got.state != probeFailed || got.key != clitext.SetupBotRefused {
		t.Fatalf("telegram probe with a wrong token = %+v", got)
	}
}

func TestATelegramProbeThatCannotConnectNeverShowsTheToken(t *testing.T) {
	prober := setupProber{client: http.DefaultClient, telegramURL: "http://127.0.0.1:1"}

	for _, token := range []string{"123:secret", "123:sec ret"} {
		got := prober.telegram(t.Context(), token)

		shown := fmt.Sprint(got.params)
		if got.state != probeFailed || strings.Contains(shown, token) || strings.Contains(shown, url.PathEscape(token)) {
			t.Fatalf("probe = %+v", got)
		}
	}
}

func TestTheTelegramSectionFlagsWhatStopsTheGateway(t *testing.T) {
	var malformed, missing clitext.Key = clitext.SetupTelegramMalformed, clitext.SetupTelegramUserMissing
	cases := []struct {
		env  map[string]string
		want clitext.Key
	}{
		{make(map[string]string), missing},
		{map[string]string{envTelegramUser: "@ana"}, malformed},
		{map[string]string{envTelegramUser: "0"}, malformed},
		{map[string]string{envTelegramMany: "42, ana"}, malformed},
		{map[string]string{envTelegramAll: "maybe"}, malformed},
		{map[string]string{envTelegramUser: "42", envTelegramMany: "42, ana"}, malformed},
		{map[string]string{envTelegramUser: "@ana", envTelegramAll: "on"}, malformed},
		{map[string]string{envTelegramUser: "42", envTelegramAll: "maybe"}, malformed},
		{map[string]string{envTelegramUser: "42", envTelegramChat: "mygroup"}, malformed},
		{map[string]string{envTelegramAll: "off"}, missing},
		{map[string]string{envTelegramUser: "42"}, ""},
		{map[string]string{envTelegramMany: "42 43"}, ""},
		{map[string]string{envTelegramAll: "on"}, ""},
		{map[string]string{envTelegramUser: "42", envTelegramAll: "off"}, ""},
		{map[string]string{envTelegramUser: "42", envTelegramChat: "-100500, -100600"}, ""},
	}
	for _, tc := range cases {
		tc.env[envTelegramBot] = "123:good"
		harness := newHarness(t, nil, screenHub, tc.env)
		harness.model.modules[moduleTelegram] = true
		for _, section := range harness.model.sections() {
			if section.screen != screenTelegram {
				continue
			}
			flagged := section.state == harness.text(malformed) || section.state == harness.text(missing)
			if (tc.want == "") == flagged || (tc.want != "" && section.state != harness.text(tc.want)) {
				t.Fatalf("env %v: telegram section = %+v, want %q", tc.env, section, tc.want)
			}
		}
	}
}

func TestTheTelegramSettingsAreWrittenToTheEnvFile(t *testing.T) {
	harness := newHarness(t, nil, screenTelegram, nil)
	harness.fill(t, envTelegramBot, "123:good")
	harness.fill(t, envTelegramUser, "42")
	harness.drive(harness.model.goTo(screenReview))
	harness.choose(t, harness.text(clitext.SetupReviewSave))

	env := harness.envFile(t)
	for _, want := range []string{"TELEGRAM_BOT_TOKEN=123:good", "TELEGRAM_USER_ID=42"} {
		if !strings.Contains(env, want) {
			t.Errorf("env file = %q, want line %q", env, want)
		}
	}
}

const setupManifest = `package main

import (
	// herrscher:plugins
	_ "github.com/Herrscherd/herrscher-claude-backend"
	_ "github.com/Herrscherd/herrscher-discord-gateway"
	_ "github.com/Herrscherd/herrscher/plugins/terminal"
	_ "github.com/acme/extra-plugin"
	// herrscher:end
)
`

func TestSourceInstallRewritesPluginsAndRebuilds(t *testing.T) {
	harness := newHarness(t, []byte(setupManifest), screenModules, nil)
	if !slices.Equal(harness.model.extras, []string{"github.com/acme/extra-plugin"}) {
		t.Fatalf("extras = %v, want the unknown module kept", harness.model.extras)
	}
	harness.model.modules[moduleCodex] = true
	harness.drive(harness.model.goTo(screenInstall))
	if !harness.model.install.done {
		t.Fatalf("install = %+v, want done", harness.model.install)
	}
	data, err := os.ReadFile(filepath.Join(harness.model.deps.hostDir, manifestFile))
	if err != nil {
		t.Fatal(err)
	}
	mods, err := listPlugins(testMessages(), string(data))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"github.com/Herrscherd/herrscher-codex-backend", "github.com/acme/extra-plugin", inTree()[0]} {
		if !slices.Contains(mods, want) {
			t.Errorf("plugins.go = %v, missing %s", mods, want)
		}
	}
	got := harness.shell.goCommands()
	build := "go build -o " + freshBinary(harness.model.deps.binary) + " ."
	if !slices.Contains(got, "go get -- github.com/Herrscherd/herrscher-codex-backend") || !slices.Contains(got, "go mod tidy") || got[len(got)-1] != build {
		t.Fatalf("commands = %v, want go get, tidy then build", got)
	}
	if binary, err := os.ReadFile(harness.model.deps.binary); err != nil || string(binary) != freshBuild {
		t.Fatalf("binary = %q (%v), want the fresh build in place", binary, err)
	}
	if slices.Contains(got, "go get -- "+inTree()[0]) {
		t.Fatalf("commands = %v, the in-tree module was fetched", got)
	}
}

func TestFailedBuildRestoresPlugins(t *testing.T) {
	harness := newHarness(t, []byte(setupManifest), screenModules, nil)
	harness.shell.replies["go build -o "+freshBinary(harness.model.deps.binary)+" ."] = fakeReply{out: "line one\nboom\n", err: errProbeStatus}
	goMod := filepath.Join(harness.model.deps.hostDir, "go.mod")
	goSum := filepath.Join(harness.model.deps.hostDir, "go.sum")
	if err := os.WriteFile(goMod, []byte("module before\n"), modeFile); err != nil {
		t.Fatal(err)
	}
	harness.shell.effect = func(cmd setupCommand) {
		if cmd.args[0] != verbGet {
			return
		}
		if err := errors.Join(os.WriteFile(goMod, []byte("module after\n"), modeFile), os.WriteFile(goSum, []byte("sum\n"), modeFile)); err != nil {
			t.Error(err)
		}
	}
	harness.model.modules[moduleCodex] = true
	harness.drive(harness.model.goTo(screenInstall))
	if !errors.Is(harness.model.install.err, errProbeStatus) || !harness.model.install.restored {
		t.Fatalf("install = %+v, want failed and restored", harness.model.install)
	}
	data, err := os.ReadFile(filepath.Join(harness.model.deps.hostDir, manifestFile))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != setupManifest {
		t.Fatalf("plugins.go = %q, want the original back", data)
	}
	if mod, err := os.ReadFile(goMod); err != nil || string(mod) != "module before\n" {
		t.Fatalf("go.mod = %q (%v), want the original back", mod, err)
	}
	if _, err := os.Stat(goSum); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("go.sum stat err = %v, want it removed since it did not exist", err)
	}
	build := harness.model.install.steps[len(harness.model.install.steps)-2]
	if build.state != probeFailed || build.output != "line one\nboom" {
		t.Fatalf("build step = %+v", build)
	}
	if _, err := os.Stat(harness.model.deps.binary); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("binary stat err = %v, want nothing put in place after a failed build", err)
	}
}

func TestUnchangedStackSkipsTheRebuild(t *testing.T) {
	harness := newHarness(t, []byte(setupManifest), screenReview, nil)
	harness.drive(harness.model.goTo(screenInstall))
	if !harness.model.install.done || len(harness.shell.goCommands()) != 0 {
		t.Fatalf("install = %+v, commands = %v, want done with no go command", harness.model.install, harness.shell.goCommands())
	}
}

func TestHostPluginsRoundTripUnchanged(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", manifestFile))
	if err != nil {
		t.Fatal(err)
	}
	paths, err := listPlugins(testMessages(), string(source))
	if err != nil {
		t.Fatal(err)
	}
	set, extras := modulesFromPaths(paths)
	rendered, err := setPlugins(testMessages(), string(source), set.paths(extras))
	if err != nil {
		t.Fatal(err)
	}
	if rendered != string(source) {
		t.Fatal("reading then writing the host plugins.go changes it")
	}
}

func TestSingleCategoryToggleIsExclusive(t *testing.T) {
	set := make(moduleSet)
	group := moduleGroups()[2]
	set.toggle(group, moduleObsidian)
	set.unset(group)
	if set[moduleObsidian] {
		t.Fatal("unset kept the memory module")
	}
	set.toggle(moduleGroups()[0], moduleTerminal)
	if set[moduleTerminal] {
		t.Fatal("the terminal toggled")
	}
}

func TestDraftForgetsAnEditBackToTheInitialValue(t *testing.T) {
	draft := newDraft(map[string]string{envModel: "a"})
	draft.set(envModel, "b")
	draft.set(fieldCodexKey, "sk")
	if got := draft.written(); len(got) != 1 || got[envModel] != "b" {
		t.Fatalf("written = %v", got)
	}
	draft.set(envModel, "a")
	if got := draft.written(); len(got) != 0 {
		t.Fatalf("written = %v, want nothing", got)
	}
}

func TestCodexStatusParsing(t *testing.T) {
	cases := []struct {
		reply  fakeReply
		key    clitext.Key
		name   string
		passed bool
	}{
		{reply: fakeReply{out: "Logged in using ChatGPT\n"}, key: clitext.SetupLoggedIn, name: "ChatGPT", passed: true},
		{reply: fakeReply{out: "Logged in using an API key - sk-***\n"}, key: clitext.SetupLoggedIn, name: "an API key", passed: true},
		{reply: fakeReply{out: "Not logged in\n", err: errProbeStatus}, key: clitext.SetupLoggedOut},
		{reply: fakeReply{err: exec.ErrNotFound}, key: clitext.SetupCLIMissing, name: kindCodex},
	}
	for _, tc := range cases {
		shell := newShell()
		shell.replies["codex login status"] = tc.reply
		got := setupProber{run: shell.run}.codex(context.Background(), false)
		if got.key != tc.key || (got.state == probePassed) != tc.passed || (tc.name != "" && got.params[clitext.ParamName] != tc.name) {
			t.Errorf("reply %q: got %+v", tc.reply.out, got)
		}
	}
}

func TestClaudeStatusWithBrokenOutput(t *testing.T) {
	shell := newShell()
	shell.replies["claude auth status --json"] = fakeReply{out: "not json", err: errProbeStatus}
	got := setupProber{run: shell.run}.claude(context.Background(), false)
	if got.key != clitext.SetupProbeFailed {
		t.Fatalf("probe = %+v, want a failure", got)
	}
}

func TestCursorNeedsTheCLIAndAKey(t *testing.T) {
	missing := setupProber{lookPath: func(string) (string, error) { return "", exec.ErrNotFound }}.cursor("key")
	if missing.key != clitext.SetupCLIMissing {
		t.Fatalf("probe = %+v, want the CLI missing", missing)
	}
	found := setupProber{lookPath: func(string) (string, error) { return "cursor-agent", nil }}
	if got := found.cursor(""); got.key != clitext.SetupKeyEmpty {
		t.Fatalf("probe = %+v, want an empty key", got)
	}
	if got := found.cursor("key"); got.state != probePassed {
		t.Fatalf("probe = %+v, want passed", got)
	}
}

func TestVaultCheck(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "note.md")
	if err := os.WriteFile(file, nil, modeFile); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		path string
		key  clitext.Key
	}{
		{path: "", key: clitext.SetupVaultEmpty},
		{path: dir, key: clitext.SetupVaultOK},
		{path: filepath.Join(dir, "new"), key: clitext.SetupVaultCreate},
		{path: filepath.Join(dir, "a", "b"), key: clitext.SetupVaultBad},
		{path: file, key: clitext.SetupVaultBad},
		{path: "relative/vault", key: clitext.SetupVaultBad},
	}
	for _, tc := range cases {
		if got := vaultCheck(tc.path); got.key != tc.key {
			t.Errorf("vaultCheck(%q) = %+v, want %s", tc.path, got, tc.key)
		}
	}
	leftovers, err := filepath.Glob(filepath.Join(dir, ".herrscher-setup-*"))
	if err != nil || len(leftovers) != 0 {
		t.Fatalf("write test left %v (%v)", leftovers, err)
	}
}

func TestSetupEntry(t *testing.T) {
	t.Setenv("HERRSCHER_ENV_FILE", filepath.Join(t.TempDir(), ".env"))
	if screen, err := setupEntry(testMessages(), nil); err != nil || screen != screenWelcome {
		t.Fatalf("no env file: screen %d, err %v, want welcome", screen, err)
	}
	if screen, err := setupEntry(testMessages(), []string{"route"}); err != nil || screen != screenRoute {
		t.Fatalf("route: screen %d, err %v", screen, err)
	}
	if _, err := setupEntry(testMessages(), []string{"nope"}); !errors.Is(err, errUnknownSection) {
		t.Fatalf("unknown section err = %v", err)
	}
	if err := os.WriteFile(os.Getenv("HERRSCHER_ENV_FILE"), nil, modeFile); err != nil {
		t.Fatal(err)
	}
	if screen, err := setupEntry(testMessages(), nil); err != nil || screen != screenHub {
		t.Fatalf("with an env file: screen %d, err %v, want the hub", screen, err)
	}
}

func TestViewEscapesCatalogText(t *testing.T) {
	harness := newHarness(t, nil, screenRoute, nil)
	harness.model.route.entries = []host.CatalogEntry{{ModelSpec: contracts.ModelSpec{ID: "evil\x1b]0;pwn\a", Label: "x"}, Vendor: kindClaude}}
	view := harness.model.View()
	if strings.Contains(view, "\x1b]0;pwn") || strings.Contains(view, "\a") {
		t.Fatalf("view leaks raw control sequences: %q", view)
	}
	if !strings.Contains(view, "evil") || !strings.Contains(view, "pwn") {
		t.Fatalf("view drops the catalog entry instead of escaping it: %q", view)
	}
}

func TestEveryScreenRenders(t *testing.T) {
	harness := newHarness(t, []byte(setupManifest), screenHub, nil)
	for screen := screenWelcome; screen <= screenHub; screen++ {
		harness.model.screen = screen
		if view := harness.model.View(); !strings.Contains(view, "herrscher · ") {
			t.Errorf("screen %d renders no header", screen)
		}
	}
}

func (harness *setupHarness) startInstallUntilGoGet(t *testing.T) tea.Cmd {
	t.Helper()
	harness.model.modules[moduleCodex] = true
	cmd := harness.model.goTo(screenInstall)
	for range 2 {
		_, cmd = harness.model.Update(cmd())
	}
	if !harness.model.install.touched {
		t.Fatalf("install = %+v, want plugins.go written before go get", harness.model.install)
	}
	return cmd
}

func (harness *setupHarness) manifestOnDisk(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(harness.model.deps.hostDir, manifestFile))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestCtrlCDuringInstallCancelsRestoresThenQuits(t *testing.T) {
	harness := newHarness(t, []byte(setupManifest), screenReview, nil)
	pending := harness.startInstallUntilGoGet(t)
	if _, cmd := harness.model.Update(tea.KeyMsg{Type: tea.KeyCtrlC}); cmd != nil {
		t.Fatal("ctrl+c quit while a step was still running")
	}
	if harness.model.install.ctx.Err() == nil {
		t.Fatal("ctrl+c left the running step's context alive")
	}
	_, cmd := harness.model.Update(pending())
	if cmd == nil {
		t.Fatal("the aborted install never quits")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("the aborted install returns a command that is not a quit")
	}
	if !errors.Is(harness.model.install.err, errInstallAborted) || !harness.model.install.restored {
		t.Fatalf("install = %+v, want aborted and restored", harness.model.install)
	}
	if got := harness.manifestOnDisk(t); got != setupManifest {
		t.Fatalf("plugins.go = %q, want the original back", got)
	}
}

func TestAbandonedProgramWaitsForTheStepThenRestores(t *testing.T) {
	harness := newHarness(t, []byte(setupManifest), screenReview, nil)
	pending := harness.startInstallUntilGoGet(t)
	entered, release := make(chan struct{}), make(chan struct{})
	harness.shell.effect = func(setupCommand) {
		close(entered)
		<-release
	}
	go pending()
	<-entered
	abandoned := make(chan struct{})
	go func() {
		harness.model.abandon()
		close(abandoned)
	}()
	select {
	case <-abandoned:
		t.Fatal("abandon returned while the step was still running")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	<-abandoned
	if !errors.Is(harness.model.install.err, errInstallAborted) || !harness.model.install.restored {
		t.Fatalf("install = %+v, want aborted and restored", harness.model.install)
	}
	if got := harness.manifestOnDisk(t); got != setupManifest {
		t.Fatalf("plugins.go = %q, want the original back", got)
	}
}

func TestStaleProbeResultIsDropped(t *testing.T) {
	harness := newHarness(t, nil, screenDiscord, nil)
	harness.fill(t, envDiscordToken, "good-token")
	pending := harness.model.runProbe(probeDiscord)
	harness.typeText("x")
	harness.model.Update(pending())
	if got, ok := harness.model.probes[probeDiscord]; ok {
		t.Fatalf("probe = %+v, want the result for the old token dropped", got)
	}
	harness.drive(harness.model.runProbe(probeDiscord))
	if got := harness.model.probes[probeDiscord]; got.state != probeFailed {
		t.Fatalf("probe of the edited token = %+v, want refused", got)
	}
}

func TestWriteSecretsTightensAnOpenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("A=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeSecretsTo(path, map[string]string{envDiscordToken: "secret"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != modeSecret {
		t.Fatalf("mode = %v, want %v", info.Mode().Perm(), os.FileMode(modeSecret))
	}
}

func TestExpandHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cases := map[string]string{
		"~":           home,
		"~/vault":     filepath.Join(home, "vault"),
		"~other/x":    "~other/x",
		"/abs/vault":  "/abs/vault",
		"rel/~/vault": "rel/~/vault",
	}
	for in, want := range cases {
		if got := expandHome(in); got != want {
			t.Errorf("expandHome(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestVaultPlaceholderPathIsAccepted(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.Mkdir(filepath.Join(home, ".herrscher"), modeDirPrivate); err != nil {
		t.Fatal(err)
	}
	harness := newHarness(t, nil, screenObsidian, nil)
	harness.fill(t, envVault, vaultPlaceholder)
	harness.drive(harness.model.runProbe(probeVault))
	if got := harness.model.probes[probeVault]; got.key != clitext.SetupVaultCreate {
		t.Fatalf("probe = %+v, want the placeholder path accepted", got)
	}
	if got := harness.model.draft.written()[envVault]; got != filepath.Join(home, ".herrscher", "memory") {
		t.Fatalf("written vault = %q, want the home expanded", got)
	}
}

func TestSecretMaskHidesTheLength(t *testing.T) {
	harness := newHarness(t, nil, screenReview, nil)
	short, long := harness.model.shownValue(envDiscordToken, "ab"), harness.model.shownValue(envDiscordToken, strings.Repeat("z", 60))
	if short != long {
		t.Fatalf("masks differ: %q vs %q", short, long)
	}
}

func TestAbandonBeforeTheStepRunsSkipsIt(t *testing.T) {
	harness := newHarness(t, []byte(setupManifest), screenReview, nil)
	pending := harness.startInstallUntilGoGet(t)
	harness.model.abandon()
	if !harness.model.install.restored {
		t.Fatalf("install = %+v, want restored", harness.model.install)
	}
	if done, ok := pending().(stepDoneMsg); !ok || !errors.Is(done.err, errInstallAborted) {
		t.Fatal("a step dispatched after abandon still ran")
	}
	if slices.ContainsFunc(harness.shell.commands(), func(line string) bool { return strings.HasPrefix(line, "go get") }) {
		t.Fatalf("commands = %v, want go get never run", harness.shell.commands())
	}
}

func TestCtrlCShowsTheInstallIsStopping(t *testing.T) {
	harness := newHarness(t, []byte(setupManifest), screenReview, nil)
	harness.startInstallUntilGoGet(t)
	harness.model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if view := harness.model.View(); !strings.Contains(view, harness.text(clitext.SetupInstallStopping)) {
		t.Fatalf("view after ctrl+c = %q, want the stopping notice", view)
	}
}

func TestExecRunnerKeepsASuccessWhoseChildHoldsTheOutput(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	out, err := execRunner(t.Context(), setupCommand{name: "sh", args: []string{"-c", "sleep 7 & echo ok"}})
	if err != nil || string(out) != "ok\n" {
		t.Fatalf("out %q, err %v, want ok and no error", out, err)
	}
}

func TestExecRunnerReportsAFailure(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	if _, err := execRunner(t.Context(), setupCommand{name: "sh", args: []string{"-c", "exit 3"}}); err == nil {
		t.Fatal("a failing command reported success")
	}
}

func TestStepGateLeavesOnce(t *testing.T) {
	gate := newStepGate()
	if !gate.enter() {
		t.Fatal("a fresh gate refused entry")
	}
	gate.leave()
	gate.leave()
	if busy := gate.close(); busy == nil {
		t.Fatal("an entered gate gave nothing to wait on")
	}
	if gate.enter() {
		t.Fatal("a closed gate let a step in")
	}
}

func TestAbandonGivesUpOnAStuckStepAndLeavesTheFiles(t *testing.T) {
	harness := newHarness(t, []byte(setupManifest), screenReview, nil)
	harness.model.deps.abandonWait = 20 * time.Millisecond
	pending := harness.startInstallUntilGoGet(t)
	entered, release := make(chan struct{}), make(chan struct{})
	harness.shell.effect = func(setupCommand) {
		close(entered)
		<-release
	}
	finished := make(chan struct{})
	go func() {
		pending()
		close(finished)
	}()
	<-entered
	harness.model.abandon()
	if !errors.Is(harness.model.install.cleanupErr, errStepStillRuns) || harness.model.install.restored {
		t.Fatalf("install = %+v, want gave up without restoring", harness.model.install)
	}
	if got := harness.manifestOnDisk(t); got == setupManifest {
		t.Fatal("plugins.go was put back while the step still ran")
	}
	close(release)
	<-finished
}

func grokHarness(t *testing.T) *setupHarness {
	t.Helper()
	modules := defaultModules()
	modules[moduleGrok] = true
	harness := newHarness(t, nil, screenGrok, nil)
	harness.model.modules = modules
	return harness
}

func TestGrokSignInRunsTheLoginThenRechecks(t *testing.T) {
	harness := grokHarness(t)
	harness.shell.replies[grokCLI+" models"] = fakeReply{out: "You are not authenticated.\n"}
	harness.drive(harness.model.statusCheck(probeGrok))
	if got := harness.model.probes[probeGrok]; got.key != clitext.SetupLoggedOut {
		t.Fatalf("status = %+v, want logged out", got)
	}
	harness.shell.replies[grokCLI+" models"] = fakeReply{out: "You are logged in with grok.com.\n"}
	harness.choose(t, harness.text(clitext.SetupLogin))
	if got := harness.model.probes[probeGrok]; got.state != probePassed || got.params[clitext.ParamName] != "grok.com" {
		t.Fatalf("after login, probe = %+v, want signed in with grok.com", got)
	}
}

func TestGrokAPIKeyIsCheckedAndWritten(t *testing.T) {
	harness := grokHarness(t)
	harness.choose(t, harness.text(clitext.SetupAPIKey))
	harness.fill(t, envXAIKey, "xai-bad")
	harness.choose(t, harness.text(clitext.SetupCheckKey))
	if got := harness.model.probes[probeGrok]; got.key != clitext.SetupKeyRefused {
		t.Fatalf("probe = %+v, want refused", got)
	}
	harness.focus(t, func(row setupRow) bool { return row.field == envXAIKey })
	harness.press(tea.KeyMsg{Type: tea.KeyCtrlU})
	harness.typeText("xai-good")
	harness.choose(t, harness.text(clitext.SetupCheckKey))
	if got := harness.model.probes[probeGrok]; got.key != clitext.SetupGrokKeyShadowed {
		t.Fatalf("probe = %+v, want the key shadowed by the grok.com login", got)
	}
	harness.shell.replies[grokCLI+" models"] = fakeReply{out: "You are not authenticated.\n"}
	harness.choose(t, harness.text(clitext.SetupCheckKey))
	if got := harness.model.probes[probeGrok]; got.key != clitext.SetupKeyAccepted {
		t.Fatalf("probe = %+v, want accepted", got)
	}
	if got := harness.model.draft.written()[envXAIKey]; got != "xai-good" {
		t.Fatalf("written key = %q", got)
	}
	harness.choose(t, harness.text(clitext.SetupGrokAccount))
	if got, ok := harness.model.draft.written()[envXAIKey]; ok {
		t.Fatalf("back on the account the key is still written: %q", got)
	}
}

func TestGrokStatusWithoutTheCLISaysItIsMissing(t *testing.T) {
	shell := newShell()
	shell.replies[grokCLI+" models"] = fakeReply{err: exec.ErrNotFound}
	got := setupProber{run: shell.run}.grok(context.Background(), false)
	if got.key != clitext.SetupCLIMissing || got.params[clitext.ParamName] != grokCLI {
		t.Fatalf("probe = %+v, want the grok CLI missing", got)
	}
}

func TestAnXAIKeyInTheSettingsPicksTheGrokModule(t *testing.T) {
	harness := newHarness(t, nil, screenWelcome, map[string]string{envXAIKey: "xai-good"})
	if !harness.model.modules[moduleGrok] || !harness.model.grokKey {
		t.Fatalf("modules = %v, grokKey = %v; want the grok module on its key", harness.model.modules, harness.model.grokKey)
	}
}

func TestGrokLoginStatusRunsWithoutAnInheritedKey(t *testing.T) {
	t.Setenv("XAI_API_KEY", "xai-shell")
	t.Setenv("GROK_CODE_XAI_API_KEY", "")
	status := grokLoginStatus()
	status.name, status.args = "sh", []string{"-c", "env"}
	out, err := execRunner(t.Context(), status)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "XAI_API_KEY") {
		t.Fatalf("the status check inherits a key, so grok would report it instead of the login:\n%s", out)
	}
	shell := newShell()
	setupProber{run: shell.run}.grok(t.Context(), false)
	if calls := shell.calls; len(calls) != 1 || len(calls[0].unset) == 0 {
		t.Fatalf("calls = %+v, want the grok probe to drop the key variables", calls)
	}
}

func TestAntigravityStatusReadsTheModelListing(t *testing.T) {
	cases := []struct {
		reply  fakeReply
		key    clitext.Key
		name   string
		listed int
	}{
		{reply: fakeReply{out: "Fetching available models...\ngemini-4-hydrogen-high\tGemini 4 Hydrogen (High)\ngemini-3.8-flash-low\tGemini 3.8 Flash (Low)\n"}, key: clitext.SetupAntigravityOK, listed: 2},
		{reply: fakeReply{out: "Fetching available models...\nError: Please sign in to view available models.\n", err: errProbeStatus}, key: clitext.SetupLoggedOut},
		{reply: fakeReply{out: "Fetching available models...\n"}, key: clitext.SetupLoggedOut},
		{reply: fakeReply{err: exec.ErrNotFound}, key: clitext.SetupCLIMissing, name: antigravityCLI},
	}
	for _, tc := range cases {
		shell := newShell()
		shell.replies["agy models"] = tc.reply
		got := setupProber{run: shell.run}.antigravity(context.Background(), false)
		if got.key != tc.key || (tc.name != "" && got.params[clitext.ParamName] != tc.name) || (tc.listed != 0 && got.params[clitext.ParamCount] != tc.listed) {
			t.Errorf("reply %q: got %+v", tc.reply.out, got)
		}
	}
}

func TestAntigravitySignInOpensAgyThenRechecks(t *testing.T) {
	modules := defaultModules()
	modules[moduleAntigravity] = true
	harness := newHarness(t, nil, screenAntigravity, nil)
	harness.model.modules = modules
	harness.shell.replies["agy models"] = fakeReply{out: "Error: Please sign in to view available models.\n", err: errProbeStatus}
	harness.drive(harness.model.statusCheck(probeAntigravity))
	if got := harness.model.probes[probeAntigravity]; got.key != clitext.SetupLoggedOut {
		t.Fatalf("status = %+v, want logged out", got)
	}
	harness.choose(t, harness.text(clitext.SetupAntigravityLogin))
	if got := harness.model.probes[probeAntigravity]; got.key != clitext.SetupLoginUnfinished {
		t.Fatalf("after a sign in that did not finish, probe = %+v, want unfinished", got)
	}
	harness.shell.replies["agy models"] = fakeReply{out: "gemini-4-hydrogen-high\tGemini 4 Hydrogen (High)\n"}
	harness.choose(t, harness.text(clitext.SetupAntigravityLogin))
	if got := harness.model.probes[probeAntigravity]; got.state != probePassed || got.key != clitext.SetupAntigravityOK {
		t.Fatalf("after sign in, probe = %+v, want signed in", got)
	}
}

func TestAntigravitySignInRunsAgyAlone(t *testing.T) {
	if args := loginCmd(t.Context(), probeAntigravity).Args; !slices.Equal(args, []string{antigravityCLI}) {
		t.Fatalf("sign in runs %q, want agy with no argument", args)
	}
}

func TestMissingSignInCLIIsNamedAfterTheCommandItRuns(t *testing.T) {
	for _, kind := range []probeKind{probeClaude, probeCodex, probeGrok, probeAntigravity} {
		if name, want := loginCLI(kind), loginCmd(t.Context(), kind).Args[0]; name != want {
			t.Fatalf("kind %d: a missing CLI is reported as %q, want %q", kind, name, want)
		}
	}
}

func TestAntigravityModuleComesFromTheBackendCatalog(t *testing.T) {
	if spec := moduleAntigravity.spec(); spec.category != catBackend || moduleAntigravity.path() != "github.com/Herrscherd/herrscher-antigravity-backend" {
		t.Fatalf("spec = %+v, path = %q", spec, moduleAntigravity.path())
	}
}

func TestRouteOffersTheModelsOfEachChosenBackend(t *testing.T) {
	for _, tc := range []struct {
		mod  setupModule
		want string
	}{{moduleGrok, "grok-build"}, {moduleAntigravity, "gemini-agy"}} {
		mod, want := tc.mod, tc.want
		harness := newHarness(t, nil, screenRoute, nil)
		harness.model.modules = make(moduleSet)
		harness.model.modules[mod] = true
		var offered []string
		for _, entry := range harness.model.offeredModels() {
			offered = append(offered, entry.ID)
		}
		if !slices.Equal(offered, []string{want}) {
			t.Fatalf("with only %s chosen, offered = %v, want %q", mod.spec().kind, offered, want)
		}
	}
}

func TestAntigravityTrialRunsOneReadOnlyHeadlessTurn(t *testing.T) {
	chosen := make(moduleSet)
	chosen[moduleAntigravity] = true
	trials := backendTrials(chosen, func(string) string { return "" })
	want := []string{"--print=" + trialPrompt, "--mode", "plan"}
	if len(trials) != 1 || trials[0].command.name != antigravityCLI || !slices.Equal(trials[0].command.args, want) {
		t.Fatalf("trials = %+v, want agy %q", trials, want)
	}
}
