package manager

import (
	"context"
	"errors"
	"fmt"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/schedule"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/worktree"
)

func (h *Handler) sessionCloseRun(ctx context.Context, input contracts.Input) (contracts.Result, error) {
	name, ok := input.Lookup(nameParam)
	if !ok {
		return contracts.Result{}, h.localized(ErrMissingName, clitext.HostErrMissingName)
	}
	sess, exists := h.st.FindSession(name)
	if !exists {
		return contracts.Result{}, &SessionNotFoundError{Name: name}
	}
	wtRoot, repoRoot, err := h.worktreesOn(sess.Host)
	if err != nil {
		return contracts.Result{}, err
	}
	if err := h.sup.StopWithin(name, supervisorGrace); err != nil {
		return contracts.Result{}, err
	}
	repo := repoFor(repoRoot, sess.Project)
	if err := h.removeWorkspace(ctx, wtRoot, repo, sess, input.Bool("force")); err != nil {
		h.warnIfFailed(h.sup.StartWithin(sess, supervisorGrace), "start "+name)
		return contracts.Result{}, err
	}
	note := h.tidyChannel(ctx, sess)
	if err := h.persisted(func() error { return h.st.RemoveSession(name) }); err != nil {
		sess.Worktree, sess.Dir = "", repo
		h.warnIfFailed(h.sup.StartWithin(sess, supervisorGrace), "start "+name)
		return contracts.Result{}, h.labelled("persist", clitext.HostErrPersist, err)
	}
	h.warnIfFailed(h.persisted(func() error { return state.RemoveParticipantJournal(state.ParticipantsPath(h.partDir, name)) }), "remove participants of "+name)
	h.warnIfFailed(h.persisted(func() error { return state.RemoveTranscript(state.TranscriptPath(h.partDir, name)) }), "remove transcript of "+name)
	if note != "" {
		return contracts.DataResult(h.text(clitext.SessionClosed, clitext.Params{clitext.ParamName: name}) + note), nil
	}
	return contracts.NoticeResult(sessionNotice(contracts.CodeSessionClosed, name)), nil
}

func (h *Handler) removeWorkspace(ctx context.Context, worktrees Worktrees, repo string, sess state.Session, force bool) error {
	switch {
	case sess.Worktree == "":
		return nil
	case sess.Scratch:
		return worktrees.RemoveScratchAt(repo, sess.Worktree)
	}
	err := worktrees.RemoveAt(ctx, repo, sess.Worktree, force)
	if err == nil || errors.Is(err, worktree.ErrOutsideRoot) {
		return err
	}
	return newDirtyWorktreeError(sess.Name, h.keptBranch(ctx, worktrees, sess.Worktree), err)
}

func (h *Handler) keptBranch(ctx context.Context, worktrees Worktrees, path string) string {
	branch, err := worktree.SessionBranch(ctx, worktrees, path)
	if err != nil {
		h.log.Warn("worktree branch unreadable", "path", path, "err", err)
	}
	return branch
}

func (h *Handler) sessionArchiveRun(ctx context.Context, input contracts.Input) (contracts.Result, error) {
	name, ok := input.Lookup(nameParam)
	if !ok {
		return contracts.Result{}, h.localized(ErrMissingName, clitext.HostErrMissingName)
	}
	sess, exists := h.st.FindSession(name)
	if !exists {
		return contracts.Result{}, &SessionNotFoundError{Name: name}
	}
	if err := h.sup.StopWithin(name, supervisorGrace); err != nil {
		return contracts.Result{}, err
	}
	note := h.tidyChannel(ctx, sess)
	if err := h.persisted(func() error { return h.st.SetArchived(name, true) }); err != nil {
		return contracts.Result{}, h.labelled("persist", clitext.HostErrPersist, err)
	}
	return contracts.DataResult(h.text(clitext.SessionArchived, clitext.Params{clitext.ParamName: name}) + note), nil
}

func (h *Handler) sessionRenameRun(_ context.Context, input contracts.Input) (contracts.Result, error) {
	name, ok := input.Lookup(nameParam)
	if !ok {
		return contracts.Result{}, h.localized(ErrMissingName, clitext.HostErrMissingName)
	}
	raw, ok := input.Lookup("to")
	if !ok {
		return contracts.Result{}, h.localized(ErrMissingTo, clitext.HostErrMissingTo)
	}
	target := slugify(raw)
	if target == "" || !sessionNameRe.MatchString(target) {
		return contracts.Result{}, &InvalidValueError{Field: nameParam, Value: raw, Hint: "use letters, digits, - or _ (max 64, no /, spaces or ..)"}
	}
	sess, exists := h.st.FindSession(name)
	if !exists {
		return contracts.Result{}, &SessionNotFoundError{Name: name}
	}
	if target == name {
		return contracts.DataResult(h.text(clitext.SessionRenameSameName, clitext.Params{clitext.ParamName: name})), nil
	}
	if _, taken := h.st.FindSession(target); taken {
		return contracts.Result{}, &SessionExistsError{Name: target}
	}
	if err := h.sup.StopWithin(name, supervisorGrace); err != nil {
		return contracts.Result{}, err
	}
	if err := h.persisted(func() error { return h.st.RenameSession(name, target) }); err != nil {
		h.warnIfFailed(h.sup.StartWithin(sess, supervisorGrace), "start "+name)
		return contracts.Result{}, h.labelled("persist", clitext.HostErrPersist, err)
	}
	note := ""
	if err := h.persisted(func() error {
		return state.MoveSessionFile(state.TranscriptPath(h.partDir, name), state.TranscriptPath(h.partDir, target))
	}); err != nil {
		note += "\n" + h.text(clitext.SessionTranscriptLeft, clitext.Params{clitext.ParamName: quoted(name), clitext.ParamErr: err.Error()})
	}
	if err := h.persisted(func() error {
		return state.MoveSessionFile(state.ParticipantsPath(h.partDir, name), state.ParticipantsPath(h.partDir, target))
	}); err != nil {
		note += "\n" + h.text(clitext.SessionParticipantsLeft, clitext.Params{clitext.ParamName: quoted(name), clitext.ParamErr: err.Error()})
	}
	if sc := scheduleOwning(h.st.SnapshotSchedules(), name); sc != "" {
		note += "\n" + h.text(clitext.SessionScheduleOwnsSession, clitext.Params{clitext.ParamSchedule: quoted(sc), clitext.ParamName: quoted(name)})
	}
	sess.Name = target
	h.warnIfFailed(h.sup.StartWithin(sess, supervisorGrace), "start "+target)
	return contracts.DataResult(h.text(clitext.SessionRenamed, clitext.Params{clitext.ParamName: name, clitext.ParamTo: target}) + note), nil
}

func quoted(value string) string {
	return fmt.Sprintf("%#q", value)
}

func scheduleOwning(schedules []schedule.Schedule, session string) string {
	for i := range schedules {
		sched := schedules[i]
		if sched.Agent != "" && schedule.SessionName(sched) == session {
			return sched.Name
		}
	}
	return ""
}

func (h *Handler) sessionResumeRun(_ context.Context, input contracts.Input) (contracts.Result, error) {
	name, ok := input.Lookup(nameParam)
	if !ok {
		return contracts.Result{}, h.localized(ErrMissingName, clitext.HostErrMissingName)
	}
	sess, exists := h.st.FindSession(name)
	if !exists {
		return contracts.Result{}, &SessionNotFoundError{Name: name}
	}
	if err := h.persisted(func() error { return h.st.SetArchived(name, false) }); err != nil {
		return contracts.Result{}, h.labelled("persist", clitext.HostErrPersist, err)
	}
	sess.Archived = false
	h.warnIfFailed(h.sup.StartWithin(sess, supervisorGrace), "start "+name)
	return contracts.DataResult(h.text(clitext.SessionResumed, clitext.Params{clitext.ParamName: name})), nil
}
