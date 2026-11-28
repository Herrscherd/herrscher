package host

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/forgeport"
	"github.com/Herrscherd/herrscher/core/internal/forge"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const prGitHubURL = "https://github.com/acme/widget.git"

const prTestNumber = 42

type fakePRForge struct {
	contracts.Forge

	opened      []contracts.PullRequestDraft
	openErr     error
	pullRequest contracts.PullRequest
	pullErr     error
	checkPages  [][]contracts.Check
	threadPages [][]contracts.ReviewThread
	replies     []string
	replySkew   time.Duration
	hideReplies bool
	withheld    []withheldReply
	replyErr    error
	connErr     error
	noLogin     bool
}

type withheldReply struct {
	thread  string
	comment contracts.ReviewComment
}

func (f *fakePRForge) ReplyToThread(_ context.Context, thread contracts.ThreadRef, body string) error {
	if f.replyErr != nil {
		return f.replyErr
	}
	f.replies = append(f.replies, body)
	comment := contracts.ReviewComment{
		Author:    contracts.ForgeActor{Login: "herrscher", Association: contracts.AssociationMember},
		Body:      body,
		CreatedAt: time.Now().Add(f.replySkew),
	}
	if f.hideReplies {
		f.withheld = append(f.withheld, withheldReply{thread: thread.ID(), comment: comment})
		return nil
	}
	f.appendComment(thread.ID(), comment)
	return nil
}

func (f *fakePRForge) appendComment(threadID string, comment contracts.ReviewComment) {
	for page := range f.threadPages {
		for index := range f.threadPages[page] {
			if f.threadPages[page][index].ID == threadID {
				f.threadPages[page][index].Comments = append(f.threadPages[page][index].Comments, comment)
			}
		}
	}
}

func (f *fakePRForge) reveal() {
	for _, reply := range f.withheld {
		f.appendComment(reply.thread, reply.comment)
	}
	f.withheld = nil
}

func (f *fakePRForge) Connection(context.Context) (contracts.ForgeConnection, error) {
	if f.connErr != nil {
		return contracts.ForgeConnection{}, f.connErr
	}
	if f.noLogin {
		return contracts.ForgeConnection{}, nil
	}
	return contracts.ForgeConnection{Login: "herrscher"}, nil
}

func (f *fakePRForge) OpenPullRequest(_ context.Context, draft contracts.PullRequestDraft) (contracts.PullRequest, error) {
	f.opened = append(f.opened, draft)
	if f.openErr != nil {
		return contracts.PullRequest{}, f.openErr
	}
	return f.pullRequest, nil
}

func (f *fakePRForge) PullRequest(_ context.Context, ref contracts.PullRequestRef, _ contracts.Revision) (contracts.PullRequest, error) {
	if f.pullErr != nil {
		return contracts.PullRequest{}, f.pullErr
	}
	pullRequest := f.pullRequest
	pullRequest.Ref = ref
	return pullRequest, nil
}

func (f *fakePRForge) Checks(_ context.Context, _ contracts.ForgeRepoRef, _ contracts.CommitSHA, page contracts.PageRequest) (contracts.Page[contracts.Check], error) {
	return pageAt(f.checkPages, page.Cursor), nil
}

func (f *fakePRForge) ReviewThreads(_ context.Context, _ contracts.PullRequestRef, page contracts.PageRequest) (contracts.Page[contracts.ReviewThread], error) {
	return pageAt(f.threadPages, page.Cursor), nil
}

func pageAt[T any](pages [][]T, cursor contracts.PageCursor) contracts.Page[T] {
	index := 0
	if cursor != "" {
		parsed, err := strconv.Atoi(string(cursor))
		if err != nil {
			return contracts.Page[T]{}
		}
		index = parsed
	}
	if index >= len(pages) {
		return contracts.Page[T]{}
	}
	var next contracts.PageCursor
	if index+1 < len(pages) {
		next = contracts.PageCursor(strconv.Itoa(index + 1))
	}
	return contracts.Page[T]{Items: pages[index], Next: next}
}

func prForgeOpener(client contracts.Forge) forgeOpener {
	return func(context.Context, contracts.ForgeHost) (contracts.Forge, error) {
		return client, nil
	}
}

func prFixtureRepo(t *testing.T) string {
	t.Helper()
	var dir, bare string
	ctx := t.Context()
	root := t.TempDir()
	bare = filepath.Join(root, "bare.git")
	dir = filepath.Join(root, "work")
	gitIn(ctx, t, root, "init", "-q", "--bare", bare)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(ctx, t, dir, "init", "-q", "-b", "master")
	commitFrame(t, dir, "v1\n")
	gitIn(ctx, t, dir, "remote", "add", originRemote, prGitHubURL)
	gitIn(ctx, t, dir, "config", "url."+bare+".insteadOf", prGitHubURL)
	gitIn(ctx, t, dir, "push", "-q", originRemote, "master")
	gitIn(ctx, t, dir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/master")
	gitIn(ctx, t, dir, "checkout", "-q", "-b", "feat")
	commitFrame(t, dir, "v2\n")
	gitIn(ctx, t, dir, "push", "-q", originRemote, "feat")
	return dir
}

func commitFrame(t *testing.T, dir, content string) {
	t.Helper()
	writeFrame(t, dir, content)
	gitIn(t.Context(), t, dir, "add", "-A")
	gitIn(t.Context(), t, dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "change")
}

func prState(t *testing.T, dir, language string) *state.State {
	t.Helper()
	st, err := state.LoadState(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddSession(state.Session{Name: "alpha", ChannelID: "chan-alpha", Dir: dir, Language: language}); err != nil {
		t.Fatal(err)
	}
	return st
}

func prRegistry(t *testing.T, st *state.State, opener forgeOpener) *cli.Registry {
	t.Helper()
	reg := cli.NewRegistry(clitext.NewMessages())
	if err := addSessionPRVerbs(reg, clitext.NewMessages(), fixedLanguage(i18n.English), st, opener, nil, nil); err != nil {
		t.Fatal(err)
	}
	return reg
}

func runPR(t *testing.T, reg *cli.Registry, args ...string) (string, error) {
	t.Helper()
	res, err := reg.Dispatch(t.Context(), append([]string{nsSession, verbPR}, args...))
	if err != nil {
		return "", err
	}
	return cli.Plain(res, nil)
}

func prRepoRef(t *testing.T) contracts.ForgeRepoRef {
	t.Helper()
	repo, err := contracts.ParseRepoRef(prGitHubURL)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

func prNumbered(t *testing.T) contracts.PullRequest {
	t.Helper()
	ref, err := contracts.NewPullRequestRef(prRepoRef(t), prTestNumber)
	if err != nil {
		t.Fatal(err)
	}
	return contracts.PullRequest{Ref: ref, Title: "Titre", URL: "https://github.com/acme/widget/pull/42", Head: "feat", Base: "master", State: contracts.PullRequestOpen}
}

func TestPROpenRecordsTheNumberAndOpensFromThePushedBranch(t *testing.T) {
	dir := prFixtureRepo(t)
	st := prState(t, dir, "")
	fake := &fakePRForge{pullRequest: prNumbered(t)}
	reg := prRegistry(t, st, prForgeOpener(fake))

	out, err := runPR(t, reg, verbPROpen, "--name", "alpha", "--title", "Titre")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if !strings.Contains(out, "#42") {
		t.Fatalf("open output %q does not name pull request #42", out)
	}
	if len(fake.opened) != 1 {
		t.Fatalf("forge opened %d pull requests, want 1", len(fake.opened))
	}
	draft := fake.opened[0]
	if draft.Head != "feat" || draft.Base != "master" || draft.Title != "Titre" || draft.Repo.String() != prRepoRef(t).String() {
		t.Fatalf("draft = %+v, want head feat, base master (origin default), title Titre on acme/widget", draft)
	}
	sess, _ := st.FindSession("alpha")
	if sess.PullRequest != 42 {
		t.Fatalf("recorded pull request = %d, want 42", sess.PullRequest)
	}
}

func TestPROpenRefusesASessionThatAlreadyHasAPullRequest(t *testing.T) {
	dir := prFixtureRepo(t)
	st := prState(t, dir, "")
	if err := st.RecordPullRequest("alpha", state.PullRequestRecord{Number: 7, State: contracts.PullRequestOpen}); err != nil {
		t.Fatal(err)
	}
	fake := &fakePRForge{pullRequest: prNumbered(t)}
	reg := prRegistry(t, st, prForgeOpener(fake))

	_, err := runPR(t, reg, verbPROpen, "--name", "alpha", "--title", "Titre", "--base", "master")
	if !errors.Is(err, state.ErrPullRequestRecorded) {
		t.Fatalf("open on a recorded session = %v, want ErrPullRequestRecorded", err)
	}
	if !strings.Contains(err.Error(), "already has pull request #7") {
		t.Fatalf("error %q does not name the recorded pull request", err)
	}
	if len(fake.opened) != 0 {
		t.Fatalf("forge opened a second pull request, want none")
	}
}

func TestPROpenRefusesAnUnpushedBranch(t *testing.T) {
	dir := prFixtureRepo(t)
	commitFrame(t, dir, "v3\n")
	st := prState(t, dir, "")
	fake := &fakePRForge{pullRequest: prNumbered(t)}
	reg := prRegistry(t, st, prForgeOpener(fake))

	_, err := runPR(t, reg, verbPROpen, "--name", "alpha", "--title", "Titre", "--base", "master")
	if err == nil || !strings.Contains(err.Error(), "branch feat is not pushed") {
		t.Fatalf("open with an unpushed commit = %v, want a refusal naming branch feat", err)
	}
	if len(fake.opened) != 0 {
		t.Fatalf("forge opened a pull request for an unpushed branch")
	}
	sess, _ := st.FindSession("alpha")
	if sess.PullRequest != 0 {
		t.Fatalf("recorded pull request = %d after a refusal, want none", sess.PullRequest)
	}
}

func TestPROpenRefusesAnOriginOffGitHub(t *testing.T) {
	dir := prFixtureRepo(t)
	gitIn(t.Context(), t, dir, "remote", "set-url", originRemote, "https://gitlab.com/acme/widget.git")
	st := prState(t, dir, "")
	fake := &fakePRForge{pullRequest: prNumbered(t)}
	reg := prRegistry(t, st, prForgeOpener(fake))

	_, err := runPR(t, reg, verbPROpen, "--name", "alpha", "--title", "Titre", "--base", "master")
	if !errors.Is(err, contracts.ErrForgeWrongHost) {
		t.Fatalf("open on a gitlab origin = %v, want ErrForgeWrongHost", err)
	}
	if !strings.Contains(err.Error(), "github.com") {
		t.Fatalf("error %q does not name github.com", err)
	}
	if len(fake.opened) != 0 {
		t.Fatalf("forge opened a pull request on a foreign host")
	}
}

func TestPROpenFailsWhenNoForgePluginIsBuilt(t *testing.T) {
	dir := prFixtureRepo(t)
	st := prState(t, dir, "")
	missing := func(context.Context, contracts.ForgeHost) (contracts.Forge, error) {
		return nil, forgeport.ErrNoPlugin
	}
	reg := prRegistry(t, st, missing)

	_, err := runPR(t, reg, verbPROpen, "--name", "alpha", "--title", "Titre", "--base", "master")
	if !errors.Is(err, forgeport.ErrNoPlugin) {
		t.Fatalf("open without a plugin = %v, want ErrNoPlugin", err)
	}
	if !strings.Contains(err.Error(), "not built in") {
		t.Fatalf("error %q does not say the plugin is missing", err)
	}
}

func TestPRStatusWithoutAPullRequestSaysHowToOpenOne(t *testing.T) {
	dir := prFixtureRepo(t)
	st := prState(t, dir, "")
	reg := prRegistry(t, st, prForgeOpener(&fakePRForge{}))

	_, err := runPR(t, reg, verbPRStatus, "--name", "alpha")
	if err == nil || !strings.Contains(err.Error(), "session alpha has no pull request, run session pr open") {
		t.Fatalf("status without a pull request = %v, want the run-open hint", err)
	}
}

func TestPRForgeNotConnectedNamesTheRemedy(t *testing.T) {
	dir := prFixtureRepo(t)
	st := prState(t, dir, "")
	if err := st.RecordPullRequest("alpha", state.PullRequestRecord{Number: 42, State: contracts.PullRequestOpen}); err != nil {
		t.Fatal(err)
	}
	reg := prRegistry(t, st, prForgeOpener(&fakePRForge{pullErr: contracts.ErrForgeNotConnected}))

	_, err := runPR(t, reg, verbPRStatus, "--name", "alpha")
	if !errors.Is(err, contracts.ErrForgeNotConnected) {
		t.Fatalf("status = %v, want ErrForgeNotConnected kept matchable", err)
	}
	if !strings.Contains(err.Error(), "herrscher connect github") {
		t.Fatalf("error %q does not name the remedy", err)
	}
}

func TestPRRateLimitedNamesTheResetTime(t *testing.T) {
	dir := prFixtureRepo(t)
	st := prState(t, dir, "")
	if err := st.RecordPullRequest("alpha", state.PullRequestRecord{Number: 42, State: contracts.PullRequestOpen}); err != nil {
		t.Fatal(err)
	}
	reset := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	reg := prRegistry(t, st, prForgeOpener(&fakePRForge{pullErr: &contracts.ForgeRateLimitedError{Reset: reset}}))

	_, err := runPR(t, reg, verbPRStatus, "--name", "alpha")
	var limited *contracts.ForgeRateLimitedError
	if !errors.As(err, &limited) {
		t.Fatalf("status = %v, want ForgeRateLimitedError kept matchable", err)
	}
	if !strings.Contains(err.Error(), "2026-11-24T10:00:00Z") {
		t.Fatalf("error %q does not name the reset time", err)
	}
}

func TestPRChecksReadsEveryPageInOrder(t *testing.T) {
	dir := prFixtureRepo(t)
	st := prState(t, dir, "")
	if err := st.RecordPullRequest("alpha", state.PullRequestRecord{Number: 42, State: contracts.PullRequestOpen}); err != nil {
		t.Fatal(err)
	}
	fake := &fakePRForge{
		pullRequest: prNumbered(t),
		checkPages: [][]contracts.Check{
			{{Name: "unit", Status: contracts.CheckCompleted, Conclusion: contracts.ConclusionSuccess}},
			{{Name: "lint", Status: contracts.CheckCompleted, Conclusion: contracts.ConclusionFailure}},
		},
	}
	reg := prRegistry(t, st, prForgeOpener(fake))

	out, err := runPR(t, reg, verbPRChecks, "--name", "alpha", "--json")
	if err != nil {
		t.Fatalf("checks: %v", err)
	}
	var checks []prCheckView
	if err := json.Unmarshal([]byte(out), &checks); err != nil {
		t.Fatalf("checks --json %q does not decode: %v", out, err)
	}
	want := []prCheckView{
		{Name: "unit", Status: contracts.CheckCompleted.String(), Conclusion: contracts.ConclusionSuccess.String()},
		{Name: "lint", Status: contracts.CheckCompleted.String(), Conclusion: contracts.ConclusionFailure.String()},
	}
	if !slices.Equal(checks, want) {
		t.Fatalf("checks = %+v, want %+v in page order", checks, want)
	}
}

func TestPRChecksRefusesMoreThanTheBoundedPages(t *testing.T) {
	dir := prFixtureRepo(t)
	st := prState(t, dir, "")
	if err := st.RecordPullRequest("alpha", state.PullRequestRecord{Number: 42, State: contracts.PullRequestOpen}); err != nil {
		t.Fatal(err)
	}
	pages := make([][]contracts.Check, forge.MaxPages+1)
	for index := range pages {
		pages[index] = []contracts.Check{{Name: "check", Status: contracts.CheckCompleted}}
	}
	fake := &fakePRForge{pullRequest: prNumbered(t), checkPages: pages}
	reg := prRegistry(t, st, prForgeOpener(fake))

	_, err := runPR(t, reg, verbPRChecks, "--name", "alpha")
	if !errors.Is(err, forge.ErrTooManyPages) {
		t.Fatalf("checks over %d pages = %v, want forge.ErrTooManyPages", forge.MaxPages, err)
	}
}

func TestPRCommentsListsUnresolvedThreadsFirst(t *testing.T) {
	dir := prFixtureRepo(t)
	st := prState(t, dir, "")
	if err := st.RecordPullRequest("alpha", state.PullRequestRecord{Number: 42, State: contracts.PullRequestOpen}); err != nil {
		t.Fatal(err)
	}
	fake := &fakePRForge{
		pullRequest: prNumbered(t),
		threadPages: [][]contracts.ReviewThread{{
			{Path: "resolved.go", Line: 1, Resolved: true, Comments: []contracts.ReviewComment{{Author: contracts.ForgeActor{Login: "ana"}, Body: "done"}}},
			{Path: "open.go", Line: 2, Comments: []contracts.ReviewComment{{Author: contracts.ForgeActor{Login: "bo"}, Body: "fix this"}}},
		}},
	}
	reg := prRegistry(t, st, prForgeOpener(fake))

	out, err := runPR(t, reg, verbPRComments, "--name", "alpha", "--json")
	if err != nil {
		t.Fatalf("comments: %v", err)
	}
	var threads []prThreadView
	if err := json.Unmarshal([]byte(out), &threads); err != nil {
		t.Fatalf("comments --json %q does not decode: %v", out, err)
	}
	if len(threads) != 2 || threads[0].Path != "open.go" || threads[0].Resolved || threads[1].Path != "resolved.go" || !threads[1].Resolved {
		t.Fatalf("threads = %+v, want open.go unresolved then resolved.go resolved", threads)
	}
}

func TestPRCommentsAreSanitized(t *testing.T) {
	dir := prFixtureRepo(t)
	st := prState(t, dir, "")
	if err := st.RecordPullRequest("alpha", state.PullRequestRecord{Number: 42, State: contracts.PullRequestOpen}); err != nil {
		t.Fatal(err)
	}
	fake := &fakePRForge{
		pullRequest: prNumbered(t),
		threadPages: [][]contracts.ReviewThread{{
			{Path: "main.go", Line: 3, Comments: []contracts.ReviewComment{{Author: contracts.ForgeActor{Login: "eve"}, Body: "\x1b[31mred\x1b[0m"}}},
		}},
	}
	reg := prRegistry(t, st, prForgeOpener(fake))

	out, err := runPR(t, reg, verbPRComments, "--name", "alpha")
	if err != nil {
		t.Fatalf("comments: %v", err)
	}
	if strings.Contains(out, "\x1b") {
		t.Fatalf("comments output %q carries a raw escape sequence", out)
	}
}

func TestPRCommentsReadInTheSessionLanguage(t *testing.T) {
	dir := prFixtureRepo(t)
	st := prState(t, dir, "fr")
	if err := st.RecordPullRequest("alpha", state.PullRequestRecord{Number: 42, State: contracts.PullRequestOpen}); err != nil {
		t.Fatal(err)
	}
	fake := &fakePRForge{
		pullRequest: prNumbered(t),
		threadPages: [][]contracts.ReviewThread{{
			{Path: "main.go", Line: 3, Comments: []contracts.ReviewComment{{Author: contracts.ForgeActor{Login: "eve"}, Body: "merci"}}},
		}},
	}
	reg := prRegistry(t, st, prForgeOpener(fake))

	out, err := runPR(t, reg, verbPRComments, "--name", "alpha")
	if err != nil {
		t.Fatalf("comments: %v", err)
	}
	if !strings.Contains(out, "non résolu") {
		t.Fatalf("comments output %q is not in French for a French session", out)
	}
}
