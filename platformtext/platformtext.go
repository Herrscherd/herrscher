// Package platformtext renders the lines of the command line that name a concrete
// chat platform. They sit outside core, which names no platform, and resolve their
// language the same way as core/clitext.
package platformtext

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"slices"

	"github.com/Herrscherd/herrscher-contracts/i18n"
)

//go:embed locales
var locales embed.FS

// ErrUndeclaredKey reports a key that the platform texts do not declare.
var ErrUndeclaredKey = errors.New("platform text key not declared")

// Key names one line of the platform texts.
type Key string

// Keys of the platform texts.
const (
	UsageTagline      Key = "cli.usage.tagline"
	UsageEnvBot       Key = "cli.usage.env.bot"
	ServeFlagInstance Key = "cli.serve.flag.instance"
	ServeFlagEnvFile  Key = "cli.serve.flag.env_file"
)

// Keys lists every key of the platform texts.
func Keys() []Key {
	return []Key{UsageTagline, UsageEnvBot, ServeFlagInstance, ServeFlagEnvFile}
}

func loadCatalog() (*i18n.Catalog, error) {
	sources, err := fs.Sub(locales, "locales")
	if err != nil {
		return nil, fmt.Errorf("platform texts locales: %w", err)
	}
	return i18n.NewCatalog(nil, sources)
}

// Translator renders the platform texts in one language. It keeps the first
// failure, like core/clitext.Translator.
type Translator struct {
	lang i18n.Language
	cat  *i18n.Catalog
	err  error
}

// For returns a translator that renders in lang.
func For(lang i18n.Language) *Translator {
	cat, err := loadCatalog()
	return &Translator{lang: lang, cat: cat, err: err}
}

// Text renders key. After a failure it returns the key itself, and Err reports
// the first failure.
func (t *Translator) Text(key Key) string {
	if !slices.Contains(Keys(), key) {
		t.fail(fmt.Errorf("%q: %w", key, ErrUndeclaredKey))
		return string(key)
	}
	if t.cat == nil {
		return string(key)
	}
	text, err := t.cat.Message(t.lang, string(key), nil)
	if err != nil {
		t.fail(fmt.Errorf("%q: %w", key, err))
		return string(key)
	}
	return text
}

func (t *Translator) fail(err error) {
	if t.err == nil {
		t.err = err
	}
}

// Err returns the first failure of the translator, or nil.
func (t *Translator) Err() error {
	return t.err
}
