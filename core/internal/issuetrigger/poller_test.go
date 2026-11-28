package issuetrigger

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

var (
	errForgeDown = errors.New("forge down")
	t0           = time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
)

type fakeForge struct {
	contracts.Forge
	mu          sync.Mutex
	issues      []contracts.Issue
	mentions    map[int]string
	version     int
	conditional bool
	failNext    bool
	notModified int
}

func (f *fakeForge) touch(number int, updated time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.version++
	for idx := range f.issues {
		if f.issues[idx].Ref.Number() == number {
			f.issues[idx].UpdatedAt = t0.Add(updated)
		}
	}
}

func (f *fakeForge) add(issue contracts.Issue) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.version++
	f.issues = append(f.issues, issue)
}

func (f *fakeForge) selected(query contracts.IssueQuery) []contracts.Issue {
	var out []contracts.Issue
	for _, issue := range f.issues {
		if !matches(issue, query, f.mentions) {
			continue
		}
		out = append(out, issue)
	}
	slices.SortFunc(out, func(a, b contracts.Issue) int { return a.UpdatedAt.Compare(b.UpdatedAt) })
	return out
}

func matches(issue contracts.Issue, query contracts.IssueQuery, mentions map[int]string) bool {
	for _, label := range query.Labels {
		if !slices.Contains(issue.Labels, label) {
			return false
		}
	}
	if !query.Mentioned.IsZero() && mentions[issue.Ref.Number()] != query.Mentioned.String() {
		return false
	}
	return query.UpdatedSince.IsZero() || !issue.UpdatedAt.Before(query.UpdatedSince)
}

func (f *fakeForge) Issues(_ context.Context, _ contracts.ForgeRepoRef, query contracts.IssueQuery, _ contracts.PageRequest) (contracts.Page[contracts.Issue], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failNext {
		f.failNext = false
		return contracts.Page[contracts.Issue]{}, errForgeDown
	}
	return contracts.Page[contracts.Issue]{Items: f.selected(query)}, nil
}

type conditionalForge struct {
	*fakeForge
}

func (f conditionalForge) IssuesSince(_ context.Context, repo contracts.ForgeRepoRef, query contracts.IssueQuery, page contracts.PageRequest, since contracts.Revision) (contracts.IssueListing, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	etag := strconv.Itoa(f.version)
	if previous, ok := contracts.IssueListingETag(since, repo, query, page); ok && previous == etag {
		f.notModified++
		return contracts.IssueListing{}, contracts.ErrForgeNotModified
	}
	if f.failNext {
		f.failNext = false
		return contracts.IssueListing{}, errForgeDown
	}
	return contracts.IssueListing{
		Page:     contracts.Page[contracts.Issue]{Items: f.selected(query)},
		Revision: contracts.IssueListingRevision(repo, query, page, etag),
	}, nil
}

var errCreate = errors.New("create refused")

type recordingCreator struct {
	mu       sync.Mutex
	sessions []contracts.CreateSession
	failNext bool
	store    *state.State
}

func (c *recordingCreator) Create(_ context.Context, spec contracts.CreateSession) (contracts.Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failNext {
		c.failNext = false
		return contracts.Result{}, errCreate
	}
	c.sessions = append(c.sessions, spec)
	if c.store != nil {
		if err := c.store.AddSession(state.Session{Name: spec.Name}); err != nil {
			return contracts.Result{}, err
		}
	}
	return contracts.Result{}, nil
}

func (c *recordingCreator) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.sessions)
}

type harness struct {
	t       *testing.T
	path    string
	forge   *fakeForge
	creator *recordingCreator
	seeded  []string
	clock   time.Time
	target  Target
	poller  *Poller
	store   *state.State
}

func newHarness(t *testing.T, conditional bool) *harness {
	t.Helper()
	repo, err := contracts.ParseRepoRef("github.com/Herrscherd/herrscher")
	if err != nil {
		t.Fatal(err)
	}
	alice, err := contracts.ParseForgeLogin("alice")
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{
		t:       t,
		path:    filepath.Join(t.TempDir(), "state.json"),
		forge:   &fakeForge{mentions: map[int]string{}, conditional: conditional},
		creator: &recordingCreator{},
		clock:   t0,
		target: Target{
			Repo:    repo,
			Agent:   "builder",
			Period:  time.Minute,
			Filters: []Filter{LabelFilter("ready"), MentionFilter(alice)},
		},
	}
	h.store = state.NewState(h.path)
	h.creator.store = h.store
	h.poller = h.newPoller(h.store)
	return h
}

func (h *harness) newPoller(store *state.State) *Poller {
	var forgeClient contracts.Forge = h.forge
	if h.forge.conditional {
		forgeClient = conditionalForge{h.forge}
	}
	open := func(context.Context, contracts.ForgeHost) (contracts.Forge, error) { return forgeClient, nil }
	seed := func(session, task string) bool {
		h.seeded = append(h.seeded, session+"|"+task)
		return true
	}
	task := func(_ contracts.ForgeRepoRef, issue contracts.Issue) string {
		return "work on #" + strconv.Itoa(issue.Ref.Number())
	}
	return NewPoller(store, open, h.creator, seed, task, func() time.Time { return h.clock })
}

func (h *harness) issue(number int, updated time.Duration, association contracts.AuthorAssociation, labels ...string) contracts.Issue {
	h.t.Helper()
	repo, err := contracts.ParseRepoRef("github.com/Herrscherd/herrscher")
	if err != nil {
		h.t.Fatal(err)
	}
	ref, err := contracts.NewIssueRef(repo, number)
	if err != nil {
		h.t.Fatal(err)
	}
	return contracts.Issue{
		Ref:       ref,
		Title:     "issue",
		Labels:    labels,
		UpdatedAt: t0.Add(updated),
		Author:    contracts.ForgeActor{Login: "bob", Association: association},
	}
}

func (h *harness) poll(t *testing.T) error {
	t.Helper()
	return h.poller.Poll(context.Background(), h.target)
}

func (h *harness) start() {
	h.t.Helper()
	if err := h.poll(h.t); err != nil {
		h.t.Fatalf("initial poll: %v", err)
	}
}

func TestLabelOpensOneSessionPerIssue(t *testing.T) {
	h := newHarness(t, false)
	h.start()
	h.forge.add(h.issue(7, time.Minute, contracts.AssociationMember, "ready"))
	h.clock = t0.Add(2 * time.Minute)
	if err := h.poll(t); err != nil {
		t.Fatal(err)
	}
	if h.creator.count() != 1 {
		t.Fatalf("sessions = %d, want 1", h.creator.count())
	}
	spec := h.creator.sessions[0]
	if spec.Name != "issue-Herrscherd-herrscher-7" || spec.Agent != "builder" || spec.Clone != "https://github.com/Herrscherd/herrscher" {
		t.Fatalf("session spec = %+v", spec)
	}
	if len(h.seeded) != 1 || h.seeded[0] != "issue-Herrscherd-herrscher-7|work on #7" {
		t.Fatalf("seeded = %v", h.seeded)
	}
}

func TestMentionOpensASession(t *testing.T) {
	h := newHarness(t, false)
	h.start()
	h.forge.mentions[8] = "alice"
	h.forge.add(h.issue(8, time.Minute, contracts.AssociationCollaborator))
	h.clock = t0.Add(2 * time.Minute)
	if err := h.poll(t); err != nil {
		t.Fatal(err)
	}
	if h.creator.count() != 1 || h.creator.sessions[0].Name != "issue-Herrscherd-herrscher-8" {
		t.Fatalf("sessions = %+v", h.creator.sessions)
	}
}

func TestPullRequestsAreNeverOpened(t *testing.T) {
	h := newHarness(t, false)
	h.start()
	pr := h.issue(9, time.Minute, contracts.AssociationMember, "ready")
	pr.IsPullRequest = true
	h.forge.add(pr)
	h.clock = t0.Add(2 * time.Minute)
	if err := h.poll(t); err != nil {
		t.Fatal(err)
	}
	if h.creator.count() != 0 {
		t.Fatalf("a pull request opened %d sessions", h.creator.count())
	}
}

func TestUntrustedAuthorsAreNeverOpened(t *testing.T) {
	h := newHarness(t, false)
	h.start()
	h.forge.add(h.issue(10, time.Minute, contracts.AssociationNone, "ready"))
	h.clock = t0.Add(2 * time.Minute)
	if err := h.poll(t); err != nil {
		t.Fatal(err)
	}
	if h.creator.count() != 0 {
		t.Fatalf("an untrusted author opened %d sessions", h.creator.count())
	}
}

func TestUnchangedListingCostsNothing(t *testing.T) {
	h := newHarness(t, true)
	h.start()
	h.forge.add(h.issue(11, time.Minute, contracts.AssociationOwner, "ready"))
	h.clock = t0.Add(2 * time.Minute)
	for range 2 {
		if err := h.poll(t); err != nil {
			t.Fatal(err)
		}
	}
	before := h.forge.notModified
	if err := h.poll(t); err != nil {
		t.Fatal(err)
	}
	if h.forge.notModified != before+len(h.target.Filters) {
		t.Fatalf("unchanged listings were not all answered with a 304: %d -> %d", before, h.forge.notModified)
	}
	if h.creator.count() != 1 {
		t.Fatalf("sessions = %d, want 1", h.creator.count())
	}
}

func TestRestartDoesNotOpenTheSameIssueAgain(t *testing.T) {
	h := newHarness(t, false)
	h.start()
	h.forge.add(h.issue(12, time.Minute, contracts.AssociationMember, "ready"))
	h.clock = t0.Add(2 * time.Minute)
	if err := h.poll(t); err != nil {
		t.Fatal(err)
	}
	restored, err := state.LoadState(h.path)
	if err != nil {
		t.Fatal(err)
	}
	h.poller = h.newPoller(restored)
	h.forge.add(h.issue(13, 3*time.Minute, contracts.AssociationMember))
	h.clock = t0.Add(4 * time.Minute)
	if err := h.poll(t); err != nil {
		t.Fatal(err)
	}
	if h.creator.count() != 1 {
		t.Fatalf("sessions after restart = %d, want 1", h.creator.count())
	}
}

func TestConcurrentTicksOpenOneSession(t *testing.T) {
	h := newHarness(t, false)
	h.start()
	h.forge.add(h.issue(14, time.Minute, contracts.AssociationMember, "ready"))
	h.clock = t0.Add(2 * time.Minute)
	other := h.newPoller(h.store)
	var wg sync.WaitGroup
	for _, poller := range []*Poller{h.poller, other} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := poller.Poll(context.Background(), h.target); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if h.creator.count() != 1 {
		t.Fatalf("concurrent ticks opened %d sessions, want 1", h.creator.count())
	}
}

func TestForgeErrorKeepsTheWatermark(t *testing.T) {
	h := newHarness(t, false)
	h.start()
	watermark := h.store.IssueTriggerRepo("github.com/Herrscherd/herrscher").Watches["label:ready"].Since
	h.forge.add(h.issue(15, time.Minute, contracts.AssociationMember, "ready"))
	h.clock = t0.Add(2 * time.Minute)
	h.forge.failNext = true
	if err := h.poll(t); !errors.Is(err, errForgeDown) {
		t.Fatalf("poll error = %v, want the forge error", err)
	}
	after := h.store.IssueTriggerRepo("github.com/Herrscherd/herrscher").Watches["label:ready"].Since
	if !after.Equal(watermark) {
		t.Fatalf("watermark moved on a failed read: %s -> %s", watermark, after)
	}
	if err := h.poll(t); err != nil {
		t.Fatal(err)
	}
	if h.creator.count() != 1 {
		t.Fatalf("sessions after retry = %d, want 1", h.creator.count())
	}
}

func TestEveryIssueOpensExactlyOnceAcrossTicks(t *testing.T) {
	h := newHarness(t, false)
	h.start()
	for number := 20; number < 28; number++ {
		h.forge.add(h.issue(number, time.Minute, contracts.AssociationMember, "ready"))
	}
	h.clock = t0.Add(2 * time.Minute)
	for tick, want := range []int{3, 6, 8} {
		if err := h.poll(t); err != nil {
			t.Fatal(err)
		}
		if h.creator.count() != want {
			t.Fatalf("after tick %d sessions = %d, want %d", tick+1, h.creator.count(), want)
		}
	}
	names := map[string]bool{}
	for _, spec := range h.creator.sessions {
		if names[spec.Name] {
			t.Fatalf("session %q opened twice", spec.Name)
		}
		names[spec.Name] = true
	}
}

func TestCreateFailureReleasesTheIssueForTheNextRead(t *testing.T) {
	h := newHarness(t, false)
	h.start()
	h.forge.add(h.issue(16, time.Minute, contracts.AssociationMember, "ready"))
	h.clock = t0.Add(2 * time.Minute)
	h.creator.failNext = true
	if err := h.poll(t); !errors.Is(err, errCreate) {
		t.Fatalf("poll error = %v, want the create error", err)
	}
	if h.creator.count() != 0 {
		t.Fatalf("sessions after a refused create = %d, want 0", h.creator.count())
	}
	if err := h.poll(t); err != nil {
		t.Fatal(err)
	}
	if h.creator.count() != 1 {
		t.Fatalf("sessions after the retry = %d, want 1", h.creator.count())
	}
}

func TestAnEvictedIssueUpdatedAgainDoesNotReopen(t *testing.T) {
	h := newHarness(t, false)
	h.start()
	h.forge.add(h.issue(1, time.Minute, contracts.AssociationMember, "ready"))
	h.clock = t0.Add(2 * time.Minute)
	if err := h.poll(t); err != nil {
		t.Fatal(err)
	}
	repo := h.target.Repo.String()
	for number := 100; number < 400; number++ {
		if _, err := h.store.ClaimIssue(repo, number, SessionName(h.target.Repo, number)); err != nil {
			t.Fatal(err)
		}
	}
	h.forge.touch(1, 5*time.Minute)
	h.clock = t0.Add(6 * time.Minute)
	if err := h.poll(t); err != nil {
		t.Fatal(err)
	}
	if h.creator.count() != 1 {
		t.Fatalf("an evicted issue reopened: sessions = %d, want 1", h.creator.count())
	}
}

func TestSessionNameFitsTheSessionNameRule(t *testing.T) {
	repo, err := contracts.ParseRepoRef("github.com/Herrscherd/a.very.long.repository.name.that.keeps.going.and.going.on.and.on")
	if err != nil {
		t.Fatal(err)
	}
	name := SessionName(repo, 123456)
	if len(name) > maxSessionName {
		t.Fatalf("name %q is %d characters", name, len(name))
	}
	if name[len(name)-7:] != "-123456" {
		t.Fatalf("name %q does not end with the issue number", name)
	}
}
