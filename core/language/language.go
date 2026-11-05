// Package language resolves the language the operator, a client and the plugins use.
package language

import (
	"log/slog"
	"os"

	"github.com/Herrscherd/herrscher-contracts/i18n"
)

// EnvVar names the environment variable that carries the resolved language to plugins.
const EnvVar = "HERRSCHER_LANG"

// Source is one place a language may be read from, in priority order.
type Source struct {
	Origin string
	Value  string
	Quiet  bool
}

// Pick returns the first source naming a supported language, English when none does. It warns once about the first ignored value unless its source is Quiet.
func Pick(log *slog.Logger, sources ...Source) i18n.Language {
	warned := false
	for _, source := range sources {
		if source.Value == "" {
			continue
		}
		lang, err := i18n.ParseLanguage(source.Value)
		if err == nil {
			return lang
		}
		if !source.Quiet && !warned {
			log.Warn("language ignored, it names no supported language", "source", source.Origin, "value", source.Value)
			warned = true
		}
	}
	return i18n.English
}

// Operator resolves the operator language from config.json, then HERRSCHER_LANG.
func Operator(log *slog.Logger, configured string) i18n.Language {
	return Pick(log,
		Source{Origin: "config.json language", Value: configured},
		Source{Origin: EnvVar, Value: os.Getenv(EnvVar)},
	)
}

// Client resolves a client language from config.json, then HERRSCHER_LANG, then LANG without warning.
func Client(log *slog.Logger, configured string) i18n.Language {
	return Pick(log,
		Source{Origin: "config.json language", Value: configured},
		Source{Origin: EnvVar, Value: os.Getenv(EnvVar)},
		Source{Origin: "LANG", Value: os.Getenv("LANG"), Quiet: true},
	)
}

// Languages holds the operator language and the client language of one process.
type Languages struct {
	Operator i18n.Language
	Client   i18n.Language
}

// Resolve resolves both languages once, and warns about an ignored value only through Operator.
func Resolve(log *slog.Logger, configured string) Languages {
	return Languages{
		Operator: Operator(log, configured),
		Client:   ForClient(configured),
	}
}

// ForClient resolves the language of a client rendering text in this process, without warning.
func ForClient(configured string) i18n.Language {
	return Client(slog.New(slog.DiscardHandler), configured)
}

// Explicit reports whether config.json or HERRSCHER_LANG names a supported language.
func Explicit(configured string) bool {
	return supported(configured) || supported(os.Getenv(EnvVar))
}

func supported(value string) bool {
	if value == "" {
		return false
	}
	_, err := i18n.ParseLanguage(value)
	return err == nil
}

// Export publishes lang in HERRSCHER_LANG so that plugins read the same language.
func Export(lang i18n.Language) error {
	return os.Setenv(EnvVar, lang.String())
}
