package host

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/reviewloop"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

type seedRecorder struct {
	accept bool
	tasks  []string
}

func (r *seedRecorder) seed(_, task string) bool {
	r.tasks = append(r.tasks, task)
	return r.accept
}

type racingStore struct {
	reviewStore

	before func()
	at     int
	calls  int
}

func (r *racingStore) CompareAndSetReviewPosition(name string, prev, next state.ReviewPosition) (bool, error) {
	r.calls++
	if r.calls == r.at {
		r.before()
	}
	return r.reviewStore.CompareAndSetReviewPosition(name, prev, next)
}

type casStore struct {
	reviewStore

	claimed bool
	err     error
}

func (s casStore) CompareAndSetReviewPosition(string, state.ReviewPosition, state.ReviewPosition) (bool, error) {
	return s.claimed, s.err
}

type unknownStore struct {
	reviewStore
}

func (unknownStore) FindSession(string) (state.Session, bool) {
	return state.Session{}, false
}

func ownerActor() contracts.ForgeActor {
	return contracts.ForgeActor{Login: "alice", Association: contracts.AssociationOwner}
}

func memberActor(login string) contracts.ForgeActor {
	return contracts.ForgeActor{Login: login, Association: contracts.AssociationMember}
}

func reviewTestLoop(t *testing.T, st reviewStore, seed func(string, string) bool) (*reviewLoop, contracts.PullRequestRef) {
	t.Helper()
	ref, err := contracts.NewPullRequestRef(prRepoRef(t), prTestNumber)
	if err != nil {
		t.Fatal(err)
	}
	return newReviewLoop(clitext.NewMessages(), fixedLanguage(i18n.English), st, seed), ref
}

func reviewTestState(t *testing.T) *state.State {
	t.Helper()
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	openPRSession(t, st, "main", "", prTestNumber)
	return st
}

func replyTestState(t *testing.T) *state.State {
	t.Helper()
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	openPRSession(t, st, "alpha", prFixtureRepo(t), prTestNumber)
	return st
}

func replyRegistry(t *testing.T, st *state.State, fake *fakePRForge, loop *reviewLoop) *cli.Registry {
	t.Helper()
	reg := cli.NewRegistry(clitext.NewMessages())
	if err := addSessionPRVerbs(reg, clitext.NewMessages(), fixedLanguage(i18n.English), st, prForgeOpener(fake), nil, nil, loop); err != nil {
		t.Fatal(err)
	}
	return reg
}

func postReply(t *testing.T, reg *cli.Registry, body string) {
	t.Helper()
	if _, err := runPR(t, reg, verbPRReply, "--name", "alpha", "--thread", "t1", "--body", body); err != nil {
		t.Fatal(err)
	}
}

func trustedThread() contracts.ReviewThread {
	return contracts.ReviewThread{ID: "t1", Path: "main.go", Line: 4, Comments: []contracts.ReviewComment{{
		Author: ownerActor(),
		Body:   "rename this",
	}}}
}

func threadOf(comments ...contracts.ReviewComment) contracts.ReviewThread {
	return contracts.ReviewThread{ID: "t1", Path: "main.go", Line: 4, Comments: comments}
}

func at(actor contracts.ForgeActor, body string, when time.Time) contracts.ReviewComment {
	return contracts.ReviewComment{Author: actor, Body: body, CreatedAt: when}
}

func rivalPending(t *testing.T, st *state.State, name string) {
	t.Helper()
	sess, ok := st.FindSession(name)
	if !ok {
		t.Fatalf("no session %q", name)
	}
	prev := sess.ReviewPosition()
	next := prev
	next.Pending = append(slices.Clone(prev.Pending), reviewloop.Pending{Thread: "t7", Digest: reviewloop.Digest("other"), PostedAt: time.Now()})
	if ok, err := st.CompareAndSetReviewPosition(name, prev, next); err != nil || !ok {
		t.Fatalf("rival claim = %v, %v, want accepted", ok, err)
	}
}

func hasPending(sess state.Session, thread string) bool {
	for _, entry := range sess.ReviewPending {
		if entry.Thread == thread {
			return true
		}
	}
	return false
}

func TestReviewLoopDeliversAThreadOnlyOnce(t *testing.T) {
	st := reviewTestState(t)
	recorder := &seedRecorder{accept: true}
	loop, ref := reviewTestLoop(t, st, recorder.seed)
	client := &fakePRForge{threadPages: [][]contracts.ReviewThread{{trustedThread()}}}

	loop.observe(t.Context(), "main", client, ref)
	loop.observe(t.Context(), "main", client, ref)

	if len(recorder.tasks) != 1 {
		t.Fatalf("messages sent = %d, want 1", len(recorder.tasks))
	}
	if !strings.Contains(recorder.tasks[0], reviewloop.DataOpen) || !strings.Contains(recorder.tasks[0], "rename this") {
		t.Fatalf("message lacks the data block:\n%s", recorder.tasks[0])
	}
}

func TestReviewLoopReturnsThePositionWhenNoSessionIsLive(t *testing.T) {
	st := reviewTestState(t)
	refused := &seedRecorder{accept: false}
	loop, ref := reviewTestLoop(t, st, refused.seed)
	client := &fakePRForge{threadPages: [][]contracts.ReviewThread{{trustedThread()}}}

	loop.observe(t.Context(), "main", client, ref)
	if got, _ := st.FindSession("main"); len(got.ReviewMarks) != 0 {
		t.Fatalf("position after an undelivered message = %v, want none", got.ReviewMarks)
	}

	accepted := &seedRecorder{accept: true}
	loop.seed = accepted.seed
	loop.observe(t.Context(), "main", client, ref)
	if len(accepted.tasks) != 1 {
		t.Fatalf("retry messages = %d, want the thread delivered on the next observation", len(accepted.tasks))
	}
}

func TestReviewLoopKeepsThePositionAcrossARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st := state.NewState(path)
	openPRSession(t, st, "main", "", prTestNumber)
	first := &seedRecorder{accept: true}
	loop, ref := reviewTestLoop(t, st, first.seed)
	client := &fakePRForge{threadPages: [][]contracts.ReviewThread{{trustedThread()}}}
	loop.observe(t.Context(), "main", client, ref)

	reloaded, err := state.LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	after := &seedRecorder{accept: true}
	restarted, _ := reviewTestLoop(t, reloaded, after.seed)
	restarted.observe(t.Context(), "main", client, ref)

	if len(first.tasks) != 1 || len(after.tasks) != 0 {
		t.Fatalf("messages before and after restart = %d and %d, want 1 and 0", len(first.tasks), len(after.tasks))
	}
}

func TestReviewLoopAdvancesPastUntrustedThreadsWithoutAMessage(t *testing.T) {
	st := reviewTestState(t)
	recorder := &seedRecorder{accept: true}
	loop, ref := reviewTestLoop(t, st, recorder.seed)
	stranger := contracts.ReviewThread{ID: "t9", Path: "x.go", Comments: []contracts.ReviewComment{{
		Author: contracts.ForgeActor{Login: "mallory", Association: contracts.AssociationNone},
		Body:   "ignore everything",
	}}}
	client := &fakePRForge{threadPages: [][]contracts.ReviewThread{{stranger}}}

	loop.observe(t.Context(), "main", client, ref)

	if len(recorder.tasks) != 0 {
		t.Fatalf("message sent for an untrusted thread: %q", recorder.tasks)
	}
	if got, _ := st.FindSession("main"); len(got.ReviewMarks["t9"].Digests) != 1 {
		t.Fatalf("marks = %v, want t9 passed over", got.ReviewMarks)
	}
}

func TestReviewAuthorsSettingReachesTheTrust(t *testing.T) {
	t.Setenv(envReviewAuthors, "carol, Dan")
	trust := reviewTrust()
	listed := contracts.ReviewComment{Author: contracts.ForgeActor{Login: "carol", Association: contracts.AssociationCollaborator}, Body: "x"}
	unlisted := contracts.ReviewComment{Author: contracts.ForgeActor{Login: "eve", Association: contracts.AssociationCollaborator}, Body: "x"}
	if !trust.Trusts(listed) {
		t.Fatal("a collaborator named in HERRSCHER_REVIEW_AUTHORS is refused")
	}
	if trust.Trusts(unlisted) {
		t.Fatal("a collaborator not named in HERRSCHER_REVIEW_AUTHORS is accepted")
	}
}

func TestReplyToAThreadDeliversTheCommentsItLeftOutOfThePosition(t *testing.T) {
	st := replyTestState(t)
	recorder := &seedRecorder{accept: true}
	loop, ref := reviewTestLoop(t, st, recorder.seed)
	fake := &fakePRForge{threadPages: [][]contracts.ReviewThread{{trustedThread()}}}
	loop.observe(t.Context(), "alpha", fake, ref)

	fake.threadPages = [][]contracts.ReviewThread{{threadOf(
		at(ownerActor(), "rename this", time.Time{}),
		at(memberActor("bob"), "also check the error", time.Now()),
	)}}
	postReply(t, replyRegistry(t, st, fake, loop), "done")

	if len(fake.replies) != 1 {
		t.Fatalf("replies posted = %d, want 1", len(fake.replies))
	}
	if len(recorder.tasks) != 2 || !strings.Contains(recorder.tasks[1], "also check the error") {
		t.Fatalf("after the reply, messages = %q, want the new member comment delivered without a change of the pull request", recorder.tasks)
	}
}

func TestReplyOfAMemberIsNotHandedBackButALaterHumanReplyIs(t *testing.T) {
	st := replyTestState(t)
	recorder := &seedRecorder{accept: true}
	loop, ref := reviewTestLoop(t, st, recorder.seed)
	fake := &fakePRForge{threadPages: [][]contracts.ReviewThread{{trustedThread()}}}
	loop.observe(t.Context(), "alpha", fake, ref)

	postReply(t, replyRegistry(t, st, fake, loop), "done")
	if len(recorder.tasks) != 1 {
		t.Fatalf("after the member reply, messages = %d, want 1: the agent's own reply came back", len(recorder.tasks))
	}
	if sess, ok := st.FindSession("alpha"); !ok || len(sess.ReviewOwn) != 1 {
		t.Fatalf("own replies kept = %v, want the reply recorded on the session", sess.ReviewOwn)
	}

	fake.threadPages[0][0].Comments = append(fake.threadPages[0][0].Comments, at(memberActor("bob"), "also check the error", time.Now()))
	loop.observe(t.Context(), "alpha", fake, ref)

	if len(recorder.tasks) != 2 || !strings.Contains(recorder.tasks[1], "also check the error") {
		t.Fatalf("after a later human reply, messages = %q, want it delivered", recorder.tasks)
	}
	if strings.Contains(recorder.tasks[1], "author: herrscher") {
		t.Fatalf("the agent's own reply came back in %q", recorder.tasks[1])
	}
}

func TestReplyInvisibleOnTheFirstReadIsRecognisedOnTheNextAndNeverDelivered(t *testing.T) {
	st := replyTestState(t)
	recorder := &seedRecorder{accept: true}
	loop, ref := reviewTestLoop(t, st, recorder.seed)
	fake := &fakePRForge{threadPages: [][]contracts.ReviewThread{{trustedThread()}}, hideReplies: true}
	loop.observe(t.Context(), "alpha", fake, ref)

	postReply(t, replyRegistry(t, st, fake, loop), "done")
	loop.observe(t.Context(), "alpha", fake, ref)
	if len(recorder.tasks) != 1 {
		t.Fatalf("while the reply is invisible, messages = %d, want 1", len(recorder.tasks))
	}
	if sess, _ := st.FindSession("alpha"); !hasPending(sess, "t1") {
		t.Fatalf("pending = %v, want the reply still waiting", sess.ReviewPending)
	}

	fake.hideReplies = false
	fake.reveal()
	loop.observe(t.Context(), "alpha", fake, ref)

	if len(recorder.tasks) != 1 {
		t.Fatalf("after the reply became visible, messages = %d, want the reply never delivered", len(recorder.tasks))
	}
	if sess, _ := st.FindSession("alpha"); len(sess.ReviewOwn) != 1 || len(sess.ReviewPending) != 0 {
		t.Fatalf("own = %v, pending = %v, want the reply recognised and no pending left", sess.ReviewOwn, sess.ReviewPending)
	}
}

func TestReplyPersistsAcrossARestartAndIsNotDeliveredAgain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st := state.NewState(path)
	openPRSession(t, st, "alpha", prFixtureRepo(t), prTestNumber)
	first := &seedRecorder{accept: true}
	loop, ref := reviewTestLoop(t, st, first.seed)
	fake := &fakePRForge{threadPages: [][]contracts.ReviewThread{{trustedThread()}}}
	loop.observe(t.Context(), "alpha", fake, ref)
	postReply(t, replyRegistry(t, st, fake, loop), "done")

	reloaded, err := state.LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	after := &seedRecorder{accept: true}
	restarted, _ := reviewTestLoop(t, reloaded, after.seed)
	restarted.observe(t.Context(), "alpha", fake, ref)

	if len(after.tasks) != 0 {
		t.Fatalf("messages after restart = %q, want none", after.tasks)
	}
}

func TestReplyRecognisedWithinTheClockMarginOfTheForge(t *testing.T) {
	st := replyTestState(t)
	recorder := &seedRecorder{accept: true}
	loop, ref := reviewTestLoop(t, st, recorder.seed)
	fake := &fakePRForge{threadPages: [][]contracts.ReviewThread{{trustedThread()}}, replySkew: -30 * time.Second}
	loop.observe(t.Context(), "alpha", fake, ref)

	postReply(t, replyRegistry(t, st, fake, loop), "done")

	if len(recorder.tasks) != 1 {
		t.Fatalf("messages = %d, want 1: the reply stamped 30 seconds early by the forge came back", len(recorder.tasks))
	}
}

func TestHumanReplyIdenticalToOursBeforeTheMarginIsDelivered(t *testing.T) {
	st := replyTestState(t)
	recorder := &seedRecorder{accept: true}
	loop, ref := reviewTestLoop(t, st, recorder.seed)
	earlier := time.Now().Add(-10 * time.Minute)
	fake := &fakePRForge{threadPages: [][]contracts.ReviewThread{{threadOf(at(memberActor("bob"), "done", earlier))}}}

	postReply(t, replyRegistry(t, st, fake, loop), "done")
	loop.observe(t.Context(), "alpha", fake, ref)

	if len(recorder.tasks) != 1 || !strings.Contains(recorder.tasks[0], "author: bob") {
		t.Fatalf("messages = %q, want bob's identical reply delivered once", recorder.tasks)
	}
	if strings.Contains(recorder.tasks[0], "author: herrscher") {
		t.Fatalf("the agent's own reply came back in %q", recorder.tasks[0])
	}
}

func TestEvictedOwnRepliesAreNotDeliveredBack(t *testing.T) {
	st := replyTestState(t)
	recorder := &seedRecorder{accept: true}
	loop, ref := reviewTestLoop(t, st, recorder.seed)
	fake := &fakePRForge{threadPages: [][]contracts.ReviewThread{{trustedThread()}}}
	loop.observe(t.Context(), "alpha", fake, ref)
	reg := replyRegistry(t, st, fake, loop)

	for i := range reviewloop.MaxOwnReplies + 5 {
		postReply(t, reg, fmt.Sprintf("done %d", i))
	}
	loop.observe(t.Context(), "alpha", fake, ref)

	if len(recorder.tasks) != 1 {
		t.Fatalf("messages = %d, want 1: an evicted reply came back", len(recorder.tasks))
	}
}

func TestDeletedCommentDoesNotHideTheNextHumanComment(t *testing.T) {
	st := reviewTestState(t)
	recorder := &seedRecorder{accept: true}
	loop, ref := reviewTestLoop(t, st, recorder.seed)
	base := time.Now().Add(-time.Hour)
	first := at(ownerActor(), "rename this", base)
	second := at(memberActor("bob"), "also check", base.Add(time.Second))
	client := &fakePRForge{threadPages: [][]contracts.ReviewThread{{threadOf(first, second)}}}
	loop.observe(t.Context(), "main", client, ref)

	client.threadPages[0][0].Comments = []contracts.ReviewComment{second, at(memberActor("carol"), "third", base.Add(2*time.Second))}
	loop.observe(t.Context(), "main", client, ref)

	if len(recorder.tasks) != 2 || !strings.Contains(recorder.tasks[1], "third") || strings.Contains(recorder.tasks[1], "also check") {
		t.Fatalf("messages = %q, want only carol's comment on the second observation", recorder.tasks)
	}
}

func TestLegacyCountsConvertToMarksWithoutRedelivery(t *testing.T) {
	st := reviewTestState(t)
	recorder := &seedRecorder{accept: true}
	loop, ref := reviewTestLoop(t, st, recorder.seed)
	base := time.Now().Add(-time.Hour)
	counted := contracts.ReviewThread{ID: "t1", Path: "main.go", Line: 4, Comments: []contracts.ReviewComment{
		at(ownerActor(), "rename this", base),
		at(memberActor("bob"), "later", base.Add(time.Second)),
	}}
	uncounted := contracts.ReviewThread{ID: "t2", Path: "other.go", Line: 1, Comments: []contracts.ReviewComment{
		at(ownerActor(), "new thread", base.Add(2*time.Second)),
	}}
	if ok, err := st.CompareAndSetReviewPosition("main", state.ReviewPosition{}, state.ReviewPosition{Number: prTestNumber, Legacy: map[string]int{"t1": 2}}); err != nil || !ok {
		t.Fatalf("legacy position = %v, %v, want written", ok, err)
	}

	loop.observe(t.Context(), "main", &fakePRForge{threadPages: [][]contracts.ReviewThread{{counted, uncounted}}}, ref)

	if len(recorder.tasks) != 1 || !strings.Contains(recorder.tasks[0], "new thread") {
		t.Fatalf("messages = %q, want only the thread the counts did not know", recorder.tasks)
	}
	if strings.Contains(recorder.tasks[0], "later") {
		t.Fatalf("a comment already counted was handed again: %q", recorder.tasks[0])
	}
	if sess, _ := st.FindSession("main"); sess.ReviewSeen != nil {
		t.Fatalf("legacy counts kept after the conversion: %v", sess.ReviewSeen)
	}
}

func TestReplyKeepsItsPendingEntryWhenAnObserverClaimsFirst(t *testing.T) {
	st := replyTestState(t)
	recorder := &seedRecorder{accept: true}
	loop, ref := reviewTestLoop(t, st, recorder.seed)
	fake := &fakePRForge{threadPages: [][]contracts.ReviewThread{{trustedThread()}}}
	loop.observe(t.Context(), "alpha", fake, ref)
	loop.store = &racingStore{reviewStore: st, before: func() { rivalPending(t, st, "alpha") }, at: 1}

	postReply(t, replyRegistry(t, st, fake, loop), "done")

	sess, _ := st.FindSession("alpha")
	if len(recorder.tasks) != 1 {
		t.Fatalf("messages = %d, want 1: the reply lost its pending entry to the racing claim", len(recorder.tasks))
	}
	if len(sess.ReviewOwn) != 1 || !hasPending(sess, "t7") {
		t.Fatalf("own = %v, pending = %v, want the reply recognised and the rival's entry kept", sess.ReviewOwn, sess.ReviewPending)
	}
}

func TestObserverRetriesWhenAReplyClaimsFirst(t *testing.T) {
	st := reviewTestState(t)
	recorder := &seedRecorder{accept: true}
	loop, ref := reviewTestLoop(t, st, recorder.seed)
	client := &fakePRForge{threadPages: [][]contracts.ReviewThread{{trustedThread()}}}
	loop.observe(t.Context(), "main", client, ref)
	client.threadPages[0][0].Comments = append(client.threadPages[0][0].Comments, at(memberActor("bob"), "also check", time.Now()))
	loop.store = &racingStore{reviewStore: st, before: func() { rivalPending(t, st, "main") }, at: 1}

	loop.observe(t.Context(), "main", client, ref)

	if len(recorder.tasks) != 2 || !strings.Contains(recorder.tasks[1], "also check") {
		t.Fatalf("messages = %q, want the new comment delivered once after the retry", recorder.tasks)
	}
	if sess, _ := st.FindSession("main"); !hasPending(sess, "t7") {
		t.Fatalf("pending = %v, want the rival's entry kept", sess.ReviewPending)
	}
}

func TestReviewAuthorsReachTheAgentOnlyWhenListed(t *testing.T) {
	cases := []struct {
		name   string
		listed string
		want   int
	}{
		{name: "listed collaborator", listed: "carol", want: 1},
		{name: "unlisted collaborator", listed: "dan", want: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(envReviewAuthors, tc.listed)
			st := reviewTestState(t)
			recorder := &seedRecorder{accept: true}
			loop, ref := reviewTestLoop(t, st, recorder.seed)
			client := &fakePRForge{threadPages: [][]contracts.ReviewThread{{collaboratorThread("carol")}}}

			loop.observe(t.Context(), "main", client, ref)

			if len(recorder.tasks) != tc.want {
				t.Fatalf("messages = %d, want %d", len(recorder.tasks), tc.want)
			}
		})
	}
}

func collaboratorThread(login string) contracts.ReviewThread {
	return contracts.ReviewThread{ID: "t2", Path: "main.go", Line: 9, Comments: []contracts.ReviewComment{{
		Author: contracts.ForgeActor{Login: login, Association: contracts.AssociationCollaborator},
		Body:   "please rename",
	}}}
}

func TestDeliveredMessageIsHandedAgainWhenItsPositionCannotBeRecorded(t *testing.T) {
	cases := []struct {
		name  string
		store func(st *state.State) reviewStore
	}{
		{name: "position kept changing", store: func(st *state.State) reviewStore {
			return casStore{reviewStore: st}
		}},
		{name: "state write fails", store: func(st *state.State) reviewStore {
			return casStore{reviewStore: st, err: errors.New("disk full")}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := reviewTestState(t)
			recorder := &seedRecorder{accept: true}
			loop, ref := reviewTestLoop(t, st, recorder.seed)
			client := &fakePRForge{threadPages: [][]contracts.ReviewThread{{trustedThread()}}}
			loop.store = tc.store(st)

			loop.observe(t.Context(), "main", client, ref)

			if sess, _ := st.FindSession("main"); len(sess.ReviewPosition().Marks) != 0 {
				t.Fatalf("marks = %v, want none recorded when the commit failed", sess.ReviewPosition().Marks)
			}
			loop.store = st
			loop.observe(t.Context(), "main", client, ref)

			if len(recorder.tasks) != 2 || recorder.tasks[0] != recorder.tasks[1] {
				t.Fatalf("messages = %d, want the same message handed again once the position is recordable", len(recorder.tasks))
			}
			if sess, _ := st.FindSession("main"); len(sess.ReviewPosition().Marks) == 0 {
				t.Fatal("marks still empty after the recovered observation")
			}
		})
	}
}

func TestUndeliveredMessageLeavesTheThreadUnmarked(t *testing.T) {
	st := reviewTestState(t)
	loop, ref := reviewTestLoop(t, st, (&seedRecorder{}).seed)
	client := &fakePRForge{threadPages: [][]contracts.ReviewThread{{trustedThread()}}}

	loop.observe(t.Context(), "main", client, ref)

	if sess, _ := st.FindSession("main"); len(sess.ReviewPosition().Marks) != 0 {
		t.Fatalf("marks = %v, want none after an undelivered message", sess.ReviewPosition().Marks)
	}
}

func TestReplyRefusedWhenItCannotBeTracked(t *testing.T) {
	cases := []struct {
		name  string
		setup func(fake *fakePRForge, loop *reviewLoop, st *state.State)
	}{
		{name: "connection read fails", setup: func(fake *fakePRForge, _ *reviewLoop, _ *state.State) {
			fake.connErr = errors.New("forge offline")
		}},
		{name: "login unknown", setup: func(fake *fakePRForge, _ *reviewLoop, _ *state.State) {
			fake.noLogin = true
		}},
		{name: "state write fails", setup: func(_ *fakePRForge, loop *reviewLoop, st *state.State) {
			loop.store = casStore{reviewStore: st, err: errors.New("disk full")}
		}},
		{name: "position kept changing", setup: func(_ *fakePRForge, loop *reviewLoop, st *state.State) {
			loop.store = casStore{reviewStore: st}
		}},
		{name: "no such session", setup: func(_ *fakePRForge, loop *reviewLoop, st *state.State) {
			loop.store = unknownStore{reviewStore: st}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := replyTestState(t)
			loop, _ := reviewTestLoop(t, st, (&seedRecorder{accept: true}).seed)
			fake := &fakePRForge{}
			tc.setup(fake, loop, st)

			_, err := runPR(t, replyRegistry(t, st, fake, loop), verbPRReply, "--name", "alpha", "--thread", "t1", "--body", "done")

			if err == nil {
				t.Fatal("reply accepted, want it refused before publication")
			}
			if len(fake.replies) != 0 {
				t.Fatalf("replies = %q, want none published", fake.replies)
			}
		})
	}
}

func TestReplyForgetsItsPendingEntryOnlyOnAClearRefusal(t *testing.T) {
	cases := []struct {
		name        string
		replyErr    error
		wantPending int
	}{
		{name: "forge refuses the reply", replyErr: contracts.ErrForgeForbidden, wantPending: 0},
		{name: "rate limited", replyErr: &contracts.ForgeRateLimitedError{}, wantPending: 0},
		{name: "reply timed out", replyErr: context.DeadlineExceeded, wantPending: 1},
		{name: "network failure", replyErr: errors.New("connection reset"), wantPending: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := replyTestState(t)
			loop, _ := reviewTestLoop(t, st, (&seedRecorder{accept: true}).seed)
			fake := &fakePRForge{replyErr: tc.replyErr}

			_, err := runPR(t, replyRegistry(t, st, fake, loop), verbPRReply, "--name", "alpha", "--thread", "t1", "--body", "done")

			if err == nil {
				t.Fatal("reply accepted, want the forge failure reported")
			}
			if sess, _ := st.FindSession("alpha"); len(sess.ReviewPending) != tc.wantPending {
				t.Fatalf("pending = %v, want %d entries", sess.ReviewPending, tc.wantPending)
			}
		})
	}
}

func TestRefusedByForgeClassifiesEachClearRefusal(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "unauthorized", err: contracts.ErrForgeUnauthorized, want: true},
		{name: "forbidden", err: contracts.ErrForgeForbidden, want: true},
		{name: "not found", err: contracts.ErrForgeNotFound, want: true},
		{name: "wrong host", err: contracts.ErrForgeWrongHost, want: true},
		{name: "not connected", err: contracts.ErrForgeNotConnected, want: true},
		{name: "rate limited", err: &contracts.ForgeRateLimitedError{}, want: true},
		{name: "empty comment", err: contracts.ErrEmptyComment, want: true},
		{name: "invalid ref", err: contracts.ErrInvalidRef, want: true},
		{name: "invalid login", err: contracts.ErrInvalidForgeLogin, want: true},
		{name: "timeout", err: context.DeadlineExceeded, want: false},
		{name: "connection reset", err: errors.New("read tcp: connection reset by peer"), want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wrapped := fmt.Errorf("reply: %w", tc.err)
			if got := refusedByForge(wrapped); got != tc.want {
				t.Fatalf("refusedByForge = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestReplyAndObserverShareOneReviewLoop(t *testing.T) {
	st := reviewTestState(t)
	var table sessionTable
	table.cliMessages = clitext.NewMessages()
	lang := fixedLanguage(i18n.English)
	loop := table.reviewLoopFor(lang, st)
	watcher := newSessionPRWatcher(clitext.NewMessages(), lang, st, prForgeOpener(&fakePRForge{}), nil, nil, table.reviewLoopFor(lang, st))
	if watcher.reviews != loop {
		t.Fatal("the watcher and the reply command do not share one review loop")
	}
	_, ref := reviewTestLoop(t, st, nil)

	gate := make(chan struct{})
	var mu sync.Mutex
	var delivered []string
	count := func() int {
		mu.Lock()
		defer mu.Unlock()
		return len(delivered)
	}
	loop.seed = func(_, task string) bool {
		mu.Lock()
		delivered = append(delivered, task)
		first := len(delivered) == 1
		mu.Unlock()
		if first {
			<-gate
		}
		return true
	}
	client := &fakePRForge{threadPages: [][]contracts.ReviewThread{{trustedThread()}}}

	done := make(chan struct{}, 2)
	go func() {
		loop.observe(t.Context(), "main", client, ref)
		done <- struct{}{}
	}()
	for i := 0; i < 500 && count() == 0; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	go func() {
		watcher.reviews.observe(t.Context(), "main", client, ref)
		done <- struct{}{}
	}()
	time.Sleep(50 * time.Millisecond)
	close(gate)
	<-done
	<-done

	if count() != 1 {
		t.Fatalf("messages = %d, want the thread handed over once", count())
	}
}
