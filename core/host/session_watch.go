package host

import (
	"context"
	"encoding/json"
	"strconv"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/manager"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const paramSince = "since"

func sessionWatchVerb(messages *hostMessages, store *state.State, journal *eventJournal) contracts.Cmd {
	return contracts.New(nsSession, "watch").
		Help(clitext.CmdHelpSessionWatch).
		Param(paramName, clitext.CmdParamSessionWatchName, true).
		ValueParam(paramSince, clitext.CmdParamSessionWatchSince, false).
		Do(func(_ context.Context, input contracts.Input) (contracts.Result, error) {
			events, err := journaledEvents(messages, store, journal, input.Get(paramName), input.Get(paramSince))
			if err != nil {
				return contracts.Result{}, err
			}
			b, err := json.Marshal(events)
			return contracts.DataResult(string(b)), err
		})
}

// SessionSlug is the name the daemon files a session under when it is created or looked up from name.
func SessionSlug(name string) string {
	return manager.SessionSlug(name)
}

func journaledEvents(messages *hostMessages, store *state.State, journal *eventJournal, raw, rawSince string) ([]wireEvent, error) {
	name := SessionSlug(raw)
	if _, ok := store.FindSession(name); !ok {
		return nil, &NotFoundError{Kind: kindSession, Name: name}
	}
	cursor := EventCursor{Session: name}
	if rawSince != "" {
		after, err := strconv.ParseUint(rawSince, 10, 64)
		if err != nil {
			return nil, wrapHost(messages, err, "session.since", map[string]any{"Raw": rawSince})
		}
		cursor.Since = &after
	}
	return journal.catchUp(cursor, journal.current()), nil
}
