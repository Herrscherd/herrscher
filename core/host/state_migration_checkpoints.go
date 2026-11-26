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
		if !checkpointable(sess) || worktreeGone(sess.Worktree) {
			continue
		}
		outside, err := worktree.OutsideAnyRepository(ctx, sess.Worktree)
		if err != nil {
			return nil, &stateMigrationRefusedError{messages: cliMessages, key: clitext.MigrationCheckpointInspect, params: checkpointFailureParams(sess, err), cause: err}
		}
		if outside {
			continue
		}
		plan, err := checkpoint.PlanRekey(ctx, sess.Worktree, sess.Name, sess.Incarnation)
		if conflict, ok := errors.AsType[*checkpoint.RekeyConflictError](err); ok {
			return nil, &stateMigrationRefusedError{messages: cliMessages, key: clitext.MigrationCheckpointConflict, params: clitext.Params{
				clitext.ParamName: strconv.Quote(sess.Name), clitext.ParamRef: conflict.Ref, clitext.ParamHave: conflict.Have, clitext.ParamWant: conflict.Want, clitext.ParamPath: sess.Worktree,
			}, cause: err}
		}
		if err != nil {
			return nil, &stateMigrationRefusedError{messages: cliMessages, key: clitext.MigrationCheckpointInspect, params: checkpointFailureParams(sess, err), cause: err}
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
			sess := state.Session{Name: rekey.session, Worktree: rekey.plan.Dir}
			return &stateMigrationRefusedError{messages: announcer.cliMessages, key: clitext.MigrationCheckpointMove, params: checkpointFailureParams(sess, err), cause: err}
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

func checkpointFailureParams(sess state.Session, err error) clitext.Params {
	return clitext.Params{clitext.ParamName: strconv.Quote(sess.Name), clitext.ParamPath: sess.Worktree, clitext.ParamErr: err.Error()}
}

func worktreeGone(path string) bool {
	_, err := os.Stat(path)
	return errors.Is(err, fs.ErrNotExist)
}
