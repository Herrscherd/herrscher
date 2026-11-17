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
	countParam  = "Count"
)

const nilErrorText = "<nil>"

type hostMessages struct {
	catalog func() (*i18n.Catalog, error)
}

func newHostMessages() *hostMessages {
	return &hostMessages{catalog: sync.OnceValues(buildHostCatalog)}
}

// Messages is the catalogue owner a caller keeps for as long as it renders host errors.
type Messages struct {
	owner *hostMessages
}

// NewMessages returns a Messages whose catalogue is built on first use.
func NewMessages() *Messages {
	return &Messages{owner: newHostMessages()}
}

// Localize renders err for an operator boundary in lang through this owner's catalogue.
func (m *Messages) Localize(lang i18n.Language, err error) error {
	return localizeHostError(m.owner, lang, err)
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
	messages *hostMessages
	key      string
	params   map[string]any
	cause    error
}

func (e *hostWrapError) Error() string {
	if e == nil {
		return nilErrorText
	}
	return e.render(e.messages, i18n.English)
}

func (e *hostWrapError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func (e *hostWrapError) render(messages *hostMessages, lang i18n.Language) string {
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
	text := fmt.Sprint(err)
	if lang == i18n.English {
		return text
	}
	english, local, found := localizedPart(messages, lang, err)
	if !found {
		return text
	}
	if strings.HasSuffix(text, english) {
		return strings.TrimSuffix(text, english) + local
	}
	return strings.Replace(text, english, local, 1)
}

func localizedPart(messages *hostMessages, lang i18n.Language, err error) (string, string, bool) {
	if localizable := firstLocalizable(err); localizable != nil {
		return localizable.Error(), localizable.render(messages, lang), true
	}
	if failure, found := errors.AsType[worktree.FailureError](err); found {
		return failure.Error(), failure.Text(lang), true
	}
	return "", "", false
}

func isNilLocalizable(localizable localizableError) bool {
	switch typed := localizable.(type) {
	case *hostWrapError:
		return typed == nil
	case *pluginKindReservedError:
		return typed == nil
	case *RemoteResolveError:
		return typed == nil
	case *gatewayEnvUnsupportedError:
		return typed == nil
	case *unsafeTarMemberError:
		return typed == nil
	case *instanceIDMismatchError:
		return typed == nil
	case *remoteSessionError:
		return typed == nil
	case *remoteAccountError:
		return typed == nil
	}
	return false
}

func firstLocalizable(err error) localizableError {
	if err == nil {
		return nil
	}
	if localizable, found := errors.AsType[localizableError](err); found && !isNilLocalizable(localizable) {
		return localizable
	}
	if joined, isJoin := err.(interface{ Unwrap() []error }); isJoin {
		for _, branch := range joined.Unwrap() {
			if localizable := firstLocalizable(branch); localizable != nil {
				return localizable
			}
		}
		return nil
	}
	return firstLocalizable(errors.Unwrap(err))
}

func isLocalized(err error) bool {
	if firstLocalizable(err) != nil {
		return true
	}
	_, found := errors.AsType[worktree.FailureError](err)
	return found
}

func wrapHost(messages *hostMessages, cause error, key string, params map[string]any) error {
	return &hostWrapError{messages: messages, key: key, params: params, cause: cause}
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
