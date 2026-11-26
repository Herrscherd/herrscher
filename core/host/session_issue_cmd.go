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
	paramNumber      = "number"
	paramLabels      = "labels"
	paramState       = "state"
	issueStateClosed = "closed"
	issueStateOpen   = "open"
)

var (
	errIssueNumber = sessionPRError{clitext.SessionIssueBadNumber}
	errIssueState  = sessionPRError{clitext.SessionIssueBadState}
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
			Do(p.listIssues),
		contracts.New(nsSession, verbIssue, verbIssueShow).
			Help(clitext.CmdHelpSessionIssueShow).
			Param(paramName, clitext.CmdParamSessionIssueName, true).
			ValueParam(paramNumber, clitext.CmdParamSessionIssueNumber, true).
			Do(p.showIssue),
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
	issues, err := forge.CollectPages(ctx, func(ctx context.Context, page contracts.PageRequest) (contracts.Page[contracts.Issue], error) {
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
		return cli.Data(marshalJSON(views))
	}
	if len(views) == 0 {
		return cli.Data(p.messages.Text(target.language, clitext.SessionIssueNone, clitext.Params{clitext.ParamRepo: target.repo.String()}))
	}
	lines := make([]string, 0, len(views))
	for _, view := range views {
		lines = append(lines, "#"+strconv.Itoa(view.Number)+"\t"+view.State+"\t"+escapeLine(view.Title)+"\t"+view.URL)
	}
	return cli.Data(strings.Join(lines, "\n"), nil)
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
