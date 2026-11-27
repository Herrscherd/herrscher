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

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/instanceid"
	"github.com/Herrscherd/herrscher/core/internal/shellquote"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/worktree"
)

type instanceOrigin int

const (
	instanceStored instanceOrigin = iota
	instanceConfigured
	instanceGenerated
)

func chooseInstanceID(messages *hostMessages, statePath, stored, optID, ownerID string) (string, instanceOrigin, error) {
	if stored != "" && !instanceid.Validate(stored) {
		return "", instanceStored, &storedInstanceInvalidError{path: statePath, value: stored}
	}
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
	progress    *migrationProgress
}

func (a stateAnnouncer) announce(key clitext.Key, params clitext.Params) {
	a.progress.record(key, params)
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
	forge           contracts.ForgeCredentials
}

func migrateHubState(ctx context.Context, messages *hostMessages, announcer stateAnnouncer, store *state.State, inputs migrationInputs) error {
	if !store.Outdated() {
		return nil
	}
	instID, origin, err := chooseInstanceID(messages, store.Path(), store.InstanceID, inputs.optID, inputs.ownerID)
	if err != nil {
		return migrationCheckRefusal(announcer.cliMessages, err)
	}
	migration := state.FormatMigration{InstanceID: instID, DefaultGateways: inputs.defaultGateways, ConfigWorkspace: inputs.configWorkspace}
	if checkErr := store.CheckMigration(migration); checkErr != nil {
		return migrationCheckRefusal(announcer.cliMessages, checkErr)
	}
	worktrees := worktree.NewWorktreer(announcer.cliMessages, instID)
	sessions := store.SnapshotSessions()
	if sharedErr := refuseSharedWorktrees(announcer.cliMessages, store.Path(), sessions); sharedErr != nil {
		return sharedErr
	}
	plans, err := planSessionRelocations(ctx, announcer.cliMessages, worktrees, sessions, inputs.forge)
	if err != nil {
		return err
	}
	rekeys, err := planCheckpointRekeys(ctx, announcer.cliMessages, sessions)
	if err != nil {
		return err
	}
	backup, err := store.Backup()
	if err != nil {
		return &stateMigrationRefusedError{messages: announcer.cliMessages, key: clitext.MigrationBackupFailed, params: clitext.Params{clitext.ParamPath: store.Path(), clitext.ParamErr: err.Error()}, cause: err}
	}
	announcer.announce(clitext.MigrationStarted, clitext.Params{clitext.ParamPath: store.Path(), clitext.ParamFrom: store.Format, clitext.ParamTo: state.CurrentFormat, clitext.ParamFile: backup})
	if pinErr := pinMigrationIdentity(announcer, store, instID, origin); pinErr != nil {
		return pinErr
	}
	announcer.progress = &migrationProgress{}
	if err := carryOutMigration(ctx, worktrees, announcer, store, migration, sessions, plans, rekeys); err != nil {
		return &stateMigrationInterruptedError{messages: announcer.cliMessages, done: announcer.progress.steps, backup: backup, cause: err}
	}
	announcer.announce(clitext.MigrationDone, clitext.Params{clitext.ParamPath: store.Path(), clitext.ParamTo: state.CurrentFormat})
	return nil
}

func carryOutMigration(ctx context.Context, worktrees *worktree.Worktreer, announcer stateAnnouncer, store *state.State, migration state.FormatMigration, sessions []state.Session, plans map[string]sessionRelocation, rekeys []checkpointRekey) error {
	if err := rekeyCheckpoints(ctx, announcer, rekeys); err != nil {
		return err
	}
	relocated, err := relocateSessions(ctx, worktrees, announcer, sessions, plans)
	if err != nil {
		return err
	}
	migration.Relocated = relocated
	changes, err := store.MigrateFormat(migration)
	if err != nil {
		return err
	}
	for _, change := range changes {
		announceFormatChange(announcer, change)
	}
	return nil
}

func migrationCheckRefusal(cliMessages *clitext.Messages, err error) error {
	if unbound, ok := errors.AsType[*state.NoGatewayToBindError](err); ok {
		return &stateMigrationRefusedError{messages: cliMessages, key: clitext.MigrationNoGateway, params: clitext.Params{clitext.ParamName: strconv.Quote(unbound.Session)}}
	}
	if invalid, ok := errors.AsType[*storedInstanceInvalidError](err); ok {
		return &stateMigrationRefusedError{messages: cliMessages, key: clitext.InstanceStoredInvalid, params: invalid.params(), cause: err}
	}
	return err
}

func announceFormatChange(announcer stateAnnouncer, change state.FormatChange) {
	switch change.Kind {
	case state.ChangeInstanceAssigned:
		announcer.announce(clitext.MigrationInstanceConfigured, clitext.Params{clitext.ParamValue: change.Value})
	case state.ChangeWorkspaceFromRepo:
		announcer.announce(clitext.MigrationWorkspaceFromRepo, clitext.Params{clitext.ParamPath: change.Value})
	case state.ChangeRepoDropped:
		announcer.announce(clitext.MigrationRepoDropped, clitext.Params{clitext.ParamPath: change.Value})
	case state.ChangeGatewaysBound:
		announcer.announce(clitext.MigrationGatewaysBound, clitext.Params{clitext.ParamName: change.Session, clitext.ParamKind: change.Value})
	}
}

type sessionRelocation struct {
	plan   worktree.Relocation
	remote bool
}

func planSessionRelocations(ctx context.Context, cliMessages *clitext.Messages, worktrees *worktree.Worktreer, sessions []state.Session, creds contracts.ForgeCredentials) (map[string]sessionRelocation, error) {
	plans := make(map[string]sessionRelocation)
	for idx := range sessions {
		sess := sessions[idx]
		if !worktree.InBareLayout(sess.Worktree, sess.Scratch) {
			continue
		}
		bare := worktree.BareSession{Name: sess.Name, Path: sess.Worktree, Scratch: sess.Scratch, PullRequest: sess.PullRequest}
		plan, err := worktrees.BareTarget(bare)
		if err == nil && sess.Host == "" {
			plan, err = planLocalBareRelocation(ctx, worktrees, creds, plan.Repo, bare, sessionPRForgeLimit)
		}
		if hidden, ok := errors.AsType[*worktree.HiddenRepositoryError](err); ok {
			return nil, repositoryDiscoveryRefusal(cliMessages, sess.Name, hidden.Dir, err)
		}
		if err != nil {
			return nil, &stateMigrationRefusedError{messages: cliMessages, cause: err}
		}
		plans[sess.Name] = sessionRelocation{plan: plan, remote: sess.Host != ""}
	}
	return plans, nil
}

func relocateSessions(ctx context.Context, worktrees *worktree.Worktreer, announcer stateAnnouncer, sessions []state.Session, plans map[string]sessionRelocation) (map[string]state.Relocation, error) {
	relocated := make(map[string]state.Relocation, len(plans))
	for idx := range sessions {
		sess := sessions[idx]
		planned, ok := plans[sess.Name]
		if !ok {
			continue
		}
		plan := planned.plan
		if planned.remote {
			announceRemoteRelocation(announcer, sess, plan)
		} else if err := relocateLocal(ctx, worktrees, announcer, sess, plan); err != nil {
			return nil, err
		}
		if !planned.remote && !plan.DirAtTarget() {
			continue
		}
		moved := state.Relocation{Worktree: plan.ToPath, Dir: retargetDir(sess.Dir, plan.FromPath, plan.ToPath)}
		relocated[sess.Name] = moved
		if sess.ResumeToken != "" && (planned.remote || plan.MovesDir()) {
			announcer.announce(clitext.MigrationConversationReset, clitext.Params{clitext.ParamName: sess.Name, clitext.ParamPath: moved.Dir})
		}
	}
	return relocated, nil
}

func relocateLocal(ctx context.Context, worktrees *worktree.Worktreer, announcer stateAnnouncer, sess state.Session, plan worktree.Relocation) error {
	if err := worktrees.Relocate(ctx, plan); err != nil {
		return &stateMigrationRefusedError{messages: announcer.cliMessages, cause: err}
	}
	if plan.MovesDir() {
		announcer.announce(clitext.MigrationWorktreeRelocated, clitext.Params{clitext.ParamName: sess.Name, clitext.ParamFrom: plan.FromPath, clitext.ParamTo: plan.ToPath})
	}
	if plan.RenamesBranch() {
		announcer.announce(clitext.MigrationBranchRenamed, clitext.Params{clitext.ParamName: sess.Name, clitext.ParamFrom: plan.FromBranch, clitext.ParamTo: plan.ToBranch})
	}
	return nil
}

func announceRemoteRelocation(announcer stateAnnouncer, sess state.Session, plan worktree.Relocation) {
	quote := shellquote.Quote
	params := clitext.Params{clitext.ParamName: sess.Name, clitext.ParamHost: sess.Host, clitext.ParamFrom: quote(plan.FromPath), clitext.ParamTo: quote(plan.ToPath), clitext.ParamWhere: quote(filepath.Dir(plan.ToPath))}
	if plan.Scratch {
		announcer.announce(clitext.MigrationRemoteScratch, params)
		return
	}
	params[clitext.ParamRepo] = quote(plan.Repo)
	params[clitext.ParamBranch] = quote(plan.FromBranch)
	params[clitext.ParamTarget] = quote(plan.ToBranch)
	announcer.announce(clitext.MigrationRemoteWorktree, params)
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
	if e.key != "" {
		return renderMigrationText(cliMessages, lang, e.key, e.params)
	}
	if failure, ok := errors.AsType[worktree.FailureError](e.cause); ok {
		return failure.Text(lang)
	}
	return e.cause.Error()
}

func stateFormatText(cliMessages *clitext.Messages, lang i18n.Language, err error) (string, bool) {
	if interrupted, ok := errors.AsType[*stateMigrationInterruptedError](err); ok {
		return interrupted.text(cliMessages, lang), true
	}
	if refusal, ok := errors.AsType[*stateMigrationRefusedError](err); ok {
		return refusal.text(cliMessages, lang), true
	}
	if invalid, ok := errors.AsType[*storedInstanceInvalidError](err); ok {
		return renderMigrationText(cliMessages, lang, clitext.InstanceStoredInvalid, invalid.params()), true
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
	instID, origin, err := chooseInstanceID(messages, hostState.Path(), hostState.InstanceID, optID, ownerID)
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
