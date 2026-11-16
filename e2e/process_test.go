//go:build e2e && linux

package e2e_test

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const procRoot = "/proc"

func instanceMarker(instance string) []byte {
	return []byte(instanceEnv + "=" + instance + "\x00")
}

const reapGrace = 5 * time.Second

func reapInstance(instance string, grace time.Duration) ([]string, []string, error) {
	marker := instanceMarker(instance)
	found, err := processesWith(marker)
	if err != nil || len(found) == 0 {
		return nil, nil, err
	}
	names := make(map[int]string, len(found))
	for _, pid := range found {
		names[pid] = describe(pid)
	}
	interruptErr := signalAll(found, syscall.SIGINT)
	remaining, waitErr := awaitGone(marker, grace)
	if waitErr != nil {
		return nil, nil, errors.Join(interruptErr, waitErr, signalAll(found, syscall.SIGKILL))
	}
	var stopped []string
	for _, pid := range found {
		if !slices.Contains(remaining, pid) {
			stopped = append(stopped, names[pid])
		}
	}
	if len(remaining) == 0 {
		return stopped, nil, interruptErr
	}
	survivors := namesOf(names, remaining)
	killErr := signalAll(remaining, syscall.SIGKILL)
	unkilled, killWaitErr := awaitGone(marker, grace)
	return stopped, survivors, errors.Join(interruptErr, killErr, killWaitErr, unkilledError(names, unkilled))
}

func unkilledError(names map[int]string, unkilled []int) error {
	if len(unkilled) == 0 {
		return nil
	}
	return fmt.Errorf("processes survived SIGKILL: %s", strings.Join(namesOf(names, unkilled), "; "))
}

func signalAll(pids []int, signo syscall.Signal) error {
	var errs []error
	for _, pid := range pids {
		if err := syscall.Kill(pid, signo); err != nil && !errors.Is(err, syscall.ESRCH) {
			errs = append(errs, fmt.Errorf("cannot send %v to %d: %w", signo, pid, err))
		}
	}
	return errors.Join(errs...)
}

func awaitGone(marker []byte, grace time.Duration) ([]int, error) {
	deadline := time.Now().Add(grace)
	for {
		pids, err := processesWith(marker)
		if err != nil || len(pids) == 0 || time.Now().After(deadline) {
			return pids, err
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func namesOf(names map[int]string, pids []int) []string {
	lines := make([]string, 0, len(pids))
	for _, pid := range pids {
		if name, known := names[pid]; known {
			lines = append(lines, name)
			continue
		}
		lines = append(lines, describe(pid))
	}
	return lines
}

func processesWith(marker []byte) ([]int, error) {
	return processesIn(procRoot, marker)
}

func processesIn(root string, marker []byte) ([]int, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("cannot list the processes: %w", err)
	}
	self := os.Getpid()
	var pids []int
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == self {
			continue
		}
		environ, err := os.ReadFile(filepath.Join(root, entry.Name(), "environ"))
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) || errors.Is(err, syscall.ESRCH) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("cannot read the environment of %d: %w", pid, err)
		}
		if bytes.Contains(append(environ, 0), marker) {
			pids = append(pids, pid)
		}
	}
	return pids, nil
}

func describe(pid int) string {
	line, err := commandLine(pid)
	if err != nil {
		return strconv.Itoa(pid)
	}
	return strconv.Itoa(pid) + " " + line
}

func commandLine(pid int) (string, error) {
	cmdline, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return "", err
	}
	return strings.Join(strings.Split(strings.TrimRight(string(cmdline), "\x00"), "\x00"), " "), nil
}
