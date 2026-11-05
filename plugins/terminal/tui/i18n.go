package tui

import (
	"embed"
	"errors"
	"io/fs"
	"log/slog"
	"strings"
	"sync"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
)

const (
	tplArgAction = "Action"
	tplArgCount  = "Count"
	tplArgHint   = "Hint"
	tplArgKey    = "Key"
	tplArgLabel  = "Label"
	tplArgName   = "Name"
	tplArgReason = "Reason"
	tplArgSha    = "Sha"
	tplArgSpec   = "Spec"
	tplArgText   = "Text"
	tplArgTotal  = "Total"
	tplArgWhen   = "When"
)

//go:embed locales
var localeFiles embed.FS

type translator struct {
	lang    i18n.Language
	catalog func() (*i18n.Catalog, error)
	cli     *clitext.Translator
	cliOnce *sync.Once
}

func newTranslator() translator {
	return translator{lang: i18n.English, catalog: sync.OnceValues(loadLocales), cli: clitext.For(i18n.English), cliOnce: new(sync.Once)}
}

func loadLocales() (*i18n.Catalog, error) {
	sources, err := fs.Sub(localeFiles, "locales")
	if err != nil {
		return nil, err
	}
	return i18n.NewCatalog(nil, sources)
}

func (trans translator) in(lang i18n.Language) translator {
	trans.lang = lang
	trans.cli = clitext.For(lang)
	trans.cliOnce = new(sync.Once)
	return trans
}

func (trans translator) t(key string, params map[string]any) string {
	catalog, err := trans.catalog()
	if err != nil {
		slog.Error("tui catalog unavailable", "key", key, "err", err)
		return key
	}
	text, err := catalog.Message(i18n.Resolve(trans.lang), key, params)
	if err != nil {
		slog.Error("tui message not rendered", "key", key, "err", err)
		return key
	}
	return text
}

func (trans translator) shared(key clitext.Key) string {
	text := trans.cli.Text(key, nil)
	if err := trans.cli.Err(); err != nil {
		trans.cliOnce.Do(func() {
			slog.Error("tui shared message not rendered", "key", key, "err", err)
		})
	}
	return text
}

type reasonKey struct {
	sentinel error
	key      string
}

func reasonKeys() []reasonKey {
	return []reasonKey{
		{errNotRepo, "tui.diffgit.not_repo"},
		{errNoBase, "tui.diffgit.no_base"},
		{errOnBase, "tui.diffgit.on_base"},
		{errNoArguments, "tui.diffgit.no_arguments"},
		{errUnknownTool, "tui.diffgit.unknown_tool"},
		{errUnknownScope, "tui.diffgit.unknown_scope"},
		{errUnknownChange, "tui.diffgit.unknown_change"},
		{errMalformedStat, "tui.diffgit.malformed_stat"},
		{errNoPullNumber, "tui.diffgit.no_pull_number"},
		{errThemeCycle, "tui.reason.theme_cycle"},
		{errUnknownKey, "tui.reason.unknown_key"},
		{errInvalidColor, "tui.reason.invalid_color"},
		{errUnknownBase, "tui.reason.unknown_base"},
		{errUnknownOption, "tui.reason.unknown_option"},
		{errUnsafeDraftDir, "tui.reason.unsafe_draft_dir"},
		{errDuplicateBinding, "tui.reason.duplicate_binding"},
		{errNoEditor, "tui.reason.no_editor"},
	}
}

func localizedReason(trans translator, err error) string {
	message := err.Error()
	for _, known := range reasonKeys() {
		if errors.Is(err, known.sentinel) {
			return strings.Replace(message, known.sentinel.Error(), trans.t(known.key, nil), 1)
		}
	}
	return message
}

// Language sets the language the TUI renders its texts in. The zero value
// resolves to English.
func Language(lang i18n.Language) Option {
	return func(m *model) { m.localize(lang) }
}

func (m *model) localize(lang i18n.Language) {
	m.tr = m.tr.in(lang)
	m.input.Placeholder = idlePlaceholder(m.tr)
	m.bindings = loadKeyBindings(m.tr, "", "", keysSection{}).table
	m.cmds = localCommands(m.tr)
	m.syncComposerKeys()
	m.installTables()
	if m.tm != nil {
		m.cmds = mergeCommands(m.cmds, m.tm.Commands())
	}
}

func idlePlaceholder(trans translator) string {
	return trans.t("tui.compose.placeholder_idle", nil)
}
