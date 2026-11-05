package tui

import (
	"embed"
	"io/fs"
	"log/slog"
	"sync"

	"github.com/Herrscherd/herrscher-contracts/i18n"
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
}

func newTranslator() translator {
	return translator{lang: i18n.English, catalog: sync.OnceValues(loadLocales)}
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
