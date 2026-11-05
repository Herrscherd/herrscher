// Package clitext renders the texts the herrscher command line shows to an
// operator: usage, help, confirmations and the final messages of each verb. Each
// message is a key of the catalog in locales, declared with its named parameters
// in the keys files, and rendered in the language the caller resolved with
// core/language.
package clitext

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"slices"

	"github.com/Herrscherd/herrscher-contracts/i18n"
)

//go:embed locales
var locales embed.FS

// Key names one message of the command line.
type Key string

// Params carries the named arguments of a message. Each entry fills the
// {{.Name}} field of that name, and a count entry, named Count, selects the
// plural form. Values are strings, booleans or integers.
type Params map[string]any

var (
	// ErrUndeclaredKey reports a key that no keys file declares.
	ErrUndeclaredKey = errors.New("message key not declared")
	// ErrParamsMismatch reports params that do not name exactly the fields the key declares.
	ErrParamsMismatch = errors.New("message params do not match the key")
)

func declaredKeys() map[Key][]string {
	return mergeDeclared(usageKeys(), cliKeys(), serveKeys(), bridgeKeys(), sessionKeys(), pluginKeys(), serviceKeys(), accountKeys(), manageKeys())
}

func loadCatalog() (*i18n.Catalog, error) {
	sources, err := fs.Sub(locales, "locales")
	if err != nil {
		return nil, fmt.Errorf("command line locales: %w", err)
	}
	return i18n.NewCatalog(nil, sources)
}

// Names of the params shared by several keys.
const (
	ParamErr       = "Err"
	ParamKey       = "Key"
	ParamName      = "Name"
	ParamKind      = "Kind"
	ParamPath      = "Path"
	ParamMessage   = "Message"
	ParamVerb      = "Verb"
	ParamArg       = "Arg"
	ParamCategory  = "Category"
	ParamOwner     = "Owner"
	ParamFailure   = "Failure"
	ParamWait      = "Wait"
	ParamModel     = "Model"
	ParamCount     = "Count"
	ParamOS        = "OS"
	ParamArch      = "Arch"
	ParamOutput    = "Output"
	ParamSessions  = "Sessions"
	ParamUsage     = "Usage"
	ParamEnvFile   = "EnvFile"
	ParamStart     = "Start"
	ParamTask      = "Task"
	ParamVersion   = "Version"
	ParamInstalled = "Installed"
	ParamRan       = "Ran"
	ParamVendor    = "Vendor"
	ParamState     = "State"
	ParamTo        = "To"
	ParamSchedule  = "Schedule"
	ParamHandoff   = "Handoff"
	ParamNote      = "Note"
	ParamBranch    = "Branch"
	ParamCommand   = "Command"
	ParamRepo      = "Repo"
	ParamChannel   = "Channel"
	ParamBanner    = "Banner"
	ParamApprovals = "Approvals"
	ParamWhy       = "Why"
	ParamAccount   = "Account"
	ParamMode      = "Mode"
)

// ParamRollbackErr names the error of the rollback that followed a failed session switch.
const ParamRollbackErr = "RollbackErr"

// Text renders key in lang with params. It refuses an undeclared key, and params
// that do not name exactly the fields the key declares.
func Text(lang i18n.Language, key Key, params Params) (string, error) {
	cat, err := loadCatalog()
	if err != nil {
		return "", err
	}
	return render(cat, lang, key, params)
}

// Failure returns an error that reads as key rendered in lang with params, and
// that still matches cause through errors.Is and errors.As. When key cannot be
// rendered, it returns that rendering error instead.
func Failure(lang i18n.Language, cause error, key Key, params Params) error {
	text, err := Text(lang, key, params)
	if err != nil {
		return err
	}
	return failureError{text: text, cause: cause}
}

type failureError struct {
	text  string
	cause error
}

func (e failureError) Error() string { return e.text }

func (e failureError) Unwrap() error { return e.cause }

func render(cat *i18n.Catalog, lang i18n.Language, key Key, params Params) (string, error) {
	fields, known := declaredKeys()[key]
	if !known {
		return "", fmt.Errorf("%q: %w", key, ErrUndeclaredKey)
	}
	if err := checkParams(fields, params); err != nil {
		return "", fmt.Errorf("%q: %w", key, err)
	}
	return cat.Message(lang, string(key), params)
}

func checkParams(fields []string, params Params) error {
	if len(fields) == len(params) && allNamed(fields, params) {
		return nil
	}
	return fmt.Errorf("takes %v, given %v: %w", fields, slices.Sorted(maps.Keys(params)), ErrParamsMismatch)
}

func allNamed(fields []string, params Params) bool {
	for name := range params {
		if !slices.Contains(fields, name) {
			return false
		}
	}
	return true
}

// Translator renders the messages of one block in one language. It keeps the
// first failure, so a block is written in one pass and its error is read once,
// at the end.
type Translator struct {
	lang i18n.Language
	cat  *i18n.Catalog
	err  error
}

// For returns a translator that renders in lang.
func For(lang i18n.Language) *Translator {
	cat, err := loadCatalog()
	translator := &Translator{lang: lang, cat: cat}
	translator.fail(err)
	return translator
}

// Text renders key with params. After a failure it returns the key itself, and
// Err reports the first failure.
func (t *Translator) Text(key Key, params Params) string {
	if t.cat == nil {
		return string(key)
	}
	text, err := render(t.cat, t.lang, key, params)
	if err != nil {
		t.fail(err)
		return string(key)
	}
	return text
}

// Err returns the first failure of the translator, or nil.
func (t *Translator) Err() error {
	return t.err
}

func (t *Translator) fail(err error) {
	if err != nil && t.err == nil {
		t.err = err
	}
}

func mergeDeclared(groups ...map[Key][]string) map[Key][]string {
	out := make(map[Key][]string)
	for _, group := range groups {
		maps.Copy(out, group)
	}
	return out
}
