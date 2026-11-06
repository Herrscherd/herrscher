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
