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

type processRef struct {
	pid   int
	start string
}

type procStat struct {
	state string
	start string
}

func (s procStat) exited() bool {
	return s.state == "Z" || s.state == "X"
}

const startTimeIndex = 19

func parseStat(data string) (procStat, error) {
	end := strings.LastIndexByte(data, ')')
	if end < 0 {
		return procStat{}, fmt.Errorf("malformed stat line %q", data)
	}
	fields := strings.Fields(data[end+1:])
	if len(fields) <= startTimeIndex {
		return procStat{}, fmt.Errorf("stat line too short: %q", data)
	}
	return procStat{state: fields[0], start: fields[startTimeIndex]}, nil
}

func readStat(root string, pid int) (procStat, error) {
	data, err := os.ReadFile(filepath.Join(root, strconv.Itoa(pid), "stat"))
	if err != nil {
		return procStat{}, err
	}
	return parseStat(string(data))
}

func reapInstance(root, instance string, grace time.Duration) ([]string, []string, error) {
	if !instanceIDPattern.MatchString(instance) {
		return nil, nil, fmt.Errorf("refusing to reap %q: it is not an instance id", instance)
	}
	entry := instanceEntry(instance)
	listed, err := scanInstance(root, entry)
	if err != nil || len(listed) == 0 {
		return nil, nil, err
	}
	set := newPinSet(root, entry)
	stopped, survivors, reapErr := reapPinned(root, set, listed, grace)
	return stopped, survivors, errors.Join(reapErr, set.close())
}

func reapPinned(root string, set *pinSet, listed []processRef, grace time.Duration) ([]string, []string, error) {
	for _, ref := range listed {
		if err := set.add(ref); err != nil {
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
	root  string
	pid   int
	fd    int
	name  string
	start string
	entry string
}

type pinSet struct {
	root  string
	entry string
	byPID map[int]pinned
}

func newPinSet(root, entry string) *pinSet {
	return &pinSet{root: root, entry: entry, byPID: make(map[int]pinned)}
}

func (s *pinSet) add(ref processRef) error {
	if _, known := s.byPID[ref.pid]; known {
		return nil
	}
	p, ok, err := pin(s.root, ref, s.entry)
	if err != nil || !ok {
		return err
	}
	s.byPID[ref.pid] = p
	return nil
}

func (s *pinSet) collect(root string) error {
	listed, err := scanInstance(root, s.entry)
	if err != nil {
		return err
	}
	for _, ref := range listed {
		if err := s.add(ref); err != nil {
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

func pin(root string, ref processRef, entry string) (pinned, bool, error) {
	fd, err := unix.PidfdOpen(ref.pid, 0)
	if errors.Is(err, syscall.ESRCH) {
		return pinned{}, false, nil
	}
	if err != nil {
		return pinned{}, false, fmt.Errorf("cannot pin process %d: %w", ref.pid, err)
	}
	candidate := pinned{root: root, pid: ref.pid, fd: fd, start: ref.start, entry: entry}
	current, err := readStat(root, ref.pid)
	if gone(err) {
		return pinned{}, false, candidate.release()
	}
	if err != nil {
		return pinned{}, false, errors.Join(fmt.Errorf("cannot verify process %d: %w", ref.pid, err), candidate.release())
	}
	if current.start != ref.start || current.exited() {
		return pinned{}, false, candidate.release()
	}
	candidate.name = describe(root, ref.pid)
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
	current, err := readStat(p.root, p.pid)
	if gone(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("cannot read the state of %d: %w", p.pid, err)
	}
	return current.start == p.start && !current.exited(), nil
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
	listed, err := scanInstance(root, entry)
	if err != nil {
		return nil, err
	}
	pids := make([]int, 0, len(listed))
	for _, ref := range listed {
		pids = append(pids, ref.pid)
	}
	return pids, nil
}

func scanInstance(root, entry string) ([]processRef, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("cannot list the processes: %w", err)
	}
	var listed []processRef
	for _, dirEntry := range entries {
		ref, found, err := instanceRef(root, dirEntry.Name(), entry)
		if err != nil {
			return nil, err
		}
		if found {
			listed = append(listed, ref)
		}
	}
	return listed, nil
}

func scannedPid(name string) (int, bool) {
	pid, err := strconv.Atoi(name)
	return pid, err == nil && pid != os.Getpid()
}

func instanceRef(root, name, entry string) (processRef, bool, error) {
	pid, ok := scannedPid(name)
	if !ok {
		return processRef{}, false, nil
	}
	stat, err := readStat(root, pid)
	if err != nil {
		return processRef{}, false, unlistable(pid, err)
	}
	environ, err := os.ReadFile(filepath.Join(root, name, "environ"))
	if err != nil {
		return processRef{}, false, unlistable(pid, err)
	}
	return processRef{pid: pid, start: stat.start}, carries(environ, entry), nil
}

func unlistable(pid int, err error) error {
	if gone(err) || errors.Is(err, fs.ErrPermission) {
		return nil
	}
	return fmt.Errorf("cannot read process %d: %w", pid, err)
}

func describe(root string, pid int) string {
	line, err := commandLine(root, pid)
	if err != nil {
		return strconv.Itoa(pid)
	}
	return strconv.Itoa(pid) + " " + line
}

func commandLine(root string, pid int) (string, error) {
	cmdline, err := os.ReadFile(filepath.Join(root, strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return "", err
	}
	return strings.Join(strings.Split(strings.TrimRight(string(cmdline), "\x00"), "\x00"), " "), nil
}
