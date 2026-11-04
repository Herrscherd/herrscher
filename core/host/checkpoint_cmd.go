package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
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

func addCheckpointVerbs(store *state.State, sessions *sessionTable) []contracts.Cmd {
	return []contracts.Cmd{
		checkpointsVerb(store),
		rewindVerb(store, sessions),
		undoVerb(store, sessions),
		forkVerb(),
	}
}

func checkpointsVerb(store *state.State) contracts.Cmd {
	return contracts.New(nsSession, verbCheckpoints).
		Help("list a session's checkpoints, one before each user message, with the files each one touched").
		Param(paramName, "session name", true).
		Do(func(ctx context.Context, in contracts.Input) (string, error) {
			sess, err := checkpointSession(store, in.Get(paramName))
			if err != nil {
				return "", err
			}
			views, err := checkpointViews(ctx, sess.Worktree, sess.Name)
			if err != nil {
				return "", err
			}
			if in.JSON {
				b, err := json.Marshal(views)
				return string(b), err
			}
			return formatCheckpoints(views), nil
		})
}

func rewindVerb(store *state.State, sessions *sessionTable) contracts.Cmd {
	return contracts.New(nsSession, verbRewind).
		Help("restore a session's code, its conversation, or both to the checkpoint before a message").
		Param(paramName, "session name", true).
		ValueParam(paramTo, "checkpoint number, as listed by session checkpoints", true).
		ValueParam(paramWhat, "code, conversation or both", true).
		Do(func(ctx context.Context, in contracts.Input) (string, error) {
			sess, index, err := checkpointTarget(store, in)
			if err != nil {
				return "", err
			}
			what := in.Get(paramWhat)
			if what != whatCode && what != whatConversation && what != whatBoth {
				return "", fmt.Errorf("--what must be %s, %s or %s", whatCode, whatConversation, whatBoth)
			}
			if sessions.Busy(sess.Name) {
				return "", busyRewindError(sess.Name)
			}
			entry, err := checkpoint.Get(ctx, sess.Worktree, sess.Name, index)
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
				if _, err := checkpoint.Restore(ctx, sess.Worktree, sess.Name, index); err != nil {
					return "", err
				}
			}
			if what != whatCode {
				restarter, ok := restarterFrom(ctx)
				if !ok {
					return "", errors.New("session rewind is only available from the running daemon")
				}
				if err := restarter.RestartWithResume(sess.Name, resumeToken); err != nil {
					return "", fmt.Errorf("rewind %s: conversation not resumed: %w", sess.Name, err)
				}
			}
			return fmt.Sprintf("rewound %s to checkpoint %d (%s)", sess.Name, index, what), nil
		})
}

func undoVerb(store *state.State, sessions *sessionTable) contracts.Cmd {
	return contracts.New(nsSession, verbUndo).
		Help("undo the last rewind of a session's code").
		Param(paramName, "session name", true).
		Do(func(ctx context.Context, in contracts.Input) (string, error) {
			sess, err := checkpointSession(store, in.Get(paramName))
			if err != nil {
				return "", err
			}
			if sessions.Busy(sess.Name) {
				return "", busyRewindError(sess.Name)
			}
			if err := checkpoint.Undo(ctx, sess.Worktree, sess.Name); err != nil {
				return "", err
			}
			return "undid the last rewind of " + sess.Name, nil
		})
}

func forkVerb() contracts.Cmd {
	return contracts.New(nsSession, verbFork).
		Help("start a new session from a checkpoint, in its own worktree; the original is left as it is").
		Param(paramName, "session to fork", true).
		ValueParam(paramTo, "checkpoint number, as listed by session checkpoints", true).
		ValueParam(paramForkAs, "name of the new session (default <name>-fork)", false).
		Do(func(ctx context.Context, in contracts.Input) (string, error) {
			fork, ok := forkerFrom(ctx)
			if !ok {
				return "", errors.New("session fork is only available from the running daemon")
			}
			return fork(ctx, in)
		})
}

type forkHost interface {
	CreateCoordinated(ctx context.Context, spec contracts.CreateSession, vendor string) (string, error)
	Close(ctx context.Context, name string, force bool) (string, error)
	RestartWithResume(name, token string) error
}

func (h *hub) forkSession(ctx context.Context, input contracts.Input) (string, error) {
	return forkCheckpoint(ctx, h, h.st, input)
}

func forkCheckpoint(ctx context.Context, host forkHost, st *state.State, input contracts.Input) (string, error) {
	source, err := checkpointSession(st, input.Get(paramName))
	if err != nil {
		return "", err
	}
	index, err := checkpointIndex(input.Get(paramTo))
	if err != nil {
		return "", err
	}
	entry, err := checkpoint.Get(ctx, source.Worktree, source.Name, index)
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
	if _, err := host.CreateCoordinated(ctx, contracts.CreateSession{
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
	fork, ok := st.FindSession(forkName)
	if !ok || fork.Worktree == "" {
		rollbackFork(ctx, host, forkName)
		return "", fmt.Errorf("fork %q: the new session has no worktree", forkName)
	}
	if err := checkpoint.Materialize(ctx, fork.Worktree, entry.Commit); err != nil {
		rollbackFork(ctx, host, forkName)
		return "", fmt.Errorf("fork %q: checkpoint %d not applied: %w", forkName, index, err)
	}
	token, err := forkConversation(source, entry, fork)
	if err != nil {
		rollbackFork(ctx, host, forkName)
		return "", fmt.Errorf("fork %q: conversation not taken to checkpoint %d: %w", forkName, index, err)
	}
	if token != "" {
		if err := host.RestartWithResume(forkName, token); err != nil {
			rollbackFork(ctx, host, forkName)
			return "", fmt.Errorf("fork %q: conversation not resumed: %w", forkName, err)
		}
	}
	if input.JSON {
		b, err := json.Marshal(map[string]any{"name": forkName, "from": source.Name, "checkpoint": index})
		return string(b), err
	}
	return forkName, nil
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

func checkpointSession(store *state.State, name string) (state.Session, error) {
	sess, ok := store.FindSession(name)
	if !ok {
		return state.Session{}, &NotFoundError{Kind: kindSession, Name: name}
	}
	if !checkpointable(sess) {
		return state.Session{}, fmt.Errorf("session %q has no checkpoints: it runs in no worktree of its own", name)
	}
	return sess, nil
}

func checkpointTarget(store *state.State, in contracts.Input) (state.Session, int, error) {
	sess, err := checkpointSession(store, in.Get(paramName))
	if err != nil {
		return state.Session{}, 0, err
	}
	index, err := checkpointIndex(in.Get(paramTo))
	return sess, index, err
}

func checkpointIndex(raw string) (int, error) {
	index, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || index < 1 {
		return 0, fmt.Errorf("checkpoint number must be a positive integer, got %q", raw)
	}
	return index, nil
}

func busyRewindError(name string) error {
	return fmt.Errorf("session %q has a turn in flight: interrupt it (esc) before rewinding", name)
}

func checkpointViews(ctx context.Context, worktree, name string) ([]checkpointView, error) {
	views := make([]checkpointView, 0)
	entries, err := checkpoint.List(ctx, worktree, name)
	if err != nil || len(entries) == 0 {
		return views, err
	}
	current, err := checkpoint.Current(ctx, worktree)
	if err != nil {
		return nil, err
	}
	for i, entry := range entries {
		next := current
		if i+1 < len(entries) {
			next = entries[i+1].Commit
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

func formatCheckpoints(views []checkpointView) string {
	if len(views) == 0 {
		return "no checkpoints"
	}
	var b strings.Builder
	for _, view := range views {
		fmt.Fprintf(&b, "%d  %s  %s\n", view.Index, view.At, view.Text)
		for _, file := range view.Files {
			fmt.Fprintf(&b, "      %s  +%d -%d\n", file.Path, file.Added, file.Removed)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
