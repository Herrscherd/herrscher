//go:build !windows

package host

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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

const forwardConfirmTimeout = 30 * time.Second

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
	layout := testRuntime(t)

	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := st.SetSource(src); err != nil {
		t.Fatal(err)
	}
	local := t.TempDir()
	if err := st.SetWorkspace(local); err != nil {
		t.Fatal(err)
	}

	provisioned := provisionE2EHost(ctx, t, layout, st, target, src)
	initE2EProject(t, local)

	p := newHostPlacer(st, layout, "e2e", nil, func() string { return service.SourceVersion(ctx, src) })
	repo := provisioned.Workspace + "/app"
	t.Cleanup(func() {
		_, rmErr := capture(context.Background(), runnerForTest(t, layout, provisioned), "rm", "-rf", repo)
		logCleanup(rmErr)
	})
	assertProjectEnsuredTwice(ctx, t, p, repo)

	wt, err := p.Worktrees("e2e")
	if err != nil {
		t.Fatal(err)
	}
	path := createE2EWorktree(ctx, t, wt, repo)
	defer func() { logCleanup(wt.Remove(ctx, repo, "demo", true)) }()

	assertE2EMaterialized(ctx, t, layout, p, st, provisioned, path)
	removeE2EWorktree(ctx, t, layout, wt, provisioned, repo, path)
}

func provisionE2EHost(ctx context.Context, t *testing.T, layout RuntimeLayout, st *state.State, target, src string) state.Host {
	t.Helper()
	h := state.Host{Name: "e2e", SSH: target, Workspace: remoteWorkspace(ctx, t, layout, target)}
	provisioned, err := provisionHost(ctx, runnerForTest(t, layout, h), h, src)
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if provisioned.Bin == "" || provisioned.Version != service.SourceVersion(ctx, src) {
		t.Fatalf("provisioning must record the binary and the version it put there, got %+v", provisioned)
	}
	if putErr := st.PutHost(provisioned); putErr != nil {
		t.Fatal(putErr)
	}
	rep := checkHost(ctx, runnerForTest(t, layout, provisioned), provisioned, service.SourceVersion(ctx, src))
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

func createE2EWorktree(ctx context.Context, t *testing.T, wt manager.Worktrees, repo string) string {
	t.Helper()
	if wt.PreExisting(ctx, repo, "demo") {
		t.Fatal("nothing has been created yet")
	}
	path, err := wt.Create(ctx, repo, "demo", "")
	if err != nil {
		t.Fatalf("remote worktree create: %v", err)
	}
	if path == "" {
		t.Fatal("a cloned repo must yield a worktree, not the shared fallback")
	}
	if !wt.PreExisting(ctx, repo, "demo") {
		t.Fatal("the worktree just created must read as pre-existing")
	}
	return path
}

func assertE2EMaterialized(ctx context.Context, t *testing.T, layout RuntimeLayout, p hostPlacer, st *state.State, provisioned state.Host, path string) {
	t.Helper()
	a, err := agent.NewStore(t.TempDir()).Create(agent.CreateSpec{Name: "e2e", Soul: "# E2E\n"})
	if err != nil {
		t.Fatal(err)
	}
	if materializeErr := p.Materialize(ctx, "e2e", a, path, true); materializeErr != nil {
		t.Fatalf("materialize on the host: %v", materializeErr)
	}
	soul, err := capture(ctx, runnerForTest(t, layout, provisioned), "cat", path+"/.claude/CLAUDE.md")
	if err != nil {
		t.Fatalf("read the materialized soul: %v", err)
	}
	if !strings.Contains(soul, "# E2E") {
		t.Fatalf("the agent's soul must land in the remote worktree, got %q", soul)
	}
	settings, err := capture(ctx, runnerForTest(t, layout, provisioned), "cat", path+"/.claude/settings.json")
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

func removeE2EWorktree(ctx context.Context, t *testing.T, layout RuntimeLayout, wt manager.Worktrees, provisioned state.Host, repo, path string) {
	t.Helper()
	if err := wt.Remove(ctx, repo, "demo", true); err != nil {
		t.Fatalf("remote worktree remove: %v", err)
	}
	if _, err := capture(ctx, runnerForTest(t, layout, provisioned), "test", "-d", path); err == nil {
		t.Fatal("remove must take the directory with it")
	}
}

func remoteSocketDir(ctx context.Context, t *testing.T, layout RuntimeLayout, target string) string {
	t.Helper()
	controlPath, err := runner.ControlPathFor(layout, target)
	if err != nil {
		t.Fatal(err)
	}
	removeRemoteRuntimeDirOnExitIfCreated(ctx, t, target)
	dir, err := runner.RemoteRuntimeDir(ctx, target, controlPath)
	if err != nil {
		t.Fatalf("remote runtime dir: %v", err)
	}
	return dir
}

func removeRemoteRuntimeDirOnExitIfCreated(ctx context.Context, t *testing.T, target string) {
	t.Helper()
	dir := expectedRemoteRuntimeDir(ctx, t, target)
	presence, err := capture(ctx, runner.SSH{Target: target}, "sh", "-c", `if [ -e "$1" ]; then echo present; else echo absent; fi`, "sh", dir)
	if err != nil {
		t.Fatalf("probe the remote runtime dir: %v", err)
	}
	if strings.TrimSpace(presence) != "absent" {
		return
	}
	t.Cleanup(func() {
		_, rmErr := capture(context.WithoutCancel(ctx), runner.SSH{Target: target}, "rm", "-rf", "--", dir)
		logCleanup(rmErr)
	})
}

func TestE2ERemoteRuntimeDirIsPrivateAndPerUser(t *testing.T) {
	target := e2eTarget(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	layout := testRuntime(t)
	dir := remoteSocketDir(ctx, t, layout, target)
	uid, err := capture(ctx, runner.SSH{Target: target}, "id", "-u")
	if err != nil {
		t.Fatal(err)
	}
	if want := expectedRemoteRuntimeDir(ctx, t, target); dir != want {
		t.Fatalf("remote runtime dir = %q, want %q", dir, want)
	}
	stat, err := capture(ctx, runner.SSH{Target: target}, "stat", "-c", "%a %u", dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(stat), "700 "+strings.TrimSpace(uid); got != want {
		t.Fatalf("remote runtime dir mode and owner = %q, want %q", got, want)
	}
}

func TestE2ERemoteCommandSocketForward(t *testing.T) {
	target := e2eTarget(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	src := sourceRoot(t)
	layout := testRuntime(t)

	h := state.Host{Name: "e2e", SSH: target, Workspace: remoteWorkspace(ctx, t, layout, target)}
	provisioned, err := provisionHost(ctx, runnerForTest(t, layout, h), h, src)
	if err != nil {
		t.Fatalf("provision: %v", err)
	}

	const instID = "e2efwd"
	local := layout.Socket("herrscher-e2e-daemon.sock")
	remote := control.RemoteCommandSocketPath(remoteSocketDir(ctx, t, layout, target), instID, "e2e-session")
	serveCommandSocket(ctx, layout, local, &fakeDispatcher{out: "reached the daemon"})

	removeRemoteOnExit(t, target, remote)
	run := runner.SSH{Target: target, Forwards: []runner.Forward{{Remote: remote, Local: local}}}
	if prep := run.PrepareForwards(ctx); prep != nil {
		logCleanup(prep.Run())
	}
	remoteState, err := capture(ctx, runnerForTest(t, layout, provisioned), "mktemp", "-d")
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
	layout := testRuntime(t)

	h := state.Host{Name: "e2e", SSH: target, Workspace: remoteWorkspace(ctx, t, layout, target)}
	provisioned, err := provisionHost(ctx, runnerForTest(t, layout, h), h, sourceRoot(t))
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	remoteState, err := capture(ctx, runnerForTest(t, layout, provisioned), "mktemp", "-d")
	if err != nil {
		t.Fatal(err)
	}

	local := layout.Socket("herrscher-e2e-two.sock")
	serveCommandSocket(ctx, layout, local, &fakeDispatcher{out: "reached the daemon"})

	sessions := []string{"e2e-one", "e2e-two"}
	dir := remoteSocketDir(ctx, t, layout, target)
	for _, name := range sessions {
		remote := control.RemoteCommandSocketPath(dir, "", name)
		removeRemoteOnExit(t, target, remote)
		run := runner.SSH{Target: target, Forwards: []runner.Forward{{Remote: remote, Local: local}}}
		if prep := run.PrepareForwards(ctx); prep != nil {
			logCleanup(prep.Run())
		}
		holdForward(ctx, t, run, remote)
	}
	plain := runner.SSH{Target: target}
	for _, name := range sessions {
		remote := control.RemoteCommandSocketPath(dir, "", name)
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

func holdForward(ctx context.Context, t *testing.T, run runner.SSH, remote string) {
	t.Helper()
	held := run.Command(ctx, "", "sleep", "60")
	held.Args = slices.Insert(held.Args, 1, "-v")
	stderr, err := held.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := held.Start(); err != nil {
		t.Fatalf("hold the forward for %s open: %v", remote, err)
	}
	confirmed := make(chan struct{}, 1)
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		lines := bufio.NewScanner(stderr)
		for lines.Scan() {
			if forwardConfirmed(lines.Text(), remote) {
				select {
				case confirmed <- struct{}{}:
				default:
				}
			}
		}
	}()
	guard := time.AfterFunc(forwardConfirmTimeout, func() { logCleanup(held.Process.Kill()) })
	t.Cleanup(func() {
		guard.Stop()
		logCleanup(held.Process.Kill())
		<-drained
		logCleanup(held.Wait())
	})
	select {
	case <-confirmed:
	case <-drained:
		t.Fatalf("the far end never confirmed the forward %s", remote)
	}
}

func forwardConfirmed(line, remote string) bool {
	const prefix = "remote forward success for: listen "
	at := strings.Index(line, prefix+remote)
	if at < 0 {
		return false
	}
	next := line[at+len(prefix)+len(remote):]
	return strings.HasPrefix(next, ":") || strings.HasPrefix(next, ",")
}

func remoteWorkspace(ctx context.Context, t *testing.T, layout RuntimeLayout, target string) string {
	t.Helper()
	controlPath, err := runner.ControlPathFor(layout, target)
	if err != nil {
		t.Fatal(err)
	}
	run := runner.SSH{Target: target, ControlPath: controlPath}
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

func runnerForTest(t *testing.T, layout RuntimeLayout, h state.Host) hostRunner {
	t.Helper()
	run, err := runnerFor(layout, h)
	if err != nil {
		t.Fatal(err)
	}
	return run
}

const remoteRuntimeDirRule = `uid=$(id -u); p=/run/user/$uid; if [ -d "$p" ] && [ ! -L "$p" ] && [ "$(stat -c '%a %u' "$p")" = "700 $uid" ]; then echo "$p/herrscher"; else echo "/tmp/herrscher-$uid"; fi`

func expectedRemoteRuntimeDir(ctx context.Context, t *testing.T, target string) string {
	t.Helper()
	out, err := capture(ctx, runner.SSH{Target: target}, "sh", "-c", remoteRuntimeDirRule)
	if err != nil {
		t.Fatalf("work out the expected remote runtime dir: %v", err)
	}
	return strings.TrimSpace(out)
}

func removeRemoteOnExit(t *testing.T, target, path string) {
	t.Helper()
	t.Cleanup(func() {
		_, err := capture(context.Background(), runner.SSH{Target: target}, "rm", "-rf", "--", path)
		logCleanup(err)
	})
}
