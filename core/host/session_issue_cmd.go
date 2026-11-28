package host

import (
	"context"
	"strconv"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/forge"
)

const (
	verbIssue        = "issue"
	verbIssueList    = "list"
	verbIssueShow    = "show"
	verbIssueOpen    = "open"
	verbIssueComment = "comment"
	paramNumber      = "number"
	paramLabels      = "labels"
	paramState       = "state"
	paramLimit       = "limit"
	defaultIssues    = 100
	maxIssues        = forge.MaxPages * contracts.MaxPageLimit
	issueStateClosed = "closed"
	issueStateOpen   = "open"
)

var (
	errIssueNoWriter = sessionPRError{clitext.SessionIssueNoWriter}
	errIssueNumber   = sessionPRError{clitext.SessionIssueBadNumber}
	errIssueState    = sessionPRError{clitext.SessionIssueBadState}
	errIssueLimit    = sessionPRError{clitext.SessionIssueBadLimit}
)

type issueView struct {
	Number int      `json:"number"`
	Title  string   `json:"title"`
	State  string   `json:"state"`
	URL    string   `json:"url"`
	Author string   `json:"author"`
	Labels []string `json:"labels"`
	Body   string   `json:"body,omitempty"`
}

type issueTarget struct {
	language i18n.Language
	forge    contracts.Forge
	repo     contracts.ForgeRepoRef
}

func (p sessionPRs) issueVerbs() []contracts.Cmd {
	return []contracts.Cmd{
		contracts.New(nsSession, verbIssue, verbIssueList).
			Help(clitext.CmdHelpSessionIssueList).
			Param(paramName, clitext.CmdParamSessionIssueName, true).
			ValueParam(paramState, clitext.CmdParamSessionIssueState, false).
			ValueParam(paramLabels, clitext.CmdParamSessionIssueLabels, false).
			ValueParam(paramLimit, clitext.CmdParamSessionIssueLimit, false).
			Do(p.listIssues),
		contracts.New(nsSession, verbIssue, verbIssueShow).
			Help(clitext.CmdHelpSessionIssueShow).
			Param(paramName, clitext.CmdParamSessionIssueName, true).
			ValueParam(paramNumber, clitext.CmdParamSessionIssueNumber, true).
			Do(p.showIssue),
		contracts.New(nsSession, verbIssue, verbIssueOpen).
			Help(clitext.CmdHelpSessionIssueOpen).
			Param(paramName, clitext.CmdParamSessionIssueName, true).
			ValueParam(paramTitle, clitext.CmdParamSessionIssueTitle, true).
			ValueParam(paramBody, clitext.CmdParamSessionIssueBody, false).
			ValueParam(paramLabels, clitext.CmdParamSessionIssueLabels, false).
			Do(p.openIssue),
		contracts.New(nsSession, verbIssue, verbIssueComment).
			Help(clitext.CmdHelpSessionIssueComment).
			Param(paramName, clitext.CmdParamSessionIssueName, true).
			ValueParam(paramNumber, clitext.CmdParamSessionIssueNumber, true).
			ValueParam(paramBody, clitext.CmdParamSessionIssueBody, true).
			Do(p.commentIssue),
	}
}

func (p sessionPRs) issueTarget(ctx context.Context, name string) (issueTarget, error) {
	sess, err := p.session(name)
	if err != nil {
		return issueTarget{}, err
	}
	language := namedSessionLanguage(p.store, name, p.lang())
	if refusal := p.remoteRefusal(language, name, sess); refusal != nil {
		return issueTarget{}, refusal
	}
	dir, err := p.workDir(language, name, sess)
	if err != nil {
		return issueTarget{}, err
	}
	repo, err := prOriginRepo(ctx, dir)
	if err != nil {
		return issueTarget{}, p.failure(language, err)
	}
	client, err := p.opener(ctx, repo.Host())
	if err != nil {
		return issueTarget{}, p.failure(language, err)
	}
	return issueTarget{language: language, forge: client, repo: repo}, nil
}

func (p sessionPRs) issueRef(target issueTarget, raw string) (contracts.IssueRef, error) {
	number, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(raw), "#"))
	if err != nil || number <= 0 {
		return contracts.IssueRef{}, p.messages.Failure(target.language, errIssueNumber, errIssueNumber.key, clitext.Params{clitext.ParamNumber: escapeLine(raw)})
	}
	ref, err := contracts.NewIssueRef(target.repo, number)
	if err != nil {
		return contracts.IssueRef{}, p.failure(target.language, err)
	}
	return ref, nil
}

func (p sessionPRs) writer(target issueTarget) (contracts.IssueWriter, error) {
	writer, ok := target.forge.(contracts.IssueWriter)
	if !ok {
		return nil, p.messages.Failure(target.language, errIssueNoWriter, errIssueNoWriter.key, nil)
	}
	return writer, nil
}

func (p sessionPRs) issueQuery(language i18n.Language, input contracts.Input) (contracts.IssueQuery, error) {
	query := contracts.IssueQuery{Labels: splitLabels(input.Get(paramLabels))}
	switch strings.ToLower(strings.TrimSpace(input.Get(paramState))) {
	case "", issueStateOpen:
		query.State = contracts.IssueOpen
	case issueStateClosed:
		query.State = contracts.IssueClosed
	default:
		return contracts.IssueQuery{}, p.messages.Failure(language, errIssueState, errIssueState.key, clitext.Params{clitext.ParamState: escapeLine(input.Get(paramState))})
	}
	return query, nil
}

func splitLabels(raw string) []string {
	var labels []string
	for _, label := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(label); trimmed != "" {
			labels = append(labels, trimmed)
		}
	}
	return labels
}

func (p sessionPRs) listIssues(ctx context.Context, input contracts.Input) (contracts.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, p.forgeLimit)
	defer cancel()
	target, err := p.issueTarget(ctx, input.Get(paramName))
	if err != nil {
		return contracts.Result{}, err
	}
	query, err := p.issueQuery(target.language, input)
	if err != nil {
		return contracts.Result{}, err
	}
	limit, err := p.issueLimit(target.language, input.Get(paramLimit))
	if err != nil {
		return contracts.Result{}, err
	}
	issues, truncated, err := forge.CollectUpTo(ctx, limit, isIssueOnly, func(ctx context.Context, page contracts.PageRequest) (contracts.Page[contracts.Issue], error) {
		return target.forge.Issues(ctx, target.repo, query, page)
	})
	if err != nil {
		return contracts.Result{}, p.failure(target.language, err)
	}
	views := make([]issueView, 0, len(issues))
	for index := range issues {
		view := viewOfIssue(issues[index])
		view.Body = ""
		views = append(views, view)
	}
	if input.JSON {
		return cli.Data(marshalJSON(issueListing{Issues: views, Truncated: truncated}))
	}
	if len(views) == 0 {
		return cli.Data(p.messages.Text(target.language, clitext.SessionIssueNone, clitext.Params{clitext.ParamRepo: target.repo.String()}))
	}
	lines := make([]string, 0, len(views))
	for _, view := range views {
		lines = append(lines, "#"+strconv.Itoa(view.Number)+"\t"+view.State+"\t"+escapeLine(view.Title)+"\t"+view.URL)
	}
	if truncated {
		more, err := p.messages.Text(target.language, clitext.SessionIssueTruncated, clitext.Params{clitext.ParamCount: limit})
		if err != nil {
			return contracts.Result{}, err
		}
		lines = append(lines, more)
	}
	return cli.Data(strings.Join(lines, "\n"), nil)
}

type issueListing struct {
	Issues    []issueView `json:"issues"`
	Truncated bool        `json:"truncated"`
}

func (p sessionPRs) issueLimit(language i18n.Language, raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return defaultIssues, nil
	}
	limit, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || limit <= 0 || limit > maxIssues {
		return 0, p.messages.Failure(language, errIssueLimit, errIssueLimit.key, clitext.Params{clitext.ParamCount: maxIssues})
	}
	return limit, nil
}

func (p sessionPRs) showIssue(ctx context.Context, input contracts.Input) (contracts.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, p.forgeLimit)
	defer cancel()
	target, err := p.issueTarget(ctx, input.Get(paramName))
	if err != nil {
		return contracts.Result{}, err
	}
	ref, err := p.issueRef(target, input.Get(paramNumber))
	if err != nil {
		return contracts.Result{}, err
	}
	issue, err := target.forge.Issue(ctx, ref)
	if err != nil {
		return contracts.Result{}, p.failure(target.language, err)
	}
	view := viewOfIssue(issue)
	if input.JSON {
		return cli.Data(marshalJSON(view))
	}
	head := "#" + strconv.Itoa(view.Number) + " " + escapeLine(view.Title) + "\n" + view.State + "\t" + view.URL
	if len(view.Labels) > 0 {
		head += "\n" + strings.Join(view.Labels, ", ")
	}
	return cli.Data(head+"\n\n"+view.Body, nil)
}

func (p sessionPRs) openIssue(ctx context.Context, input contracts.Input) (contracts.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, p.forgeLimit)
	defer cancel()
	target, err := p.issueTarget(ctx, input.Get(paramName))
	if err != nil {
		return contracts.Result{}, err
	}
	writer, err := p.writer(target)
	if err != nil {
		return contracts.Result{}, err
	}
	opened, err := writer.OpenIssue(ctx, contracts.IssueDraft{Repo: target.repo, Title: input.Get(paramTitle), Body: input.Get(paramBody), Labels: splitLabels(input.Get(paramLabels))})
	if err != nil {
		return contracts.Result{}, p.failure(target.language, err)
	}
	if input.JSON {
		return cli.Data(marshalJSON(viewOfIssue(opened)))
	}
	return cli.Data(p.messages.Text(target.language, clitext.SessionIssueOpened, clitext.Params{clitext.ParamNumber: opened.Ref.Number(), clitext.ParamURL: escapeLine(opened.URL)}))
}

func (p sessionPRs) commentIssue(ctx context.Context, input contracts.Input) (contracts.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, p.forgeLimit)
	defer cancel()
	target, err := p.issueTarget(ctx, input.Get(paramName))
	if err != nil {
		return contracts.Result{}, err
	}
	ref, err := p.issueRef(target, input.Get(paramNumber))
	if err != nil {
		return contracts.Result{}, err
	}
	writer, err := p.writer(target)
	if err != nil {
		return contracts.Result{}, err
	}
	if err := writer.CommentOnIssue(ctx, ref, input.Get(paramBody)); err != nil {
		return contracts.Result{}, p.failure(target.language, err)
	}
	return cli.Data(p.messages.Text(target.language, clitext.SessionIssueCommented, clitext.Params{clitext.ParamNumber: ref.Number()}))
}

func isIssueOnly(issue contracts.Issue) bool {
	return !issue.IsPullRequest
}

func viewOfIssue(issue contracts.Issue) issueView {
	state := issueStateOpen
	if issue.State != contracts.IssueOpen {
		state = issueStateClosed
	}
	labels := issue.Labels
	if labels == nil {
		labels = []string{}
	}
	return issueView{Number: issue.Ref.Number(), Title: issue.Title, State: state, URL: issue.URL, Author: issue.Author.Login, Labels: labels, Body: issue.Body}
}
