package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/Herrscherd/herrscher/core/cli"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/checkpoint"
	"github.com/Herrscherd/herrscher/core/internal/conversation"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const (
	verbCheckpoints   = "checkpoints"
	verbRewind        = "rewind"
	verbUndo          = "undo"
	verbFork          = "fork"
	paramTo           = "to"
	paramWhat         = "what"
	paramForkAs       = "as"
	whatCode          = "code"
	whatConversation  = "conversation"
	whatBoth          = "both"
	noConversation    = "none"
	forkSuffix        = "-fork"
	checkpointTimeFmt = "15:04"
)

var (
	errConversationRestore = errors.New("conversation restore is not available at this checkpoint: " +
		"its backend keeps no message position to return to; nothing was changed")
	errConversationFork = errors.New("fork refused: the conversation cannot be taken to this checkpoint " +
		"(its backend keeps no message position); nothing was created")
	errWhatInvalid       = errors.New("--what must be code, conversation or both")
	errRewindNeedsDaemon = errors.New("session rewind is only available from the running daemon")
	errForkNeedsDaemon   = errors.New("session fork is only available from the running daemon")
	errTurnInFlight      = errors.New("has a turn in flight: interrupt it (esc) before rewinding")
	errCheckpointNumber  = errors.New("checkpoint number must be a positive integer")
	errNotCheckpointable = errors.New("has no checkpoints: it runs in no worktree of its own")
	errForkNoWorktree    = errors.New("the new session has no worktree")
)

type sessionRestarter interface {
	RestartWithResume(name, token string) error
}

type restarterKey struct{}

func withRestarter(ctx context.Context, restarter sessionRestarter) context.Context {
	return context.WithValue(ctx, restarterKey{}, restarter)
}

func restarterFrom(ctx context.Context) (sessionRestarter, bool) {
	restarter, ok := ctx.Value(restarterKey{}).(sessionRestarter)
	return restarter, ok && restarter != nil
}

type forkFunc func(context.Context, contracts.Input) (string, error)

type forkerKey struct{}

func withForker(ctx context.Context, fork forkFunc) context.Context {
	return context.WithValue(ctx, forkerKey{}, fork)
}

func forkerFrom(ctx context.Context) (forkFunc, bool) {
	fork, ok := ctx.Value(forkerKey{}).(forkFunc)
	return fork, ok && fork != nil
}

func isSessionForkCommand(args []string) bool {
	return len(args) >= 2 && args[0] == nsSession && args[1] == verbFork
}

type checkpointFile struct {
	Path    string `json:"path"`
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
}

type checkpointView struct {
	Index        int              `json:"index"`
	At           string           `json:"at"`
	Turn         string           `json:"turn"`
	Text         string           `json:"text"`
	Conversation string           `json:"conversation"`
	Files        []checkpointFile `json:"files"`
}

func addCheckpointVerbs(lang languageSource, sessionStore *state.State, sessions *sessionTable) []contracts.Cmd {
	return []contracts.Cmd{
		checkpointsVerb(lang, sessionStore, &sessions.checkpoints),
		rewindVerb(lang, sessionStore, sessions),
		undoVerb(lang, sessionStore, sessions),
		forkVerb(),
	}
}

func checkpointsVerb(lang languageSource, sessionStore *state.State, checkpoints *checkpoint.Store) contracts.Cmd {
	return contracts.New(nsSession, verbCheckpoints).
		Help(clitext.CmdHelpSessionCheckpoints).
		Param(paramName, clitext.CmdParamSessionCheckpointsName, true).
		Do(func(ctx context.Context, input contracts.Input) (contracts.Result, error) {
			sess, err := checkpointSession(sessionStore, input.Get(paramName))
			if err != nil {
				return contracts.Result{}, err
			}
			views, err := checkpointViews(ctx, checkpoints, sess.Worktree, sess.Name)
			if err != nil {
				return contracts.Result{}, err
			}
			if input.JSON {
				return cli.Data(marshalJSON(views))
			}
			return contracts.DataResult(formatCheckpoints(namedSessionLanguage(sessionStore, sess.Name, lang()), views)), nil
		})
}

func marshalJSON[T any](value T) (string, error) {
	payload, err := json.Marshal(value)
	return string(payload), err
}

func rewindVerb(lang languageSource, sessionStore *state.State, sessions *sessionTable) contracts.Cmd {
	return contracts.New(nsSession, verbRewind).
		Help(clitext.CmdHelpSessionRewind).
		Param(paramName, clitext.CmdParamSessionRewindName, true).
		ValueParam(paramTo, clitext.CmdParamSessionRewindTo, true).
		ValueParam(paramWhat, clitext.CmdParamSessionRewindWhat, true).
		Do(func(ctx context.Context, input contracts.Input) (contracts.Result, error) {
			return cli.Data(rewindSession(ctx, lang(), sessionStore, sessions, input))
		})
}

func rewindSession(ctx context.Context, lang i18n.Language, sessionStore *state.State, sessions *sessionTable, input contracts.Input) (string, error) {
	sess, position, err := checkpointTarget(sessionStore, input)
	if err != nil {
		return "", err
	}
	what := input.Get(paramWhat)
	if what != whatCode && what != whatConversation && what != whatBoth {
		return "", errWhatInvalid
	}
	release, ok := sessions.claimIdle(sess.Name)
	if !ok {
		return "", busyRewindError(sess.Name)
	}
	defer release()
	entry, err := sessions.checkpoints.Get(ctx, sess.Worktree, sess.Name, position)
	if err != nil {
		return "", err
	}
	resumeToken := ""
	if what != whatCode {
		if resumeToken, err = forkConversation(sess, entry, sess); err != nil {
			return "", err
		}
	}
	if what != whatConversation {
		if _, err = sessions.checkpoints.Restore(ctx, sess.Worktree, sess.Name, position); err != nil {
			return "", err
		}
	}
	if what != whatCode {
		if err := restartWithConversation(ctx, sess.Name, resumeToken); err != nil {
			return "", err
		}
	}
	params := clitext.Params{clitext.ParamName: sess.Name, clitext.ParamPosition: strconv.Itoa(position), clitext.ParamWhat: what}
	return replyText(namedSessionLanguage(sessionStore, sess.Name, lang)).Text(clitext.HostCheckpointRewound, params), nil
}

func restartWithConversation(ctx context.Context, name, token string) error {
	restarter, ok := restarterFrom(ctx)
	if !ok {
		return errRewindNeedsDaemon
	}
	if err := restarter.RestartWithResume(name, token); err != nil {
		return fmt.Errorf("rewind %s: conversation not resumed: %w", name, err)
	}
	return nil
}

func undoVerb(lang languageSource, sessionStore *state.State, sessions *sessionTable) contracts.Cmd {
	return contracts.New(nsSession, verbUndo).
		Help(clitext.CmdHelpSessionUndo).
		Param(paramName, clitext.CmdParamSessionUndoName, true).
		Do(func(ctx context.Context, input contracts.Input) (contracts.Result, error) {
			sess, err := checkpointSession(sessionStore, input.Get(paramName))
			if err != nil {
				return contracts.Result{}, err
			}
			release, ok := sessions.claimIdle(sess.Name)
			if !ok {
				return contracts.Result{}, busyRewindError(sess.Name)
			}
			defer release()
			if err := sessions.checkpoints.Undo(ctx, sess.Worktree, sess.Name); err != nil {
				return contracts.Result{}, err
			}
			return contracts.DataResult(replyText(namedSessionLanguage(sessionStore, sess.Name, lang())).Text(clitext.HostCheckpointUndone, clitext.Params{clitext.ParamName: sess.Name})), nil
		})
}

func forkVerb() contracts.Cmd {
	return contracts.New(nsSession, verbFork).
		Help(clitext.CmdHelpSessionFork).
		Param(paramName, clitext.CmdParamSessionForkName, true).
		ValueParam(paramTo, clitext.CmdParamSessionForkTo, true).
		ValueParam(paramForkAs, clitext.CmdParamSessionForkAs, false).
		Do(func(ctx context.Context, input contracts.Input) (contracts.Result, error) {
			fork, ok := forkerFrom(ctx)
			if !ok {
				return contracts.Result{}, errForkNeedsDaemon
			}
			return cli.Data(fork(ctx, input))
		})
}

type forkHost interface {
	CreateCoordinated(ctx context.Context, spec contracts.CreateSession, vendor string) (contracts.Result, error)
	Close(ctx context.Context, name string, force bool) (contracts.Result, error)
	RestartWithResume(name, token string) error
}

func (h *hub) forkSession(ctx context.Context, input contracts.Input) (string, error) {
	return forkCheckpoint(ctx, h, h.st, &h.sessions.checkpoints, input)
}

func forkCheckpoint(ctx context.Context, host forkHost, sessionStore *state.State, checkpoints *checkpoint.Store, input contracts.Input) (string, error) {
	source, err := checkpointSession(sessionStore, input.Get(paramName))
	if err != nil {
		return "", err
	}
	index, err := checkpointIndex(input.Get(paramTo))
	if err != nil {
		return "", err
	}
	entry, err := checkpoints.Get(ctx, source.Worktree, source.Name, index)
	if err != nil {
		return "", err
	}
	if !conversationRestorable(entry) {
		return "", errConversationFork
	}
	head, err := checkpoint.Head(ctx, source.Worktree)
	if err != nil {
		return "", err
	}
	forkName := input.Get(paramForkAs)
	if forkName == "" {
		forkName = source.Name + forkSuffix
	}
	if _, err = host.CreateCoordinated(ctx, contracts.CreateSession{
		Name:         forkName,
		Project:      source.Project,
		Cmd:          source.Cmd,
		Backend:      source.Backend,
		Agent:        source.Agent,
		Base:         head,
		TerminalOnly: true,
	}, source.Vendor); err != nil {
		return "", fmt.Errorf("fork %q: %w", source.Name, err)
	}
	fork, ok := sessionStore.FindSession(forkName)
	if !ok || fork.Worktree == "" {
		rollbackFork(ctx, host, forkName)
		return "", fmt.Errorf("fork %q: %w", forkName, errForkNoWorktree)
	}
	if err = checkpoints.Materialize(ctx, fork.Worktree, entry.Commit); err != nil {
		rollbackFork(ctx, host, forkName)
		return "", fmt.Errorf("fork %q: checkpoint %d not applied: %w", forkName, index, err)
	}
	token, err := forkConversation(source, entry, fork)
	if err != nil {
		rollbackFork(ctx, host, forkName)
		return "", fmt.Errorf("fork %q: conversation not taken to checkpoint %d: %w", forkName, index, err)
	}
	if token != "" {
		if err = host.RestartWithResume(forkName, token); err != nil {
			rollbackFork(ctx, host, forkName)
			return "", fmt.Errorf("fork %q: conversation not resumed: %w", forkName, err)
		}
	}
	if input.JSON {
		return marshalJSON(map[string]any{"name": forkName, "from": source.Name, "checkpoint": index})
	}
	return forkName, nil
}

func (h *hub) takeCheckpoint(ctx context.Context, sess state.Session, turnID, text string) {
	if !checkpointable(sess) {
		return
	}
	current, ok := h.st.FindSession(sess.Name)
	if !ok {
		current = sess
	}
	meta := checkpoint.Meta{Turn: turnID, Text: text, Conversation: conversationPosition(current)}
	if _, err := h.sessions.checkpoints.Take(context.WithoutCancel(ctx), sess.Worktree, sess.Name, meta); err != nil {
		warnStderr("herrscher serve: session %q: checkpoint not taken, /rewind cannot return to this message: %v\n", sess.Name, err)
	}
}

func rollbackFork(ctx context.Context, host forkHost, name string) {
	_, err := host.Close(context.WithoutCancel(ctx), name, true)
	logCleanup(err)
}

func conversationRestorable(entry checkpoint.Entry) bool {
	return entry.Conversation != "" && entry.Conversation != noConversation
}

func conversationPosition(sess state.Session) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return noConversation
	}
	position, err := conversation.Position(home, backendDir(sess), sess.ResumeToken)
	if err != nil {
		return noConversation
	}
	return position
}

func forkConversation(source state.Session, entry checkpoint.Entry, target state.Session) (string, error) {
	if !conversationRestorable(entry) {
		return "", errConversationRestore
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	token, err := conversation.NewToken()
	if err != nil {
		return "", err
	}
	return conversation.Fork(home, backendDir(source), source.ResumeToken, entry.Conversation, backendDir(target), token)
}

func backendDir(sess state.Session) string {
	dir := sess.Dir
	if dir == "" {
		dir = sess.Worktree
	}
	return resolveBackendDir(dir)
}

func checkpointable(sess state.Session) bool {
	return sess.Worktree != "" && !sess.Scratch && sess.Host == ""
}

func checkpointSession(sessionStore *state.State, name string) (state.Session, error) {
	sess, ok := sessionStore.FindSession(name)
	if !ok {
		return state.Session{}, &NotFoundError{Kind: kindSession, Name: name}
	}
	if !checkpointable(sess) {
		return state.Session{}, fmt.Errorf("session %q %w", name, errNotCheckpointable)
	}
	return sess, nil
}

func checkpointTarget(sessionStore *state.State, input contracts.Input) (state.Session, int, error) {
	sess, err := checkpointSession(sessionStore, input.Get(paramName))
	if err != nil {
		return state.Session{}, 0, err
	}
	index, err := checkpointIndex(input.Get(paramTo))
	return sess, index, err
}

func checkpointIndex(raw string) (int, error) {
	index, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || index < 1 {
		return 0, fmt.Errorf("%w, got %q", errCheckpointNumber, raw)
	}
	return index, nil
}

func busyRewindError(name string) error {
	return fmt.Errorf("session %q %w", name, errTurnInFlight)
}

func checkpointViews(ctx context.Context, checkpoints *checkpoint.Store, worktree, name string) ([]checkpointView, error) {
	views := make([]checkpointView, 0)
	entries, err := checkpoints.List(ctx, worktree, name)
	if err != nil || len(entries) == 0 {
		return views, err
	}
	current, err := checkpoints.Current(ctx, worktree)
	if err != nil {
		return nil, err
	}
	for position, entry := range entries {
		next := current
		if position+1 < len(entries) {
			next = entries[position+1].Commit
		}
		changes, err := checkpoint.Changes(ctx, worktree, entry.Commit, next)
		if err != nil {
			return nil, err
		}
		files := make([]checkpointFile, 0, len(changes))
		for _, change := range changes {
			files = append(files, checkpointFile{Path: change.Path, Added: change.Added, Removed: change.Removed})
		}
		views = append(views, checkpointView{
			Index:        entry.Index,
			At:           entry.At.Local().Format(checkpointTimeFmt),
			Turn:         entry.Turn,
			Text:         entry.Text,
			Conversation: entry.Conversation,
			Files:        files,
		})
	}
	return views, nil
}

func formatCheckpoints(lang i18n.Language, views []checkpointView) string {
	if len(views) == 0 {
		return replyText(lang).Text(clitext.HostCheckpointNone, nil)
	}
	var text strings.Builder
	for _, view := range views {
		fmt.Fprintf(&text, "%d  %s  %s\n", view.Index, view.At, view.Text)
		for _, file := range view.Files {
			fmt.Fprintf(&text, "      %s  +%d -%d\n", file.Path, file.Added, file.Removed)
		}
	}
	return strings.TrimRight(text.String(), "\n")
}
