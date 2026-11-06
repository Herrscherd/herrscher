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
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/accounts"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/manager"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const (
	verbHandoff         = "handoff"
	paramUnder          = "under"
	paramChannel        = "channel_id"
	paramParent         = "parent"
	handoffBriefTimeout = "10m"
	shellSafeChars      = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._/:=+-"
	handoffAgentPerHour = 3
	handoffWindow       = time.Hour
	handoffBriefTask    = "Write the handover brief of this conversation, for a session that does not have your history: " +
		"what is built, what is decided, what stays open (the questions the operator has not settled), " +
		"the files, branches and PRs in play, and the immediate next step. " +
		"Reply only with the brief, without preamble, and change nothing in the repository."
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
	lang         i18n.Language
	sessionLang  func(name string) i18n.Language
	exists       func(name string) bool
	root         func(name string) string
	account      func(name string) string
	brief        func(ctx context.Context, source string) (string, error)
	create       func(ctx context.Context, in contracts.Input) (string, error)
	send         func(ctx context.Context, target, author, text string) error
	limit        *handoffLimiter
	checkAccount func(input contracts.Input) error
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

func (l *handoffLimiter) acquire(tree string) (func(), error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.inFlight[tree] {
		return nil, errHandoffBusy
	}
	now := l.now()
	l.forgetIdle(now)
	if len(l.granted[tree]) >= handoffAgentPerHour {
		return nil, errHandoffQuota
	}
	l.granted[tree] = append(l.granted[tree], now)
	l.inFlight[tree] = true
	return func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		delete(l.inFlight, tree)
	}, nil
}

func (l *handoffLimiter) forgetIdle(now time.Time) {
	for tree := range l.granted {
		recent := slices.DeleteFunc(slices.Clone(l.granted[tree]), func(at time.Time) bool {
			return now.Sub(at) >= handoffWindow
		})
		if len(recent) == 0 {
			delete(l.granted, tree)
			continue
		}
		l.granted[tree] = recent
	}
}

func sessionHandoffCmd(lang i18n.Language, managerCmds []contracts.Cmd, store *state.State, sessions *sessionTable, instID string, seedCoord *coordinatorSlot, checkAccount func(contracts.Input) error) (contracts.Cmd, error) {
	create, ok := findCommand(managerCmds, nsSession, "create")
	if !ok {
		return contracts.Cmd{}, errHandoffNoCreate
	}
	return sessionHandoffVerb(handoffDeps{
		lang: lang,
		sessionLang: func(name string) i18n.Language {
			return namedSessionLanguage(store, name, lang)
		},
		exists: func(name string) bool {
			_, ok := store.FindSession(name)
			return ok
		},
		root: func(name string) string {
			return state.TreeRoot(store.SnapshotSessions(), name)
		},
		account: func(name string) string {
			session, _ := store.FindSession(name)
			return session.Account
		},
		brief: func(ctx context.Context, source string) (string, error) {
			in := contracts.Input{Args: map[string]string{paramName: source, "task": handoffBriefTaskFor(store, source), "timeout": handoffBriefTimeout}}
			return runSeedVerb(ctx, in, store, instID, seedCoord.coord)
		},
		create: func(ctx context.Context, in contracts.Input) (string, error) {
			res, err := create.Run(ctx, in)
			return cli.Render(res, err, i18n.English)
		},
		send: func(ctx context.Context, target, author, text string) error {
			return sendToSession(ctx, sessions, contracts.Input{Args: map[string]string{paramName: target, "author": author, "text": text}})
		},
		limit:        newHandoffLimiter(time.Now),
		checkAccount: checkAccount,
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
			"3 handoffs an hour for its whole session tree, one at a time, and a handoff counts once its brief starts, even if it then fails; a refused account costs neither a brief nor a slot, unless its profile is removed between the check and the creation; the operator has no such bound. When the operator closes a session, its direct children become roots and their quota starts again. "+
			"This bound is a local guard, not a security boundary: the operator socket is reachable by the same user, "+
			"and its state resets when the daemon restarts").
		Param(paramName, "session to hand off (default: the calling session)", false).
		Param(paramTo, "name of the new session; without it nothing is created", false).
		Param("project", "workspace sub-dir the new session works on", false).
		Param("model", "catalog model id for the new session (see `herrscher models list`)", false).
		Param("vendor", "agent backend vendor for the new session: claude | codex | cursor", false).
		Param("account", "account profile for the new session, resolved in its vendor (default: the source session's account; --account \"\" starts on the vendor default)", false).
		Param(paramUnder, "create the new conversation under this category/forum instead of the daemon's home (operator only)", false).
		Param(paramChannel, "adopt this existing conversation for the new session (operator only)", false).
		Do(cli.Handler(deps.run))
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
		return handoffPointer(d.replyLang(source), source), nil
	}
	if err := checkHandoffTargets(input, byAgent); err != nil {
		return "", err
	}
	if source == "" {
		return "", errHandoffNoSource
	}
	if !d.exists(source) {
		return "", &NotFoundError{Kind: kindSession, Name: source}
	}
	if byAgent {
		return d.handOffAsAgent(ctx, caller.Session, source, target, input)
	}
	args, account, err := d.checkedArgs(source, target, "", input)
	if err != nil {
		return "", err
	}
	reply, brief, err := d.launch(ctx, source, input, account, args)
	if err != nil {
		return "", err
	}
	return d.deliver(ctx, source, target, reply, brief, account)
}

func checkHandoffTargets(input contracts.Input, byAgent bool) error {
	under, channel := input.Get(paramUnder), input.Get(paramChannel)
	if under != "" && channel != "" {
		return errHandoffTargets
	}
	if byAgent && (under != "" || channel != "") {
		return errHandoffAgentTargets
	}
	return nil
}

func (d handoffDeps) handOffAsAgent(ctx context.Context, caller, source, target string, input contracts.Input) (string, error) {
	args, account, err := d.checkedArgs(source, target, source, input)
	if err != nil {
		return "", err
	}
	release, err := d.limit.acquire(d.root(caller))
	if err != nil {
		return "", err
	}
	defer release()
	reply, brief, err := d.launch(ctx, source, input, account, args)
	if err != nil {
		return "", err
	}
	return d.deliver(ctx, source, target, reply, brief, account)
}

func (d handoffDeps) checkedArgs(source, target, parent string, input contracts.Input) (map[string]string, string, error) {
	account := handoffAccount(input, d.account(source))
	args := newSessionArgs(target, account, input, parent)
	if err := d.checkAccount(contracts.Input{Args: args}); err != nil {
		return nil, "", inheritedAccountError(input, source, account, err)
	}
	return args, account, nil
}

func (d handoffDeps) launch(ctx context.Context, source string, input contracts.Input, account string, args map[string]string) (string, string, error) {
	brief, err := d.brief(ctx, source)
	if err != nil {
		return "", "", fmt.Errorf("the brief from %q: %w", source, err)
	}
	reply, err := d.create(ctx, contracts.Input{Args: args})
	if err != nil {
		return "", "", inheritedAccountError(input, source, account, err)
	}
	return reply, brief, nil
}

func handoffAccount(input contracts.Input, inherited string) string {
	if explicit, given := input.Lookup("account"); given {
		return explicit
	}
	return inherited
}

func isAccountRefusal(err error) bool {
	return errors.Is(err, accounts.ErrNotFound) || errors.Is(err, manager.ErrAccountVendor) ||
		errors.Is(err, manager.ErrAccountHost) || errors.Is(err, manager.ErrAccountRemote)
}

func inheritedAccountError(input contracts.Input, source, account string, err error) error {
	if _, given := input.Lookup("account"); given || account == "" || !isAccountRefusal(err) {
		return err
	}
	return fmt.Errorf("the account %q inherited from session %q is refused for the new session: %w; retry with `%s` to start on the vendor default, or replace its --account \"\" with --account <profile>",
		account, source, err, retryCommand(source, input))
}

func retryCommand(source string, input contracts.Input) string {
	parts := []string{"herrscher", paramSession, verbHandoff, retryFlag(paramName, source)}
	for _, key := range []string{paramTo, "project", "model", "vendor", paramUnder, paramChannel} {
		if value := input.Get(key); value != "" {
			parts = append(parts, retryFlag(key, value))
		}
	}
	return strings.Join(append(parts, "--account", `""`), " ")
}

func retryFlag(key, value string) string {
	tokens := cli.FlagArg(key, value)
	quoted := make([]string, 0, len(tokens))
	for _, token := range tokens {
		quoted = append(quoted, shellQuote(token))
	}
	return strings.Join(quoted, " ")
}

func shellQuote(value string) string {
	if strings.Trim(value, shellSafeChars) == "" {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func (d handoffDeps) deliver(ctx context.Context, source, target, reply, brief, account string) (string, error) {
	if err := d.send(ctx, target, "session "+source, handoffMessage(source, brief)); err != nil {
		return "", fmt.Errorf("session %q was created but the brief did not reach it: %w", target, err)
	}
	return handoffReport(d.replyLang(source), source, target, reply, brief, account), nil
}

func newSessionArgs(target, account string, input contracts.Input, parent string) map[string]string {
	args := map[string]string{paramName: target}
	if account != "" {
		args["account"] = account
	}
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

func (d handoffDeps) replyLang(source string) i18n.Language {
	if d.sessionLang == nil {
		return i18n.Resolve(d.lang)
	}
	return d.sessionLang(source)
}

func handoffPointer(lang i18n.Language, source string) string {
	text := replyText(lang)
	where := text.Text(clitext.HostHandoffSession, nil)
	if source != "" {
		where = text.Text(clitext.HostHandoffSessionNamed, clitext.Params{clitext.ParamName: source})
	}
	return text.Text(clitext.HostHandoffPointer, clitext.Params{clitext.ParamWhere: where, clitext.ParamTo: paramTo})
}

func handoffMessage(source, brief string) string {
	return "[Handover from session " + source + "]\n\n" + brief
}

func handoffReport(lang i18n.Language, source, target, reply, brief, account string) string {
	text := replyText(lang)
	if account == "" {
		account = text.Text(clitext.HostHandoffVendorDefault, nil)
	}
	return text.Text(clitext.HostHandoffReport, clitext.Params{
		clitext.ParamTarget:  target,
		clitext.ParamSource:  source,
		clitext.ParamAccount: account,
		clitext.ParamReply:   strings.TrimSpace(reply),
		clitext.ParamBrief:   strings.TrimSpace(brief),
	})
}

func handoffBriefTaskFor(store *state.State, source string) string {
	session, _ := store.FindSession(source)
	lang, err := i18n.ParseLanguage(session.Language)
	if err != nil {
		return handoffBriefTask
	}
	directive := briefLanguageDirective(lang)
	if directive == "" {
		return handoffBriefTask
	}
	return handoffBriefTask + " " + directive
}

func briefLanguageDirective(lang i18n.Language) string {
	switch lang {
	case i18n.French:
		return "Write the brief in French."
	case i18n.English:
		return "Write the brief in English."
	}
	return ""
}
