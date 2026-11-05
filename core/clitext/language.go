package clitext

import (
	"sync"

	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/config"
	"github.com/Herrscherd/herrscher/core/language"
)

// ClientLanguage resolves the language the command line renders in, through
// core/language, from the language of the config file when it can be read.
var ClientLanguage = sync.OnceValue(func() i18n.Language {
	cfg, err := config.Load(config.DefaultPath())
	if err != nil {
		return language.ForClient("")
	}
	return language.ForClient(cfg.Language)
})
