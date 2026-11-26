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
	var views []issueView
	if err := json.Unmarshal([]byte(out), &views); err != nil {
		t.Fatalf("list output %q: %v", out, err)
	}
	if len(views) != 2 || views[0].Number != 3 || views[1].Number != 4 || views[0].Body != "" {
		t.Fatalf("views = %+v, want #3 then #4 without their bodies", views)
	}
	if query := fake.queries[0]; query.State != contracts.IssueClosed || !slices.Equal(query.Labels, []string{"bug", "ui"}) {
		t.Fatalf("query = %+v, want closed issues labelled bug and ui", query)
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
