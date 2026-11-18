package clitext

import (
	"context"
	"log/slog"

	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/config"
	"github.com/Herrscherd/herrscher/core/language"
)

type callerLanguageKey struct{}

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

// WithCallerLanguage carries the language of the client that sent a command
// along its context. A tag that names no supported language leaves ctx unchanged.
func WithCallerLanguage(ctx context.Context, tag string) context.Context {
	lang, err := i18n.ParseLanguage(tag)
	if err != nil {
		return ctx
	}
	return context.WithValue(ctx, callerLanguageKey{}, lang)
}

// CallerLanguage returns the language of the client that sent the command in
// ctx, or fallback when the command came without one.
func CallerLanguage(ctx context.Context, fallback func() i18n.Language) i18n.Language {
	if lang, ok := ctx.Value(callerLanguageKey{}).(i18n.Language); ok {
		return lang
	}
	return fallback()
}
