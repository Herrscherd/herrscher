package host

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const (
	verbHandoff         = "handoff"
	handoffBriefTimeout = "10m"
	handoffBriefTask    = "Rédige le brief de passation de cette conversation, pour une session qui n'a pas ton historique : " +
		"ce qui est construit, ce qui est décidé, ce qui reste ouvert (les questions que l'opérateur n'a pas tranchées), " +
		"les fichiers, branches et PR en jeu, et la prochaine étape immédiate. " +
		"Réponds uniquement par le brief, en français, sans préambule, et ne modifie rien dans le dépôt."
)

var (
	errHandoffTargets  = errors.New("--under and --channel_id are exclusive: a category holds a new conversation, a channel adopts an existing one")
	errHandoffNoCreate = errors.New("session create is not registered, so session handoff has nothing to create the new session with")
	errHandoffNoSource = errors.New("name the session to hand off with --name")
)

type handoffDeps struct {
	exists func(name string) bool
	brief  func(ctx context.Context, source string) (string, error)
	create func(ctx context.Context, in contracts.Input) (string, error)
	send   func(ctx context.Context, target, author, text string) error
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
		Help("start a new session whose first message is a brief this session writes; without --to it creates nothing and says how to open this session instead").
		Param(paramName, "session to hand off (default: the calling session)", false).
		Param(paramTo, "name of the new session; without it nothing is created", false).
		Param("project", "workspace sub-dir the new session works on", false).
		Param("model", "catalog model id for the new session (see `herrscher models list`)", false).
		Param("vendor", "agent backend vendor for the new session: claude | codex | cursor", false).
		Param("under", "create the new conversation under this category/forum instead of the daemon's home", false).
		Param("channel_id", "adopt this existing conversation for the new session", false).
		Do(deps.run)
}

func (d handoffDeps) run(ctx context.Context, input contracts.Input) (string, error) {
	source := strings.TrimSpace(input.Get(paramName))
	if source == "" {
		source = principalOf(ctx).Session
	}
	target := strings.TrimSpace(input.Get(paramTo))
	if target == "" {
		return handoffPointer(source), nil
	}
	if input.Get("under") != "" && input.Get("channel_id") != "" {
		return "", errHandoffTargets
	}
	if source == "" {
		return "", errHandoffNoSource
	}
	if !d.exists(source) {
		return "", &NotFoundError{Kind: kindSession, Name: source}
	}
	brief, err := d.brief(ctx, source)
	if err != nil {
		return "", fmt.Errorf("the brief from %q: %w", source, err)
	}
	reply, err := d.create(ctx, contracts.Input{Args: newSessionArgs(target, input)})
	if err != nil {
		return "", err
	}
	if err := d.send(ctx, target, "session "+source, handoffMessage(source, brief)); err != nil {
		return "", fmt.Errorf("session %q was created but the brief did not reach it: %w", target, err)
	}
	return handoffReport(source, target, reply, brief), nil
}

func newSessionArgs(target string, input contracts.Input) map[string]string {
	args := map[string]string{paramName: target}
	for _, key := range []string{"project", "model", "vendor", "under", "channel_id"} {
		if value := input.Get(key); value != "" {
			args[key] = value
		}
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
