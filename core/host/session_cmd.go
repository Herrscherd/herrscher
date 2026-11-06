package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

var (
	errNoPendingChoice = errors.New("has no pending choice")
	errUnsupportedLang = errors.New("unsupported language, use en, fr or auto")
)

func replyText(lang i18n.Language) *clitext.Translator {
	return clitext.For(i18n.Resolve(lang))
}

func namedSessionLanguage(st *state.State, name string, operator i18n.Language) i18n.Language {
	sess, ok := st.FindSession(name)
	if !ok {
		return i18n.Resolve(operator)
	}
	return sessionLanguage(sess, i18n.Resolve(operator))
}

func addSessionVerbs(reg *cli.Registry, lang languageSource, store *state.State, partDir, instID string, approvals *approvalTable, sessions *sessionTable, seedCoord *coordinatorSlot, models *ModelDiscovery) error {
	seen := newSeenTable(time.Now())
	return addVerbs(
		reg,
		seedVerb(store, instID, seedCoord),
		sessionSendVerb(sessions),
		sessionPickVerb(lang, approvals, sessions),
		sessionInfoVerb(store, partDir, sessions, models),
		sessionOverviewVerb(overviewSource{st: store, partDir: partDir, sessions: sessions, approvals: approvals, tallies: newTallyCache(), seen: seen}),
		sessionSeenVerb(seen),
		sessionScrollbackVerb(partDir),
		sessionInterruptVerb(lang, sessions),
		sessionCompactVerb(lang, store, sessions),
		sessionSetCompactVerb(store),
		sessionSetAccountVerb(lang, store),
		sessionLangVerb(store, lang),
		sessionAccountsVerb(lang, store),
	)
}

func sessionSeenVerb(seen *seenTable) contracts.Cmd {
	return contracts.New(nsSession, "seen").
		Help(clitext.CmdHelpSessionSeen).
		Param(paramName, clitext.CmdParamSessionSeenName, true).
		Do(func(_ context.Context, input contracts.Input) (contracts.Result, error) {
			seen.mark(input.Get(paramName), time.Now())
			return contracts.Result{}, nil
		})
}

func addCheckpointCommands(reg *cli.Registry, lang languageSource, store *state.State, sessions *sessionTable) error {
	return addVerbs(reg, addCheckpointVerbs(lang, store, sessions)...)
}

func seedVerb(store *state.State, instID string, seedCoord *coordinatorSlot) contracts.Cmd {
	return contracts.New(nsSession, verbSeed).
		Help(clitext.CmdHelpSessionSeed).
		Param(paramName, clitext.CmdParamSessionSeedName, true).
		Param("task", clitext.CmdParamSessionSeedTask, true).
		ValueParam("turn_id", clitext.CmdParamSessionSeedTurnID, false).
		ValueParam("timeout", clitext.CmdParamSessionSeedTimeout, false).
		Do(func(cmdCtx context.Context, in contracts.Input) (contracts.Result, error) {
			return cli.Data(runSeedVerb(cmdCtx, in, store, instID, seedCoord.coord))
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
		Help(clitext.CmdHelpSessionSend).
		Param(paramName, clitext.CmdParamSessionSendName, true).
		Param("text", clitext.CmdParamSessionSendText, true).
		Param("author", clitext.CmdParamSessionSendAuthor, false).
		Param("attachments", clitext.CmdParamSessionSendAttachments, false).
		Param("origin", clitext.CmdParamSessionSendOrigin, false).
		Do(func(ctx context.Context, in contracts.Input) (contracts.Result, error) {
			return contracts.Result{}, sendToSession(ctx, sessions, in)
		})
}

func sendToSession(ctx context.Context, sessions *sessionTable, input contracts.Input) error {
	atts, err := parseAttachmentPaths(input.Get("attachments"))
	if err != nil {
		return err
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
		return &NotFoundError{Kind: "live session", Name: input.Get(paramName)}
	}
	return nil
}

func sessionInfoVerb(store *state.State, partDir string, sessions *sessionTable, models *ModelDiscovery) contracts.Cmd {
	return contracts.New(nsSession, "info").
		Help(clitext.CmdHelpSessionInfo).
		Do(func(_ context.Context, _ contracts.Input) (contracts.Result, error) {
			b, err := json.Marshal(sessionInfos(store, partDir, sessions.compaction, modelTermsFor(models)))
			return contracts.DataResult(string(b)), err
		})
}

func sessionOverviewVerb(src overviewSource) contracts.Cmd {
	return contracts.New(nsSession, "overview").
		Help(clitext.CmdHelpSessionOverview).
		Do(func(_ context.Context, _ contracts.Input) (contracts.Result, error) {
			b, err := json.Marshal(src.overviews())
			return contracts.DataResult(string(b)), err
		})
}

func sessionScrollbackVerb(partDir string) contracts.Cmd {
	return contracts.New(nsSession, "scrollback").
		Help(clitext.CmdHelpSessionScrollback).
		Param(paramName, clitext.CmdParamSessionScrollbackName, true).
		Do(func(_ context.Context, in contracts.Input) (contracts.Result, error) {
			b, err := json.Marshal(scrollbackOf(partDir, in.Get(paramName)))
			return contracts.DataResult(string(b)), err
		})
}

func sessionPickVerb(lang languageSource, approvals *approvalTable, sessions *sessionTable) contracts.Cmd {
	return contracts.New(nsSession, "pick").
		Help(clitext.CmdHelpSessionPick).
		Param(paramName, clitext.CmdParamSessionPickName, true).
		ValueParam("value", clitext.CmdParamSessionPickValue, true).
		Do(func(_ context.Context, input contracts.Input) (contracts.Result, error) {
			name, value := input.Get(paramName), input.Get("value")
			if answerApprovalPick(approvals, sessions, name, value) || sessions.pick(name, value) {
				return contracts.DataResult(replyText(lang()).Text(clitext.SessionPicked, clitext.Params{clitext.ParamName: name})), nil
			}
			return contracts.Result{}, fmt.Errorf("session %q %w", name, errNoPendingChoice)
		})
}

func sessionInterruptVerb(lang languageSource, sessions *sessionTable) contracts.Cmd {
	return contracts.New(nsSession, "interrupt").
		Help(clitext.CmdHelpSessionInterrupt).
		Param(paramName, clitext.CmdParamSessionInterruptName, true).
		Do(func(_ context.Context, input contracts.Input) (contracts.Result, error) {
			if !sessions.Interrupt(input.Get(paramName)) {
				return contracts.Result{}, &noTurnInFlightError{Session: input.Get(paramName)}
			}
			return contracts.DataResult(replyText(lang()).Text(clitext.SessionInterrupted, clitext.Params{clitext.ParamName: input.Get(paramName)})), nil
		})
}

const verbCompact = "compact"

func sessionCompactVerb(lang languageSource, store *state.State, sessions *sessionTable) contracts.Cmd {
	return contracts.New(nsSession, verbCompact).
		Help(clitext.CmdHelpSessionCompact).
		Param(paramName, clitext.CmdParamSessionCompactName, true).
		Param("instruction", clitext.CmdParamSessionCompactInstruction, false).
		Param("origin", clitext.CmdParamSessionCompactOrigin, false).
		Do(func(ctx context.Context, in contracts.Input) (contracts.Result, error) {
			return cli.Data(compactSession(ctx, lang(), store, sessions, in))
		})
}

func compactSession(_ context.Context, lang i18n.Language, store *state.State, sessions *sessionTable, input contracts.Input) (string, error) {
	name := input.Get(paramName)
	if _, ok := store.FindSession(name); !ok {
		return "", &NotFoundError{Kind: "session", Name: name}
	}
	driver := sessions.lookup(name)
	if driver == nil {
		return "", &NotFoundError{Kind: "live session", Name: name}
	}
	if !driver.compactSupported() {
		return "", unsupportedCompaction(name, driver.modelNow().vendor)
	}
	merged, accepted := driver.requestCompaction(input.Get("instruction"), contracts.Conversation{Gateway: input.Get("origin")})
	if !accepted {
		return "", queueFullCompaction(name)
	}
	if merged {
		return mergedCompactNotice(lang, name, input.Get("instruction")), nil
	}
	return "", nil
}

func mergedCompactNotice(lang i18n.Language, name, instruction string) string {
	var key clitext.Key = clitext.SessionCompactMergedKept
	if instruction != "" {
		key = clitext.SessionCompactMergedReplaced
	}
	return replyText(lang).Text(key, clitext.Params{clitext.ParamName: name})
}

func sessionSetCompactVerb(store *state.State) contracts.Cmd {
	return contracts.New(nsSession, "set-compact").
		Help(clitext.CmdHelpSessionSetCompact).
		Param(paramName, clitext.CmdParamSessionSetCompactName, true).
		Param("percent", clitext.CmdParamSessionSetCompactPercent, false).
		Do(func(_ context.Context, input contracts.Input) (contracts.Result, error) {
			name := input.Get(paramName)
			ok, err := applyCompactPercent(store, name, input.Get("percent"))
			if err != nil {
				return contracts.Result{}, err
			}
			if !ok {
				return contracts.Result{}, &NotFoundError{Kind: "session", Name: name}
			}
			return contracts.Result{}, nil
		})
}

func applyCompactPercent(store *state.State, name, raw string) (bool, error) {
	if raw == "" {
		return store.SetCompactPercent(name, nil)
	}
	percent, err := strconv.Atoi(raw)
	if err != nil {
		return false, fmt.Errorf("percent %q: %w", raw, err)
	}
	if err := contracts.ValidateAutoCompactPercent(percent); err != nil {
		return false, err
	}
	return store.SetCompactPercent(name, &percent)
}

func sessionLangVerb(store *state.State, operator languageSource) contracts.Cmd {
	return contracts.New(nsSession, "lang").
		Help(clitext.CmdHelpSessionLang).
		Param(paramName, clitext.CmdParamSessionLangName, true).
		Param(paramLang, clitext.CmdParamSessionLangLang, true).
		Do(func(_ context.Context, input contracts.Input) (contracts.Result, error) {
			return cli.Data(setSessionLang(store, operator(), input.Get(paramName), input.Get(paramLang)))
		})
}

func setSessionLang(store *state.State, operator i18n.Language, name, raw string) (string, error) {
	choice, reset, ok := parseSessionLangChoice(strings.TrimSpace(raw))
	if !ok {
		return "", fmt.Errorf("%w: %q", errUnsupportedLang, raw)
	}
	stored := ""
	if !reset {
		stored = choice.String()
	}
	found, err := store.SetSessionLanguage(name, stored)
	if err != nil {
		return "", err
	}
	if !found {
		return "", &NotFoundError{Kind: kindSession, Name: name}
	}
	if reset {
		return replyText(operator).Text(clitext.HostSessionLangReset, clitext.Params{clitext.ParamName: name, clitext.ParamLang: i18n.Resolve(operator).String()}), nil
	}
	return replyText(choice).Text(clitext.HostSessionLangSet, clitext.Params{clitext.ParamName: name, clitext.ParamLang: choice.String()}), nil
}

func parseSessionLangChoice(raw string) (i18n.Language, bool, bool) {
	switch raw {
	case "", "auto":
		return 0, true, true
	case "en":
		return i18n.English, false, true
	case "fr":
		return i18n.French, false, true
	}
	return 0, false, false
}
