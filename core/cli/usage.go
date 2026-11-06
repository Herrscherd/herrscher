package cli

import (
	"errors"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
)

type usageKey struct {
	cause error
	key   clitext.Key
}

func usageKeys() []usageKey {
	return []usageKey{
		{errUnknownCommand, clitext.CommandUnknown},
		{errMissingRequired, clitext.ParamMissing},
		{errUnknownFlag, clitext.FlagUnknown},
		{errFlagNeedsValue, clitext.FlagNeedsValue},
	}
}

type usageError struct {
	cause  error
	key    clitext.Key
	params clitext.Params
}

func usageFailure(cause error, params clitext.Params) error {
	for _, entry := range usageKeys() {
		if errors.Is(entry.cause, cause) {
			return usageError{cause: cause, key: entry.key, params: params}
		}
	}
	return cause
}

func (e usageError) Error() string {
	text, err := e.render(i18n.English)
	if err != nil {
		return e.cause.Error()
	}
	return text
}

func (e usageError) Unwrap() error { return e.cause }

func (e usageError) render(lang i18n.Language) (string, error) {
	return clitext.Text(lang, e.key, e.params)
}

func localizedUsage(usage usageError, err error, lang i18n.Language) error {
	text, renderErr := usage.render(orEnglish(lang))
	if renderErr != nil {
		return err
	}
	return renderedError{text: text, err: err}
}

func isUsage(err error) (usageError, bool) {
	var usage usageError
	found := errors.As(err, &usage)
	return usage, found
}
