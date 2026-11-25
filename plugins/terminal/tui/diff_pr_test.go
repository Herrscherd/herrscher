package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/forgeport"
	"github.com/Herrscherd/herrscher/core/health"
)

type prOpenBackend struct {
	fakeBackend

	reply  string
	err    error
	health string
	hold   chan struct{}
	onOpen func()
}

func newPROpenBackend(t *testing.T, reply string) *prOpenBackend {
	t.Helper()
	return &prOpenBackend{reply: reply, health: gitHubHealth(t, contracts.ForgeConnection{Login: "octocat"})}
}

func gitHubHealth(t *testing.T, connection contracts.ForgeConnection) string {
	t.Helper()
	return healthWith(t, health.ForgeReport(connection))
}

func healthWith(t *testing.T, report contracts.ProbeReport) string {
	t.Helper()
	return healthWithFacts(t, report, health.Facts{})
}

func healthWithFacts(t *testing.T, report contracts.ProbeReport, facts health.Facts) string {
	t.Helper()
	data, err := health.Encode(health.Snapshot{Checks: []health.Check{{Group: health.GroupForge, Name: forgeport.KindGitHub, Report: report, Facts: facts}}})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func (p *prOpenBackend) Dispatch(args []string) (string, error) {
	if args[0] == verbHealth {
		return p.health, nil
	}
	p.dispatched = append(p.dispatched, args)
	if p.onOpen != nil {
		p.onOpen()
	}
	if p.hold != nil {
		<-p.hold
	}
	return p.reply, p.err
}

func pressPROnGitHub(t *testing.T, connection contracts.ForgeConnection) (*recordingRunner, *prOpenBackend) {
	t.Helper()
	dir := initRepo(t)
	gitIn(t, dir, "checkout", "-q", "-b", "feature")
	writeAndCommitBase(t, dir)
	runner := &recordingRunner{remote: "git@github.com:o/r.git\n"}
	m, _ := newDiffModel(t, dir, runner.run, 120, 30)
	backend := newPROpenBackend(t, "PR #42 opened against master")
	backend.health = gitHubHealth(t, connection)
	m.tm = backend
	press(m, "ctrl+x", "d")
	press(m, "p")
	return runner, backend
}

func TestPRCommitsPushesAndDispatchesSessionPROpen(t *testing.T) {
	dir := initRepo(t)
	gitIn(t, dir, "checkout", "-q", "-b", "feature")
	writeAndCommitBase(t, dir)
	runner := &recordingRunner{remote: "git@github.com:o/r.git\n"}
	m, _ := newDiffModel(t, dir, runner.run, 120, 30)
	backend := newPROpenBackend(t, "PR #42 opened against master")
	m.tm = backend
	press(m, "ctrl+x", "d")

	press(m, "p")

	if !strings.Contains(screen(m), "PR #42 opened against master") {
		t.Fatalf("PR notice missing:\n%s", screen(m))
	}
	if runner.pushes() != 1 {
		t.Fatalf("pushes = %d, want 1", runner.pushes())
	}
	if len(backend.dispatched) != 1 {
		t.Fatalf("dispatched = %v, want one session pr open", backend.dispatched)
	}
	args := backend.dispatched[0]
	if !slicesHavePrefix(args, "session", "pr", "open", "--name", "s") || !slicesContainPair(args, "--base", "master") {
		t.Fatalf("dispatched = %v, want session pr open --name s --base master", args)
	}
}

func TestPRNoticeNamesTheCommitSHAAndTheHostResult(t *testing.T) {
	dir := initRepo(t)
	gitIn(t, dir, "checkout", "-q", "-b", "feature")
	writeAndCommitBase(t, dir)
	runner := &recordingRunner{remote: "git@github.com:o/r.git\n"}
	m, _ := newDiffModel(t, dir, runner.run, 120, 30)
	m.tm = newPROpenBackend(t, "PR #42 opened against master")
	press(m, "ctrl+x", "d")

	press(m, "p")

	sha := strings.TrimSpace(gitIn(t, dir, "rev-parse", "--short", "HEAD"))
	if !strings.Contains(screen(m), "commit "+sha+", PR opened: PR #42 opened against master") {
		t.Fatalf("notice does not name commit %s and the host result:\n%s", sha, screen(m))
	}
}

func TestPRRefusesAChannelWithoutASession(t *testing.T) {
	dir := initRepo(t)
	gitIn(t, dir, "checkout", "-q", "-b", "feature")
	writeAndCommitBase(t, dir)
	runner := &recordingRunner{remote: "git@github.com:o/r.git\n"}
	m, _ := newDiffModel(t, dir, runner.run, 120, 30)
	backend := newPROpenBackend(t, "unused")
	m.tm = backend
	m.sessionInfos["c1"] = contracts.SessionInfo{ChannelID: "c1", Dir: dir}
	press(m, "ctrl+x", "d")

	press(m, "p")

	if !strings.Contains(screen(m), "this channel has no session") {
		t.Fatalf("refusal missing:\n%s", screen(m))
	}
	if runner.pushes() != 0 || len(backend.dispatched) != 0 {
		t.Fatalf("pushes = %d, dispatched = %v, want nothing sent", runner.pushes(), backend.dispatched)
	}
}

func TestPRRefusesToPushWhenGitHubIsNotConnected(t *testing.T) {
	dir := initRepo(t)
	gitIn(t, dir, "checkout", "-q", "-b", "feature")
	writeAndCommitBase(t, dir)
	runner := &recordingRunner{remote: "git@github.com:o/r.git\n"}
	m, _ := newDiffModel(t, dir, runner.run, 120, 30)
	backend := newPROpenBackend(t, "unused")
	backend.health = healthWith(t, health.ForgeFailure(contracts.ErrForgeNotConnected))
	m.tm = backend
	press(m, "ctrl+x", "d")

	press(m, "p")

	if !strings.Contains(screen(m), "herrscher connect github") {
		t.Fatalf("connect hint missing:\n%s", screen(m))
	}
	if runner.pushes() != 0 || len(backend.dispatched) != 0 {
		t.Fatalf("pushes = %d, dispatched = %v, want nothing pushed or opened", runner.pushes(), backend.dispatched)
	}
}

func TestPRProceedsWhenGitHubIsConnectedWithoutAccountName(t *testing.T) {
	runner, backend := pressPROnGitHub(t, contracts.ForgeConnection{})

	if runner.pushes() != 1 || len(backend.dispatched) != 1 {
		t.Fatalf("pushes = %d, dispatched = %v, want the branch pushed and one session pr open", runner.pushes(), backend.dispatched)
	}
}

func TestPRProceedsWhenGitHubAccountIsSignedIn(t *testing.T) {
	runner, backend := pressPROnGitHub(t, contracts.ForgeConnection{Login: "octocat"})

	if runner.pushes() != 1 || len(backend.dispatched) != 1 {
		t.Fatalf("pushes = %d, dispatched = %v, want the branch pushed and one session pr open", runner.pushes(), backend.dispatched)
	}
}

func TestPRStopsWaitingWhenTheTUIQuits(t *testing.T) {
	dir := initRepo(t)
	gitIn(t, dir, "checkout", "-q", "-b", "feature")
	writeAndCommitBase(t, dir)
	runner := &recordingRunner{remote: "git@github.com:o/r.git\n"}
	m, _ := newDiffModel(t, dir, runner.run, 120, 30)
	ctx, quit := context.WithCancel(context.Background())
	defer quit()
	m.ctx = ctx
	backend := newPROpenBackend(t, "unused")
	backend.hold = make(chan struct{})
	t.Cleanup(func() { close(backend.hold) })
	backend.onOpen = quit
	m.tm = backend
	press(m, "ctrl+x", "d")
	m.diff.openLimit = 50 * time.Millisecond

	press(m, "p")

	sha := strings.TrimSpace(gitIn(t, dir, "rev-parse", "--short", "HEAD"))
	if !strings.Contains(screen(m), "commit "+sha+", PR failed") {
		t.Fatalf("quitting the TUI did not end the wait for the pull request:\n%s", screen(m))
	}
}

func TestPRShowsTheConnectHintTheHostReturnsWhenNotConnected(t *testing.T) {
	dir := initRepo(t)
	gitIn(t, dir, "checkout", "-q", "-b", "feature")
	writeAndCommitBase(t, dir)
	runner := &recordingRunner{remote: "git@github.com:o/r.git\n"}
	m, _ := newDiffModel(t, dir, runner.run, 120, 30)
	backend := newPROpenBackend(t, "")
	backend.err = clitext.NewMessages().Failure(i18n.English, contracts.ErrForgeNotConnected, clitext.SessionPRNotConnected, nil)
	m.tm = backend
	press(m, "ctrl+x", "d")

	press(m, "p")

	if !strings.Contains(screen(m), "herrscher connect github") {
		t.Fatalf("connect hint from the host missing:\n%s", screen(m))
	}
}

func TestPRGivesUpWhenTheHostStaysSilent(t *testing.T) {
	dir := initRepo(t)
	gitIn(t, dir, "checkout", "-q", "-b", "feature")
	writeAndCommitBase(t, dir)
	runner := &recordingRunner{remote: "git@github.com:o/r.git\n"}
	m, _ := newDiffModel(t, dir, runner.run, 120, 30)
	backend := newPROpenBackend(t, "unused")
	backend.hold = make(chan struct{})
	t.Cleanup(func() { close(backend.hold) })
	m.tm = backend
	press(m, "ctrl+x", "d")
	m.diff.openLimit = 50 * time.Millisecond

	press(m, "p")

	sha := strings.TrimSpace(gitIn(t, dir, "rev-parse", "--short", "HEAD"))
	if !strings.Contains(screen(m), "commit "+sha+", GitHub did not answer in time") {
		t.Fatalf("timeout notice missing commit %s:\n%s", sha, screen(m))
	}
}

func slicesHavePrefix(args []string, prefix ...string) bool {
	if len(args) < len(prefix) {
		return false
	}
	for i, want := range prefix {
		if args[i] != want {
			return false
		}
	}
	return true
}

func slicesContainPair(args []string, flag, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}
