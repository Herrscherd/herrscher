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
	var node any = err
	if wrapped, ok := node.(*hostWrapError); ok && wrapped == nil {
		return ""
	}
	if leaf, ok := node.(localizableError); ok {
		return leaf.render(messages, lang)
	}
	if failure, ok := node.(worktree.FailureError); ok {
		return failure.Text(lang)
	}
	if branches, ok := node.(interface{ Unwrap() []error }); ok {
		return joinedText(messages, lang, branches.Unwrap())
	}
	cause := errors.Unwrap(err)
	if cause == nil {
		return fmt.Sprint(err)
	}
	return wrappedText(messages, lang, err, cause)
}

func joinedText(messages *hostMessages, lang i18n.Language, branches []error) string {
	parts := make([]string, 0, len(branches))
	for _, branch := range branches {
		if branch != nil {
			parts = append(parts, errorTextIn(messages, lang, branch))
		}
	}
	return strings.Join(parts, "\n")
}

func wrappedText(messages *hostMessages, lang i18n.Language, err, cause error) string {
	message := fmt.Sprint(err)
	english := errorTextIn(messages, i18n.English, cause)
	local := english
	if lang != i18n.English {
		local = errorTextIn(messages, lang, cause)
	}
	if strings.HasSuffix(message, english) {
		return strings.TrimSuffix(message, english) + local
	}
	return strings.Replace(message, english, local, 1)
}

func isLocalized(err error) bool {
	if _, found := errors.AsType[localizableError](err); found {
		return true
	}
	_, found := errors.AsType[worktree.FailureError](err)
	return found
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
