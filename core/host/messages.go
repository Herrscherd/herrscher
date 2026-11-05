package host

import (
	"embed"
	"io/fs"
	"log/slog"
	"sync"

	"github.com/Herrscherd/herrscher-contracts/i18n"
)

//go:embed locales/en.json locales/fr.json
var hostLocales embed.FS

const detailParam = "Detail"

type hostMessages struct {
	catalog func() (*i18n.Catalog, error)
}

func newHostMessages() *hostMessages {
	return &hostMessages{catalog: sync.OnceValues(buildHostCatalog)}
}

func buildHostCatalog() (*i18n.Catalog, error) {
	sub, err := fs.Sub(hostLocales, "locales")
	if err != nil {
		return nil, err
	}
	return i18n.NewCatalog(slog.Default(), sub)
}

func (m *hostMessages) text(lang i18n.Language, key string, params map[string]any) string {
	if m == nil {
		m = newHostMessages()
	}
	if lang != i18n.English && lang != i18n.French {
		lang = i18n.English
	}
	catalog, err := m.catalog()
	if err != nil {
		slog.Error("host message catalog unavailable", "key", key, "err", err)
		return key
	}
	text, err := catalog.Message(lang, key, params)
	if err != nil {
		slog.Error("host message does not render", "key", key, "lang", lang.String(), "err", err)
		return key
	}
	return text
}

func (d *sessionDriver) hostText(key string, params map[string]any) string {
	return d.messages.text(d.lang, key, params)
}
