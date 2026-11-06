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
		"coordination.fanout_partial":    {"Detail", "Launched"},
		"coordination.route_refused":     {"Detail"},
		"coordination.seal_refused":      {"Detail"},
		"coordination.merge_refused":     {"Detail"},
		"coordination.handoff_refused":   {"Detail"},
		"compaction.no_effect":           {"Percent"},
		"compaction.bridge_gone":         nil,
		"compaction.no_backend_factory":  nil,
		"compaction.not_compactor_port":  nil,
		"compaction.no_compactor_plugin": nil,
		"compaction.failed":              {"Detail"},
		"coordination.routed":            {"Agent", "Session"},
		"coordination.sealed":            {"Node"},
		"coordination.merged":            {"Lead"},
		"coordination.launched":          {"Count", "Workers"},
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
			name: "no backend factory",
			err:  errNoBackendFactory,
			fr:   "le plugin déclare Capabilities.Compactor mais ne fournit aucune fabrique de backend",
			en:   "the plugin declares Capabilities.Compactor but provides no backend factory",
		},
		{
			name: "port not implemented",
			err:  errNotCompactorPort,
			fr:   "le plugin déclare Capabilities.Compactor mais n'implémente pas le port",
			en:   "the plugin declares Capabilities.Compactor but does not implement the port",
		},
		{
			name: "no compactor plugin",
			err:  errNoCompactorPlugin,
			fr:   "aucun plugin ne déclare Capabilities.Compactor sous ce nom",
			en:   "no plugin declares Capabilities.Compactor under this name",
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
