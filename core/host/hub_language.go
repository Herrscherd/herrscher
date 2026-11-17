package host

import (
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func (h *hub) textLanguage() languageSource {
	if h.language != nil {
		return h.language
	}
	return fixedLanguageSource(h.lang)
}

func (h *hub) sessionLanguageSource(name string) languageSource {
	recorded := liveSessionLanguage(h.st, name)
	operator := h.textLanguage()
	return func() i18n.Language {
		if lang := recorded(); lang != 0 {
			return lang
		}
		return operator()
	}
}

func sessionLanguage(sess state.Session, operator i18n.Language) i18n.Language {
	if lang := knownSessionLanguage(sess); lang != 0 {
		return lang
	}
	return operator
}

func knownSessionLanguage(sess state.Session) i18n.Language {
	lang, err := i18n.ParseLanguage(sess.Language)
	if err != nil {
		return 0
	}
	return lang
}

func liveSessionLanguage(store *state.State, name string) func() i18n.Language {
	return func() i18n.Language {
		if store == nil {
			return 0
		}
		session, ok := store.FindSession(name)
		if !ok {
			return 0
		}
		return knownSessionLanguage(session)
	}
}
