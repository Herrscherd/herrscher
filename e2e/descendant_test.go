//go:build e2e && linux

package e2e_test

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
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
	environ, err := environOf(pid)
	if err != nil {
		return false, fmt.Errorf("cannot read the environment of %d: %w", pid, err)
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
	p, pinnedOK, err := pin(procRoot, processRef{pid: pid, start: stat.start}, entry)
	if err != nil || !pinnedOK {
		return err
	}
	killErr := signalAll([]pinned{p}, syscall.SIGKILL)
	return errors.Join(killErr, p.release())
}

func TestDescendantAliveRefusesAnUnreadablePid(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads the environment of every process")
	}
	if _, err := descendantAlive(1, instanceEntry("unused")); !errors.Is(err, fs.ErrPermission) {
		t.Fatal("an unreadable pid counted as a live or dead descendant instead of an error")
	}
}
