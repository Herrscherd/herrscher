package issuetrigger

import (
	"context"
	"errors"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

type listingReader struct {
	forge       contracts.Forge
	repo        contracts.ForgeRepoRef
	query       contracts.IssueQuery
	revision    contracts.Revision
	conditional contracts.ConditionalIssues
	notModified bool
	latest      time.Time
	revisionOut contracts.Revision
}

func newListingReader(forgeClient contracts.Forge, repo contracts.ForgeRepoRef, query contracts.IssueQuery, revision contracts.Revision) *listingReader {
	reader := &listingReader{forge: forgeClient, repo: repo, query: query, revision: revision}
	if conditional, ok := forgeClient.(contracts.ConditionalIssues); ok {
		reader.conditional = conditional
	}
	return reader
}

func (r *listingReader) keep(issue contracts.Issue, taken map[int]bool) bool {
	if issue.UpdatedAt.After(r.latest) {
		r.latest = issue.UpdatedAt
	}
	return !issue.IsPullRequest && Trusted(issue.Author) && !taken[issue.Ref.Number()]
}

func (r *listingReader) fetch(ctx context.Context, page contracts.PageRequest) (contracts.Page[contracts.Issue], error) {
	if r.conditional == nil {
		return r.forge.Issues(ctx, r.repo, r.query, page)
	}
	since := contracts.Revision("")
	if page.Cursor == "" {
		since = r.revision
	}
	listing, err := r.conditional.IssuesSince(ctx, r.repo, r.query, page, since)
	if errors.Is(err, contracts.ErrForgeNotModified) {
		r.notModified = true
		return contracts.Page[contracts.Issue]{}, nil
	}
	if err != nil {
		return contracts.Page[contracts.Issue]{}, err
	}
	if page.Cursor == "" {
		r.revisionOut = listing.Revision
	}
	return listing.Page, nil
}
