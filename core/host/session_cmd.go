package host

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func addSessionVerbs(reg *cli.Registry, store *state.State, partDir, instID string, approvals *approvalTable, sessions *sessionTable, seedCoord *coordinatorSlot) error {
	seen := newSeenTable(time.Now())
	return addVerbs(reg,
		seedVerb(store, instID, seedCoord),
		sessionSendVerb(sessions),
		sessionPickVerb(approvals, sessions),
		sessionInfoVerb(store, partDir),
		sessionOverviewVerb(overviewSource{st: store, partDir: partDir, sessions: sessions, approvals: approvals, tallies: newTallyCache(), seen: seen}),
		sessionSeenVerb(seen),
		sessionScrollbackVerb(partDir),
		sessionInterruptVerb(sessions),
		sessionCompactVerb(store, sessions),
	)
}

func sessionSeenVerb(seen *seenTable) contracts.Cmd {
	return contracts.New(nsSession, "seen").
		Help("record that the operator has read a session's last answer, which clears its review mark").
		Param(paramName, "session name", true).
		Do(func(_ context.Context, input contracts.Input) (string, error) {
			seen.mark(input.Get(paramName), time.Now())
			return "", nil
		})
}

func seedVerb(store *state.State, instID string, seedCoord *coordinatorSlot) contracts.Cmd {
	return contracts.New(nsSession, verbSeed).
		Help("run one agent turn in a session and print the reply").
		Param(paramName, "session name", true).
		Param("task", "opening task", true).
		ValueParam("turn_id", "optional caller-supplied turn identity", false).
		ValueParam("timeout", "cap this turn (Go duration, e.g. 30m); default HERRSCHER_SEED_TIMEOUT then 120s", false).
		Do(func(cmdCtx context.Context, in contracts.Input) (string, error) {
			return runSeedVerb(cmdCtx, in, store, instID, seedCoord.coord)
		})
}

func runSeedVerb(ctx context.Context, input contracts.Input, store *state.State, instID string, coord contracts.Coordinator) (string, error) {
	rawTurnID, supplied := input.Lookup("turn_id")
	turnID, err := resolveTurnID(rawTurnID, supplied)
	if err != nil {
		return "", err
	}
	timeout, err := resolveSeedTimeout(input.Get("timeout"), os.Getenv)
	if err != nil {
		return "", err
	}
	runtime := oneShotSeedRuntimeFrom(ctx)
	runtime.timeout = timeout
	reply, err := runOneShotSeedCommand(
		ctx, store, input.Get(paramName), input.Get("task"),
		turnID, runtime,
		coord, instID, dispatchLiveCommand,
	)
	if err != nil {
		return "", err
	}
	if input.JSON {
		b, err := json.Marshal(map[string]string{keySession: input.Get(paramName), "reply": reply})
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

func sendToSession(ctx context.Context, sessions *sessionTable, input contracts.Input) (string, error) {
	atts, err := parseAttachmentPaths(input.Get("attachments"))
	if err != nil {
		return "", err
	}
	author := input.Get("author")
	if author == "" {
		author = "you"
	}
	if !sessions.Submit(ctx, input.Get(paramName), contracts.Inbound{
		Author:       author,
		AuthorID:     "local",
		Text:         input.Get("text"),
		Attachments:  atts,
		Conversation: contracts.Conversation{Gateway: input.Get("origin")},
	}) {
		return "", &NotFoundError{Kind: "live session", Name: input.Get(paramName)}
	}
	return "", nil
}

func sessionInfoVerb(store *state.State, partDir string) contracts.Cmd {
	return contracts.New(nsSession, "info").
		Help("list sessions as a frontend sees them (channel, vendor, dir, archived). JSON only").
		Do(func(_ context.Context, _ contracts.Input) (string, error) {
			b, err := json.Marshal(sessionInfos(store, partDir))
			return string(b), err
		})
}

func sessionOverviewVerb(src overviewSource) contracts.Cmd {
	return contracts.New(nsSession, "overview").
		Help("list every session with its live state (waiting, working, archived, review, idle), cost and last activity. JSON only").
		Do(func(_ context.Context, _ contracts.Input) (string, error) {
			b, err := json.Marshal(src.overviews())
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

func sessionPickVerb(approvals *approvalTable, sessions *sessionTable) contracts.Cmd {
	return contracts.New(nsSession, "pick").
		Help("answer a session's pending choice (an approval) with a menu value").
		Param(paramName, "session name", true).
		ValueParam("value", "menu value to answer with", true).
		Do(func(_ context.Context, input contracts.Input) (string, error) {
			name, value := input.Get(paramName), input.Get("value")
			if answerApprovalPick(approvals, sessions, name, value) || sessions.pick(name, value) {
				return "picked " + name, nil
			}
			return "", fmt.Errorf("session %q has no pending choice", name)
		})
}

func sessionInterruptVerb(sessions *sessionTable) contracts.Cmd {
	return contracts.New(nsSession, "interrupt").
		Help("cancel a session's in-flight turn").
		Param(paramName, "session name", true).
		Do(func(_ context.Context, input contracts.Input) (string, error) {
			if !sessions.Interrupt(input.Get(paramName)) {
				return "", &noTurnInFlightError{Session: input.Get(paramName)}
			}
			return "interrupted " + input.Get(paramName), nil
		})
}

const (
	verbCompact    = "compact"
	compactCommand = "/compact"
	vendorClaude   = "claude"
)

func sessionCompactVerb(store *state.State, sessions *sessionTable) contracts.Cmd {
	return contracts.New(nsSession, verbCompact).
		Help("ask a claude session to compact its context (sends /compact into its turn queue)").
		Param(paramName, "session name", true).
		Param("origin", "gateway kind this was typed in (e.g. terminal): the turn is rendered there "+
			"instead of in every channel the session is bound to", false).
		Do(func(ctx context.Context, in contracts.Input) (string, error) {
			return compactSession(ctx, store, sessions, in)
		})
}

func compactSession(ctx context.Context, store *state.State, sessions *sessionTable, input contracts.Input) (string, error) {
	name := input.Get(paramName)
	live, ok := store.FindSession(name)
	if !ok {
		return "", &NotFoundError{Kind: "session", Name: name}
	}
	if live.Vendor != vendorClaude {
		return "", fmt.Errorf("session %q runs %q: /compact is only available on claude sessions", name, live.Vendor)
	}
	if !sessions.Submit(ctx, name, contracts.Inbound{
		Author:       "you",
		AuthorID:     "local",
		Text:         compactCommand,
		Conversation: contracts.Conversation{Gateway: input.Get("origin")},
	}) {
		return "", &NotFoundError{Kind: "live session", Name: name}
	}
	return "", nil
}
