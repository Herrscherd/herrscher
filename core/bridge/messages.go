package bridge

import (
	"embed"
	"io/fs"
	"sync"

	"github.com/Herrscherd/herrscher-contracts/i18n"
)

//go:embed locales/en.json locales/fr.json
var bridgeLocales embed.FS

const detailParam = "Detail"

type bridgeMessages struct {
	catalog func() (*i18n.Catalog, error)
}

func newBridgeMessages() *bridgeMessages {
	return &bridgeMessages{catalog: sync.OnceValues(loadBridgeCatalog)}
}

func loadBridgeCatalog() (*i18n.Catalog, error) {
	sub, err := fs.Sub(bridgeLocales, "locales")
	if err != nil {
		return nil, err
	}
	return i18n.NewCatalog(logger, sub)
}

func (m *bridgeMessages) render(lang i18n.Language, key string, params map[string]any) string {
	catalog, err := m.catalog()
	if err != nil {
		logger.Error("bridge message catalog unavailable", "key", key, "err", err)
		return key
	}
	if lang != i18n.English && lang != i18n.French {
		lang = i18n.English
	}
	text, err := catalog.Message(lang, key, params)
	if err != nil && lang != i18n.English {
		logger.Error("bridge message does not render, using english", "key", key, "lang", lang.String(), "err", err)
		text, err = catalog.Message(i18n.English, key, params)
	}
	if err != nil {
		logger.Error("bridge message does not render in english", "key", key, "err", err)
		return key
	}
	return text
}
