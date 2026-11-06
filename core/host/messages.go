package host

import (
	"embed"
	"errors"
	"io/fs"
	"log/slog"
	"maps"
	"strings"
	"sync"

	"github.com/Herrscherd/herrscher-contracts/i18n"
)

//go:embed locales/en.json locales/fr.json
var hostLocales embed.FS

const (
	detailParam = "Detail"
	nameParam   = "Name"
)

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
		return err.Error()
	}
	text, err := catalog.Message(lang, key, params)
	if err != nil && lang != i18n.English {
		slog.Error("host message does not render, using english", "key", key, "lang", lang.String(), "err", err)
		text, err = catalog.Message(i18n.English, key, params)
	}
	if err != nil {
		slog.Error("host message does not render in english", "key", key, "err", err)
		return err.Error()
	}
	return text
}

func (d *sessionDriver) hostText(key string, params map[string]any) string {
	return d.messages.text(d.lang, key, params)
}

type hostWrapError struct {
	key    string
	params map[string]any
	cause  error
}

func (e hostWrapError) Error() string { return e.render(i18n.English) }

func (e hostWrapError) Unwrap() error { return e.cause }

func (e hostWrapError) render(lang i18n.Language) string {
	params := make(map[string]any, len(e.params)+1)
	maps.Copy(params, e.params)
	params[detailParam] = errorTextIn(lang, e.cause)
	return newHostMessages().text(lang, e.key, params)
}

func errorTextIn(lang i18n.Language, err error) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	var wrapped hostWrapError
	if errors.As(err, &wrapped) {
		text = strings.Replace(text, wrapped.Error(), wrapped.render(lang), 1)
	}
	return text
}

func wrapHost(cause error, key string, params map[string]any) error {
	return hostWrapError{key: key, params: params, cause: cause}
}
