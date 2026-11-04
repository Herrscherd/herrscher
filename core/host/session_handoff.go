package host

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const (
	verbHandoff         = "handoff"
	paramUnder          = "under"
	paramChannel        = "channel_id"
	paramParent         = "parent"
	handoffBriefTimeout = "10m"
	handoffAgentPerHour = 3
	handoffWindow       = time.Hour
	handoffBriefTask    = "Rédige le brief de passation de cette conversation, pour une session qui n'a pas ton historique : " +
		"ce qui est construit, ce qui est décidé, ce qui reste ouvert (les questions que l'opérateur n'a pas tranchées), " +
		"les fichiers, branches et PR en jeu, et la prochaine étape immédiate. " +
		"Réponds uniquement par le brief, en français, sans préambule, et ne modifie rien dans le dépôt."
)

var (
	errHandoffTargets      = errors.New("--under and --channel_id are exclusive: a category holds a new conversation, a channel adopts an existing one")
	errHandoffAgentTargets = errors.New("an agent hands off only under its own session: --under and --channel_id are the operator's to choose")
	errHandoffNoCreate     = errors.New("session create is not registered, so session handoff has nothing to create the new session with")
	errHandoffNoSource     = errors.New("name the session to hand off with --name")
	errHandoffBusy         = errors.New("a handoff from this session is already running: wait for it to finish")
	errHandoffQuota        = errors.New("this session has reached its handoff limit for the last hour: the operator can still run the handoff")
)

type handoffDeps struct {
	exists func(name string) bool
	brief  func(ctx context.Context, source string) (string, error)
	create func(ctx context.Context, in contracts.Input) (string, error)
	send   func(ctx context.Context, target, author, text string) error
	limit  *handoffLimiter
}

type handoffLimiter struct {
	mu       sync.Mutex
	now      func() time.Time
	inFlight map[string]bool
	granted  map[string][]time.Time
}

func newHandoffLimiter(now func() time.Time) *handoffLimiter {
	return &handoffLimiter{now: now, inFlight: make(map[string]bool), granted: make(map[string][]time.Time)}
}

func (l *handoffLimiter) acquire(source string) (func(), error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.inFlight[source] {
		return nil, errHandoffBusy
	}
	now := l.now()
	recent := slices.DeleteFunc(slices.Clone(l.granted[source]), func(at time.Time) bool {
		return now.Sub(at) >= handoffWindow
	})
	if len(recent) >= handoffAgentPerHour {
		l.granted[source] = recent
		return nil, errHandoffQuota
	}
	l.granted[source] = append(recent, now)
	l.inFlight[source] = true
	return func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		delete(l.inFlight, source)
	}, nil
}

func sessionHandoffCmd(managerCmds []contracts.Cmd, store *state.State, sessions *sessionTable, instID string, seedCoord *coordinatorSlot) (contracts.Cmd, error) {
	create, ok := findCommand(managerCmds, nsSession, "create")
	if !ok {
		return contracts.Cmd{}, errHandoffNoCreate
	}
	return sessionHandoffVerb(handoffDeps{
		exists: func(name string) bool {
			_, ok := store.FindSession(name)
			return ok
		},
		brief: func(ctx context.Context, source string) (string, error) {
			in := contracts.Input{Args: map[string]string{paramName: source, "task": handoffBriefTask, "timeout": handoffBriefTimeout}}
			return runSeedVerb(ctx, in, store, instID, seedCoord.coord)
		},
		create: create.Run,
		send: func(ctx context.Context, target, author, text string) error {
			_, err := sendToSession(ctx, sessions, contracts.Input{Args: map[string]string{paramName: target, "author": author, "text": text}})
			return err
		},
		limit: newHandoffLimiter(time.Now),
	}), nil
}

func findCommand(cmds []contracts.Cmd, path ...string) (contracts.Cmd, bool) {
	for _, cmd := range cmds {
		if slices.Equal(cmd.Path, path) {
			return cmd, true
		}
	}
	return contracts.Cmd{}, false
}

func sessionHandoffVerb(deps handoffDeps) contracts.Cmd {
	return contracts.New(nsSession, verbHandoff).
		Help("start a new session whose first message is a brief this session writes; without --to it creates nothing and says how to open this session instead. "+
			"This is not the ⟢ handoff: trailer, which passes a task to another agent inside the coordination flow. "+
			"An agent caller attaches the new session under its source, cannot pick --under or --channel_id, and gets at most "+
			"3 handoffs per source an hour, one at a time; the operator has no such bound").
		Param(paramName, "session to hand off (default: the calling session)", false).
		Param(paramTo, "name of the new session; without it nothing is created", false).
		Param("project", "workspace sub-dir the new session works on", false).
		Param("model", "catalog model id for the new session (see `herrscher models list`)", false).
		Param("vendor", "agent backend vendor for the new session: claude | codex | cursor", false).
		Param(paramUnder, "create the new conversation under this category/forum instead of the daemon's home (operator only)", false).
		Param(paramChannel, "adopt this existing conversation for the new session (operator only)", false).
		Do(deps.run)
}

func (d handoffDeps) run(ctx context.Context, input contracts.Input) (string, error) {
	caller := principalOf(ctx)
	byAgent := caller.Session != ""
	source := strings.TrimSpace(input.Get(paramName))
	if source == "" {
		source = caller.Session
	}
	target := strings.TrimSpace(input.Get(paramTo))
	if target == "" {
		return handoffPointer(source), nil
	}
	if input.Get(paramUnder) != "" && input.Get(paramChannel) != "" {
		return "", errHandoffTargets
	}
	if byAgent && (input.Get(paramUnder) != "" || input.Get(paramChannel) != "") {
		return "", errHandoffAgentTargets
	}
	if source == "" {
		return "", errHandoffNoSource
	}
	if !d.exists(source) {
		return "", &NotFoundError{Kind: kindSession, Name: source}
	}
	parent := ""
	if byAgent {
		release, err := d.limit.acquire(source)
		if err != nil {
			return "", err
		}
		defer release()
		parent = source
	}
	brief, err := d.brief(ctx, source)
	if err != nil {
		return "", fmt.Errorf("the brief from %q: %w", source, err)
	}
	reply, err := d.create(ctx, contracts.Input{Args: newSessionArgs(target, input, parent)})
	if err != nil {
		return "", err
	}
	if err := d.send(ctx, target, "session "+source, handoffMessage(source, brief)); err != nil {
		return "", fmt.Errorf("session %q was created but the brief did not reach it: %w", target, err)
	}
	return handoffReport(source, target, reply, brief), nil
}

func newSessionArgs(target string, input contracts.Input, parent string) map[string]string {
	args := map[string]string{paramName: target}
	for _, key := range []string{"project", "model", "vendor", paramUnder, paramChannel} {
		if value := input.Get(key); value != "" {
			args[key] = value
		}
	}
	if parent != "" {
		args[paramParent] = parent
	}
	return args
}

func handoffPointer(source string) string {
	where := "the session"
	if source != "" {
		where = "session " + source
	}
	return "Nothing was created: " + where + " is already running, and its bound gateways share the same conversation. " +
		"To open it in the TUI, run `herrscher` on the running daemon, which shows it as a tab. " +
		"For a new session with a brief of this conversation, pass --" + paramTo + " <name>."
}

func handoffMessage(source, brief string) string {
	return "[Passation depuis la session " + source + "]\n\n" + brief
}

func handoffReport(source, target, reply, brief string) string {
	return fmt.Sprintf("Session %s created from %s, brief sent as its first message.\n\n%s\n\nBrief:\n%s",
		target, source, strings.TrimSpace(reply), strings.TrimSpace(brief))
}
