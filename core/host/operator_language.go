package host

import (
	"sync"

	"github.com/Herrscherd/herrscher-contracts/i18n"
)

type operatorLanguage struct {
	mu      sync.RWMutex
	current i18n.Language
}

func newOperatorLanguage(lang i18n.Language) *operatorLanguage {
	return &operatorLanguage{current: lang}
}

func withOperatorLanguage(options Options) Options {
	if options.live == nil {
		options.live = newOperatorLanguage(options.Language)
	}
	return options
}

func (options Options) languageSource() languageSource {
	if options.live != nil {
		return options.live.Get
	}
	return fixedLanguageSource(options.Language)
}

func fixedLanguageSource(lang i18n.Language) languageSource {
	return func() i18n.Language { return lang }
}

func (live *operatorLanguage) Get() i18n.Language {
	live.mu.RLock()
	defer live.mu.RUnlock()
	return live.current
}

func (live *operatorLanguage) set(lang i18n.Language) {
	live.mu.Lock()
	defer live.mu.Unlock()
	live.current = lang
}

type languageSource func() i18n.Language
