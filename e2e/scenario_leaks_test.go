//go:build e2e && linux

package e2e_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/rogpeppe/go-internal/testscript"

	"github.com/Herrscherd/herrscher/core/host"
	"github.com/Herrscherd/herrscher/e2e/vendortest"
)

var (
	errStopped = errors.New("processes were still running when the scenario ended and were stopped with SIGINT")
	errLeaked  = errors.New("processes outlived the scenario, ignored SIGINT and were killed with SIGKILL")
)

func settle(root, instance string, before map[string]bool) error {
	stopped, survivors, reapErr := reapInstance(procRoot, instance, reapGrace)
	var stoppedErr, leakErr error
	if len(stopped) > 0 {
		stoppedErr = fmt.Errorf("%w: %s", errStopped, strings.Join(stopped, "; "))
	}
	if len(survivors) > 0 {
		leakErr = fmt.Errorf("%w: %s", errLeaked, strings.Join(survivors, "; "))
	}
	removeErr := removeRuntimeEntries(root, instance)
	return errors.Join(stoppedErr, leakErr, reapErr, removeErr, appearedHashed(root, before))
}

var (
	errHashedEntry = errors.New("a socket with a hashed name, hs-<24 hex>.sock, appeared in the runtime directory during the scenario and was left there; a session name of this scenario or of another herrscher of this user is too long for the unix limit: shorten it, and remove the socket only once no running herrscher holds it")
	hashedSocket   = regexp.MustCompile(`^hs-[0-9a-f]{24}\.sock$`)
)

func hashedEntries(root string) (map[string]bool, error) {
	return hashedEntriesIn(runtimeDirs(root))
}

func hashedEntriesIn(dirs []string) (map[string]bool, error) {
	found := make(map[string]bool)
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if hashedSocket.MatchString(entry.Name()) {
				found[filepath.Join(dir, entry.Name())] = true
			}
		}
	}
	return found, nil
}

func appearedHashed(root string, before map[string]bool) error {
	return appearedHashedIn(runtimeDirs(root), before)
}

func appearedHashedIn(dirs []string, before map[string]bool) error {
	after, err := hashedEntriesIn(dirs)
	if err != nil {
		return err
	}
	var appeared []string
	for path := range after {
		if !before[path] {
			appeared = append(appeared, path)
		}
	}
	if len(appeared) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %s", errHashedEntry, strings.Join(appeared, ", "))
}

func newInstanceID() (string, error) {
	suffix := make([]byte, 4)
	if _, err := rand.Read(suffix); err != nil {
		return "", err
	}
	return "e2e-" + hex.EncodeToString(suffix), nil
}

const (
	runtimeRootEnv = "HERRSCHER_E2E_RUNTIME_ROOT"
	tmpRoot        = "/tmp"
)

func runtimeDirs(root string) []string {
	return []string{herrscherDir(root), filepath.Join(root, "herrscher-"+strconv.Itoa(os.Getuid()))}
}

type daemonEntries struct {
	layout  host.RuntimeLayout
	command string
	events  string
	lock    string
}

func entriesOf(dir, instance string) (daemonEntries, error) {
	layout, err := host.RuntimeAt(dir)
	if err != nil {
		return daemonEntries{}, err
	}
	return daemonEntries{
		layout:  layout,
		command: host.CommandSocketPathIn(layout, instance),
		events:  host.EventsSocketPathIn(layout, instance),
		lock:    layout.Lock(instance),
	}, nil
}

func (d daemonEntries) paths() []string {
	return []string{d.command, d.events, d.lock}
}

func removeRuntimeEntries(root, instance string) error {
	var failures []error
	for _, dir := range runtimeDirs(root) {
		failures = append(failures, removeInstanceEntries(dir, instance)...)
	}
	return errors.Join(failures...)
}

func removeInstanceEntries(dir, instance string) []error {
	if _, err := os.Lstat(dir); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	known, err := entriesOf(dir, instance)
	if err != nil {
		return []error{err}
	}
	if unsafe := known.layout.Check(known.command); unsafe != nil {
		return []error{unsafe}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []error{err}
	}
	owned := make(map[string]bool, len(known.paths()))
	for _, path := range known.paths() {
		owned[filepath.Base(path)] = true
	}
	var failures []error
	for _, entry := range entries {
		name := entry.Name()
		if !strings.Contains(name, instance) && !owned[name] {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			failures = append(failures, err)
		}
	}
	return failures
}

func commandSocket(root, instance string) (string, error) {
	for _, dir := range runtimeDirs(root) {
		known, err := entriesOf(dir, instance)
		if err != nil {
			return "", err
		}
		if exists(known.command) {
			return known.command, nil
		}
	}
	return "", errNoSocket
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

var (
	errNoSocket = errors.New("no command socket yet")
	errNoLock   = errors.New("the daemon answers but its lock is not where the cleanup looks")
)

func lockBeside(root, instance string) error {
	socket, err := commandSocket(root, instance)
	if err != nil {
		return err
	}
	known, err := entriesOf(filepath.Dir(socket), instance)
	if err != nil {
		return err
	}
	if _, err := os.Stat(known.lock); err != nil {
		return fmt.Errorf("%w: %w", errNoLock, err)
	}
	return nil
}

const daemonWait = 30 * time.Second

const orphanExitWait = 5 * time.Second

func waitInstanceExit(instance string, wait time.Duration) error {
	deadline := time.Now().Add(wait)
	for {
		listed, err := scanInstance(procRoot, instanceEntry(instance))
		if err != nil || len(listed) == 0 || time.Now().After(deadline) {
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func instanceGone(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 0 {
		ts.Fatalf("usage: instancegone")
	}
	instance := ts.Getenv(instanceEnv)
	ts.Check(waitInstanceExit(instance, orphanExitWait))
	stopped, survivors, err := reapInstance(procRoot, instance, reapGrace)
	ts.Check(err)
	if len(stopped)+len(survivors) > 0 {
		ts.Fatalf("processes of the daemon outlived it: stopped with SIGINT [%s], killed with SIGKILL [%s]", strings.Join(stopped, "; "), strings.Join(survivors, "; "))
	}
}

func waitDaemon(ts *testscript.TestScript, neg bool, args []string) {
	if len(args) != 0 {
		ts.Fatalf("usage: [!] waitdaemon")
	}
	root := ts.Getenv(runtimeRootEnv)
	instance := ts.Getenv(instanceEnv)
	deadline := time.Now().Add(daemonWait)
	for time.Now().Before(deadline) {
		if answering(root, instance) != neg {
			if !neg {
				ts.Check(lockBeside(root, instance))
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	if neg {
		ts.Fatalf("the daemon of %s still answers after %s", instance, daemonWait)
	}
	ts.Fatalf("no daemon of %s answered within %s", instance, daemonWait)
}

func waitLog(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 1 {
		ts.Fatalf("usage: waitlog <regexp>")
	}
	pattern, err := regexp.Compile(args[0])
	ts.Check(err)
	deadline := time.Now().Add(daemonWait)
	for time.Now().Before(deadline) {
		if logged, err := os.ReadFile(vendortest.FilesFrom(ts.Getenv).Log); err == nil && pattern.Match(logged) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	ts.Fatalf("the vendor log never matched %q within %s", args[0], daemonWait)
}

func answering(root, instance string) bool {
	path, err := commandSocket(root, instance)
	if err != nil {
		return false
	}
	dialer := net.Dialer{Timeout: time.Second}
	conn, err := dialer.DialContext(context.Background(), "unix", path)
	if err != nil {
		return false
	}
	return conn.Close() == nil
}

const pendingFile = "pending.json"

func waitApproval(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 1 {
		ts.Fatalf("usage: waitapproval <variable>")
	}
	deadline := time.Now().Add(daemonWait)
	for time.Now().Before(deadline) {
		if id, ok := onlyPendingApproval(ts); ok {
			ts.Setenv(args[0], id)
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	ts.Fatalf("no single approval waited for within %s", daemonWait)
}

func onlyPendingApproval(ts *testscript.TestScript) (string, bool) {
	if err := ts.Exec("sh", "-c", "herrscher approve list --json > "+pendingFile); err != nil {
		return "", false
	}
	data, err := os.ReadFile(ts.MkAbs(pendingFile))
	if err != nil {
		return "", false
	}
	var pending []struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(data, &pending) != nil || len(pending) != 1 {
		return "", false
	}
	return pending[0].ID, true
}

func sessionSocket(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 2 {
		ts.Fatalf("usage: sessionsocket <session> <variable>")
	}
	root, instance := ts.Getenv(runtimeRootEnv), ts.Getenv(instanceEnv)
	deadline := time.Now().Add(daemonWait)
	for time.Now().Before(deadline) {
		for _, dir := range runtimeDirs(root) {
			layout, err := host.RuntimeAt(dir)
			if err != nil {
				continue
			}
			path := host.SessionCommandSocketPathIn(layout, instance, args[0])
			if _, statErr := os.Stat(path); statErr == nil {
				ts.Setenv(args[1], path)
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	ts.Fatalf("no command socket for session %s within %s", args[0], daemonWait)
}
