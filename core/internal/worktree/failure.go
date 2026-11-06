package worktree

import (
	"errors"
	"strings"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
)

// FailureError is an error a person reads. Its message is a key of the catalog and
// its params, so the presentation layer renders it in the language of the
// session it concerns. An error among the params is rendered the same way.
type FailureError struct {
	Key    clitext.Key
	Params clitext.Params
	Cause  error
}

func (f FailureError) Error() string { return f.Text(i18n.English) }

func (f FailureError) Unwrap() error { return f.Cause }

// Text renders the failure in lang. A key the catalog cannot render shows as the key.
func (f FailureError) Text(lang i18n.Language) string {
	params := make(clitext.Params, len(f.Params))
	for name, value := range f.Params {
		params[name] = renderParam(lang, value)
	}
	text, err := clitext.Text(lang, f.Key, params)
	if err != nil {
		return string(f.Key)
	}
	return text
}

func renderParam(lang i18n.Language, value any) any {
	err, isError := value.(error)
	if !isError {
		return value
	}
	var failure FailureError
	if errors.As(err, &failure) {
		return strings.Replace(err.Error(), failure.Error(), failure.Text(lang), 1)
	}
	return err.Error()
}
