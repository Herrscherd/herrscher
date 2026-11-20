package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher-contracts/i18n/i18ntest"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/historywire"
)

func hostLocaleSource(t *testing.T) fs.FS {
	t.Helper()
	sub, err := fs.Sub(hostLocales, "locales")
	if err != nil {
		t.Fatal(err)
	}
	return sub
}

func TestHostCatalogIsParityComplete(t *testing.T) {
	i18ntest.Check(t, hostLocaleSource(t))
}

func TestEnglishCatalogDescriptionsAreEnglish(t *testing.T) {
	raw, err := fs.ReadFile(hostLocaleSource(t), "en.json")
	if err != nil {
		t.Fatal(err)
	}
	var entries []struct {
		ID          string `json:"id"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatal(err)
	}
	frenchWords := map[string]bool{"le": true, "la": true, "les": true, "un": true, "une": true, "des": true, "du": true, "de": true, "est": true, "pas": true, "sans": true, "pour": true, "dans": true, "qui": true}
	for _, entry := range entries {
		if strings.ContainsAny(entry.Description, "àâçéèêëîïôûùüÿœ") {
			t.Errorf("%s: description %q is not in English", entry.ID, entry.Description)
		}
		for _, word := range strings.Fields(strings.ToLower(entry.Description)) {
			if frenchWords[strings.Trim(word, ".,;:")] {
				t.Errorf("%s: description %q is not in English", entry.ID, entry.Description)
			}
		}
	}
}

func TestHostCatalogDeclaresEveryKeyWithItsParameters(t *testing.T) {
	declared := map[string][]string{
		"coordination.fanout_partial":  {"Detail", "Launched"},
		"coordination.route_refused":   {"Detail"},
		"coordination.seal_refused":    {"Detail"},
		"coordination.merge_refused":   {"Detail"},
		"coordination.handoff_refused": {"Detail"},
		"compaction.no_effect":         {"Percent"},
		"coordination.routed":          {"Agent", "Session"},
		"coordination.sealed":          {"Node"},
		"coordination.merged":          {"Lead"},
		"coordination.launched":        {"Count", "Workers"},

		"outbound.refused":   {"Names"},
		"outbound.unsent":    {"Names"},
		"outbound.failed":    {"Names"},
		"outbound.elsewhere": {"Names"},
		"outbound.unnamed":   nil,

		"account.home":                      {"Detail"},
		"account.profile_open":              {"Detail"},
		"account.codex_home":                {"Detail"},
		"account.codex_home_create":         {"Detail"},
		"account.codex_config_open":         {"Detail"},
		"account.codex_config_write":        {"Detail"},
		"session.read":                      {"Detail"},
		"gateway.model":                     {"Detail", "Model"},
		"gateway.base_url":                  {"Detail"},
		"gateway.key_proxy":                 {"Detail"},
		"checkpoint.rewind_resume":          {"Detail", "Name"},
		"fork.failed":                       {"Detail", "Name"},
		"fork.checkpoint_not_applied":       {"Detail", "Name", "Position"},
		"fork.not_taken":                    {"Detail", "Name", "Position"},
		"fork.not_resumed":                  {"Detail", "Name"},
		"session.refused":                   {"Detail", "Name"},
		"checkpoint.index":                  {"Detail", "Got"},
		"command.socket_decode":             {"Detail"},
		"compaction.at":                     {"Detail"},
		"compaction.stack":                  {"Detail"},
		"compaction.stack_named":            {"Detail", "Name"},
		"compaction.stage_trigger":          {"Name", "Percent"},
		"coordination.wrap":                 {"Detail"},
		"coordination.refused":              {"Detail"},
		"coordination.create":               {"Detail", "Name"},
		"handoff.wrap":                      {"Detail"},
		"delegate.wrap":                     {"Detail"},
		"report.wrap":                       {"Detail"},
		"report.refused":                    {"Detail"},
		"merge.wrap":                        {"Detail"},
		"merge.refused":                     {"Detail"},
		"seal.wrap":                         {"Detail"},
		"seal.refused":                      {"Detail"},
		"fanout.wrap":                       {"Detail"},
		"route.wrap":                        {"Detail"},
		"daemon.events_socket":              {"Detail", "Path"},
		"daemon.socket_taken":               {"Detail", "Path"},
		"daemon.instance_locked":            {"Path"},
		"daemon.attachments":                {"Detail"},
		"hosts.clone":                       {"Detail", "Host", "Project"},
		"lock.dir":                          {"Detail"},
		"lock.state":                        {"Detail"},
		"memory.wrap":                       {"Detail"},
		"models.cache_read":                 {"Detail"},
		"models.cache_decode":               {"Detail"},
		"models.cache_encode":               {"Detail"},
		"models.cache_dir":                  {"Detail"},
		"models.cache_dir_open":             {"Detail"},
		"models.cache_temp":                 {"Detail"},
		"models.cache_write":                {"Detail"},
		"models.cache_replace":              {"Detail"},
		"models.cache_lock_open":            {"Detail"},
		"models.cache_lock":                 {"Detail"},
		"provision.smoke":                   {"Detail", "Host"},
		"remote.stage_agent":                {"Detail", "Name"},
		"remote.pack_agent":                 {"Detail", "Name"},
		"remote.no_remote":                  {"Category", "Detail"},
		"remote.unavailable":                {"Attempts", "Category", "Detail", "Elapsed"},
		"remote.extract_archive":            {"Detail", "Path"},
		"remote.extract_member":             {"Detail", "Member"},
		"remote.unsafe_member":              {"Member", "Root"},
		"remote.gateway_env_unsupported":    {"Model"},
		"remote.session_unsupported":        {"Session"},
		"remote.account_unsupported":        {"Account"},
		"approval.prompt":                   {"Request"},
		"approval.hint":                     {"Command", "Help"},
		"seed.consolidate":                  {"Detail", "Elapsed", "Name"},
		"seed.late_transcript":              {"Name"},
		"seed.late_event":                   {"Name"},
		"seed.late_usage":                   {"Name"},
		"seed.late_resume":                  {"Name"},
		"serve.instance_id_mismatch":        {"Have", "Want"},
		"serve.instance_persist":            {"Detail"},
		"serve.load_state":                  {"Detail"},
		"serve.resolve_instance":            {"Detail"},
		"serve.compaction":                  {"Detail"},
		"plugin.kind_reserved":              {"Name"},
		"serve.registry":                    {"Detail"},
		"serve.scheduler_clock":             {"Detail"},
		"serve.scheduler_tick":              {"Detail"},
		"serve.config":                      {"Detail"},
		"serve.key_proxy":                   {"Detail"},
		"serve.outbox":                      {"Detail"},
		"session.percent":                   {"Detail", "Raw"},
		"session.go_live":                   {"Detail", "Name"},
		"session.launch_failed":             {"Detail", "Name"},
		"session.prepare_failed":            {"Detail", "Name"},
		"lock.unsafe":                       {"Detail", "Path"},
		"runtime.unsafe_dir":                {"Detail", "Path"},
		"runtime.unsafe_dir.symlink":        {"Path"},
		"runtime.unsafe_dir.foreign_owner":  {"Path"},
		"runtime.unsafe_dir.open_access":    {"Path"},
		"handoff.brief_from":                {"Detail", "Source"},
		"handoff.account_inherited":         {"Account", "Command", "Detail", "Source"},
		"handoff.not_delivered":             {"Detail", "Name"},
		"turn.budget_refused":               {"Reason"},
		"turn.token_ceiling":                {"Margin", "Spent"},
		"coordination.report_refused":       nil,
		"coordination.report_delivered":     {"Parent"},
		"coordination.delegate_refused":     nil,
		"warn.session_start":                {"Detail", "Name"},
		"warn.session_start_recovered":      {"Name"},
		"warn.session_checkpoint":           {"Detail", "Name"},
		"warn.session_resume_token":         {"Detail", "Name"},
		"warn.session_turn_mark_set":        {"Detail", "Name"},
		"warn.session_turn_mark_clear":      {"Detail", "Name"},
		"warn.session_project_pin":          {"Detail", "Name"},
		"warn.session_transcript":           {"Detail", "Name"},
		"warn.session_accept_failed":        {"Detail", "Name"},
		"warn.session_teardown_overrun":     {"Grace", "Name"},
		"warn.hub_teardown_overrun":         {"Grace"},
		"warn.command_handler_overrun":      {"Grace"},
		"warn.persist_overrun":              {"Grace"},
		"warn.warnings_dropped":             {"Count"},
		"warn.write_still_running":          nil,
		"warn.session_seed_transcript":      {"Detail", "Name"},
		"warn.session_usage":                {"Detail", "Name"},
		"warn.session_compaction_notice":    {"Detail", "Name"},
		"warn.role_undefined":               {"Name", "Role"},
		"warn.ssh_multiplexing_disabled":    {"Detail", "Name"},
		"warn.health_server":                {"Detail"},
		"warn.approvals_unreadable":         {"Detail", "Path"},
		"warn.approvals_file":               {"Detail", "Path"},
		"warn.approvals_state_rule":         {"Detail", "Rule"},
		"warn.context_limit_env":            {"Detail", "Name"},
		"warn.context_limit_unknown_vendor": {"Name"},
		"warn.history_port_missing":         {"Name"},
		"warn.compactor_port_missing":       {"Name"},
		"hook.no_session":                   {"Variable"},
		"hook.no_token":                     {"Variable"},
		"approve.ask_unauthenticated":       {"Name"},
		"hook.payload_unreadable":           {"Detail"},
		"hook.payload_too_large":            nil,
		"hook.names_no_tool":                nil,
		"hook.no_daemon":                    {"Socket"},
		"hook.cancelled":                    nil,
		"hook.daemon_failed":                {"Detail"},
		"hook.answer_unreadable":            {"Answer"},
		"hook.other_event":                  {"Event", "Expected"},
		"hook.approval_socket_failed":       {"Detail"},
		"hook.approval_timeout":             {"Wait"},
		"approval.request_unreadable":       {"Detail"},
		"approval.no_desk":                  nil,
		"hook.decision_unwritable":          {"Decision", "Detail"},
	}
	i18ntest.CheckDeclared(t, declared, hostLocaleSource(t))
}

func renderedHostText(lang i18n.Language, key string, params map[string]any) string {
	if strings.HasPrefix(key, "cli.") {
		text, err := clitext.Text(lang, clitext.Key(key), clitext.Params(params))
		if err != nil {
			return err.Error()
		}
		return text
	}
	return newHostMessages().text(lang, key, params)
}

func TestHostTextRendersEachLanguage(t *testing.T) {
	tests := []struct {
		name   string
		lang   i18n.Language
		key    string
		params map[string]any
		want   string
	}{
		{
			name:   "route refusal in french",
			lang:   i18n.French,
			key:    "coordination.route_refused",
			params: map[string]any{"Detail": "boom"},
			want:   "routage refusé : boom",
		},
		{
			name:   "route refusal in english",
			lang:   i18n.English,
			key:    "coordination.route_refused",
			params: map[string]any{"Detail": "boom"},
			want:   "route refused: boom",
		},
		{
			name:   "no effect in french",
			lang:   i18n.French,
			key:    "compaction.no_effect",
			params: map[string]any{"Percent": 92},
			want:   "compaction automatique sans effet : le contexte reste au-dessus de 92 %, pas de nouvelle tentative avant qu'il repasse sous le seuil",
		},
		{
			name:   "no effect in english",
			lang:   i18n.English,
			key:    "compaction.no_effect",
			params: map[string]any{"Percent": 92},
			want:   "automatic compaction had no effect: the context stays above 92 %, no new attempt until it drops below the threshold",
		},
		{
			name: "still stopping in french",
			lang: i18n.French,
			key:  "cli.host.fault.still_stopping",
			want: "l'ancien pont de cette session est encore en arrêt",
		},
		{
			name: "still stopping in english",
			lang: i18n.English,
			key:  "cli.host.fault.still_stopping",
			want: "the previous bridge of this session is still stopping",
		},
		{
			name:   "unset language falls back to english",
			lang:   i18n.Language(0),
			key:    "compaction.at",
			params: map[string]any{"Detail": "disk full"},
			want:   "compactAt: disk full",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := renderedHostText(tt.lang, tt.key, tt.params); got != tt.want {
				t.Fatalf("text(%v, %q) = %q, want %q", tt.lang, tt.key, got, tt.want)
			}
		})
	}
}

func TestKnownCompactFailureNamesEachCause(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		reason string
	}{
		{name: "bridge closed", err: fmt.Errorf("stage: %w", errBridgeGone), reason: eventwire.CompactReasonBridgeGone},
		{name: "no compactor factory", err: errNoCompactorFactory, reason: eventwire.CompactReasonNoCompactorFactory},
		{name: "factory returned no stage", err: errNilCompactor, reason: eventwire.CompactReasonNilCompactor},
		{name: "no compactor plugin", err: errNoCompactorPlugin, reason: eventwire.CompactReasonNoCompactorPlugin},
		{name: "history port not implemented", err: fmt.Errorf("stage: %w", historywire.ErrNoHistoryEditor), reason: eventwire.CompactReasonNoHistoryPort},
		{name: "history call timed out", err: fmt.Errorf("%w: %w", errHistoryTimeout, context.DeadlineExceeded), reason: eventwire.CompactReasonHistoryTimeout},
		{name: "history too large", err: fmt.Errorf("stage: %w", historywire.ErrHistoryTooLarge), reason: eventwire.CompactReasonHistoryTooLarge},
		{name: "too many edits", err: fmt.Errorf("stage: %w", errTooManyEdits), reason: eventwire.CompactReasonTooManyEdits},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason, known := knownCompactFailure(tt.err)
			if !known || reason != tt.reason {
				t.Fatalf("knownCompactFailure = %q, %t, want %q", reason, known, tt.reason)
			}
		})
	}
	if _, known := knownCompactFailure(errors.New("unclassified")); known {
		t.Fatal("an unclassified failure must not name a cause")
	}
}

func TestApprovalFailureCarriesUnexpectedNotice(t *testing.T) {
	cause := errors.New("store locked")
	noticed, ok := errors.AsType[contracts.NoticeError](approvalFailure(cause))
	if !ok {
		t.Fatal("approvalFailure must be a NoticeError, or the CLI keeps its text in English")
	}
	if code := noticed.Notice().Code; code != contracts.CodeUnexpected {
		t.Fatalf("code = %v, want %v", code, contracts.CodeUnexpected)
	}
	if !errors.Is(approvalFailure(cause), cause) {
		t.Fatal("approvalFailure must keep the cause reachable with errors.Is")
	}
}

func TestApprovalFailureKeepsAnExistingNotice(t *testing.T) {
	typed := compactUnavailableError{cause: errCompactUnsupported, reason: "no compactor"}
	if got := contracts.NoticeOf(approvalFailure(typed)); got.Code != contracts.CodeCompactUnavailable {
		t.Fatalf("approvalFailure replaced the typed notice: code %v", got.Code)
	}
}

type recallFailingMemory struct {
	contracts.Memory

	err error
}

func (m recallFailingMemory) Recall(context.Context, string, int) (contracts.Subgraph, error) {
	return contracts.Subgraph{}, m.err
}

func TestSkillApprovalFailureTravelsAsAnUnexpectedNotice(t *testing.T) {
	input := contracts.Input{Args: map[string]string{"key": "skill/x"}}
	_, err := setSkillApproval(context.Background(), clitext.NewMessages(), i18n.English, recallFailingMemory{err: errors.New("store locked")}, input)
	noticed, ok := errors.AsType[contracts.NoticeError](err)
	if !ok {
		t.Fatal("skill approval failure must be a NoticeError")
	}
	if code := noticed.Notice().Code; code != contracts.CodeUnexpected {
		t.Fatalf("code = %v, want %v", code, contracts.CodeUnexpected)
	}
}

func TestSkillApprovalFailureRendersInFrenchThroughTheCLI(t *testing.T) {
	input := contracts.Input{Args: map[string]string{"key": "skill/x"}}
	_, err := setSkillApproval(context.Background(), clitext.NewMessages(), i18n.English, recallFailingMemory{err: errors.New("store locked")}, input)
	got := cli.Localize(clitext.NewMessages(), err, i18n.French).Error()
	if !strings.HasPrefix(got, "erreur inattendue : ") || !strings.Contains(got, "store locked") {
		t.Fatalf("cli.Localize(%v, French) = %q, want the French unexpected notice carrying the cause", err, got)
	}
}

func TestHostMessagesBuildTheCatalogOnce(t *testing.T) {
	messages := newHostMessages()
	first, err := messages.catalog()
	if err != nil {
		t.Fatal(err)
	}
	second, err := messages.catalog()
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("the host catalog is built again on each call, want it memoized")
	}
}

func TestHostTextFallsBackToEnglishWhenFrenchDoesNotRender(t *testing.T) {
	source := fstest.MapFS{
		"en.json": {Data: []byte(`[{"id":"probe","description":"probe","other":"english {{.Detail}}"},{"id":"broken","description":"broken","other":"english {{.Detail.Nope}}"}]`)},
		"fr.json": {Data: []byte(`[{"id":"probe","description":"sonde","other":"français {{.Detail.Nope}}"},{"id":"broken","description":"cassé","other":"français {{.Detail.Nope}}"}]`)},
	}
	messages := &hostMessages{catalog: func() (*i18n.Catalog, error) {
		return i18n.NewCatalog(slog.Default(), source)
	}}
	params := map[string]any{detailParam: "disk full"}
	if got := messages.text(i18n.French, "probe", params); got != "english disk full" {
		t.Fatalf("text in french = %q, want the english text", got)
	}
	if got := messages.text(i18n.French, "broken", params); got == "broken" || got == "" {
		t.Fatalf("text of a key that renders in no language = %q, want the render error", got)
	}
}

func countingOwner() (*hostMessages, *int) {
	built := sync.OnceValues(buildHostCatalog)
	catalogueLookups := 0
	owner := &hostMessages{catalog: func() (*i18n.Catalog, error) {
		catalogueLookups++
		return built()
	}}
	return owner, &catalogueLookups
}

func wrapChain(owner *hostMessages, depth int) error {
	err := errors.New("disque plein")
	return wrapLevels(owner, err, depth)
}

func wrapLevels(owner *hostMessages, err error, depth int) error {
	for range depth {
		err = wrapHost(owner, err, "serve.load_state", nil)
	}
	return err
}

func TestOneOwnerKeepsOneCatalogueAcrossEveryRenderPath(t *testing.T) {
	owner, catalogueLookups := countingOwner()
	expectLookups := func(path string, want int, render func()) {
		t.Helper()
		before := *catalogueLookups
		render()
		if got := *catalogueLookups - before; got != want {
			t.Fatalf("%s looked up the owner's catalogue %d times, want exactly %d: a render that goes through another owner changes this count", path, got, want)
		}
	}

	driver := newSessionDriver(owner, clitext.NewMessages(), "alpha", nil, nil, nil, io.Discard)
	driver.operator = fixedLanguage(i18n.French)
	expectLookups("the session driver", 1, func() {
		driver.hostText("warn.session_start", map[string]any{nameParam: "alpha"})
	})
	expectLookups("warnTo", 1, func() {
		warnTo(owner, io.Discard, i18n.French, "warn.session_start", map[string]any{nameParam: "alpha", detailParam: errors.New("boom")})
	})
	top := wrapHost(owner, wrapChain(owner, 1), "warn.session_start", map[string]any{nameParam: "digest"})
	var englishTop, frenchTop error
	expectLookups("localizeHostError in English over two levels", 2, func() {
		englishTop = localizeHostError(owner, clitext.NewMessages(), i18n.English, top)
	})
	expectLookups("localizeHostError in French over two levels", 8, func() {
		frenchTop = localizeHostError(owner, clitext.NewMessages(), i18n.French, top)
	})
	const wantEnglish = `herrscher serve: session "digest": load state: disque plein`
	const wantFrench = `herrscher serve : session "digest" : chargement de l'état impossible : disque plein`
	if got := englishTop.Error(); got != wantEnglish {
		t.Fatalf("localizeHostError in English rendered %q, want %q", got, wantEnglish)
	}
	if got := frenchTop.Error(); got != wantFrench {
		t.Fatalf("localizeHostError in French rendered %q, want %q", got, wantFrench)
	}
	expectLookups("Error() over two levels", 2, func() {
		_ = top.Error()
	})
	expectLookups("errorTextIn in French over two levels", 8, func() {
		errorTextIn(owner, i18n.French, top)
	})
}

func TestCatalogueLookupsGrowAsDepthSquaredInFrench(t *testing.T) {
	for depth := 1; depth <= 4; depth++ {
		t.Run(fmt.Sprintf("depth %d", depth), func(t *testing.T) {
			owner, catalogueLookups := countingOwner()
			chain := wrapChain(owner, depth)
			*catalogueLookups = 0
			_ = chain.Error()
			if got := *catalogueLookups; got != depth {
				t.Fatalf("English over %d levels looked up the catalogue %d times, want %d (one per level)", depth, got, depth)
			}
			*catalogueLookups = 0
			errorTextIn(owner, i18n.French, chain)
			if want := depth*depth + 2*depth; *catalogueLookups != want {
				t.Fatalf("French over %d levels looked up the catalogue %d times, want %d (n² + 2n)", depth, *catalogueLookups, want)
			}
		})
	}
}

func TestErrorsIsSurvivesATypedNilRemoteResolveError(t *testing.T) {
	var nilRemote *RemoteResolveError
	var err error = nilRemote
	if errors.Is(err, errors.New("cible")) {
		t.Fatal("a typed nil RemoteResolveError matched a foreign target")
	}
	if errors.Is(fmt.Errorf("contexte: %w", err), errors.New("cible")) {
		t.Fatal("a typed nil RemoteResolveError inside a wrap matched a foreign target")
	}
}

func typedNilLocalizables() []error {
	return []error{
		(*hostWrapError)(nil),
		(*pluginKindReservedError)(nil),
		(*RemoteResolveError)(nil),
		(*gatewayEnvUnsupportedError)(nil),
		(*unsafeTarMemberError)(nil),
		(*instanceIDMismatchError)(nil),
		(*remoteSessionError)(nil),
		(*remoteAccountError)(nil),
	}
}

func TestNilLocalizableInsideAJoinKeepsTheTextOfItsBranches(t *testing.T) {
	owner := newHostMessages()
	cause := wrapHost(owner, errors.New("disque plein"), "serve.load_state", nil)
	var nilWrap *hostWrapError
	err := errors.Join(fmt.Errorf("a: %w", cause), nilWrap)
	if got, want := localizeHostError(owner, clitext.NewMessages(), i18n.English, err).Error(), "a: load state: disque plein\n<nil>"; got != want {
		t.Fatalf("a typed nil under errors.Join rendered %q in English, want %q", got, want)
	}
	if got, want := localizeHostError(owner, clitext.NewMessages(), i18n.French, err).Error(), "a: chargement de l'état impossible : disque plein\n<nil>"; got != want {
		t.Fatalf("a typed nil under errors.Join rendered %q in French, want %q", got, want)
	}
}

func TestErrorsIsSurvivesEveryLocalizableTypedNil(t *testing.T) {
	owner := newHostMessages()
	foreign := errors.New("cible")
	params := map[string]any{nameParam: "digest"}
	plainWrap := wrapHost(owner, errors.New("<nil>"), "warn.session_start", params)
	for _, typedNil := range typedNilLocalizables() {
		wrapped := wrapHost(owner, typedNil, "warn.session_start", params)
		assertTypedNilMatchesNothing(t, typedNil, wrapped, foreign)
		for _, lang := range []i18n.Language{i18n.French, i18n.English} {
			assertTypedNilRendersAsNilText(t, owner, lang, typedNil, wrapped, plainWrap)
		}
	}
}

func assertTypedNilMatchesNothing(t *testing.T, typedNil, wrapped, foreign error) {
	t.Helper()
	if errors.Is(typedNil, foreign) {
		t.Fatalf("a typed nil %T matched a foreign target", typedNil)
	}
	if errors.Is(fmt.Errorf("contexte: %w", typedNil), foreign) {
		t.Fatalf("a typed nil %T inside a %%w wrap matched a foreign target", typedNil)
	}
	if errors.Is(wrapped, foreign) {
		t.Fatalf("a typed nil %T inside a host wrap matched a foreign target", typedNil)
	}
}

func assertTypedNilRendersAsNilText(t *testing.T, owner *hostMessages, lang i18n.Language, typedNil, wrapped, plainWrap error) {
	t.Helper()
	if got, want := localizeHostError(owner, clitext.NewMessages(), lang, typedNil).Error(), "<nil>"; got != want {
		t.Fatalf("a bare typed nil %T rendered %q in %v, want %q", typedNil, got, lang, want)
	}
	if got, want := localizeHostError(owner, clitext.NewMessages(), lang, fmt.Errorf("contexte: %w", typedNil)).Error(), "contexte: <nil>"; got != want {
		t.Fatalf("a typed nil %T under %%w rendered %q in %v, want %q", typedNil, got, lang, want)
	}
	if got, want := localizeHostError(owner, clitext.NewMessages(), lang, errors.Join(errors.New("a"), typedNil)).Error(), "a\n<nil>"; got != want {
		t.Fatalf("a typed nil %T under errors.Join rendered %q in %v, want %q", typedNil, got, lang, want)
	}
	if got, want := localizeHostError(owner, clitext.NewMessages(), lang, wrapped).Error(), localizeHostError(owner, clitext.NewMessages(), lang, plainWrap).Error(); got != want {
		t.Fatalf("a typed nil %T in a host wrap rendered %q in %v, want %q", typedNil, got, lang, want)
	}
}

func TestLocalizeHostErrorTranslatesASentinelBesideATypedNil(t *testing.T) {
	owner := newHostMessages()
	var nilWrap *hostWrapError
	err := errors.Join(fmt.Errorf("a: %w", errHubClosed), nilWrap)
	if got, want := localizeHostError(owner, clitext.NewMessages(), i18n.French, err).Error(), "a: le démon s'arrête et ne démarre plus de session\n<nil>"; got != want {
		t.Fatalf("a sentinel beside a typed nil rendered %q in French, want %q", got, want)
	}
	if got, want := localizeHostError(owner, clitext.NewMessages(), i18n.English, err).Error(), "a: the daemon is shutting down and starts no session\n<nil>"; got != want {
		t.Fatalf("a sentinel beside a typed nil rendered %q in English, want %q", got, want)
	}
}

func TestLocalizeHostErrorTranslatesALocalizableAfterATypedNil(t *testing.T) {
	owner := newHostMessages()
	var nilWrap *hostWrapError
	err := errors.Join(nilWrap, wrapHost(owner, errors.New("disque plein"), "serve.load_state", nil))
	if got, want := localizeHostError(owner, clitext.NewMessages(), i18n.French, err).Error(), "<nil>\nchargement de l'état impossible : disque plein"; got != want {
		t.Fatalf("a typed nil before a localizable rendered %q in French, want %q", got, want)
	}
	if got, want := localizeHostError(owner, clitext.NewMessages(), i18n.English, err).Error(), "<nil>\nload state: disque plein"; got != want {
		t.Fatalf("a typed nil before a localizable rendered %q in English, want %q", got, want)
	}
}

func TestLocalizeHostErrorTranslatesANestedJoinBehindATypedNil(t *testing.T) {
	owner := newHostMessages()
	var nilWrap *hostWrapError
	inner := errors.Join(nilWrap, wrapHost(owner, errors.New("disque plein"), "serve.load_state", nil))
	err := errors.Join(errors.New("a"), inner)
	if got, want := localizeHostError(owner, clitext.NewMessages(), i18n.French, err).Error(), "a\n<nil>\nchargement de l'état impossible : disque plein"; got != want {
		t.Fatalf("a localizable in a nested join rendered %q in French, want %q", got, want)
	}
	if got, want := localizeHostError(owner, clitext.NewMessages(), i18n.English, err).Error(), "a\n<nil>\nload state: disque plein"; got != want {
		t.Fatalf("a localizable in a nested join rendered %q in English, want %q", got, want)
	}
}

func TestLocalizeHostErrorFallsBackToEnglishWhenEveryLocalizableIsATypedNil(t *testing.T) {
	owner := newHostMessages()
	var nilWrap *hostWrapError
	var nilRemote *RemoteResolveError
	err := errors.Join(nilWrap, nilRemote)
	for _, lang := range []i18n.Language{i18n.French, i18n.English} {
		if got, want := localizeHostError(owner, clitext.NewMessages(), lang, err).Error(), "<nil>\n<nil>"; got != want {
			t.Fatalf("a join of typed nils rendered %q in %v, want the english text %q", got, lang, want)
		}
	}
}

func TestEveryLocalizableTypedNilRendersAsNilText(t *testing.T) {
	owner := newHostMessages()
	params := map[string]any{nameParam: "digest"}
	for _, typedNil := range typedNilLocalizables() {
		for _, lang := range []i18n.Language{i18n.French, i18n.English} {
			got := errorTextIn(owner, lang, wrapHost(owner, typedNil, "warn.session_start", params))
			want := errorTextIn(owner, lang, wrapHost(owner, errors.New("<nil>"), "warn.session_start", params))
			if got != want {
				t.Fatalf("a typed nil %T rendered %q in %v, want the text of a nil cause %q", typedNil, got, lang, want)
			}
		}
	}
}

func TestLocalizeHostErrorSurvivesATypedNilInTheChain(t *testing.T) {
	owner := newHostMessages()
	params := map[string]any{nameParam: "digest"}
	var nilWrap *hostWrapError
	nested := wrapHost(owner, nilWrap, "warn.session_start", params)
	plain := wrapHost(owner, errors.New("<nil>"), "warn.session_start", params)
	for _, lang := range []i18n.Language{i18n.French, i18n.English} {
		got := fmt.Sprint(localizeHostError(owner, clitext.NewMessages(), lang, nested))
		want := fmt.Sprint(localizeHostError(owner, clitext.NewMessages(), lang, plain))
		if got != want {
			t.Fatalf("a typed nil in the chain rendered %q in %v, want %q", got, lang, want)
		}
		if top := fmt.Sprint(localizeHostError(owner, clitext.NewMessages(), lang, nilWrap)); top != "<nil>" {
			t.Fatalf("a top-level typed nil rendered %q in %v, want <nil>", top, lang)
		}
	}
}

func TestWrapHostErrorRendersThroughItsOwnerCatalog(t *testing.T) {
	owner := &hostMessages{catalog: func() (*i18n.Catalog, error) {
		return nil, errors.New("catalogue indisponible")
	}}
	err := wrapHost(owner, errors.New("boom"), "serve.load_state", nil)
	if got := err.Error(); got != "catalogue indisponible" {
		t.Fatalf("Error() = %q, want the text of its own owner's catalog", got)
	}
}

func TestErrorTextKeepsTheWrapperPrefix(t *testing.T) {
	messages := newHostMessages()
	inner := &hostWrapError{messages: messages, key: "serve.load_state", cause: errors.New("boom")}
	err := fmt.Errorf("ctx: %w", inner)
	want := "ctx: " + inner.render(messages, i18n.French)
	if got := errorTextIn(messages, i18n.French, err); got != want {
		t.Fatalf("errorTextIn = %q, want %q", got, want)
	}
	repeated := fmt.Errorf("%s: %w", inner.render(messages, i18n.English), inner)
	wantRepeated := inner.render(messages, i18n.English) + ": " + inner.render(messages, i18n.French)
	if got := errorTextIn(messages, i18n.French, repeated); got != wantRepeated {
		t.Fatalf("errorTextIn with the english text repeated = %q, want %q", got, wantRepeated)
	}
}

func TestErrorTextLocalisesOnlyTheFirstOfTwoSiblingErrors(t *testing.T) {
	messages := newHostMessages()
	first := &hostWrapError{messages: messages, key: "serve.load_state", cause: errors.New("premier")}
	second := &hostWrapError{messages: messages, key: "serve.load_state", cause: errors.New("second")}
	err := fmt.Errorf("%w: %w", first, second)
	want := first.render(messages, i18n.French) + ": " + second.Error()
	if got := errorTextIn(messages, i18n.French, err); got != want {
		t.Fatalf("errorTextIn of two wrapped errors = %q, want %q", got, want)
	}
}

func TestErrorTextKeepsTheTextOfEveryJoinedBranch(t *testing.T) {
	messages := newHostMessages()
	inner := &hostWrapError{messages: messages, key: "serve.load_state", cause: errors.New("boom")}
	err := errors.Join(inner, errors.New("close"))
	want := inner.render(messages, i18n.French) + "\n" + "close"
	if got := errorTextIn(messages, i18n.French, err); got != want {
		t.Fatalf("errorTextIn of a join = %q, want %q", got, want)
	}
}
