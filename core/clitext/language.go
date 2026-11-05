package clitext

import (
	"log/slog"

	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/config"
	"github.com/Herrscherd/herrscher/core/language"
)

// ClientLanguage resolves the language the command line renders in, through
// core/language, from the language of the config file when it can be read.
func ClientLanguage() i18n.Language {
	cfg, err := config.Load(config.DefaultPath())
	if err != nil {
		slog.Warn("config.json unreadable, client language falls back to LANG", "err", err)
		return language.ForClient("")
	}
	return language.ForClient(cfg.Language)
}
