package host

import (
	"embed"
	"errors"
	"fmt"
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

func effectiveLanguage(lang i18n.Language) i18n.Language {
	if lang != i18n.English && lang != i18n.French {
		return i18n.English
	}
	return lang
}

func (m *hostMessages) text(lang i18n.Language, key string, params map[string]any) string {
	lang = effectiveLanguage(lang)
	params = renderableParams(m, lang, params)
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

func (e hostWrapError) Error() string { return e.render(newHostMessages(), i18n.English) }

func (e hostWrapError) Unwrap() error { return e.cause }

func (e hostWrapError) render(messages *hostMessages, lang i18n.Language) string {
	params := make(map[string]any, len(e.params)+1)
	maps.Copy(params, e.params)
	params[detailParam] = errorTextIn(messages, lang, e.cause)
	return messages.text(lang, e.key, params)
}

type localizableError interface {
	error
	render(messages *hostMessages, lang i18n.Language) string
}

func errorTextIn(messages *hostMessages, lang i18n.Language, err error) string {
	if err == nil {
		return ""
	}
	english, local, ok := localizedPart(messages, lang, err)
	if !ok {
		return fmt.Sprint(err)
	}
	if isDirectLocalizable(err) {
		return local
	}
	return strings.Replace(fmt.Sprint(err), english, local, 1)
}

func isDirectLocalizable(err error) bool {
	if _, found := errors.AsType[localizableError](err); !found {
		return false
	}
	inner := errors.Unwrap(err)
	if inner == nil {
		return true
	}
	_, nested := errors.AsType[localizableError](inner)
	return !nested
}

func localizedPart(messages *hostMessages, lang i18n.Language, err error) (string, string, bool) {
	var wrapped *hostWrapError
	if errors.As(err, &wrapped) && wrapped == nil {
		return "", "", false
	}
	var local localizableError
	if errors.As(err, &local) {
		return local.render(messages, i18n.English), local.render(messages, lang), true
	}
	var failure worktree.FailureError
	if errors.As(err, &failure) {
		return failure.Text(i18n.English), failure.Text(lang), true
	}
	return "", "", false
}

func wrapHost(cause error, key string, params map[string]any) error {
	return hostWrapError{key: key, params: params, cause: cause}
}

func renderableParams(messages *hostMessages, lang i18n.Language, params map[string]any) map[string]any {
	var out map[string]any
	for key, value := range params {
		err, isErr := value.(error)
		if !isErr {
			continue
		}
		if out == nil {
			out = maps.Clone(params)
		}
		out[key] = errorTextIn(messages, lang, err)
	}
	if out == nil {
		return params
	}
	return out
}
