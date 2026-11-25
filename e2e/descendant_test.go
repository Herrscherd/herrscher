//go:build e2e && linux

package e2e_test

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rogpeppe/go-internal/testscript"

	"github.com/Herrscherd/herrscher/e2e/vendortest"
)

const (
	descendantWait = 5 * time.Second
	descendantPoll = 100 * time.Millisecond
)

var errStillRunning = errors.New("the descendant still alive")

func descendantGone(ts *testscript.TestScript, neg bool, args []string) {
	if neg {
		ts.Fatalf("descendantgone cannot be negated: it waits for the descendant to exit")
	}
	if len(args) != 1 {
		ts.Fatalf("usage: descendantgone <pid file>")
	}
	pidPath := ts.MkAbs(args[0])
	pid, token, err := readDescendant(pidPath)
	ts.Check(err)
	ts.Check(descendantNotExpired(pidPath))
	entry := descendantEntry(token)
	ts.Defer(func() { ts.Check(killDescendant(pid, entry)) })
	waitErr := awaitGone(time.Now().Add(descendantWait), func() (bool, error) {
		return descendantAlive(pid, entry)
	})
	if waitErr != nil {
		ts.Fatalf("the descendant %d of the fake claude still alive %s after the turn ended: %v", pid, descendantWait, waitErr)
	}
}

func awaitGone(deadline time.Time, alive func() (bool, error)) error {
	var last error
	for time.Now().Before(deadline) {
		running, err := alive()
		if err == nil && !running {
			return nil
		}
		last = err
		time.Sleep(descendantPoll)
	}
	if last != nil {
		return last
	}
	return errStillRunning
}

var errDescendantExpired = errors.New("the check starts too late to see the descendant die: it may have ended on its own")

func descendantNotExpired(pidPath string) error {
	info, err := os.Stat(pidPath)
	if err != nil {
		return err
	}
	life := vendortest.DescendantSleep
	if time.Since(info.ModTime()) > life-descendantWait {
		return errDescendantExpired
	}
	return nil
}

func descendantEntry(token string) string {
	return vendortest.DescendantMarker + "=" + token
}

func readDescendant(path string) (int, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, "", err
	}
	fields := strings.Fields(string(data))
	if len(fields) != 2 {
		return 0, "", fmt.Errorf("the pid file %s holds %d fields, want a pid and a token", path, len(fields))
	}
	pid, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, "", err
	}
	return pid, fields[1], nil
}

func descendantAlive(pid int, entry string) (bool, error) {
	return descendantAliveIn(procRoot, pid, entry)
}

func descendantAliveIn(root string, pid int, entry string) (bool, error) {
	status, err := os.ReadFile(filepath.Join(root, strconv.Itoa(pid), "status"))
	if gone(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("cannot read the status of %d: %w", pid, err)
	}
	uid, err := realUID(string(status))
	if err != nil {
		return false, fmt.Errorf("cannot read the owner of %d: %w", pid, err)
	}
	if uid != os.Getuid() || statusField(string(status), "Name") != "sleep" {
		return false, nil
	}
	environ, err := environOf(root, pid)
	if err != nil {
		return false, fmt.Errorf("cannot read the environment of %d: %w", pid, err)
	}
	if !carries(environ, entry) {
		return false, nil
	}
	line, err := commandLine(root, pid)
	if gone(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("cannot read the command of %d: %w", pid, err)
	}
	return line == "sleep "+strconv.Itoa(int(vendortest.DescendantSleep/time.Second)), nil
}

func gone(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ESRCH)
}

func statusField(status, key string) string {
	for _, line := range strings.Split(status, "\n") {
		if value, found := strings.CutPrefix(line, key+":"); found {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func realUID(status string) (int, error) {
	fields := strings.Fields(statusField(status, "Uid"))
	if len(fields) == 0 {
		return 0, errors.New("the status file has no readable Uid line")
	}
	return strconv.Atoi(fields[0])
}

func killDescendant(pid int, entry string) error {
	alive, err := descendantAlive(pid, entry)
	if err != nil || !alive {
		return err
	}
	stat, err := readStat(procRoot, pid)
	if gone(err) {
		return nil
	}
	if err != nil {
		return err
	}
	p, pinnedOK, err := pin(procRoot, openPidfd, processRef{pid: pid, start: stat.start}, entry)
	if err != nil || !pinnedOK {
		return err
	}
	killErr := signalAll([]pinned{p}, syscall.SIGKILL)
	return errors.Join(killErr, p.release())
}

func TestDescendantAliveIgnoresAForeignProcess(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root owns every process, so no pid is foreign")
	}
	alive, err := descendantAlive(1, instanceEntry("unused"))
	if err != nil || alive {
		t.Fatalf("a process of another uid was counted as a descendant: alive=%v err=%v", alive, err)
	}
}

func plantSleepStatus(t *testing.T, root string, pid int, name string, environMode fs.FileMode) {
	t.Helper()
	dir := filepath.Join(root, strconv.Itoa(pid))
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	uid := strconv.Itoa(os.Getuid())
	status := "Name:\t" + name + "\nUid:\t" + uid + "\t" + uid + "\t" + uid + "\t" + uid + "\n"
	if err := os.WriteFile(filepath.Join(dir, "status"), []byte(status), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "environ"), []byte("NOTHING=here\x00"), environMode); err != nil {
		t.Fatal(err)
	}
}

func TestDescendantAliveFailsWhenTheEnvironOfOurSleepIsRefused(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every environ, so no read of ours is refused")
	}
	root := t.TempDir()
	const pid = 4242
	plantSleepStatus(t, root, pid, "sleep", 0)
	alive, err := descendantAliveIn(root, pid, instanceEntry("probe"))
	if err == nil {
		t.Fatalf("a refused environ of our own sleep was read as a gone descendant (alive=%v): the turn-end check would pass while the descendant runs", alive)
	}
}

func TestDescendantAliveIgnoresAReusedPidOfAnotherProgram(t *testing.T) {
	root := t.TempDir()
	const pid = 4243
	plantSleepStatus(t, root, pid, "sudo", 0)
	alive, err := descendantAliveIn(root, pid, instanceEntry("probe"))
	if err != nil || alive {
		t.Fatalf("a reused pid of another program of our uid was taken for the descendant: alive=%v err=%v", alive, err)
	}
}

func startProbeSleep(t *testing.T, entry string) int {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "sleep", strconv.Itoa(int(vendortest.DescendantSleep/time.Second)))
	cmd.Env = append(os.Environ(), entry)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if killErr := cmd.Process.Kill(); killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
			t.Errorf("kill the probe: %v", killErr)
		}
		var exitErr *exec.ExitError
		waitErr := cmd.Wait()
		if waitErr != nil && (!errors.As(waitErr, &exitErr) || exitErr.String() != "signal: killed") {
			t.Errorf("wait for the probe: %v", waitErr)
		}
	})
	awaitMarker(t, cmd.Process.Pid, entry)
	return cmd.Process.Pid
}

func awaitMarker(t *testing.T, pid int, entry string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		environ, err := environOf(procRoot, pid)
		if err == nil && carries(environ, entry) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the probe %d never showed its marker in its environment: err=%v", pid, err)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestDescendantAliveSeesALiveDescendantOfOurs(t *testing.T) {
	entry := descendantEntry("probe-" + strconv.Itoa(os.Getpid()))
	pid := startProbeSleep(t, entry)
	alive, err := descendantAlive(pid, entry)
	if err != nil || !alive {
		t.Fatalf("a live descendant of ours carrying the marker was not seen: alive=%v err=%v: the scan would report a surviving descendant as gone", alive, err)
	}
}

func TestDescendantAliveIgnoresAnotherDescendantOfTheSameRun(t *testing.T) {
	pid := startProbeSleep(t, descendantEntry("other-scenario"))
	alive, err := descendantAlive(pid, descendantEntry("this-scenario"))
	if err != nil || alive {
		t.Fatalf("a sleep of another scenario carrying the same marker name was taken for this scenario's descendant: alive=%v err=%v", alive, err)
	}
}

func TestRealUIDReadsTheUidLineOfTheStatusFile(t *testing.T) {
	uid, err := realUID("Name:\tsleep\nUmask:\t0022\nUid:\t1000\t1000\t1000\t1000\nGid:\t0\t0\t0\t0\n")
	if err != nil || uid != 1000 {
		t.Fatalf("realUID = %d, %v, want 1000: the real uid is the first field of the Uid line, whatever the owner of the directory", uid, err)
	}
	if _, err := realUID("Name:\tsleep\n"); err == nil {
		t.Fatal("a status file without a Uid line was accepted")
	}
}

func TestAwaitGoneRidesOutATransientRefusal(t *testing.T) {
	calls := 0
	err := awaitGone(time.Now().Add(descendantWait), func() (bool, error) {
		calls++
		switch calls {
		case 1:
			return false, errors.New("permission denied on the environment")
		case 2:
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		t.Fatalf("a transient refusal ended the wait for a descendant that did exit: %v", err)
	}
}

func TestAwaitGoneReportsAPersistentRefusal(t *testing.T) {
	refused := errors.New("cannot read the environment")
	err := awaitGone(time.Now().Add(3*descendantPoll), func() (bool, error) {
		return false, refused
	})
	if !errors.Is(err, refused) {
		t.Fatalf("a refusal that never clears was reported as %v, want the refusal itself", err)
	}
}

func TestAwaitGoneReportsASurvivingDescendant(t *testing.T) {
	err := awaitGone(time.Now().Add(3*descendantPoll), func() (bool, error) {
		return true, nil
	})
	if !errors.Is(err, errStillRunning) {
		t.Fatalf("a descendant alive at the deadline was reported as %v, want errStillRunning", err)
	}
}
