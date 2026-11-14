package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"strings"
	"testing"
	"testing/fstest"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher-contracts/i18n/i18ntest"
	"github.com/Herrscherd/herrscher/core/cli"
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
		"coordination.fanout_partial":     {"Detail", "Launched"},
		"coordination.route_refused":      {"Detail"},
		"coordination.seal_refused":       {"Detail"},
		"coordination.merge_refused":      {"Detail"},
		"coordination.handoff_refused":    {"Detail"},
		"compaction.no_effect":            {"Percent"},
		"compaction.bridge_gone":          nil,
		"compaction.no_compactor_factory": nil,
		"compaction.nil_compactor":        nil,
		"compaction.no_compactor_plugin":  nil,
		"compaction.no_history_port":      nil,
		"compaction.history_timeout":      nil,
		"compaction.history_too_large":    nil,
		"compaction.failed":               {"Detail"},
		"coordination.routed":             {"Agent", "Session"},
		"coordination.sealed":             {"Node"},
		"coordination.merged":             {"Lead"},
		"coordination.launched":           {"Count", "Workers"},

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
		"approval.prompt":                   {"Request"},
		"approval.hint":                     {"Command", "Help"},
		"seed.consolidate":                  {"Detail", "Elapsed", "Name"},
		"seed.turn_failed":                  {"Detail"},
		"serve.instance_persist":            {"Detail"},
		"serve.load_state":                  {"Detail"},
		"serve.resolve_instance":            {"Detail"},
		"serve.compaction":                  {"Detail"},
		"serve.registry":                    {"Detail"},
		"serve.config":                      {"Detail"},
		"serve.key_proxy":                   {"Detail"},
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
		"warn.session_checkpoint":           {"Detail", "Name"},
		"warn.session_resume_token":         {"Detail", "Name"},
		"warn.session_project_pin":          {"Detail", "Name"},
		"warn.session_transcript":           {"Detail", "Name"},
		"warn.session_teardown_overrun":     {"Grace", "Name"},
		"warn.session_seed_transcript":      {"Detail", "Name"},
		"warn.session_usage":                {"Detail", "Name"},
		"warn.session_compaction_notice":    {"Detail", "Name"},
		"warn.role_undefined":               {"Name", "Role"},
		"warn.ssh_multiplexing_disabled":    {"Detail", "Name"},
		"warn.health_server":                {"Detail"},
		"warn.command_socket_listen":        {"Detail", "Path"},
		"warn.approvals_unreadable":         {"Detail", "Path"},
		"warn.approvals_file":               {"Detail", "Path"},
		"warn.approvals_state_rule":         {"Detail", "Rule"},
		"warn.context_limit_env":            {"Detail", "Name"},
		"warn.context_limit_unknown_vendor": {"Name"},
		"warn.history_port_missing":         {"Name"},
		"warn.compactor_port_missing":       {"Name"},
	}
	i18ntest.CheckDeclared(t, declared, hostLocaleSource(t))
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
			name:   "unset language falls back to english",
			lang:   i18n.Language(0),
			key:    "compaction.failed",
			params: map[string]any{"Detail": "disk full"},
			want:   "compaction failed: disk full",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := newHostMessages().text(tt.lang, tt.key, tt.params); got != tt.want {
				t.Fatalf("text(%v, %q) = %q, want %q", tt.lang, tt.key, got, tt.want)
			}
		})
	}
}

func TestCompactFailureTextMapsUntypedCauses(t *testing.T) {
	tests := []struct {
		name string
		err  error
		fr   string
		en   string
	}{
		{
			name: "bridge closed",
			err:  fmt.Errorf("stage: %w", errBridgeGone),
			fr:   "le relais s'est fermé pendant une compaction",
			en:   "the bridge closed during a compaction",
		},
		{
			name: "no compactor factory",
			err:  errNoCompactorFactory,
			fr:   "le plugin compacteur ne fournit aucune fabrique Compactor",
			en:   "the compactor plugin provides no Compactor factory",
		},
		{
			name: "factory returned no stage",
			err:  errNilCompactor,
			fr:   "la fabrique du plugin compacteur n'a renvoyé aucune étape",
			en:   "the compactor plugin's factory returned no stage",
		},
		{
			name: "no compactor plugin",
			err:  errNoCompactorPlugin,
			fr:   "aucun plugin compacteur n'est enregistré sous ce nom",
			en:   "no compactor plugin is registered under this name",
		},
		{
			name: "history port not implemented",
			err:  fmt.Errorf("stage: %w", historywire.ErrNoHistoryEditor),
			fr:   "le backend déclare Capabilities.HistoryEditor mais n'implémente pas le port, son historique ne peut pas être modifié",
			en:   "the backend declares Capabilities.HistoryEditor but does not implement the port, its history cannot be edited",
		},
		{
			name: "history call timed out",
			err:  fmt.Errorf("%w: %w", errHistoryTimeout, context.DeadlineExceeded),
			fr:   "le relais n'a pas répondu à temps à un appel sur l'historique",
			en:   "the bridge did not answer a history call in time",
		},
		{
			name: "history too large",
			err:  fmt.Errorf("stage: %w", historywire.ErrHistoryTooLarge),
			fr:   "l'historique de la session est trop volumineux pour être modifié",
			en:   "the history of the session is too large to be edited",
		},
		{
			name: "unclassified failure keeps its technical detail",
			err:  errors.New("disk full"),
			fr:   "échec de la compaction : disk full",
			en:   "compaction failed: disk full",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := compactFailureText(newHostMessages(), i18n.French, tt.err); got != tt.fr {
				t.Fatalf("french = %q, want %q", got, tt.fr)
			}
			if got := compactFailureText(newHostMessages(), i18n.English, tt.err); got != tt.en {
				t.Fatalf("english = %q, want %q", got, tt.en)
			}
		})
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
	_, err := setSkillApproval(context.Background(), i18n.English, recallFailingMemory{err: errors.New("store locked")}, input)
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
	_, err := setSkillApproval(context.Background(), i18n.English, recallFailingMemory{err: errors.New("store locked")}, input)
	got := cli.Localize(err, i18n.French).Error()
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
