package host

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/instanceid"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/worktree"
)

type instanceOrigin int

const (
	instanceStored instanceOrigin = iota
	instanceConfigured
	instanceGenerated
)

func chooseInstanceID(messages *hostMessages, stored, optID, ownerID string) (string, instanceOrigin, error) {
	resolved, err := instanceid.Resolve(optID, ownerID)
	switch {
	case errors.Is(err, instanceid.ErrUnconfigured):
		if stored != "" {
			return stored, instanceStored, nil
		}
		return instanceid.Generate(), instanceGenerated, nil
	case err != nil:
		return "", instanceStored, err
	case stored == "":
		return resolved, instanceConfigured, nil
	case stored != resolved:
		return "", instanceStored, &instanceIDMismatchError{messages: messages, Have: stored, Want: resolved}
	}
	return stored, instanceStored, nil
}

type stateAnnouncer struct {
	cliMessages *clitext.Messages
	lang        i18n.Language
	log         *slog.Logger
	out         io.Writer
}

func (a stateAnnouncer) announce(key clitext.Key, params clitext.Params) {
	text := renderMigrationText(a.cliMessages, a.lang, key, params)
	a.log.Info("state migration", "key", string(key), "change", text)
	logWriteFailure(fmt.Fprintln(warningWriter(a.out), text))
}

func renderMigrationText(cliMessages *clitext.Messages, lang i18n.Language, key clitext.Key, params clitext.Params) string {
	text, err := cliMessages.Text(i18n.Resolve(lang), key, clitext.Printable(params))
	if err != nil {
		return string(key)
	}
	return text
}

type migrationInputs struct {
	optID           string
	ownerID         string
	defaultGateways []string
	configWorkspace string
}

func migrateHubState(ctx context.Context, messages *hostMessages, announcer stateAnnouncer, store *state.State, inputs migrationInputs) error {
	if !store.Outdated() {
		return nil
	}
	instID, origin, err := chooseInstanceID(messages, store.InstanceID, inputs.optID, inputs.ownerID)
	if err != nil {
		return err
	}
	worktrees := worktree.NewWorktreer(announcer.cliMessages, instID)
	sessions := store.SnapshotSessions()
	plans, err := planSessionRelocations(ctx, announcer.cliMessages, worktrees, sessions)
	if err != nil {
		return err
	}
	if unbound := store.UnboundChannelSessions(); len(unbound) > 0 && len(inputs.defaultGateways) == 0 {
		return &stateMigrationRefusedError{messages: announcer.cliMessages, key: clitext.MigrationNoGateway, params: clitext.Params{clitext.ParamName: strconv.Quote(unbound[0])}}
	}
	from := store.Format
	announcer.announce(clitext.MigrationStarted, clitext.Params{clitext.ParamPath: store.Path(), clitext.ParamFrom: from, clitext.ParamTo: state.CurrentFormat, clitext.ParamFile: store.BackupPath()})
	relocated, err := relocateSessions(ctx, worktrees, announcer, sessions, plans)
	if err != nil {
		return err
	}
	changes, err := store.MigrateFormat(state.FormatMigration{InstanceID: instID, DefaultGateways: inputs.defaultGateways, ConfigWorkspace: inputs.configWorkspace, Relocated: relocated})
	if err != nil {
		return err
	}
	for _, change := range changes {
		announceFormatChange(announcer, change, origin)
	}
	announcer.announce(clitext.MigrationDone, clitext.Params{clitext.ParamPath: store.Path(), clitext.ParamTo: state.CurrentFormat})
	return nil
}

func announceFormatChange(announcer stateAnnouncer, change state.FormatChange, origin instanceOrigin) {
	switch change.Kind {
	case state.ChangeInstanceAssigned:
		var key clitext.Key = clitext.MigrationInstanceConfigured
		if origin == instanceGenerated {
			key = clitext.MigrationInstanceGenerated
		}
		announcer.announce(key, clitext.Params{clitext.ParamValue: change.Value})
	case state.ChangeWorkspaceFromRepo:
		announcer.announce(clitext.MigrationWorkspaceFromRepo, clitext.Params{clitext.ParamPath: change.Value})
	case state.ChangeRepoDropped:
		announcer.announce(clitext.MigrationRepoDropped, clitext.Params{clitext.ParamPath: change.Value})
	case state.ChangeGatewaysBound:
		announcer.announce(clitext.MigrationGatewaysBound, clitext.Params{clitext.ParamName: change.Session, clitext.ParamKind: change.Value})
	case state.ChangeSessionRelocated:
	}
}

func planSessionRelocations(ctx context.Context, cliMessages *clitext.Messages, worktrees *worktree.Worktreer, sessions []state.Session) (map[string]worktree.Relocation, error) {
	plans := make(map[string]worktree.Relocation)
	for idx := range sessions {
		sess := sessions[idx]
		if !worktree.InBareLayout(sess.Name, sess.Worktree, sess.Scratch) {
			continue
		}
		if sess.Host != "" {
			return nil, &stateMigrationRefusedError{messages: cliMessages, key: clitext.MigrationRemoteSession, params: clitext.Params{clitext.ParamName: strconv.Quote(sess.Name), clitext.ParamHost: sess.Host, clitext.ParamPath: strconv.Quote(sess.Worktree)}}
		}
		plan, err := worktrees.PlanBareRelocation(ctx, sess.Name, sess.Worktree, sess.Scratch)
		if err != nil {
			return nil, &stateMigrationRefusedError{messages: cliMessages, cause: err}
		}
		plans[sess.Name] = plan
	}
	return plans, nil
}

func relocateSessions(ctx context.Context, worktrees *worktree.Worktreer, announcer stateAnnouncer, sessions []state.Session, plans map[string]worktree.Relocation) (map[string]state.Relocation, error) {
	relocated := make(map[string]state.Relocation, len(plans))
	for idx := range sessions {
		sess := sessions[idx]
		plan, ok := plans[sess.Name]
		if !ok {
			continue
		}
		if err := worktrees.Relocate(ctx, plan); err != nil {
			return nil, &stateMigrationRefusedError{messages: announcer.cliMessages, cause: err}
		}
		announcer.announce(clitext.MigrationWorktreeRelocated, clitext.Params{clitext.ParamName: sess.Name, clitext.ParamFrom: plan.FromPath, clitext.ParamTo: plan.ToPath})
		if plan.ToBranch != "" {
			announcer.announce(clitext.MigrationBranchRenamed, clitext.Params{clitext.ParamName: sess.Name, clitext.ParamFrom: plan.FromBranch, clitext.ParamTo: plan.ToBranch})
		}
		relocated[sess.Name] = state.Relocation{Worktree: plan.ToPath, Dir: retargetDir(sess.Dir, plan.FromPath, plan.ToPath)}
	}
	return relocated, nil
}

func retargetDir(dir, oldRoot, newRoot string) string {
	if dir == oldRoot {
		return newRoot
	}
	rest, ok := strings.CutPrefix(dir, oldRoot+string(filepath.Separator))
	if !ok {
		return dir
	}
	return filepath.Join(newRoot, rest)
}

type stateMigrationRefusedError struct {
	messages *clitext.Messages
	key      clitext.Key
	params   clitext.Params
	cause    error
}

func (e *stateMigrationRefusedError) Error() string {
	return e.text(e.messages, i18n.English)
}

func (e *stateMigrationRefusedError) Unwrap() error { return e.cause }

func (e *stateMigrationRefusedError) text(cliMessages *clitext.Messages, lang i18n.Language) string {
	return renderMigrationText(cliMessages, lang, clitext.MigrationRefused, clitext.Params{clitext.ParamErr: e.reason(cliMessages, lang)})
}

func (e *stateMigrationRefusedError) reason(cliMessages *clitext.Messages, lang i18n.Language) string {
	if failure, ok := errors.AsType[worktree.FailureError](e.cause); ok {
		return failure.Text(lang)
	}
	if e.cause != nil {
		return e.cause.Error()
	}
	return renderMigrationText(cliMessages, lang, e.key, e.params)
}

func stateFormatText(cliMessages *clitext.Messages, lang i18n.Language, err error) (string, bool) {
	if refusal, ok := errors.AsType[*stateMigrationRefusedError](err); ok {
		return refusal.text(cliMessages, lang), true
	}
	if newer, ok := errors.AsType[*state.NewerFormatError](err); ok {
		return renderMigrationText(cliMessages, lang, clitext.MigrationNewerFormat, clitext.Params{clitext.ParamPath: newer.Path, clitext.ParamHave: newer.Found, clitext.ParamWant: newer.Supported}), true
	}
	if pending, ok := errors.AsType[*statePendingMigrationError](err); ok {
		return renderMigrationText(cliMessages, lang, clitext.MigrationPending, clitext.Params{clitext.ParamPath: pending.path}), true
	}
	return "", false
}

func localizeStateFormatError(cliMessages *clitext.Messages, lang i18n.Language, err error) error {
	text, ok := stateFormatText(cliMessages, lang, err)
	if !ok {
		return err
	}
	return localizedHostError{text: text, cause: err}
}

type statePendingMigrationError struct {
	path string
}

func (e *statePendingMigrationError) Error() string {
	return "state " + e.path + " has an old format: run herrscher serve once to migrate it"
}

func resolveInstanceID(messages *hostMessages, announcer stateAnnouncer, hostState *state.State, optID, ownerID string) (string, error) {
	instID, origin, err := chooseInstanceID(messages, hostState.InstanceID, optID, ownerID)
	if err != nil || origin == instanceStored {
		return instID, err
	}
	if err := hostState.SetInstanceID(instID); err != nil {
		return "", wrapHost(messages, err, "serve.instance_persist", nil)
	}
	if origin == instanceGenerated {
		announcer.announce(clitext.InstanceGenerated, clitext.Params{clitext.ParamValue: instID})
	}
	return instID, nil
}
