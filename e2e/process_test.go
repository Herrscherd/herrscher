//go:build e2e && linux

package e2e_test

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const procRoot = "/proc"

const (
	reapGrace = 5 * time.Second
	pollEvery = 100 * time.Millisecond
)

var instanceIDPattern = regexp.MustCompile(`^e2e-[0-9a-f]{8}$`)

func instanceEntry(instance string) string {
	return instanceEnv + "=" + instance
}

func reapInstance(root, instance string, grace time.Duration) ([]string, []string, error) {
	if !instanceIDPattern.MatchString(instance) {
		return nil, nil, fmt.Errorf("refusing to reap %q: it is not an instance id", instance)
	}
	entry := instanceEntry(instance)
	found, err := processesIn(root, entry)
	if err != nil || len(found) == 0 {
		return nil, nil, err
	}
	set := newPinSet(entry)
	stopped, survivors, reapErr := reapPinned(root, set, found, grace)
	return stopped, survivors, errors.Join(reapErr, set.close())
}

func reapPinned(root string, set *pinSet, found []int, grace time.Duration) ([]string, []string, error) {
	for _, pid := range found {
		if err := set.add(pid); err != nil {
			return nil, nil, err
		}
	}
	targets := set.all()
	interruptErr := signalAll(targets, syscall.SIGINT)
	survivors, waitErr := waitFor(grace, func() ([]pinned, error) {
		if err := set.collect(root); err != nil {
			return nil, err
		}
		return set.live()
	})
	if waitErr != nil {
		return nil, nil, errors.Join(interruptErr, waitErr)
	}
	stopped := namesExcept(targets, survivors)
	if len(survivors) == 0 {
		return stopped, nil, interruptErr
	}
	killErr := signalAll(survivors, syscall.SIGKILL)
	unkilled, killWaitErr := waitFor(grace, func() ([]pinned, error) {
		return liveOf(survivors)
	})
	return stopped, names(survivors), errors.Join(interruptErr, killErr, killWaitErr, unkilledError(unkilled))
}

type pinned struct {
	pid   int
	fd    int
	name  string
	entry string
}

type pinSet struct {
	entry string
	byPID map[int]pinned
}

func newPinSet(entry string) *pinSet {
	return &pinSet{entry: entry, byPID: make(map[int]pinned)}
}

func (s *pinSet) add(pid int) error {
	if _, known := s.byPID[pid]; known {
		return nil
	}
	p, ok, err := pin(pid, s.entry)
	if err != nil || !ok {
		return err
	}
	s.byPID[pid] = p
	return nil
}

func (s *pinSet) collect(root string) error {
	pids, err := processesIn(root, s.entry)
	if err != nil {
		return err
	}
	for _, pid := range pids {
		if err := s.add(pid); err != nil {
			return err
		}
	}
	return nil
}

func (s *pinSet) all() []pinned {
	pinnedAll := make([]pinned, 0, len(s.byPID))
	for _, p := range s.byPID {
		pinnedAll = append(pinnedAll, p)
	}
	sort.Slice(pinnedAll, func(i, j int) bool { return pinnedAll[i].pid < pinnedAll[j].pid })
	return pinnedAll
}

func (s *pinSet) live() ([]pinned, error) {
	return liveOf(s.all())
}

func (s *pinSet) close() error {
	var errs []error
	for _, p := range s.byPID {
		errs = append(errs, p.release())
	}
	return errors.Join(errs...)
}

func (p pinned) release() error {
	return unix.Close(p.fd)
}

func pin(pid int, entry string) (pinned, bool, error) {
	fd, err := unix.PidfdOpen(pid, 0)
	if errors.Is(err, syscall.ESRCH) {
		return pinned{}, false, nil
	}
	if err != nil {
		return pinned{}, false, fmt.Errorf("cannot pin process %d: %w", pid, err)
	}
	candidate := pinned{pid: pid, fd: fd, entry: entry}
	environ, err := environOf(pid)
	if err != nil {
		return pinned{}, false, errors.Join(fmt.Errorf("cannot verify process %d: %w", pid, err), candidate.release())
	}
	if len(environ) == 0 {
		return pinned{}, false, candidate.release()
	}
	if !carries(environ, entry) {
		return pinned{}, false, errors.Join(fmt.Errorf("process %d no longer carries %s: refusing to signal it", pid, entry), candidate.release())
	}
	candidate.name = describe(pid)
	return candidate, true, nil
}

func liveOf(targets []pinned) ([]pinned, error) {
	var live []pinned
	for _, p := range targets {
		alive, err := p.alive()
		if err != nil {
			return nil, err
		}
		if alive {
			live = append(live, p)
		}
	}
	return live, nil
}

func (p pinned) alive() (bool, error) {
	err := unix.PidfdSendSignal(p.fd, 0, nil, 0)
	if errors.Is(err, syscall.ESRCH) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("cannot probe %d: %w", p.pid, err)
	}
	environ, err := environOf(p.pid)
	if err != nil {
		return false, fmt.Errorf("cannot verify %d: %w", p.pid, err)
	}
	return carries(environ, p.entry), nil
}

func signalAll(targets []pinned, signo syscall.Signal) error {
	var errs []error
	for _, p := range targets {
		if err := unix.PidfdSendSignal(p.fd, signo, nil, 0); err != nil && !errors.Is(err, syscall.ESRCH) {
			errs = append(errs, fmt.Errorf("cannot send %v to %d: %w", signo, p.pid, err))
		}
	}
	return errors.Join(errs...)
}

func waitFor(grace time.Duration, poll func() ([]pinned, error)) ([]pinned, error) {
	deadline := time.Now().Add(grace)
	for {
		live, err := poll()
		if err != nil || len(live) == 0 || time.Now().After(deadline) {
			return live, err
		}
		time.Sleep(pollEvery)
	}
}

func namesExcept(targets, survivors []pinned) []string {
	var stopped []string
	for _, p := range targets {
		if !slices.ContainsFunc(survivors, func(s pinned) bool { return s.pid == p.pid }) {
			stopped = append(stopped, p.name)
		}
	}
	return stopped
}

func names(targets []pinned) []string {
	lines := make([]string, 0, len(targets))
	for _, p := range targets {
		lines = append(lines, p.name)
	}
	return lines
}

func unkilledError(unkilled []pinned) error {
	if len(unkilled) == 0 {
		return nil
	}
	return fmt.Errorf("processes survived SIGKILL: %s", strings.Join(names(unkilled), "; "))
}

func environOf(pid int) ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "environ"))
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
		return nil, nil
	}
	return data, err
}

func carries(environ []byte, entry string) bool {
	return slices.Contains(strings.Split(string(environ), "\x00"), entry)
}

func processesIn(root, entry string) ([]int, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("cannot list the processes: %w", err)
	}
	self := os.Getpid()
	var pids []int
	for _, dirEntry := range entries {
		pid, err := strconv.Atoi(dirEntry.Name())
		if err != nil || pid == self {
			continue
		}
		environ, err := os.ReadFile(filepath.Join(root, dirEntry.Name(), "environ"))
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) || errors.Is(err, syscall.ESRCH) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("cannot read the environment of %d: %w", pid, err)
		}
		if carries(environ, entry) {
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
