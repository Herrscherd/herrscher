package host

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/worktree"
)

type migrationStep struct {
	key    clitext.Key
	params clitext.Params
}

type migrationProgress struct {
	steps []migrationStep
}

func (p *migrationProgress) record(key clitext.Key, params clitext.Params) {
	if p == nil {
		return
	}
	p.steps = append(p.steps, migrationStep{key: key, params: params})
}

func pinMigrationIdentity(announcer stateAnnouncer, store *state.State, instID string, origin instanceOrigin) error {
	if err := store.PinMigration(instID); err != nil {
		return &stateMigrationRefusedError{messages: announcer.cliMessages, cause: err}
	}
	switch origin {
	case instanceGenerated:
		announcer.announce(clitext.MigrationInstanceGenerated, clitext.Params{clitext.ParamValue: instID})
	case instanceConfigured:
		announcer.announce(clitext.MigrationInstanceConfigured, clitext.Params{clitext.ParamValue: instID})
	case instanceStored:
	}
	return nil
}

type stateMigrationInterruptedError struct {
	messages *clitext.Messages
	done     []migrationStep
	backup   string
	cause    error
}

func (e *stateMigrationInterruptedError) Error() string {
	return e.text(e.messages, i18n.English)
}

func (e *stateMigrationInterruptedError) Unwrap() error { return e.cause }

func (e *stateMigrationInterruptedError) text(cliMessages *clitext.Messages, lang i18n.Language) string {
	done := renderMigrationText(cliMessages, lang, clitext.MigrationNothingDone, nil)
	if len(e.done) > 0 {
		lines := make([]string, 0, len(e.done))
		for _, step := range e.done {
			lines = append(lines, renderMigrationText(cliMessages, lang, step.key, step.params))
		}
		done = strings.Join(lines, "; ")
	}
	return renderMigrationText(cliMessages, lang, clitext.MigrationInterrupted, clitext.Params{
		clitext.ParamErr:    e.reason(cliMessages, lang),
		clitext.ParamDetail: done,
		clitext.ParamFile:   e.backup,
	})
}

func (e *stateMigrationInterruptedError) reason(cliMessages *clitext.Messages, lang i18n.Language) string {
	if refusal, ok := errors.AsType[*stateMigrationRefusedError](e.cause); ok {
		return refusal.reason(cliMessages, lang)
	}
	return e.cause.Error()
}

type storedInstanceInvalidError struct {
	path  string
	value string
}

func (e *storedInstanceInvalidError) Error() string {
	return "state " + e.path + " records the invalid instance id " + e.value
}

func (e *storedInstanceInvalidError) params() clitext.Params {
	return clitext.Params{clitext.ParamPath: e.path, clitext.ParamValue: e.value}
}

func refuseSharedWorktrees(cliMessages *clitext.Messages, statePath string, sessions []state.Session) error {
	for later := range sessions {
		for earlier := range later {
			if !placementOf(sessions[earlier]).Same(placementOf(sessions[later])) {
				continue
			}
			return &stateMigrationRefusedError{messages: cliMessages, key: clitext.MigrationSharedWorktree, params: clitext.Params{
				clitext.ParamName:   sessions[earlier].Name,
				clitext.ParamTarget: sessions[later].Name,
				clitext.ParamPath:   filepath.Clean(sessions[later].Worktree),
				clitext.ParamFile:   statePath,
			}}
		}
	}
	return nil
}

func placementOf(sess state.Session) worktree.Placement {
	return worktree.Placement{Host: sess.Host, Path: sess.Worktree}
}
