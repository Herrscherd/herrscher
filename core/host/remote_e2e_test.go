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
	"github.com/Herrscherd/herrscher/core/internal/manager"
	"github.com/Herrscherd/herrscher/core/internal/runner"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/service"
)

func e2eTarget(t *testing.T) string {
	t.Helper()
	target := os.Getenv("HERRSCHER_E2E_SSH")
	if target == "" {
		t.Skip("set HERRSCHER_E2E_SSH=<ssh target> to run the remote end-to-end test")
	}
	return target
}

func sourceRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Dir(filepath.Dir(wd))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("cannot find the module root from %s: %v", wd, err)
	}
	return root
}

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

	provisioned := provisionE2EHost(ctx, t, st, target, src)
	initE2EProject(t, local)

	p := newHostPlacer(ctx, st, "e2e", nil, func() string { return service.SourceVersion(ctx, src) })
	repo := provisioned.Workspace + "/app"
	t.Cleanup(func() {
		_, rmErr := capture(context.Background(), runnerFor(provisioned), "rm", "-rf", repo)
		logCleanup(rmErr)
	})
	assertProjectEnsuredTwice(ctx, t, p, repo)

	wt, err := p.Worktrees("e2e")
	if err != nil {
		t.Fatal(err)
	}
	path := createE2EWorktree(t, wt, repo)
	defer func() { logCleanup(wt.Remove(repo, "demo", true)) }()

	assertE2EMaterialized(ctx, t, p, st, provisioned, path)
	removeE2EWorktree(ctx, t, wt, provisioned, repo, path)
}

func provisionE2EHost(ctx context.Context, t *testing.T, st *state.State, target, src string) state.Host {
	t.Helper()
	h := state.Host{Name: "e2e", SSH: target, Workspace: remoteWorkspace(ctx, t, target)}
	provisioned, err := provisionHost(ctx, runnerFor(h), h, src)
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if provisioned.Bin == "" || provisioned.Version != service.SourceVersion(ctx, src) {
		t.Fatalf("provisioning must record the binary and the version it put there, got %+v", provisioned)
	}
	if putErr := st.PutHost(provisioned); putErr != nil {
		t.Fatal(putErr)
	}
	rep := checkHost(ctx, runnerFor(provisioned), provisioned, service.SourceVersion(ctx, src))
	if !rep.Reachable || !rep.Workspace || !rep.Git || rep.Drift {
		t.Fatalf("a freshly provisioned host must check clean, got %+v", rep)
	}
	return provisioned
}

func initE2EProject(t *testing.T, local string) {
	t.Helper()
	origin := filepath.Join(t.TempDir(), "origin.git")
	mustGit(t, "", "init", "-q", "--bare", origin)
	project := filepath.Join(local, "app")
	mustGit(t, "", "init", "-q", project)
	mustGit(t, project, "config", "user.email", "e2e@test")
	mustGit(t, project, "config", "user.name", "e2e")
	if writeErr := os.WriteFile(filepath.Join(project, "README.md"), []byte("e2e\n"), 0o644); writeErr != nil {
		t.Fatal(writeErr)
	}
	mustGit(t, project, "add", "-A")
	mustGit(t, project, "commit", "-qm", "init")
	mustGit(t, project, "remote", "add", "origin", origin)
	mustGit(t, project, "push", "-q", "origin", "HEAD:master")
}

func assertProjectEnsuredTwice(ctx context.Context, t *testing.T, p hostPlacer, repo string) {
	t.Helper()
	if ensureErr := p.EnsureProject(ctx, "e2e", "app", repo); ensureErr != nil {
		t.Fatalf("clone onto the host: %v", ensureErr)
	}
	if ensureErr := p.EnsureProject(ctx, "e2e", "app", repo); ensureErr != nil {
		t.Fatalf("re-ensure: %v", ensureErr)
	}
}

func createE2EWorktree(t *testing.T, wt manager.Worktrees, repo string) string {
	t.Helper()
	if wt.PreExisting(repo, "demo") {
		t.Fatal("nothing has been created yet")
	}
	path, err := wt.Create(ctx, repo, "demo", "")
	if err != nil {
		t.Fatalf("remote worktree create: %v", err)
	}
	if path == "" {
		t.Fatal("a cloned repo must yield a worktree, not the shared fallback")
	}
	if !wt.PreExisting(repo, "demo") {
		t.Fatal("the worktree just created must read as pre-existing")
	}
	return path
}

func assertE2EMaterialized(ctx context.Context, t *testing.T, p hostPlacer, st *state.State, provisioned state.Host, path string) {
	t.Helper()
	a, err := agent.NewStore(t.TempDir()).Create(agent.CreateSpec{Name: "e2e", Soul: "# E2E\n"})
	if err != nil {
		t.Fatal(err)
	}
	if materializeErr := p.Materialize(ctx, "e2e", a, path, true); materializeErr != nil {
		t.Fatalf("materialize on the host: %v", materializeErr)
	}
	soul, err := capture(ctx, runnerFor(provisioned), "cat", path+"/.claude/CLAUDE.md")
	if err != nil {
		t.Fatalf("read the materialized soul: %v", err)
	}
	if !strings.Contains(soul, "# E2E") {
		t.Fatalf("the agent's soul must land in the remote worktree, got %q", soul)
	}
	settings, err := capture(ctx, runnerFor(provisioned), "cat", path+"/.claude/settings.json")
	if err != nil {
		t.Fatalf("read the materialized settings: %v", err)
	}
	h, ok := st.FindHost("e2e")
	if !ok {
		t.Fatal("the host we just provisioned must be findable")
	}
	if !strings.Contains(settings, "'"+h.Bin+"' approve hook") {
		t.Fatalf("the hook must invoke the host's own herrscher (%s), got %q", h.Bin, settings)
	}
}

func removeE2EWorktree(ctx context.Context, t *testing.T, wt manager.Worktrees, provisioned state.Host, repo, path string) {
	t.Helper()
	if err := wt.Remove(repo, "demo", true); err != nil {
		t.Fatalf("remote worktree remove: %v", err)
	}
	if _, err := capture(ctx, runnerFor(provisioned), "test", "-d", path); err == nil {
		t.Fatal("remove must take the directory with it")
	}
}

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

	const instID = "e2efwd"
	local := filepath.Join(os.TempDir(), "herrscher-e2e-daemon.sock")
	remote := control.RemoteCommandSocketPath("e2e-session")
	serveCommandSocket(ctx, local, &fakeDispatcher{out: "reached the daemon"})

	run := runner.SSH{Target: target, Forwards: []runner.Forward{{Remote: remote, Local: local}}}
	if prep := run.PrepareForwards(ctx); prep != nil {
		logCleanup(prep.Run())
	}
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
	serveCommandSocket(ctx, local, &fakeDispatcher{out: "reached the daemon"})

	sessions := []string{"e2e-one", "e2e-two"}
	for _, name := range sessions {
		remote := control.RemoteCommandSocketPath(name)
		run := runner.SSH{Target: target, Forwards: []runner.Forward{{Remote: remote, Local: local}}}
		if prep := run.PrepareForwards(ctx); prep != nil {
			logCleanup(prep.Run())
		}
		held := run.Command(ctx, "", "sleep", "60")
		if err := held.Start(); err != nil {
			t.Fatalf("hold the forward for %s open: %v", name, err)
		}
		t.Cleanup(func() { logCleanup(held.Process.Kill()) })
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

func waitForRemoteSocket(ctx context.Context, t *testing.T, run hostRunner, path string) {
	t.Helper()
	for range 100 {
		if _, err := capture(ctx, run, "test", "-S", path); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("the forward never bound %s", path)
}

func remoteWorkspace(ctx context.Context, t *testing.T, target string) string {
	t.Helper()
	run := runner.SSH{Target: target, ControlPath: runner.ControlPathFor(target)}
	out, err := capture(ctx, run, "sh", "-c", "d=$(mktemp -d)/ws; mkdir -p $d; echo $d")
	if err != nil {
		t.Fatalf("make a remote workspace: %v", err)
	}
	ws := strings.TrimSpace(out)
	t.Cleanup(func() { _, rmErr := capture(context.WithoutCancel(ctx), run, "rm", "-rf", ws); logCleanup(rmErr) })
	return ws
}

func mustGit(t *testing.T, dir string, argv ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", argv...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(argv, " "), err, out)
	}
}
