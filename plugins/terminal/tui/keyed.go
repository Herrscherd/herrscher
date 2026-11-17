package tui

import (
	"log/slog"

	"github.com/Herrscherd/herrscher-contracts/i18n"
)

type keyedError struct {
	cause   error
	key     string
	params  map[string]any
	catalog func() (*i18n.Catalog, error)
}

type renderedError struct {
	cause error
	text  string
}

func (e keyedError) Error() string {
	return catalogText{lang: i18n.English, catalog: e.catalog, log: slog.Default()}.t(e.key, e.params)
}

func (e keyedError) Unwrap() error {
	return e.cause
}

func (e renderedError) Error() string {
	return e.text
}

func (e renderedError) Unwrap() error {
	return e.cause
}
