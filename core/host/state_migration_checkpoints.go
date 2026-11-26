package host

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"strconv"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/checkpoint"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/worktree"
)

type checkpointRekey struct {
	session string
	key     string
	plan    checkpoint.Rekey
}

func planCheckpointRekeys(ctx context.Context, cliMessages *clitext.Messages, sessions []state.Session) ([]checkpointRekey, error) {
	var rekeys []checkpointRekey
	for idx := range sessions {
		sess := sessions[idx]
		if !checkpointable(sess) {
			continue
		}
		dir, found := checkpointRefsHolder(sess.Worktree)
		if !found {
			continue
		}
		outside, err := worktree.OutsideAnyRepository(ctx, dir)
		if err != nil {
			return nil, repositoryDiscoveryRefusal(cliMessages, sess.Name, dir, err)
		}
		if outside {
			continue
		}
		plan, err := checkpoint.PlanRekey(ctx, dir, sess.Name, sess.Incarnation)
		if conflict, ok := errors.AsType[*checkpoint.RekeyConflictError](err); ok {
			return nil, &stateMigrationRefusedError{messages: cliMessages, key: clitext.MigrationCheckpointConflict, params: clitext.Params{
				clitext.ParamName: strconv.Quote(sess.Name), clitext.ParamRef: conflict.Ref, clitext.ParamHave: conflict.Have, clitext.ParamWant: conflict.Want, clitext.ParamPath: dir,
			}, cause: err}
		}
		if err != nil {
			return nil, &stateMigrationRefusedError{messages: cliMessages, key: clitext.MigrationCheckpointInspect, params: checkpointFailureParams(sess.Name, dir, err), cause: err}
		}
		if !plan.Empty() {
			rekeys = append(rekeys, checkpointRekey{session: sess.Name, key: sess.Incarnation, plan: plan})
		}
	}
	return rekeys, nil
}

func rekeyCheckpoints(ctx context.Context, announcer stateAnnouncer, rekeys []checkpointRekey) error {
	var store checkpoint.Store
	for _, rekey := range rekeys {
		if err := store.ApplyRekey(ctx, rekey.plan); err != nil {
			return &stateMigrationRefusedError{messages: announcer.cliMessages, key: clitext.MigrationCheckpointMove, params: checkpointFailureParams(rekey.session, rekey.plan.Dir, err), cause: err}
		}
		announceRekey(announcer, rekey)
	}
	return nil
}

func announceRekey(announcer stateAnnouncer, rekey checkpointRekey) {
	tally := rekey.plan.Tally()
	if tally.Checkpoints > 0 {
		announcer.announce(clitext.MigrationCheckpointsRekeyed, clitext.Params{
			clitext.ParamName:  rekey.session,
			clitext.ParamCount: tally.Checkpoints,
			clitext.ParamFrom:  checkpoint.RefsOf(rekey.session),
			clitext.ParamTo:    checkpoint.RefsOf(rekey.key),
		})
	}
	if tally.Undo {
		announcer.announce(clitext.MigrationUndoRekeyed, clitext.Params{
			clitext.ParamName: rekey.session,
			clitext.ParamFrom: checkpoint.UndoRefOf(rekey.session),
			clitext.ParamTo:   checkpoint.UndoRefOf(rekey.key),
		})
	}
	if tally.Finished > 0 {
		announcer.announce(clitext.MigrationRekeyFinished, clitext.Params{
			clitext.ParamName:  rekey.session,
			clitext.ParamCount: tally.Finished,
		})
	}
}

func repositoryDiscoveryRefusal(cliMessages *clitext.Messages, name, dir string, err error) error {
	hidden, ok := errors.AsType[*worktree.HiddenRepositoryError](err)
	if !ok {
		return &stateMigrationRefusedError{messages: cliMessages, key: clitext.MigrationCheckpointInspect, params: checkpointFailureParams(name, dir, err), cause: err}
	}
	params := checkpointFailureParams(name, dir, hidden.Err)
	params[clitext.ParamRepo] = hidden.Entry
	return &stateMigrationRefusedError{messages: cliMessages, key: clitext.MigrationCheckpointHidden, params: params, cause: err}
}

func checkpointFailureParams(name, dir string, err error) clitext.Params {
	return clitext.Params{clitext.ParamName: strconv.Quote(name), clitext.ParamPath: dir, clitext.ParamErr: err.Error()}
}

func checkpointRefsHolder(worktreePath string) (string, bool) {
	if !worktreeGone(worktreePath) {
		return worktreePath, true
	}
	repo, ok := worktree.LayoutRepository(worktreePath)
	if !ok || worktreeGone(repo) {
		return "", false
	}
	return repo, true
}

func worktreeGone(path string) bool {
	_, err := os.Stat(path)
	return errors.Is(err, fs.ErrNotExist)
}
