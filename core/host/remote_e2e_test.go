//go:build !windows

package host

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/control"
	"github.com/Herrscherd/herrscher/core/internal/runner"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/service"
)

// The remote path is the one piece the unit tests can only approximate: they
// drive fakes, so they prove the argv is right and never that the far side
// agrees. This test drives a real ssh target end to end, which is why it is
// opt-in rather than skipped-by-default noise:
//
//	HERRSCHER_E2E_SSH=me@build1 go test ./core/host/ -run TestE2ERemote -v
//
// Any reachable machine works, including this one over a local sshd. The
// account needs git, a writable home, and nothing else: the binary under test
// is cross-compiled and shipped by the test itself.
func e2eTarget(t *testing.T) string {
	t.Helper()
	target := os.Getenv("HERRSCHER_E2E_SSH")
	if target == "" {
		t.Skip("set HERRSCHER_E2E_SSH=<ssh target> to run the remote end-to-end test")
	}
	return target
}

// sourceRoot walks up from this package to the module root, which is what
// provisioning builds from.
func sourceRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Dir(filepath.Dir(wd)) // core/host -> core -> module root
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("cannot find the module root from %s: %v", wd, err)
	}
	return root
}

// TestE2ERemoteProvisionAndWorktree walks the whole remote chain: provision a
// host, clone a project onto it, create a worktree there, materialize an agent
// into it, then remove it.
func TestE2ERemoteProvisionAndWorktree(t *testing.T) {
	target := e2eTarget(t)
	ctx := context.Background()
	src := sourceRoot(t)

	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := st.SetSource(src); err != nil {
		t.Fatal(err)
	}
	local := t.TempDir()
	if err := st.SetWorkspace(local); err != nil {
		t.Fatal(err)
	}

	h := state.Host{Name: "e2e", SSH: target, Workspace: remoteWorkspace(ctx, t, target)}
	provisioned, err := provisionHost(ctx, runnerFor(h), h, src)
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if provisioned.Bin == "" || provisioned.Version != service.SourceVersion(ctx, src) {
		t.Fatalf("provisioning must record the binary and the version it put there, got %+v", provisioned)
	}
	if err := st.PutHost(provisioned); err != nil {
		t.Fatal(err)
	}

	rep := checkHost(ctx, runnerFor(provisioned), provisioned, service.SourceVersion(ctx, src))
	if !rep.Reachable || !rep.Workspace || !rep.Git || rep.Drift {
		t.Fatalf("a freshly provisioned host must check clean, got %+v", rep)
	}

	// A project with an origin here is what the host clones from.
	origin := filepath.Join(t.TempDir(), "origin.git")
	mustGit(t, "", "init", "-q", "--bare", origin)
	project := filepath.Join(local, "app")
	mustGit(t, "", "init", "-q", project)
	mustGit(t, project, "config", "user.email", "e2e@test")
	mustGit(t, project, "config", "user.name", "e2e")
	if err := os.WriteFile(filepath.Join(project, "README.md"), []byte("e2e\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, project, "add", "-A")
	mustGit(t, project, "commit", "-qm", "init")
	mustGit(t, project, "remote", "add", "origin", origin)
	mustGit(t, project, "push", "-q", "origin", "HEAD:master")

	p := hostPlacer{st: st, instanceID: "e2e", sourceVersion: func() string { return service.SourceVersion(ctx, src) }}
	repo := provisioned.Workspace + "/app"
	t.Cleanup(func() { _, _ = capture(context.Background(), runnerFor(provisioned), "rm", "-rf", repo) })
	if err := p.EnsureProject(ctx, "e2e", "app", repo); err != nil {
		t.Fatalf("clone onto the host: %v", err)
	}
	// A second call must be a no-op rather than a second clone.
	if err := p.EnsureProject(ctx, "e2e", "app", repo); err != nil {
		t.Fatalf("re-ensure: %v", err)
	}

	wt, err := p.Worktrees("e2e")
	if err != nil {
		t.Fatal(err)
	}
	if wt.PreExisting(repo, "demo") {
		t.Fatal("nothing has been created yet")
	}
	path, err := wt.Create(repo, "demo", "")
	if err != nil {
		t.Fatalf("remote worktree create: %v", err)
	}
	if path == "" {
		t.Fatal("a cloned repo must yield a worktree, not the shared fallback")
	}
	defer func() { _ = wt.Remove(repo, "demo", true) }()
	if !wt.PreExisting(repo, "demo") {
		t.Fatal("the worktree just created must read as pre-existing")
	}

	// Agent materialization: the files are rendered here with the remote path
	// substituted, shipped as one tar stream, and unpacked over there.
	a, err := agent.NewStore(t.TempDir()).Create(agent.CreateSpec{Name: "e2e", Soul: "# E2E\n"})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Materialize(ctx, "e2e", a, path, true); err != nil {
		t.Fatalf("materialize on the host: %v", err)
	}
	soul, err := capture(ctx, runnerFor(provisioned), "cat", path+"/.claude/CLAUDE.md")
	if err != nil {
		t.Fatalf("read the materialized soul: %v", err)
	}
	if !strings.Contains(soul, "# E2E") {
		t.Fatalf("the agent's soul must land in the remote worktree, got %q", soul)
	}
	// The hook must name the binary of the machine the session runs on: the
	// daemon's own path means nothing over there.
	settings, err := capture(ctx, runnerFor(provisioned), "cat", path+"/.claude/settings.json")
	if err != nil {
		t.Fatalf("read the materialized settings: %v", err)
	}
	h, ok := st.FindHost("e2e")
	if !ok {
		t.Fatal("the host we just provisioned must be findable")
	}
	if !strings.Contains(settings, h.Bin+" approve hook") {
		t.Fatalf("the hook must invoke the host's own herrscher (%s), got %q", h.Bin, settings)
	}

	if err := wt.Remove(repo, "demo", true); err != nil {
		t.Fatalf("remote worktree remove: %v", err)
	}
	if _, err := capture(ctx, runnerFor(provisioned), "test", "-d", path); err == nil {
		t.Fatal("remove must take the directory with it")
	}
}

// TestE2ERemoteCommandSocketForward is the other half of a remote session: the
// agent over there runs `herrscher <verb>` and it must reach THIS daemon. That
// is what the second -R forward buys, and it is the one claim the unit tests
// can only assert the argv of.
func TestE2ERemoteCommandSocketForward(t *testing.T) {
	target := e2eTarget(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	src := sourceRoot(t)

	h := state.Host{Name: "e2e", SSH: target, Workspace: remoteWorkspace(ctx, t, target)}
	provisioned, err := provisionHost(ctx, runnerFor(h), h, src)
	if err != nil {
		t.Fatalf("provision: %v", err)
	}

	// A daemon here, reduced to the one thing the far side asks of it. Its socket
	// is this machine's, at this machine's path; the forward is what makes it
	// answer under the session's remote name.
	const instID = "e2efwd"
	local := filepath.Join(os.TempDir(), "herrscher-e2e-daemon.sock")
	remote := control.RemoteCommandSocketPath("e2e-session")
	go serveCommandSocket(ctx, local, &fakeDispatcher{out: "reached the daemon"})
	waitForSocket(t, local)

	// No ControlPath, exactly as the launch builds it: forwards must belong to
	// this connection and not to a master that outlives it.
	run := runner.SSH{Target: target, Forwards: []runner.Forward{{Remote: remote, Local: local}}}
	if prep := run.PrepareForwards(ctx); prep != nil {
		_ = prep.Run()
	}
	// The env block is the launch's, minus what only a bridge needs: the socket
	// path the forward bound, and a state dir that keeps a herrscher already
	// installed over there from claiming the verb.
	remoteState, err := capture(ctx, runnerFor(provisioned), "mktemp", "-d")
	if err != nil {
		t.Fatal(err)
	}
	out, err := capture(ctx, run, "env",
		"HERRSCHER_INSTANCE_ID="+instID,
		"TMPDIR=/tmp",
		control.CommandSocketVar+"="+remote,
		"HERRSCHER_STATE_DIR="+strings.TrimSpace(remoteState),
		provisioned.Bin, "e2e-ping")
	if err != nil {
		t.Fatalf("the far side could not reach this daemon: %v", err)
	}
	if !strings.Contains(out, "reached the daemon") {
		t.Fatalf("the forwarded socket must carry the daemon's answer, got %q", out)
	}
}

// TestE2ETwoSessionsReachTheDaemonAtOnce is the case unit tests cannot see: two
// sessions on one host, both forwarding the same daemon socket, at the same
// time. It is the whole reason the remote path is named after the session and
// the launch owns its connection, and both agents must reach the daemon while
// the other is still running.
func TestE2ETwoSessionsReachTheDaemonAtOnce(t *testing.T) {
	target := e2eTarget(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h := state.Host{Name: "e2e", SSH: target, Workspace: remoteWorkspace(ctx, t, target)}
	provisioned, err := provisionHost(ctx, runnerFor(h), h, sourceRoot(t))
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	remoteState, err := capture(ctx, runnerFor(provisioned), "mktemp", "-d")
	if err != nil {
		t.Fatal(err)
	}

	local := filepath.Join(os.TempDir(), "herrscher-e2e-two.sock")
	go serveCommandSocket(ctx, local, &fakeDispatcher{out: "reached the daemon"})
	waitForSocket(t, local)

	// Two launches, held open the way a bridge holds its ssh open.
	sessions := []string{"e2e-one", "e2e-two"}
	for _, name := range sessions {
		remote := control.RemoteCommandSocketPath(name)
		run := runner.SSH{Target: target, Forwards: []runner.Forward{{Remote: remote, Local: local}}}
		if prep := run.PrepareForwards(ctx); prep != nil {
			_ = prep.Run()
		}
		held := run.Command(ctx, "", "sleep", "60")
		if err := held.Start(); err != nil {
			t.Fatalf("hold the forward for %s open: %v", name, err)
		}
		defer func() { _ = held.Process.Kill() }()
	}
	plain := runner.SSH{Target: target}
	for _, name := range sessions {
		remote := control.RemoteCommandSocketPath(name)
		waitForRemoteSocket(ctx, t, plain, remote)
		out, err := capture(ctx, plain, "env",
			"TMPDIR=/tmp",
			control.CommandSocketVar+"="+remote,
			"HERRSCHER_STATE_DIR="+strings.TrimSpace(remoteState),
			provisioned.Bin, "e2e-ping")
		if err != nil {
			t.Fatalf("session %s could not reach the daemon while the other was running: %v", name, err)
		}
		if !strings.Contains(out, "reached the daemon") {
			t.Fatalf("session %s got %q", name, out)
		}
	}
}

// waitForRemoteSocket waits for a forwarded socket to appear over there. The
// bind happens as the connection comes up, so it is ready shortly after Start
// returns and not at it.
func waitForRemoteSocket(ctx context.Context, t *testing.T, run hostRunner, path string) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if _, err := capture(ctx, run, "test", "-S", path); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("the forward never bound %s", path)
}

// remoteWorkspace makes a scratch workspace over there and returns its path.
func remoteWorkspace(ctx context.Context, t *testing.T, target string) string {
	t.Helper()
	run := runner.SSH{Target: target, ControlPath: runner.ControlPathFor(target)}
	out, err := capture(ctx, run, "sh", "-c", "d=$(mktemp -d)/ws; mkdir -p $d; echo $d")
	if err != nil {
		t.Fatalf("make a remote workspace: %v", err)
	}
	ws := strings.TrimSpace(out)
	t.Cleanup(func() { _, _ = capture(context.Background(), run, "rm", "-rf", ws) })
	return ws
}

func mustGit(t *testing.T, dir string, argv ...string) {
	t.Helper()
	cmd := exec.Command("git", argv...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(argv, " "), err, out)
	}
}
