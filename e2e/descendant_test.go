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

const descendantWait = 5 * time.Second

func descendantGone(ts *testscript.TestScript, neg bool, args []string) {
	if neg {
		ts.Fatalf("descendantgone cannot be negated: it waits for the descendant to exit")
	}
	if len(args) != 1 {
		ts.Fatalf("usage: descendantgone <pid file>")
	}
	pidPath := ts.MkAbs(args[0])
	pid, err := readPid(pidPath)
	ts.Check(err)
	ts.Check(descendantNotExpired(pidPath))
	entry := instanceEntry(ts.Getenv(instanceEnv))
	ts.Defer(func() { ts.Check(killDescendant(pid, entry)) })
	deadline := time.Now().Add(descendantWait)
	for time.Now().Before(deadline) {
		alive, err := descendantAlive(pid, entry)
		ts.Check(err)
		if !alive {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	ts.Fatalf("the descendant %d of the fake claude still runs %s after the turn ended", pid, descendantWait)
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

func readPid(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

func descendantAlive(pid int, entry string) (bool, error) {
	owned, err := ownedByUs(pid)
	if err != nil || !owned {
		return false, err
	}
	environ, err := environOf(pid)
	foreign, err := environVerdict(err)
	if err != nil {
		return false, fmt.Errorf("cannot read the environment of %d: %w", pid, err)
	}
	if foreign {
		return false, nil
	}
	if !carries(environ, entry) {
		return false, nil
	}
	line, err := commandLine(procRoot, pid)
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

func environVerdict(err error) (bool, error) {
	switch {
	case err == nil, gone(err):
		return false, nil
	case errors.Is(err, fs.ErrPermission):
		return true, nil
	default:
		return false, err
	}
}

func ownedByUs(pid int) (bool, error) {
	status, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "status"))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("cannot read the status of %d: %w", pid, err)
	}
	uid, err := realUID(string(status))
	if err != nil {
		return false, fmt.Errorf("cannot read the owner of %d: %w", pid, err)
	}
	return uid == os.Getuid(), nil
}

func realUID(status string) (int, error) {
	for _, line := range strings.Split(status, "\n") {
		value, found := strings.CutPrefix(line, "Uid:")
		if !found {
			continue
		}
		fields := strings.Fields(value)
		if len(fields) == 0 {
			break
		}
		return strconv.Atoi(fields[0])
	}
	return 0, errors.New("the status file has no readable Uid line")
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

func TestEnvironVerdictTreatsARefusedEnvironAsForeign(t *testing.T) {
	const path = "/proc/1/environ"
	for _, errno := range []syscall.Errno{syscall.EACCES, syscall.EPERM} {
		foreign, err := environVerdict(&fs.PathError{Op: "open", Path: path, Err: errno})
		if !foreign || err != nil {
			t.Fatalf("%v on environ: foreign=%v err=%v, want a foreign process and no failure", errno, foreign, err)
		}
	}
	for _, errno := range []syscall.Errno{syscall.ENOENT, syscall.ESRCH} {
		foreign, err := environVerdict(&fs.PathError{Op: "open", Path: path, Err: errno})
		if foreign || err != nil {
			t.Fatalf("%v on environ: foreign=%v err=%v, want neither foreign nor failed: the process is gone", errno, foreign, err)
		}
	}
	foreign, err := environVerdict(&fs.PathError{Op: "open", Path: path, Err: syscall.EIO})
	if foreign || !errors.Is(err, syscall.EIO) {
		t.Fatalf("EIO on environ: foreign=%v err=%v, want the unexpected errno returned as a failure", foreign, err)
	}
	if foreign, err := environVerdict(nil); foreign || err != nil {
		t.Fatalf("a readable environ: foreign=%v err=%v, want neither", foreign, err)
	}
}

func TestDescendantAliveSeesALiveDescendantOfOurs(t *testing.T) {
	entry := instanceEntry("probe-" + strconv.Itoa(os.Getpid()))
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
	alive, err := descendantAlive(cmd.Process.Pid, entry)
	if err != nil || !alive {
		t.Fatalf("a live descendant of ours carrying the marker was not seen: alive=%v err=%v: the scan would report a surviving descendant as gone", alive, err)
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
