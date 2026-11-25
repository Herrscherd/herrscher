package main

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"slices"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
)

//go:embed locales
var locales embed.FS

type msgKey string

type params map[string]any

var (
	errUndeclaredKey  = errors.New("membench: message key not declared")
	errParamsMismatch = errors.New("membench: message params do not match the key")
)

type catalogue struct {
	cat *i18n.Catalog
	err error
}

func loadCatalogue() catalogue {
	sub, err := fs.Sub(locales, "locales")
	if err != nil {
		return catalogue{err: fmt.Errorf("membench locales: %w", err)}
	}
	cat, err := i18n.NewCatalog(nil, sub)
	if err != nil {
		return catalogue{err: fmt.Errorf("membench locales: %w", err)}
	}
	return catalogue{cat: cat}
}

func (c catalogue) text(lang i18n.Language, key msgKey, args params) (string, error) {
	if c.err != nil {
		return "", c.err
	}
	if !c.cat.Has(string(key)) {
		return "", fmt.Errorf("%q: %w", key, errUndeclaredKey)
	}
	fields := paramsOf(key)
	if !namesExactly(fields, args) {
		return "", fmt.Errorf("%q takes %v, given %v: %w", key, fields, slices.Sorted(maps.Keys(args)), errParamsMismatch)
	}
	return c.cat.Message(lang, string(key), args)
}

func namesExactly(fields []string, args params) bool {
	if len(fields) != len(args) {
		return false
	}
	for name := range args {
		if !slices.Contains(fields, name) {
			return false
		}
	}
	return true
}

type translator struct {
	lang i18n.Language
	cat  catalogue
	err  error
}

func (c catalogue) For(lang i18n.Language) *translator {
	return &translator{lang: lang, cat: c, err: c.err}
}

func (t *translator) Text(key msgKey, args params) string {
	text, err := t.cat.text(t.lang, key, t.localize(args))
	if err != nil {
		if t.err == nil {
			t.err = err
		}
		return string(key)
	}
	return text
}

func (t *translator) localize(args params) params {
	localized := make(params, len(args))
	for name, value := range args {
		if size, ok := value.(byteSize); ok {
			localized[name] = byteText(t.lang, size)
			continue
		}
		localized[name] = value
	}
	return localized
}

type byteSize int64

const (
	kibibyte = 1 << 10
	mebibyte = 1 << 20
)

type byteSymbols struct {
	bytes, kibi, mebi string
}

func byteText(lang i18n.Language, size byteSize) string {
	symbols := byteSymbolsOf(lang)
	value, unit := float64(size), symbols.bytes
	switch {
	case size >= mebibyte && size%mebibyte == 0:
		value, unit = float64(size/mebibyte), symbols.mebi
	case size >= kibibyte && size%kibibyte == 0:
		value, unit = float64(size/kibibyte), symbols.kibi
	}
	return clitext.FormatNumber(lang, value, 0) + " " + unit
}

func byteSymbolsOf(lang i18n.Language) byteSymbols {
	if lang == i18n.French {
		return byteSymbols{bytes: "o", kibi: "Kio", mebi: "Mio"}
	}
	return byteSymbols{bytes: "B", kibi: "KiB", mebi: "MiB"}
}

func (t *translator) Err() error {
	return t.err
}
