package host

import (
	"context"
	"encoding/json"
	"os"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func addSessionVerbs(reg *cli.Registry, st *state.State, partDir, instID string, sessions *sessionTable, seedCoord *coordinatorSlot) error {
	return addVerbs(reg,
		seedVerb(st, instID, seedCoord),
		sessionSendVerb(sessions),
		sessionInfoVerb(st, partDir),
		sessionScrollbackVerb(partDir),
		sessionInterruptVerb(sessions),
	)
}

func seedVerb(st *state.State, instID string, seedCoord *coordinatorSlot) contracts.Cmd {
	return contracts.New(nsSession, verbSeed).
		Help("run one agent turn in a session and print the reply").
		Param(paramName, "session name", true).
		Param("task", "opening task", true).
		ValueParam("turn_id", "optional caller-supplied turn identity", false).
		ValueParam("timeout", "cap this turn (Go duration, e.g. 30m); default HERRSCHER_SEED_TIMEOUT then 120s", false).
		Do(func(cmdCtx context.Context, in contracts.Input) (string, error) {
			return runSeedVerb(cmdCtx, in, st, instID, seedCoord.coord)
		})
}

func runSeedVerb(ctx context.Context, in contracts.Input, st *state.State, instID string, coord contracts.Coordinator) (string, error) {
	rawTurnID, supplied := in.Lookup("turn_id")
	turnID, err := resolveTurnID(rawTurnID, supplied)
	if err != nil {
		return "", err
	}
	timeout, err := resolveSeedTimeout(in.Get("timeout"), os.Getenv)
	if err != nil {
		return "", err
	}
	runtime := oneShotSeedRuntimeFrom(ctx)
	runtime.timeout = timeout
	reply, err := runOneShotSeedCommand(
		ctx, st, in.Get(paramName), in.Get("task"),
		turnID, runtime,
		coord, instID, dispatchLiveCommand,
	)
	if err != nil {
		return "", err
	}
	if in.JSON {
		b, err := json.Marshal(map[string]string{keySession: in.Get(paramName), "reply": reply})
		return string(b), err
	}
	return reply, nil
}

func sessionSendVerb(sessions *sessionTable) contracts.Cmd {
	return contracts.New(nsSession, "send").
		Help("send one message into a live session's turn queue (the daemon runs the turn)").
		Param(paramName, "session name", true).
		Param("text", "message text", true).
		Param("author", "display name shown to the agent (default: you)", false).
		Param("attachments", "JSON array of local file paths to attach", false).
		Param("origin", "gateway kind this was typed in (e.g. terminal): the turn is rendered there "+
			"instead of in every channel the session is bound to", false).
		Do(func(ctx context.Context, in contracts.Input) (string, error) {
			return sendToSession(ctx, sessions, in)
		})
}

func sendToSession(ctx context.Context, sessions *sessionTable, in contracts.Input) (string, error) {
	atts, err := parseAttachmentPaths(in.Get("attachments"))
	if err != nil {
		return "", err
	}
	author := in.Get("author")
	if author == "" {
		author = "you"
	}
	if !sessions.Submit(ctx, in.Get(paramName), contracts.Inbound{
		Author:       author,
		AuthorID:     "local",
		Text:         in.Get("text"),
		Attachments:  atts,
		Conversation: contracts.Conversation{Gateway: in.Get("origin")},
	}) {
		return "", &NotFoundError{Kind: "live session", Name: in.Get(paramName)}
	}
	return "", nil
}

func sessionInfoVerb(st *state.State, partDir string) contracts.Cmd {
	return contracts.New(nsSession, "info").
		Help("list sessions as a frontend sees them (channel, vendor, dir, archived). JSON only").
		Do(func(_ context.Context, _ contracts.Input) (string, error) {
			b, err := json.Marshal(sessionInfos(st, partDir))
			return string(b), err
		})
}

func sessionScrollbackVerb(partDir string) contracts.Cmd {
	return contracts.New(nsSession, "scrollback").
		Help("dump a session's replayable history as the frontend renders it. JSON only").
		Param(paramName, "session name", true).
		Do(func(_ context.Context, in contracts.Input) (string, error) {
			b, err := json.Marshal(scrollbackOf(partDir, in.Get(paramName)))
			return string(b), err
		})
}

func sessionInterruptVerb(sessions *sessionTable) contracts.Cmd {
	return contracts.New(nsSession, "interrupt").
		Help("cancel a session's in-flight turn").
		Param(paramName, "session name", true).
		Do(func(_ context.Context, in contracts.Input) (string, error) {
			if !sessions.Interrupt(in.Get(paramName)) {
				return "", &noTurnInFlightError{Session: in.Get(paramName)}
			}
			return "interrupted " + in.Get(paramName), nil
		})
}
