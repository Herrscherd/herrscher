package host

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/forgeport"
	"github.com/Herrscherd/herrscher/core/internal/forge"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/termpaint"
)

const (
	verbPR         = "pr"
	verbPROpen     = "open"
	verbPRStatus   = "status"
	verbPRChecks   = "checks"
	verbPRComments = "comments"
	paramTitle     = "title"
	paramBody      = "body"
	paramBase      = "base"
	paramDraft     = "draft"
	resetUnknown   = "unknown time"
)

const sessionPRForgeLimit = time.Minute

type sessionPRError struct {
	key clitext.Key
}

func (f sessionPRError) Error() string { return string(f.key) }

var (
	errSessionPRNone    = sessionPRError{clitext.SessionPRNone}
	errPRBranchUnpushed = sessionPRError{clitext.SessionPRUnpushed}
	errPRNoBase         = sessionPRError{clitext.SessionPRNoBase}
	errPRNoDir          = sessionPRError{clitext.SessionPRNoDir}
	errPRRemote         = sessionPRError{clitext.SessionPRRemote}
	errPRNoOrigin       = sessionPRError{clitext.SessionPRNoOrigin}
)

type sessionPRs struct {
	messages    *clitext.Messages
	lang        languageSource
	store       *state.State
	opener      forgeOpener
	credentials contracts.ForgeCredentials
	refreshes   *refreshLocks
	forgeLimit  time.Duration
}

type prTarget struct {
	name     string
	language i18n.Language
	forge    contracts.Forge
	ref      contracts.PullRequestRef
}

type prStatusView struct {
	Number     int    `json:"number"`
	Title      string `json:"title"`
	URL        string `json:"url"`
	State      string `json:"state"`
	Draft      bool   `json:"draft"`
	Merge      string `json:"merge"`
	Head       string `json:"head"`
	Base       string `json:"base"`
	HeadSHA    string `json:"headSha"`
	Unrecorded bool   `json:"unrecorded,omitempty"`
}

type prCheckView struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	URL        string `json:"url"`
}

type prThreadView struct {
	Path     string          `json:"path"`
	Line     int             `json:"line"`
	Resolved bool            `json:"resolved"`
	Comments []prCommentView `json:"comments"`
}

type prCommentView struct {
	Author    string    `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
}

func addSessionPRVerbs(reg *cli.Registry, messages *clitext.Messages, lang languageSource, store *state.State, opener forgeOpener, credentials contracts.ForgeCredentials) error {
	return newSessionPRs(messages, lang, store, opener, credentials, sessionPRForgeLimit).addTo(reg)
}

func newSessionPRs(messages *clitext.Messages, lang languageSource, store *state.State, opener forgeOpener, credentials contracts.ForgeCredentials, forgeLimit time.Duration) sessionPRs {
	return sessionPRs{messages: messages, lang: lang, store: store, opener: opener, credentials: credentials, refreshes: newRefreshLocks(), forgeLimit: forgeLimit}
}

func (p sessionPRs) addTo(reg *cli.Registry) error {
	return addVerbs(reg, p.openVerb(), p.statusVerb(), p.checksVerb(), p.commentsVerb())
}

func (p sessionPRs) openVerb() contracts.Cmd {
	return contracts.New(nsSession, verbPR, verbPROpen).
		Help(clitext.CmdHelpSessionPROpen).
		Param(paramName, clitext.CmdParamSessionPRName, true).
		ValueParam(paramTitle, clitext.CmdParamSessionPRTitle, true).
		ValueParam(paramBody, clitext.CmdParamSessionPRBody, false).
		ValueParam(paramBase, clitext.CmdParamSessionPRBase, false).
		Param(paramDraft, clitext.CmdParamSessionPRDraft, false).
		Do(func(ctx context.Context, input contracts.Input) (contracts.Result, error) {
			return cli.Data(p.openPR(ctx, input))
		})
}

func (p sessionPRs) statusVerb() contracts.Cmd {
	return contracts.New(nsSession, verbPR, verbPRStatus).
		Help(clitext.CmdHelpSessionPRStatus).
		Param(paramName, clitext.CmdParamSessionPRName, true).
		Do(p.status)
}

func (p sessionPRs) checksVerb() contracts.Cmd {
	return contracts.New(nsSession, verbPR, verbPRChecks).
		Help(clitext.CmdHelpSessionPRChecks).
		Param(paramName, clitext.CmdParamSessionPRName, true).
		Do(p.checks)
}

func (p sessionPRs) commentsVerb() contracts.Cmd {
	return contracts.New(nsSession, verbPR, verbPRComments).
		Help(clitext.CmdHelpSessionPRComments).
		Param(paramName, clitext.CmdParamSessionPRName, true).
		Do(p.comments)
}

func (p sessionPRs) pushedBranch(ctx context.Context, language i18n.Language, dir string) (contracts.ForgeRepoRef, string, error) {
	repo, err := prOriginRepo(ctx, dir)
	if err != nil {
		return contracts.ForgeRepoRef{}, "", p.failure(language, err)
	}
	branch, err := prGit(ctx, dir, nil, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil || branch == "" {
		return contracts.ForgeRepoRef{}, "", p.messages.Failure(language, errPRBranchUnpushed, errPRBranchUnpushed.key, clitext.Params{clitext.ParamBranch: "HEAD"})
	}
	env, err := forge.GitEnvironment(ctx, p.credentials, repo.Host())
	if err != nil {
		return contracts.ForgeRepoRef{}, "", p.failure(language, err)
	}
	pushed, err := branchPushed(ctx, dir, branch, env)
	if err != nil {
		return contracts.ForgeRepoRef{}, "", p.failure(language, err)
	}
	if !pushed {
		return contracts.ForgeRepoRef{}, "", p.messages.Failure(language, errPRBranchUnpushed, errPRBranchUnpushed.key, clitext.Params{clitext.ParamBranch: escapeLine(branch)})
	}
	return repo, branch, nil
}

func (p sessionPRs) openPR(ctx context.Context, input contracts.Input) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, p.forgeLimit)
	defer cancel()
	name := input.Get(paramName)
	language := namedSessionLanguage(p.store, name, p.lang())
	if err := p.store.ReservePullRequest(name); err != nil {
		return "", p.messages.Failure(language, err, clitext.SessionPROpening, clitext.Params{clitext.ParamName: escapeLine(name)})
	}
	defer p.store.ReleasePullRequest(name)
	sess, err := p.session(name)
	if err != nil {
		return "", err
	}
	if refusal := p.remoteRefusal(language, name, sess); refusal != nil {
		return "", refusal
	}
	if sess.PullRequest != 0 {
		return "", p.messages.Failure(language, state.ErrPullRequestRecorded, clitext.SessionPRRecorded, clitext.Params{clitext.ParamName: escapeLine(name), clitext.ParamNumber: sess.PullRequest})
	}
	dir, err := p.workDir(language, name, sess)
	if err != nil {
		return "", err
	}
	repo, branch, err := p.pushedBranch(ctx, language, dir)
	if err != nil {
		return "", err
	}
	base := input.Get(paramBase)
	if base == "" {
		base, err = defaultBranch(ctx, dir)
		if err != nil {
			return "", p.failure(language, err)
		}
	}
	client, err := p.opener(ctx, repo.Host())
	if err != nil {
		return "", p.failure(language, err)
	}
	opened, err := client.OpenPullRequest(ctx, contracts.PullRequestDraft{Repo: repo, Head: branch, Base: base, Title: input.Get(paramTitle), Body: input.Get(paramBody), Draft: input.Bool(paramDraft)})
	if err != nil {
		return "", p.failure(language, err)
	}
	number := opened.Ref.Number()
	record := state.PullRequestRecord{Number: number, State: opened.State, Draft: opened.Draft, URL: opened.URL, Revision: opened.Revision}
	if err := p.store.RecordPullRequest(name, record); err != nil {
		return "", p.messages.Failure(language, err, clitext.SessionPROpenedUnrecorded, clitext.Params{
			clitext.ParamName:   escapeLine(name),
			clitext.ParamNumber: number,
			clitext.ParamURL:    escapeLine(opened.URL),
			clitext.ParamErr:    escapeLine(err.Error()),
		})
	}
	return p.messages.Text(language, clitext.SessionPROpened, clitext.Params{clitext.ParamName: escapeLine(name), clitext.ParamNumber: number, clitext.ParamURL: escapeLine(opened.URL)})
}

func (p sessionPRs) checks(ctx context.Context, input contracts.Input) (contracts.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, p.forgeLimit)
	defer cancel()
	target, err := p.target(ctx, input.Get(paramName))
	if err != nil {
		return contracts.Result{}, err
	}
	pullRequest, err := target.forge.PullRequest(ctx, target.ref, contracts.Revision(""))
	if err != nil {
		return contracts.Result{}, p.failure(target.language, err)
	}
	checks, err := forge.CollectPages(ctx, func(ctx context.Context, page contracts.PageRequest) (contracts.Page[contracts.Check], error) {
		return target.forge.Checks(ctx, target.ref.Repo(), pullRequest.HeadSHA, page)
	})
	if err != nil {
		return contracts.Result{}, p.failure(target.language, err)
	}
	if input.JSON {
		views := make([]prCheckView, 0, len(checks))
		for _, check := range checks {
			views = append(views, checkView(check))
		}
		return cli.Data(marshalJSON(views))
	}
	if len(checks) == 0 {
		text, err := p.messages.Text(target.language, clitext.SessionPRChecksEmpty, clitext.Params{clitext.ParamNumber: target.ref.Number()})
		return cli.Data(text, err)
	}
	lines := make([]string, 0, len(checks))
	for _, check := range checks {
		word, err := p.checkWord(target.language, check)
		if err != nil {
			return contracts.Result{}, err
		}
		line, err := p.messages.Text(target.language, clitext.SessionPRCheck, clitext.Params{
			clitext.ParamStatus: word,
			clitext.ParamName:   escapeLine(check.Name),
			clitext.ParamURL:    escapeLine(check.URL),
		})
		if err != nil {
			return contracts.Result{}, err
		}
		lines = append(lines, line)
	}
	return cli.Data(strings.Join(lines, "\n"), nil)
}

func (p sessionPRs) comments(ctx context.Context, input contracts.Input) (contracts.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, p.forgeLimit)
	defer cancel()
	target, err := p.target(ctx, input.Get(paramName))
	if err != nil {
		return contracts.Result{}, err
	}
	threads, err := forge.CollectPages(ctx, func(ctx context.Context, page contracts.PageRequest) (contracts.Page[contracts.ReviewThread], error) {
		return target.forge.ReviewThreads(ctx, target.ref, page)
	})
	if err != nil {
		return contracts.Result{}, p.failure(target.language, err)
	}
	slices.SortStableFunc(threads, compareThreads)
	if input.JSON {
		views := make([]prThreadView, 0, len(threads))
		for _, thread := range threads {
			views = append(views, threadView(thread))
		}
		return cli.Data(marshalJSON(views))
	}
	if len(threads) == 0 {
		text, err := p.messages.Text(target.language, clitext.SessionPRCommentsEmpty, clitext.Params{clitext.ParamNumber: target.ref.Number()})
		return cli.Data(text, err)
	}
	blocks := make([]string, 0, len(threads))
	for _, thread := range threads {
		block, err := p.threadText(target.language, thread)
		if err != nil {
			return contracts.Result{}, err
		}
		blocks = append(blocks, block)
	}
	return cli.Data(strings.Join(blocks, "\n"), nil)
}

func (p sessionPRs) threadText(language i18n.Language, thread contracts.ReviewThread) (string, error) {
	resolution := clitext.Key(clitext.SessionPRUnresolved)
	if thread.Resolved {
		resolution = clitext.SessionPRResolved
	}
	word, err := p.messages.Text(language, resolution, nil)
	if err != nil {
		return "", err
	}
	header, err := p.messages.Text(language, clitext.SessionPRThread, clitext.Params{
		clitext.ParamPath:       escapeLine(thread.Path),
		clitext.ParamLine:       thread.Line,
		clitext.ParamResolution: word,
	})
	if err != nil {
		return "", err
	}
	lines := []string{header}
	for _, comment := range thread.Comments {
		line, err := p.messages.Text(language, clitext.SessionPRComment, clitext.Params{
			clitext.ParamAuthor: escapeLine(comment.Author.Login),
			clitext.ParamBody:   escapeLines(comment.Body),
		})
		if err != nil {
			return "", err
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n"), nil
}

func (p sessionPRs) session(name string) (state.Session, error) {
	sess, ok := p.store.FindSession(name)
	if !ok {
		return state.Session{}, &NotFoundError{Kind: kindSession, Name: name}
	}
	return sess, nil
}

func (p sessionPRs) remoteRefusal(language i18n.Language, name string, sess state.Session) error {
	if sess.Host == "" {
		return nil
	}
	return p.messages.Failure(language, errPRRemote, errPRRemote.key, clitext.Params{clitext.ParamName: escapeLine(name)})
}

func (p sessionPRs) workDir(language i18n.Language, name string, sess state.Session) (string, error) {
	dir := sessionDir(sess)
	if dir == "" {
		return "", p.messages.Failure(language, errPRNoDir, errPRNoDir.key, clitext.Params{clitext.ParamName: escapeLine(name)})
	}
	return dir, nil
}

func (p sessionPRs) target(ctx context.Context, name string) (prTarget, error) {
	sess, err := p.session(name)
	if err != nil {
		return prTarget{}, err
	}
	language := namedSessionLanguage(p.store, name, p.lang())
	if refusal := p.remoteRefusal(language, name, sess); refusal != nil {
		return prTarget{}, refusal
	}
	if sess.PullRequest == 0 {
		return prTarget{}, p.messages.Failure(language, errSessionPRNone, errSessionPRNone.key, clitext.Params{clitext.ParamName: escapeLine(name)})
	}
	dir, err := p.workDir(language, name, sess)
	if err != nil {
		return prTarget{}, err
	}
	repo, err := prOriginRepo(ctx, dir)
	if err != nil {
		return prTarget{}, p.failure(language, err)
	}
	ref, err := contracts.NewPullRequestRef(repo, sess.PullRequest)
	if err != nil {
		return prTarget{}, p.failure(language, err)
	}
	client, err := p.opener(ctx, repo.Host())
	if err != nil {
		return prTarget{}, p.failure(language, err)
	}
	return prTarget{name: name, language: language, forge: client, ref: ref}, nil
}

func (p sessionPRs) failure(language i18n.Language, err error) error {
	var limited *contracts.ForgeRateLimitedError
	var origin prOriginError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return p.messages.Failure(language, err, clitext.SessionPRTimedOut, nil)
	case errors.Is(err, contracts.ErrForgeNotConnected):
		return p.messages.Failure(language, err, clitext.SessionPRNotConnected, nil)
	case errors.Is(err, contracts.ErrForgeUnauthorized):
		return p.messages.Failure(language, err, clitext.SessionPRUnauthorized, nil)
	case errors.As(err, &limited):
		return p.messages.Failure(language, err, clitext.SessionPRRateLimited, clitext.Params{clitext.ParamReset: resetText(limited.Reset)})
	case errors.Is(err, contracts.ErrForgeWrongHost):
		return p.messages.Failure(language, err, clitext.SessionPRWrongHost, nil)
	case errors.Is(err, forgeport.ErrNoPlugin):
		return p.messages.Failure(language, err, clitext.SessionPRForgeMissing, nil)
	case errors.Is(err, forge.ErrTooManyPages):
		return p.messages.Failure(language, err, clitext.SessionPRPagesExceeded, clitext.Params{clitext.ParamPages: forge.MaxPages})
	case errors.Is(err, errPRNoBase):
		return p.messages.Failure(language, err, errPRNoBase.key, nil)
	case errors.As(err, &origin):
		return p.originFailure(language, origin)
	}
	return p.messages.Failure(language, err, clitext.SessionPRFailed, clitext.Params{clitext.ParamErr: escapeLine(err.Error())})
}

func (p sessionPRs) originFailure(language i18n.Language, origin prOriginError) error {
	if errors.Is(origin.cause, errPRNoOrigin) {
		return p.messages.Failure(language, origin, clitext.SessionPRNoOrigin, clitext.Params{clitext.ParamPath: escapeLine(origin.dir)})
	}
	return p.messages.Failure(language, origin, clitext.SessionPROriginRead, clitext.Params{clitext.ParamPath: escapeLine(origin.dir), clitext.ParamErr: escapeLine(origin.cause.Error())})
}

func compareThreads(a, b contracts.ReviewThread) int {
	return cmp.Compare(resolvedRank(a.Resolved), resolvedRank(b.Resolved))
}

func resolvedRank(resolved bool) int {
	if resolved {
		return 1
	}
	return 0
}

func checkView(check contracts.Check) prCheckView {
	view := prCheckView{Name: check.Name, Status: check.Status.String(), URL: check.URL}
	if conclusion, done := check.Concluded(); done {
		view.Conclusion = conclusion.String()
	}
	return view
}

func threadView(thread contracts.ReviewThread) prThreadView {
	view := prThreadView{Path: thread.Path, Line: thread.Line, Resolved: thread.Resolved, Comments: make([]prCommentView, 0, len(thread.Comments))}
	for _, comment := range thread.Comments {
		view.Comments = append(view.Comments, prCommentView{Author: comment.Author.Login, Body: comment.Body, CreatedAt: comment.CreatedAt})
	}
	return view
}

func resetText(reset time.Time) string {
	if reset.IsZero() {
		return resetUnknown
	}
	return reset.UTC().Format(time.RFC3339)
}

func escapeLine(text string) string {
	return termpaint.Escape(text).String()
}

func escapeLines(text string) string {
	return termpaint.EscapeLines(text).String()
}
