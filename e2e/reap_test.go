//go:build e2e && linux

package e2e_test

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/host"
)

func TestReapInstanceKillsAndNamesWhatCarriesTheInstance(t *testing.T) {
	instance, err := newInstanceID()
	if err != nil {
		t.Fatal(err)
	}
	carrier := exec.CommandContext(t.Context(), "sleep", "60")
	carrier.Env = []string{instanceEnv + "=" + instance}
	bystander := exec.CommandContext(t.Context(), "sleep", "60")
	bystander.Env = []string{instanceEnv + "=" + instance + "-other"}
	for _, cmd := range []*exec.Cmd{carrier, bystander} {
		if startErr := cmd.Start(); startErr != nil {
			t.Fatal(startErr)
		}
	}
	stopped, survivors, err := reapInstance(procRoot, instance, reapGrace)
	if err != nil {
		t.Fatal(err)
	}
	if len(stopped) != 1 || !strings.Contains(stopped[0], "sleep 60") || len(survivors) != 0 {
		t.Fatalf("got stopped %q and survivors %q, want the one carrier stopped", stopped, survivors)
	}
	if signo := stoppedBy(t, carrier); signo != syscall.SIGINT {
		t.Fatalf("carrier: got %v, want SIGINT", signo)
	}
	if err := bystander.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("the bystander was touched: %v", err)
	}
}

func TestReapInstanceGivesASurvivorTheGraceBeforeSIGKILL(t *testing.T) {
	instance, err := newInstanceID()
	if err != nil {
		t.Fatal(err)
	}
	survivor := ignoringINT(t, instance)
	started := time.Now()
	stopped, survivors, err := reapInstance(procRoot, instance, reapGrace)
	if err != nil {
		t.Fatal(err)
	}
	if len(stopped) != 0 || len(survivors) != 1 {
		t.Fatalf("got stopped %q and survivors %q, want the one survivor of SIGINT", stopped, survivors)
	}
	if elapsed := time.Since(started); elapsed < reapGrace {
		t.Fatalf("got %v, want the grace waited out before SIGKILL", elapsed)
	}
	if signo := stoppedBy(t, survivor); signo != syscall.SIGKILL {
		t.Fatalf("survivor: got %v, want SIGKILL", signo)
	}
}

func TestSettleStopsAStillRunningProcessWithSIGINTBeforeAnySIGKILL(t *testing.T) {
	instance, err := newInstanceID()
	if err != nil {
		t.Fatal(err)
	}
	running := exec.CommandContext(t.Context(), "sleep", "60")
	running.Env = []string{instanceEnv + "=" + instance}
	if startErr := running.Start(); startErr != nil {
		t.Fatal(startErr)
	}
	root := t.TempDir()
	before, err := hashedEntries(root)
	if err != nil {
		t.Fatal(err)
	}
	settleErr := settle(root, instance, before)
	if !errors.Is(settleErr, errStopped) || errors.Is(settleErr, errLeaked) {
		t.Fatalf("got %v, want the process reported as stopped, not as leaked", settleErr)
	}
	if signo := stoppedBy(t, running); signo != syscall.SIGINT {
		t.Fatalf("got %v, want the process stopped by SIGINT", signo)
	}
}

func ignoringINT(t *testing.T, instance string) *exec.Cmd {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "sh", "-c", `trap "" INT; echo ready; exec sleep 60`)
	cmd.Env = []string{instanceEnv + "=" + instance}
	return startReady(t, cmd)
}

func stoppedBy(t *testing.T, cmd *exec.Cmd) syscall.Signal {
	t.Helper()
	exit, ok := errors.AsType[*exec.ExitError](cmd.Wait())
	if !ok {
		t.Fatal("the process exited on its own instead of being signalled")
	}
	status, ok := exit.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() {
		t.Fatalf("the process was not ended by a signal: %v", exit)
	}
	return status.Signal()
}

func TestProcessesInReportsAnUnreadableEnvironment(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "7", "environ"), 0o700); err != nil {
		t.Fatal(err)
	}
	fakeStat(t, root, 7, fakeStart)
	if _, err := processesIn(root, "x"); !errors.Is(err, syscall.EISDIR) {
		t.Fatalf("got %v, want the read failure", err)
	}
	if _, err := processesIn(filepath.Join(root, "missing"), "x"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("got %v, want the listing failure", err)
	}
}

func TestSettleFailsOnALeakAndKillsIt(t *testing.T) {
	instance, err := newInstanceID()
	if err != nil {
		t.Fatal(err)
	}
	leaker := ignoringINT(t, instance)
	root := t.TempDir()
	before, err := hashedEntries(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := settle(root, instance, before); !errors.Is(err, errLeaked) {
		t.Fatalf("got %v, want the leak reported", err)
	}
	if signo := stoppedBy(t, leaker); signo != syscall.SIGKILL {
		t.Fatalf("the leaked process: got %v, want SIGKILL after SIGINT was ignored", signo)
	}
}

func TestSettleInterruptsTheDaemonBeforeKillingIt(t *testing.T) {
	instance, err := newInstanceID()
	if err != nil {
		t.Fatal(err)
	}
	daemon := exec.CommandContext(t.Context(), "sh", "-c", "sleep 60 >/dev/null 2>&1 & child=$!; trap 'kill $child; exit 130' INT; echo ready; wait", "sh", "serve")
	daemon.Env = []string{instanceEnv + "=" + instance}
	daemon.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	t.Cleanup(func() { stopDaemon(t, daemon) })
	startReady(t, daemon)
	root := t.TempDir()
	before, err := hashedEntries(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := settle(root, instance, before); err != nil && !errors.Is(err, errStopped) {
		t.Fatalf("got %v, want the daemon to stop on the interrupt", err)
	}
	waitErr := daemon.Wait()
	exit, ok := errors.AsType[*exec.ExitError](waitErr)
	if !ok || exit.ExitCode() != 130 {
		t.Fatalf("daemon: got %v, want it to exit on the interrupt with status 130", waitErr)
	}
}

func privateDir(t *testing.T) string {
	t.Helper()
	id, err := newInstanceID()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(tmpRoot, id)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	return dir
}

func requireShortEnough(t *testing.T, dirs ...string) {
	t.Helper()
	for _, dir := range dirs {
		if _, err := host.RuntimeAt(dir); err != nil {
			t.Fatalf("the fixture %s is refused for its length before any check: %v", dir, err)
		}
	}
}

func refusedRuntimeDirs(t *testing.T) (string, []string) {
	t.Helper()
	root := privateDir(t)
	file := filepath.Join(root, "file")
	touch(t, file)
	shared := filepath.Join(root, "shared")
	if err := os.Mkdir(shared, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(privateDir(t), link); err != nil {
		t.Fatal(err)
	}
	return file, []string{shared, link}
}

func TestRemoveInstanceEntriesLeavesWhatIsNotAPrivateDirectoryAlone(t *testing.T) {
	instance, err := newInstanceID()
	if err != nil {
		t.Fatal(err)
	}
	file, dirs := refusedRuntimeDirs(t)
	requireShortEnough(t, append(dirs, file)...)
	if failures := removeInstanceEntries(file, instance); len(failures) == 0 || errors.Is(errors.Join(failures...), syscall.ENOTDIR) {
		t.Errorf("the file %s was not refused as an unsafe runtime directory: %v", file, failures)
	}
	for _, dir := range dirs {
		entry := filepath.Join(dir, "herrscher-command-"+instance+".sock")
		touch(t, entry)
		if failures := removeInstanceEntries(dir, instance); len(failures) == 0 {
			t.Errorf("%s was accepted as a private runtime directory", dir)
		}
		if !exists(entry) {
			t.Errorf("%s was removed", entry)
		}
	}
}

func hashedRuntimeDir(t *testing.T, instance string) string {
	t.Helper()
	base := privateDir(t)
	for pad := 1; ; pad++ {
		dir := filepath.Join(base, strings.Repeat("d", pad))
		known, err := entriesOf(dir, instance)
		if err != nil {
			t.Fatalf("no directory under %s gives hashed socket names before it is too long: %v", base, err)
		}
		if strings.HasPrefix(filepath.Base(known.command), "herrscher-") || strings.HasPrefix(filepath.Base(known.events), "herrscher-") {
			continue
		}
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		return dir
	}
}

func entriesIn(t *testing.T, dir, instance string) []string {
	t.Helper()
	known, err := entriesOf(dir, instance)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range known.paths() {
		touch(t, path)
	}
	return known.paths()
}

func TestRemoveHashedEntries(t *testing.T) {
	instance, err := newInstanceID()
	if err != nil {
		t.Fatal(err)
	}
	dir := hashedRuntimeDir(t, instance)
	ours := entriesIn(t, dir, instance)
	theirs := entriesIn(t, dir, "e2e-00000000")
	if failures := removeInstanceEntries(dir, instance); len(failures) != 0 {
		t.Fatal(failures)
	}
	for _, path := range ours {
		if exists(path) {
			t.Errorf("%s was left behind", path)
		}
	}
	for _, path := range theirs {
		if !exists(path) {
			t.Errorf("%s of another instance was removed", path)
		}
	}
}

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRemoveInstanceEntriesCreatesNoMissingDirectory(t *testing.T) {
	instance, err := newInstanceID()
	if err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(privateDir(t), "missing")
	requireShortEnough(t, missing)
	if failures := removeInstanceEntries(missing, instance); len(failures) != 0 {
		t.Fatal(failures)
	}
	if exists(missing) {
		t.Errorf("%s was created", missing)
	}
}

func hashedSocketIn(t *testing.T, dir string) string {
	t.Helper()
	layout, err := host.RuntimeAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	name := host.CommandSocketPathIn(layout, strings.Repeat("s", 120))
	if !hashedSocket.MatchString(filepath.Base(name)) {
		t.Fatalf("the guard does not know the hashed name %s that core/host gives", name)
	}
	return name
}

func TestSettleNamesAHashedEntryThatAppeared(t *testing.T) {
	dir := privateDir(t)
	touch(t, filepath.Join(dir, "hs-000000000000000000000000.sock"))
	dirs := []string{dir, filepath.Join(dir, "missing")}
	before, err := hashedEntriesIn(dirs)
	if err != nil {
		t.Fatal(err)
	}
	touch(t, filepath.Join(dir, "herrscher-command-x.sock"))
	touch(t, filepath.Join(dir, "3f9a0c1d2e4b5a6978"))
	if quiet := appearedHashedIn(dirs, before); quiet != nil {
		t.Fatalf("a readable name, a staged name or an older entry was reported: %v", quiet)
	}
	fresh := hashedSocketIn(t, dir)
	touch(t, fresh)
	reported := appearedHashedIn(dirs, before)
	if !errors.Is(reported, errHashedEntry) || !strings.Contains(reported.Error(), fresh) {
		t.Fatalf("got %v, want %s reported", reported, fresh)
	}
}

func TestReapInstanceHonorsAGraceOtherThanTheDefault(t *testing.T) {
	instance, err := newInstanceID()
	if err != nil {
		t.Fatal(err)
	}
	survivor := ignoringINT(t, instance)
	grace := 300 * time.Millisecond
	started := time.Now()
	stopped, survivors, err := reapInstance(procRoot, instance, grace)
	if err != nil {
		t.Fatal(err)
	}
	if len(stopped) != 0 || len(survivors) != 1 {
		t.Fatalf("got stopped %q and survivors %q, want the one survivor of SIGINT", stopped, survivors)
	}
	if elapsed := time.Since(started); elapsed < grace || elapsed >= reapGrace {
		t.Fatalf("got %v, want the first wait to last the grace %v rather than %v", elapsed, grace, reapGrace)
	}
	if signo := stoppedBy(t, survivor); signo != syscall.SIGKILL {
		t.Fatalf("survivor: got %v, want SIGKILL", signo)
	}
}

func TestSettleReportsAProcessThatExitsAfterSIGINTAsStopped(t *testing.T) {
	instance, err := newInstanceID()
	if err != nil {
		t.Fatal(err)
	}
	exiting := exitingAfterINT(t, instance)
	root := t.TempDir()
	before, err := hashedEntries(root)
	if err != nil {
		t.Fatal(err)
	}
	settleErr := settle(root, instance, before)
	if !errors.Is(settleErr, errStopped) || errors.Is(settleErr, errLeaked) {
		t.Fatalf("got %v, want the process reported as stopped, not as leaked", settleErr)
	}
	if waitErr := exiting.Wait(); waitErr != nil {
		t.Fatalf("the process ended with %v, want its own exit after SIGINT", waitErr)
	}
}

func exitingAfterINT(t *testing.T, instance string) *exec.Cmd {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "sh", "-c", `trap 'sleep 1; exit 0' INT; echo ready; while :; do sleep 0.05; done`)
	cmd.Env = []string{instanceEnv + "=" + instance}
	return startReady(t, cmd)
}

func TestSettleDoesNotWaitForAProcessToExitByItself(t *testing.T) {
	instance, err := newInstanceID()
	if err != nil {
		t.Fatal(err)
	}
	running := exec.CommandContext(t.Context(), "cat")
	running.Env = []string{instanceEnv + "=" + instance}
	if _, pipeErr := running.StdinPipe(); pipeErr != nil {
		t.Fatal(pipeErr)
	}
	if startErr := running.Start(); startErr != nil {
		t.Fatal(startErr)
	}
	reaped := false
	t.Cleanup(func() {
		if reaped {
			return
		}
		if killErr := running.Process.Kill(); killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
			t.Error(killErr)
		}
		if _, waitErr := running.Process.Wait(); waitErr != nil {
			t.Error(waitErr)
		}
	})
	root := t.TempDir()
	before, err := hashedEntries(root)
	if err != nil {
		t.Fatal(err)
	}
	if settleErr := settle(root, instance, before); !errors.Is(settleErr, errStopped) {
		t.Fatalf("got %v, want the process still running at the end reported as stopped", settleErr)
	}
	reaped = true
	if signo := stoppedBy(t, running); signo != syscall.SIGINT {
		t.Fatalf("got %v, want the process stopped by SIGINT", signo)
	}
}
