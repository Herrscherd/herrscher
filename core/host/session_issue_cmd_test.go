package host

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

type fakeIssueForge struct {
	contracts.Forge

	queries []contracts.IssueQuery
	pages   [][]contracts.Issue
	issue   contracts.Issue
	asked   []contracts.IssueRef
}

func (f *fakeIssueForge) Issues(_ context.Context, _ contracts.ForgeRepoRef, query contracts.IssueQuery, page contracts.PageRequest) (contracts.Page[contracts.Issue], error) {
	f.queries = append(f.queries, query)
	return pageAt(f.pages, page.Cursor), nil
}

func (f *fakeIssueForge) Issue(_ context.Context, ref contracts.IssueRef) (contracts.Issue, error) {
	f.asked = append(f.asked, ref)
	if f.issue.Ref.Number() != ref.Number() {
		return contracts.Issue{}, contracts.ErrForgeNotFound
	}
	return f.issue, nil
}

func issueNumbered(t *testing.T, number int, title string) contracts.Issue {
	t.Helper()
	ref, err := contracts.NewIssueRef(prRepoRef(t), number)
	if err != nil {
		t.Fatal(err)
	}
	return contracts.Issue{Ref: ref, Title: title, Body: "steps", URL: "https://github.com/acme/widget/issues/" + title, State: contracts.IssueOpen, Labels: []string{"bug"}}
}

func pullRequestNumbered(t *testing.T, number int, title string) contracts.Issue {
	t.Helper()
	issue := issueNumbered(t, number, title)
	issue.IsPullRequest = true
	return issue
}

func runIssue(t *testing.T, reg *cli.Registry, args ...string) (string, error) {
	t.Helper()
	res, err := reg.Dispatch(t.Context(), append([]string{nsSession, verbIssue}, args...))
	if err != nil {
		return "", err
	}
	return cli.Plain(res, nil)
}

func TestIssueListReadsEveryPageOfTheSessionsRepository(t *testing.T) {
	st := prState(t, prFixtureRepo(t), "")
	fake := &fakeIssueForge{pages: [][]contracts.Issue{{issueNumbered(t, 3, "one")}, {issueNumbered(t, 4, "two")}}}
	reg := prRegistry(t, st, prForgeOpener(fake))

	out, err := runIssue(t, reg, verbIssueList, "--name", "alpha", "--state", "closed", "--labels", "bug, ui", "--json")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var listing issueListing
	if err := json.Unmarshal([]byte(out), &listing); err != nil {
		t.Fatalf("list output %q: %v", out, err)
	}
	views := listing.Issues
	if listing.Truncated || len(views) != 2 || views[0].Number != 3 || views[1].Number != 4 || views[0].Body != "" {
		t.Fatalf("views = %+v, want #3 then #4 without their bodies", views)
	}
	if query := fake.queries[0]; query.State != contracts.IssueClosed || !slices.Equal(query.Labels, []string{"bug", "ui"}) {
		t.Fatalf("query = %+v, want closed issues labelled bug and ui", query)
	}
}

func TestIssueListLeavesPullRequestsOut(t *testing.T) {
	st := prState(t, prFixtureRepo(t), "")
	fake := &fakeIssueForge{pages: [][]contracts.Issue{
		{issueNumbered(t, 1, "a"), pullRequestNumbered(t, 2, "pr"), issueNumbered(t, 3, "b")},
		{pullRequestNumbered(t, 4, "pr"), issueNumbered(t, 5, "c")},
	}}
	reg := prRegistry(t, st, prForgeOpener(fake))

	out, err := runIssue(t, reg, verbIssueList, "--name", "alpha", "--json")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var listing issueListing
	if err := json.Unmarshal([]byte(out), &listing); err != nil {
		t.Fatalf("list output %q: %v", out, err)
	}
	numbers := make([]int, 0, len(listing.Issues))
	for _, view := range listing.Issues {
		numbers = append(numbers, view.Number)
	}
	if !slices.Equal(numbers, []int{1, 3, 5}) || listing.Truncated {
		t.Fatalf("numbers = %v truncated %v, want the issues #1, #3, #5 and no pull request", numbers, listing.Truncated)
	}
}

func TestIssueLimitCountsOnlyIssuesAcrossPages(t *testing.T) {
	st := prState(t, prFixtureRepo(t), "")
	fake := &fakeIssueForge{pages: [][]contracts.Issue{
		{pullRequestNumbered(t, 1, "pr"), pullRequestNumbered(t, 2, "pr"), issueNumbered(t, 3, "a")},
		{pullRequestNumbered(t, 4, "pr"), issueNumbered(t, 5, "b"), issueNumbered(t, 6, "c")},
	}}
	reg := prRegistry(t, st, prForgeOpener(fake))

	out, err := runIssue(t, reg, verbIssueList, "--name", "alpha", "--limit", "2")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, "#3") || !strings.Contains(out, "#5") || strings.Contains(out, "#6") || strings.Contains(out, "#1") || strings.Contains(out, "#4") {
		t.Fatalf("list output %q, want #3 and #5 only, the pull requests skipped and the limit reached on issues", out)
	}
	if !strings.Contains(out, "--limit") {
		t.Fatalf("list output %q, want the line naming --limit", out)
	}
}

func TestIssueShowRefusesAPullRequestNumberAsNotFound(t *testing.T) {
	st := prState(t, prFixtureRepo(t), "")
	fake := &fakeIssueForge{issue: issueNumbered(t, 7, "seven")}
	reg := prRegistry(t, st, prForgeOpener(fake))

	if _, err := runIssue(t, reg, verbIssueShow, "--name", "alpha", "--number", "8"); !errors.Is(err, contracts.ErrForgeNotFound) {
		t.Fatalf("show of a pull request number: %v, want the not-found refusal", err)
	}
}

func TestIssueListStopsAtItsLimitAndSaysSo(t *testing.T) {
	st := prState(t, prFixtureRepo(t), "")
	fake := &fakeIssueForge{pages: [][]contracts.Issue{{issueNumbered(t, 1, "a"), issueNumbered(t, 2, "b")}, {issueNumbered(t, 3, "c")}}}
	reg := prRegistry(t, st, prForgeOpener(fake))
	out, err := runIssue(t, reg, verbIssueList, "--name", "alpha", "--limit", "2")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, "#2") || strings.Contains(out, "#3") || !strings.Contains(out, "--limit") {
		t.Fatalf("list output %q, want the first two and a line naming --limit", out)
	}
	if _, err := runIssue(t, reg, verbIssueList, "--name", "alpha", "--limit", "0"); !errors.Is(err, errIssueLimit) {
		t.Fatalf("limit 0: %v, want the limit refusal", err)
	}
}

func TestIssueShowGivesTheBodyAndRefusesANumberThatIsNotOne(t *testing.T) {
	st := prState(t, prFixtureRepo(t), "")
	fake := &fakeIssueForge{issue: issueNumbered(t, 7, "seven")}
	reg := prRegistry(t, st, prForgeOpener(fake))

	out, err := runIssue(t, reg, verbIssueShow, "--name", "alpha", "--number", "#7")
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if !strings.Contains(out, "#7 seven") || !strings.Contains(out, "steps") {
		t.Fatalf("show output %q, want the title and the body of #7", out)
	}
	if _, err := runIssue(t, reg, verbIssueShow, "--name", "alpha", "--number", "seven"); !errors.Is(err, errIssueNumber) {
		t.Fatalf("show of a word: %v, want the number refusal", err)
	}
	if len(fake.asked) != 1 {
		t.Fatalf("the forge was asked %d times, want once: a bad number never reaches it", len(fake.asked))
	}
}

type writingIssueForge struct {
	fakeIssueForge

	drafts   []contracts.IssueDraft
	comments map[int]string
}

func (f *writingIssueForge) OpenIssue(_ context.Context, draft contracts.IssueDraft) (contracts.Issue, error) {
	f.drafts = append(f.drafts, draft)
	ref, err := contracts.NewIssueRef(draft.Repo, 9)
	if err != nil {
		return contracts.Issue{}, err
	}
	return contracts.Issue{Ref: ref, Title: draft.Title, URL: "https://github.com/acme/widget/issues/9", State: contracts.IssueOpen}, nil
}

func (f *writingIssueForge) CommentOnIssue(_ context.Context, ref contracts.IssueRef, body string) error {
	if f.comments == nil {
		f.comments = make(map[int]string)
	}
	f.comments[ref.Number()] = body
	return nil
}

func TestIssueOpenAndCommentGoThroughTheForgeOfTheSessionsRepository(t *testing.T) {
	st := prState(t, prFixtureRepo(t), "")
	fake := &writingIssueForge{}
	reg := prRegistry(t, st, prForgeOpener(fake))

	out, err := runIssue(t, reg, "open", "--name", "alpha", "--title", "Crash", "--body=--verbose breaks it", "--labels", "bug")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if !strings.Contains(out, "#9") || !strings.Contains(out, "issues/9") {
		t.Fatalf("open output %q, want #9 and its URL", out)
	}
	draft := fake.drafts[0]
	if draft.Title != "Crash" || draft.Body != "--verbose breaks it" || !slices.Equal(draft.Labels, []string{"bug"}) || draft.Repo.String() != prRepoRef(t).String() {
		t.Fatalf("draft = %+v, want the title, body and label on acme/widget", draft)
	}
	if _, err := runIssue(t, reg, "comment", "--name", "alpha", "--number", "9", "--body", "fixed"); err != nil {
		t.Fatalf("comment: %v", err)
	}
	if fake.comments[9] != "fixed" {
		t.Fatalf("comments = %v, want fixed on #9", fake.comments)
	}
}

func TestIssueOpenSaysSoWhenTheForgeCannotWrite(t *testing.T) {
	st := prState(t, prFixtureRepo(t), "")
	reg := prRegistry(t, st, prForgeOpener(&fakeIssueForge{}))
	if _, err := runIssue(t, reg, "open", "--name", "alpha", "--title", "Crash"); !errors.Is(err, errIssueNoWriter) {
		t.Fatalf("open on a read-only forge: %v, want the no-writer refusal", err)
	}
}

type replyingPRForge struct {
	fakePRForge

	replies map[string]string
}

func (f *replyingPRForge) ReplyToThread(_ context.Context, thread contracts.ThreadRef, body string) error {
	if f.replies == nil {
		f.replies = make(map[string]string)
	}
	f.replies[thread.ID()] = body
	return nil
}

func TestPRReplyAnswersTheNamedThreadOfTheSessionsPullRequest(t *testing.T) {
	st := prState(t, prFixtureRepo(t), "")
	if err := st.RecordPullRequest("alpha", state.PullRequestRecord{Number: prTestNumber, State: contracts.PullRequestOpen}); err != nil {
		t.Fatal(err)
	}
	fake := &replyingPRForge{fakePRForge: fakePRForge{pullRequest: prNumbered(t)}}
	reg := prRegistry(t, st, prForgeOpener(fake))
	out, err := runPR(t, reg, verbPRReply, "--name", "alpha", "--thread", "PRRT_1", "--body", "done in abc123")
	if err != nil {
		t.Fatalf("reply: %v", err)
	}
	if fake.replies["PRRT_1"] != "done in abc123" || !strings.Contains(out, "#42") {
		t.Fatalf("replies = %v, out = %q, want the body on PRRT_1 and #42 named", fake.replies, out)
	}
	if _, err := runPR(t, reg, verbPRReply, "--name", "alpha", "--thread", "PRRT_1", "--body", " "); err == nil {
		t.Fatal("an empty reply was sent")
	}
}
