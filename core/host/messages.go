package host

import (
	"embed"
	"io/fs"
	"log/slog"

	"github.com/Herrscherd/herrscher-contracts/i18n"
)

//go:embed locales/en.json locales/fr.json
var hostLocales embed.FS

const detailParam = "Detail"

func newHostCatalog() (*i18n.Catalog, error) {
	sub, err := fs.Sub(hostLocales, "locales")
	if err != nil {
		return nil, err
	}
	return i18n.NewCatalog(slog.Default(), sub)
}

func hostText(lang i18n.Language, key string, params map[string]any) string {
	if lang != i18n.English && lang != i18n.French {
		lang = i18n.English
	}
	catalog, err := newHostCatalog()
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
