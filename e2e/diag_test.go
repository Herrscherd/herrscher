//go:build e2e && linux

package e2e_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func diagFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "ERR " + err.Error()
	}
	return strings.TrimSpace(string(data))
}

func diagStatusLines(pid int) string {
	data, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "status"))
	if err != nil {
		return "ERR " + err.Error()
	}
	var keep []string
	for _, line := range strings.Split(string(data), "\n") {
		for _, key := range []string{"Name:", "State:", "Uid:", "Gid:", "NoNewPrivs:", "Seccomp:", "CapEff:"} {
			if strings.HasPrefix(line, key) {
				keep = append(keep, line)
			}
		}
	}
	return strings.Join(keep, " | ")
}

func TestDiagEACCES(t *testing.T) {
	var b strings.Builder
	fmt.Fprintf(&b, "uid=%d gid=%d ptrace_scope=%s suid_dumpable=%s\n", os.Getuid(), os.Getgid(),
		diagFile("/proc/sys/kernel/yama/ptrace_scope"), diagFile("/proc/sys/fs/suid_dumpable"))
	fmt.Fprintf(&b, "self status: %s\n", diagStatusLines(os.Getpid()))
	fmt.Fprintf(&b, "lsm: %s\n", diagFile("/sys/kernel/security/lsm"))
	for _, variant := range []string{"direct", "shell-exec"} {
		instance, err := newInstanceID()
		if err != nil {
			t.Fatal(err)
		}
		entry := instanceEntry(instance)
		var cmd *exec.Cmd
		if variant == "direct" {
			cmd = exec.Command("sleep", "60")
			cmd.Env = []string{entry}
		} else {
			cmd = exec.Command("sh", "-c", `trap "" INT; echo ready; exec sleep 60`)
			cmd.Env = []string{entry}
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		pid := cmd.Process.Pid
		fmt.Fprintf(&b, "[%s] pid=%d\n", variant, pid)
		fmt.Fprintf(&b, "  at start: %s\n", diagStatusLines(pid))
		found, scanErr := processesIn(procRoot, entry)
		fmt.Fprintf(&b, "  scan found=%v err=%v\n", found, scanErr)
		for attempt := range 5 {
			data, rerr := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "environ"))
			fmt.Fprintf(&b, "  read#%d len=%d carries=%v err=%v\n", attempt, len(data), carries(data, entry), rerr)
			time.Sleep(2 * time.Millisecond)
		}
		fd, perr := unix.PidfdOpen(pid, 0)
		fmt.Fprintf(&b, "  pidfd fd=%d err=%v\n", fd, perr)
		if perr == nil {
			data, rerr := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "environ"))
			fmt.Fprintf(&b, "  after pidfd: len=%d carries=%v err=%v\n", len(data), carries(data, entry), rerr)
			_ = unix.Close(fd)
		}
		fmt.Fprintf(&b, "  attr/current=%s\n", diagFile(filepath.Join(procRoot, strconv.Itoa(pid), "attr", "current")))
		fmt.Fprintf(&b, "  exe=%s\n", diagFile(filepath.Join(procRoot, strconv.Itoa(pid), "cmdline")))
		fmt.Fprintf(&b, "  end status: %s\n", diagStatusLines(pid))
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
	t.Fatal(b.String())
}

var _ = syscall.SIGINT
