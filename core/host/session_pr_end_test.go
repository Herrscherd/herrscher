package host

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

type noticeRecorder struct {
	sessions []string
	events   []contracts.Event
}

func (r *noticeRecorder) publish(session string, event contracts.Event) {
	r.sessions = append(r.sessions, session)
	r.events = append(r.events, event)
}

func endState(t *testing.T, path, dir string) *state.State {
	t.Helper()
	st, err := state.LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddSession(state.Session{Name: "alpha", ChannelID: "chan-alpha", Dir: dir}); err != nil {
		t.Fatal(err)
	}
	return st
}

func endPRs(st *state.State, notices *sessionNotices) sessionPRs {
	return newSessionPRs(clitext.NewMessages(), fixedLanguage(i18n.English), st, prForgeOpener(&fakePRForge{}), nil, sessionPRForgeLimit, notices)
}

func endText(t *testing.T, key clitext.Key, params clitext.Params) string {
	t.Helper()
	text, err := clitext.NewMessages().Text(i18n.English, key, params)
	if err != nil {
		t.Fatal(err)
	}
	return text
}

func endGit(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

func remoteHasBranch(t *testing.T, dir, branch string) bool {
	t.Helper()
	out, err := endGit(t, dir, "ls-remote", "--heads", originRemote, "refs/heads/"+branch)
	if err != nil {
		t.Fatal(err)
	}
	return out != ""
}

func localHasBranch(t *testing.T, dir, branch string) bool {
	t.Helper()
	_, err := endGit(t, dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

func TestPRAnnouncesAMergedPullRequestOnceAcrossARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st := endState(t, path, t.TempDir())
	if err := st.RecordPullRequest("alpha", state.PullRequestRecord{Number: prTestNumber, State: contracts.PullRequestOpen}); err != nil {
		t.Fatal(err)
	}
	merged := state.PullRequestRecord{Number: prTestNumber, State: contracts.PullRequestMerged}

	var first noticeRecorder
	firstNotices := &sessionNotices{}
	firstNotices.bind(first.publish)
	if err := endPRs(st, firstNotices).recordPullRequest("alpha", merged); err != nil {
		t.Fatal(err)
	}
	if err := endPRs(st, firstNotices).recordPullRequest("alpha", merged); err != nil {
		t.Fatal(err)
	}
	if len(first.events) != 1 {
		t.Fatalf("announcements before the restart = %d, want 1", len(first.events))
	}
	if first.sessions[0] != "alpha" || first.events[0].T != eventwire.EventNotice {
		t.Fatalf("announcement = %q %+v, want a notice for alpha", first.sessions[0], first.events[0])
	}
	want := endText(t, clitext.SessionPREndedMerged, clitext.Params{clitext.ParamName: "alpha", clitext.ParamNumber: prTestNumber})
	if first.events[0].Text != want {
		t.Fatalf("announcement text = %q, want %q", first.events[0].Text, want)
	}

	reloaded, err := state.LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	var second noticeRecorder
	secondNotices := &sessionNotices{}
	secondNotices.bind(second.publish)
	if err := endPRs(reloaded, secondNotices).recordPullRequest("alpha", merged); err != nil {
		t.Fatal(err)
	}
	if len(second.events) != 0 {
		t.Fatalf("announcements after the restart = %d, want none", len(second.events))
	}
}

func TestPRAnnouncesAClosedPullRequestWithItsOwnText(t *testing.T) {
	st := endState(t, filepath.Join(t.TempDir(), "state.json"), t.TempDir())
	if err := st.RecordPullRequest("alpha", state.PullRequestRecord{Number: prTestNumber, State: contracts.PullRequestOpen}); err != nil {
		t.Fatal(err)
	}
	var rec noticeRecorder
	notices := &sessionNotices{}
	notices.bind(rec.publish)
	if err := endPRs(st, notices).recordPullRequest("alpha", state.PullRequestRecord{Number: prTestNumber, State: contracts.PullRequestClosed}); err != nil {
		t.Fatal(err)
	}
	want := endText(t, clitext.SessionPREndedClosed, clitext.Params{clitext.ParamName: "alpha", clitext.ParamNumber: prTestNumber})
	if len(rec.events) != 1 || rec.events[0].Text != want {
		t.Fatalf("announcements = %+v, want the closed text once", rec.events)
	}
}

func TestPRDoesNotAnnounceAnOpenPullRequest(t *testing.T) {
	st := endState(t, filepath.Join(t.TempDir(), "state.json"), t.TempDir())
	if err := st.RecordPullRequest("alpha", state.PullRequestRecord{Number: prTestNumber, State: contracts.PullRequestOpen}); err != nil {
		t.Fatal(err)
	}
	var rec noticeRecorder
	notices := &sessionNotices{}
	notices.bind(rec.publish)
	if err := endPRs(st, notices).recordPullRequest("alpha", state.PullRequestRecord{Number: prTestNumber, State: contracts.PullRequestOpen, Draft: true}); err != nil {
		t.Fatal(err)
	}
	if len(rec.events) != 0 {
		t.Fatalf("announcements for an open pull request = %d, want none", len(rec.events))
	}
}

func endRefusal(t *testing.T, st *state.State, want error) {
	t.Helper()
	closed := false
	closer := func(context.Context, string) error {
		closed = true
		return nil
	}
	_, err := endPRs(st, nil).end(t.Context(), "alpha", closer)
	if !errors.Is(err, want) {
		t.Fatalf("end error = %v, want %v", err, want)
	}
	if closed {
		t.Fatal("end closed the session despite the refusal")
	}
}

func TestSessionEndRefusesASessionWithoutPullRequest(t *testing.T) {
	st := endState(t, filepath.Join(t.TempDir(), "state.json"), prFixtureRepo(t))
	endRefusal(t, st, errSessionEndNoPR)
}

func TestSessionEndRefusesAnOpenPullRequest(t *testing.T) {
	dir := prFixtureRepo(t)
	st := endState(t, filepath.Join(t.TempDir(), "state.json"), dir)
	if err := st.RecordPullRequest("alpha", state.PullRequestRecord{Number: prTestNumber, State: contracts.PullRequestOpen}); err != nil {
		t.Fatal(err)
	}
	endRefusal(t, st, errSessionEndOpen)
}

func TestSessionEndRefusesUncommittedChanges(t *testing.T) {
	dir := prFixtureRepo(t)
	st := endState(t, filepath.Join(t.TempDir(), "state.json"), dir)
	if err := st.RecordPullRequest("alpha", state.PullRequestRecord{Number: prTestNumber, State: contracts.PullRequestMerged}); err != nil {
		t.Fatal(err)
	}
	writeFrame(t, dir, "dirty\n")
	endRefusal(t, st, errSessionEndDirty)
}

func TestSessionEndRefusesCommitsNotPushed(t *testing.T) {
	dir := prFixtureRepo(t)
	st := endState(t, filepath.Join(t.TempDir(), "state.json"), dir)
	if err := st.RecordPullRequest("alpha", state.PullRequestRecord{Number: prTestNumber, State: contracts.PullRequestMerged}); err != nil {
		t.Fatal(err)
	}
	commitFrame(t, dir, "v3\n")
	endRefusal(t, st, errSessionEndUnpushed)
}

func TestSessionEndClosesAndDeletesTheMergedBranches(t *testing.T) {
	dir := prFixtureRepo(t)
	st := endState(t, filepath.Join(t.TempDir(), "state.json"), dir)
	if err := st.RecordPullRequest("alpha", state.PullRequestRecord{Number: prTestNumber, State: contracts.PullRequestMerged}); err != nil {
		t.Fatal(err)
	}
	gitIn(t.Context(), t, dir, "checkout", "-q", "master")
	gitIn(t.Context(), t, dir, "merge", "-q", "--ff-only", "feat")
	gitIn(t.Context(), t, dir, "checkout", "-q", "feat")

	closed := 0
	closer := func(ctx context.Context, _ string) error {
		closed++
		gitIn(ctx, t, dir, "checkout", "-q", "master")
		return nil
	}
	res, err := endPRs(st, nil).end(t.Context(), "alpha", closer)
	if err != nil {
		t.Fatal(err)
	}
	if closed != 1 {
		t.Fatalf("session closed %d times, want 1", closed)
	}
	if remoteHasBranch(t, dir, "feat") {
		t.Fatal("remote branch feat still exists after a merged end")
	}
	if localHasBranch(t, dir, "feat") {
		t.Fatal("local branch feat still exists after a merged end")
	}
	out, err := cli.Plain(res, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, endText(t, clitext.SessionEndRemoteDeleted, clitext.Params{clitext.ParamBranch: "feat"})) {
		t.Fatalf("end output %q does not report the remote deletion", out)
	}
}

func TestSessionEndKeepsTheRemoteBranchOfAClosedPullRequest(t *testing.T) {
	dir := prFixtureRepo(t)
	st := endState(t, filepath.Join(t.TempDir(), "state.json"), dir)
	if err := st.RecordPullRequest("alpha", state.PullRequestRecord{Number: prTestNumber, State: contracts.PullRequestClosed}); err != nil {
		t.Fatal(err)
	}
	closer := func(ctx context.Context, _ string) error {
		gitIn(ctx, t, dir, "checkout", "-q", "master")
		return nil
	}
	res, err := endPRs(st, nil).end(t.Context(), "alpha", closer)
	if err != nil {
		t.Fatal(err)
	}
	if !remoteHasBranch(t, dir, "feat") {
		t.Fatal("remote branch feat was deleted for a closed, not merged, pull request")
	}
	if !localHasBranch(t, dir, "feat") {
		t.Fatal("local branch feat was deleted although git refuses to delete an unmerged branch")
	}
	out, err := cli.Plain(res, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, endText(t, clitext.SessionEndLocalKept, clitext.Params{clitext.ParamBranch: "feat"})) {
		t.Fatalf("end output %q does not report the kept local branch", out)
	}
}

func TestPRDoesNotClaimAnEndWithoutAPublisher(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st := endState(t, path, t.TempDir())
	if err := st.RecordPullRequest("alpha", state.PullRequestRecord{Number: prTestNumber, State: contracts.PullRequestOpen}); err != nil {
		t.Fatal(err)
	}
	merged := state.PullRequestRecord{Number: prTestNumber, State: contracts.PullRequestMerged}
	if err := endPRs(st, &sessionNotices{}).recordPullRequest("alpha", merged); err != nil {
		t.Fatal(err)
	}

	reloaded, err := state.LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	var rec noticeRecorder
	notices := &sessionNotices{}
	notices.bind(rec.publish)
	if err := endPRs(reloaded, notices).recordPullRequest("alpha", merged); err != nil {
		t.Fatal(err)
	}
	if len(rec.events) != 1 {
		t.Fatalf("announcements once a publisher is linked = %d, want 1: the end was claimed while nothing was published", len(rec.events))
	}
}

func mergedFeatureFixture(t *testing.T) (string, *state.State, string) {
	t.Helper()
	dir := prFixtureRepo(t)
	st := endState(t, filepath.Join(t.TempDir(), "state.json"), dir)
	head, err := endGit(t, dir, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RecordPullRequest("alpha", state.PullRequestRecord{Number: prTestNumber, State: contracts.PullRequestMerged, HeadSHA: head}); err != nil {
		t.Fatal(err)
	}
	gitIn(t.Context(), t, dir, "checkout", "-q", "master")
	gitIn(t.Context(), t, dir, "merge", "-q", "--squash", "feat")
	gitIn(t.Context(), t, dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "squash")
	gitIn(t.Context(), t, dir, "checkout", "-q", "feat")
	gitIn(t.Context(), t, dir, "push", "-q", originRemote, "--delete", "feat")
	gitIn(t.Context(), t, dir, "update-ref", "-d", "refs/remotes/origin/feat")
	return dir, st, head
}

func masterCloser(t *testing.T, dir string) func(context.Context, string) error {
	return func(ctx context.Context, _ string) error {
		gitIn(ctx, t, dir, "checkout", "-q", "master")
		return nil
	}
}

func pushPullRequestHead(t *testing.T, dir, head string) {
	t.Helper()
	gitIn(t.Context(), t, dir, "push", "-q", originRemote, head+":"+pullRequestHeadRef(prTestNumber))
}

func endOutput(t *testing.T, res contracts.Result) string {
	t.Helper()
	out, err := cli.Plain(res, nil)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

type gitIntercept func(ctx context.Context, args []string) (handled bool, out string, err error)

func interceptGit(p sessionPRs, intercept gitIntercept) sessionPRs {
	real := p.git
	p.git = func(ctx context.Context, dir string, env []string, args ...string) (string, error) {
		if handled, out, err := intercept(ctx, args); handled {
			return out, err
		}
		return real(ctx, dir, env, args...)
	}
	return p
}

func isPullRequestHeadRead(args []string) bool {
	return slices.Contains(args, "ls-remote") && slices.Contains(args, pullRequestHeadRef(prTestNumber))
}

func TestSessionEndAcceptsASquashMergedPullRequest(t *testing.T) {
	dir, st, head := mergedFeatureFixture(t)
	pushPullRequestHead(t, dir, head)
	res, err := endPRs(st, nil).end(t.Context(), "alpha", masterCloser(t, dir))
	if err != nil {
		t.Fatalf("a squash-merged session was refused: %v", err)
	}
	if localHasBranch(t, dir, "feat") {
		t.Fatal("local branch feat still exists after a squash-merged end")
	}
	if remoteHasBranch(t, dir, "feat") {
		t.Fatal("remote branch feat still exists after a squash-merged end")
	}
	want := endText(t, clitext.SessionEndLocalDeleted, clitext.Params{clitext.ParamBranch: "feat"})
	if out := endOutput(t, res); !strings.Contains(out, want) {
		t.Fatalf("end output %q does not report the forced local deletion", out)
	}
}

func TestSessionEndKeepsTheLocalBranchWhenTheForgeRefDiffers(t *testing.T) {
	dir, st, _ := mergedFeatureFixture(t)
	p := interceptGit(endPRs(st, nil), func(_ context.Context, args []string) (bool, string, error) {
		if !isPullRequestHeadRead(args) {
			return false, "", nil
		}
		return true, "0000000000000000000000000000000000000001\t" + pullRequestHeadRef(prTestNumber), nil
	})
	res, err := p.end(t.Context(), "alpha", masterCloser(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	if !localHasBranch(t, dir, "feat") {
		t.Fatal("local branch feat was deleted although the forge ref differs from HEAD")
	}
	want := endText(t, clitext.SessionEndLocalUnverified, clitext.Params{clitext.ParamBranch: "feat"})
	if out := endOutput(t, res); !strings.Contains(out, want) {
		t.Fatalf("end output %q does not signal the kept branch", out)
	}
}

func TestSessionEndKeepsTheLocalBranchWhenTheForgeRefCannotBeRead(t *testing.T) {
	dir, st, _ := mergedFeatureFixture(t)
	p := interceptGit(endPRs(st, nil), func(_ context.Context, args []string) (bool, string, error) {
		if !isPullRequestHeadRead(args) {
			return false, "", nil
		}
		return true, "", errors.New("ls-remote refused")
	})
	res, err := p.end(t.Context(), "alpha", masterCloser(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	if !localHasBranch(t, dir, "feat") {
		t.Fatal("local branch feat was deleted although the forge ref could not be read")
	}
	want := endText(t, clitext.SessionEndLocalUnverified, clitext.Params{clitext.ParamBranch: "feat"})
	if out := endOutput(t, res); !strings.Contains(out, want) {
		t.Fatalf("end output %q does not signal the kept branch", out)
	}
}

func TestSessionEndKeepsTheLocalBranchWhenItMovedBeforeDeletion(t *testing.T) {
	dir, st, head := mergedFeatureFixture(t)
	pushPullRequestHead(t, dir, head)
	p := interceptGit(endPRs(st, nil), func(ctx context.Context, args []string) (bool, string, error) {
		if slices.Contains(args, "update-ref") {
			gitIn(ctx, t, dir, "update-ref", "refs/heads/feat", "master")
		}
		return false, "", nil
	})
	res, err := p.end(t.Context(), "alpha", masterCloser(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	if !localHasBranch(t, dir, "feat") {
		t.Fatal("local branch feat was deleted although it moved after the forge check")
	}
	want := endText(t, clitext.SessionEndLocalUnverified, clitext.Params{clitext.ParamBranch: "feat"})
	if out := endOutput(t, res); !strings.Contains(out, want) {
		t.Fatalf("end output %q does not signal the kept branch", out)
	}
}

func TestSessionEndKeepsTheBranchWhenHeadMovedPastTheMergedHeadWithCommitsNotPushed(t *testing.T) {
	dir, st, _ := mergedFeatureFixture(t)
	commitFrame(t, dir, "after merge\n")
	endRefusal(t, st, errSessionEndUnpushed)
	if !localHasBranch(t, dir, "feat") {
		t.Fatal("local branch feat was removed although it holds commits not pushed")
	}
}
