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
	"github.com/Herrscherd/herrscher/core/internal/worktree"
)

//go:embed locales/en.json locales/fr.json
var hostLocales embed.FS

const (
	detailParam = "Detail"
	nameParam   = "Name"
	pathParam   = "Path"
	graceParam  = "Grace"
	roleParam   = "Role"
	ruleParam   = "Rule"
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
	params = renderableParams(params)
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
	return d.messages.text(d.textLanguage()(), key, params)
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

type localizableError interface {
	error
	render(lang i18n.Language) string
}

func errorTextIn(lang i18n.Language, err error) string {
	if err == nil {
		return ""
	}
	if english, local, ok := localizedPart(lang, err); ok {
		return strings.Replace(err.Error(), english, local, 1)
	}
	return err.Error()
}

func localizedPart(lang i18n.Language, err error) (string, string, bool) {
	var local localizableError
	if errors.As(err, &local) {
		return local.Error(), local.render(lang), true
	}
	var failure worktree.FailureError
	if errors.As(err, &failure) {
		return failure.Error(), failure.Text(lang), true
	}
	return "", "", false
}

func wrapHost(cause error, key string, params map[string]any) error {
	return hostWrapError{key: key, params: params, cause: cause}
}

func renderableParams(params map[string]any) map[string]any {
	var out map[string]any
	for key, value := range params {
		err, isErr := value.(error)
		if !isErr {
			continue
		}
		if out == nil {
			out = maps.Clone(params)
		}
		out[key] = err.Error()
	}
	if out == nil {
		return params
	}
	return out
}
