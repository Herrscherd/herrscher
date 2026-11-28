package issuetrigger

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/internal/forge"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const MaxSessionsPerPoll = 3

const maxSessionName = 64

var errWatchMoved = errors.New("issue watch moved by another read")

type Opener func(ctx context.Context, host contracts.ForgeHost) (contracts.Forge, error)

type Creator interface {
	Create(context.Context, contracts.CreateSession) (contracts.Result, error)
}

type Store interface {
	IssueTriggerRepo(repo string) state.IssueTriggerState
	SessionNamed(name string) bool
	AdvanceIssueWatch(repo, filter string, expected, next state.IssueWatch) (bool, error)
	ClaimIssue(repo string, number int, session string) (bool, error)
	ReleaseIssue(repo string, number int) error
}

type Poller struct {
	store   Store
	open    Opener
	creator Creator
	seed    func(session, task string) bool
	task    func(contracts.ForgeRepoRef, contracts.Issue) string
	now     func() time.Time
}

func NewPoller(store Store, open Opener, creator Creator, seed func(session, task string) bool,
	task func(contracts.ForgeRepoRef, contracts.Issue) string, now func() time.Time,
) *Poller {
	return &Poller{store: store, open: open, creator: creator, seed: seed, task: task, now: now}
}

func (p *Poller) Poll(ctx context.Context, target Target) error {
	forgeClient, err := p.open(ctx, target.Repo.Host())
	if err != nil {
		return fmt.Errorf("issue trigger %s: open forge: %w", target.Repo.String(), err)
	}
	remaining := MaxSessionsPerPoll
	for _, filter := range target.Filters {
		if remaining == 0 {
			break
		}
		opened, err := p.pollFilter(ctx, forgeClient, target, filter, remaining)
		if err != nil {
			return err
		}
		remaining -= opened
	}
	return nil
}

func (p *Poller) pollFilter(ctx context.Context, forgeClient contracts.Forge, target Target, filter Filter, limit int) (int, error) {
	repo := target.Repo.String()
	record := p.store.IssueTriggerRepo(repo)
	watch := record.Watches[filter.Key()]
	if watch.Since.IsZero() {
		_, err := p.store.AdvanceIssueWatch(repo, filter.Key(), state.IssueWatch{}, state.IssueWatch{Since: p.now().UTC()})
		return 0, err
	}
	reader := newListingReader(forgeClient, target.Repo, filter.query(watch.Since), watch.Revision)
	taken := takenNumbers(record)
	issues, truncated, err := forge.CollectUpTo(ctx, limit, func(issue contracts.Issue) bool {
		return reader.keep(issue, taken)
	}, reader.fetch)
	if err != nil {
		return 0, fmt.Errorf("issue trigger %s: read %s: %w", repo, filter.Key(), err)
	}
	if reader.notModified {
		return 0, nil
	}
	writer := &watchWriter{store: p.store, repo: repo, filter: filter.Key(), current: watch}
	opened, err := p.openIssues(ctx, target, issues, writer)
	if errors.Is(err, errWatchMoved) {
		return opened, nil
	}
	if err != nil {
		return opened, err
	}
	if err := writer.settle(reader, truncated, len(issues), watch.Since); err != nil && !errors.Is(err, errWatchMoved) {
		return opened, err
	}
	return opened, nil
}

func (p *Poller) openIssues(ctx context.Context, target Target, issues []contracts.Issue, writer *watchWriter) (int, error) {
	repo := target.Repo.String()
	opened := 0
	for _, issue := range issues {
		number := issue.Ref.Number()
		name := SessionName(target.Repo, number)
		claimed, err := p.store.ClaimIssue(repo, number, name)
		if err != nil {
			return opened, fmt.Errorf("issue trigger %s: claim #%d: %w", repo, number, err)
		}
		if claimed && !p.store.SessionNamed(name) {
			if err := p.openSession(ctx, target, issue, name); err != nil {
				return opened, err
			}
			opened++
		}
		if err := writer.moveTo(state.IssueWatch{Since: issue.UpdatedAt}); err != nil {
			return opened, err
		}
	}
	return opened, nil
}

func (p *Poller) openSession(ctx context.Context, target Target, issue contracts.Issue, name string) error {
	repo := target.Repo.String()
	number := issue.Ref.Number()
	spec := contracts.CreateSession{Name: name, Agent: target.Agent, Clone: "https://" + repo}
	if _, err := p.creator.Create(ctx, spec); err != nil {
		if releaseErr := p.store.ReleaseIssue(repo, number); releaseErr != nil {
			return errors.Join(fmt.Errorf("issue trigger %s: open #%d: %w", repo, number, err), releaseErr)
		}
		return fmt.Errorf("issue trigger %s: open #%d: %w", repo, number, err)
	}
	if !p.seed(name, p.task(target.Repo, issue)) {
		slog.Warn("issue trigger: the session opened but took no task", "repo", repo, "session", name)
	}
	return nil
}

type watchWriter struct {
	store   Store
	repo    string
	filter  string
	current state.IssueWatch
}

func (w *watchWriter) moveTo(next state.IssueWatch) error {
	if sameWatchState(next, w.current) {
		return nil
	}
	moved, err := w.store.AdvanceIssueWatch(w.repo, w.filter, w.current, next)
	if err != nil {
		return fmt.Errorf("issue trigger %s: advance %s: %w", w.repo, w.filter, err)
	}
	if !moved {
		return errWatchMoved
	}
	w.current = next
	return nil
}

func (w *watchWriter) settle(reader *listingReader, truncated bool, handled int, readSince time.Time) error {
	if truncated {
		if handled == 0 && reader.latest.After(w.current.Since) {
			if err := w.moveTo(state.IssueWatch{Since: reader.latest}); err != nil {
				return err
			}
		}
		return w.moveTo(state.IssueWatch{Since: w.current.Since})
	}
	if reader.latest.After(w.current.Since) {
		return w.moveTo(state.IssueWatch{Since: reader.latest})
	}
	if !w.current.Since.Equal(readSince) {
		return nil
	}
	return w.moveTo(state.IssueWatch{Since: w.current.Since, Revision: reader.revisionOut})
}

func sameWatchState(a, b state.IssueWatch) bool {
	return a.Since.Equal(b.Since) && a.Revision == b.Revision
}

func takenNumbers(record state.IssueTriggerState) map[int]bool {
	taken := make(map[int]bool, len(record.Taken))
	for _, entry := range record.Taken {
		taken[entry.Number] = true
	}
	return taken
}

func SessionName(repo contracts.ForgeRepoRef, number int) string {
	suffix := fmt.Sprintf("-%d", number)
	slug := sanitize("issue-" + repo.Namespace() + "-" + repo.Name())
	if room := maxSessionName - len(suffix); len(slug) > room {
		slug = strings.TrimRight(slug[:room], "-")
	}
	return slug + suffix
}

func sanitize(text string) string {
	var out strings.Builder
	for _, r := range text {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			out.WriteRune(r)
		default:
			out.WriteByte('-')
		}
	}
	return out.String()
}
