//go:build e2e && linux

package e2e_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rogpeppe/go-internal/testscript"

	"github.com/Herrscherd/herrscher/e2e/vendortest"
)

func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		"claude": func() {
			os.Exit(vendortest.Claude(os.Args[1:], vendortest.Streams{In: os.Stdin, Out: os.Stdout, Err: os.Stderr}, vendortest.FilesFrom(os.Getenv)))
		},
	})
}

func TestScenarios(t *testing.T) {
	binDir := buildHerrscher(t)
	testscript.Run(t, testscript.Params{
		Dir:                 filepath.Join("testdata", "scenarios"),
		RequireExplicitExec: true,
		Setup: func(env *testscript.Env) error {
			return isolate(env, binDir)
		},
		Cmds: map[string]func(*testscript.TestScript, bool, []string){
			"waitdaemon": waitDaemon,
			"waitlog":    waitLog,
		},
	})
}

func buildHerrscher(tb testing.TB) string {
	tb.Helper()
	binDir := tb.TempDir()
	build := exec.CommandContext(tb.Context(), "go", "build", "-o", filepath.Join(binDir, "herrscher"), ".")
	build.Dir = ".."
	build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOWORK=off")
	if out, err := build.CombinedOutput(); err != nil {
		tb.Fatalf("go build herrscher: %v\n%s", err, out)
	}
	return binDir
}

const instanceEnv = "HERRSCHER_INSTANCE_ID"

func isolate(env *testscript.Env, binDir string) error {
	instance, err := newInstanceID()
	if err != nil {
		return err
	}
	home := filepath.Join(env.WorkDir, "home")
	repo := filepath.Join(env.WorkDir, "repo")
	for _, dir := range []string{home, repo} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	files := vendortest.Files{
		Script: filepath.Join(env.WorkDir, "claude.script"),
		Log:    filepath.Join(env.WorkDir, "claude.log"),
		Turns:  filepath.Join(env.WorkDir, "claude.turns"),
	}
	vars := append([]string{
		"HOME=" + home,
		"PATH=" + binDir + string(os.PathListSeparator) + env.Getenv("PATH"),
		"LANG=C.UTF-8",
		instanceEnv + "=" + instance,
		"HERRSCHER_MODELS_DISCOVER=none",
		"GIT_AUTHOR_NAME=e2e", "GIT_AUTHOR_EMAIL=e2e@example.invalid",
		"GIT_COMMITTER_NAME=e2e", "GIT_COMMITTER_EMAIL=e2e@example.invalid",
		"INSTANCE=" + instance,
		"REPO=" + repo,
	}, files.Environ()...)
	for _, pair := range vars {
		name, value, _ := strings.Cut(pair, "=")
		env.Setenv(name, value)
	}
	if out, err := exec.CommandContext(context.Background(), "git", "init", "-q", repo).CombinedOutput(); err != nil {
		return fmt.Errorf("git init: %w: %s", err, out)
	}
	env.Cd = repo
	env.Defer(func() {
		if err := settle(instance); err != nil {
			env.T().Fatal(err)
		}
	})
	return nil
}

var errLeaked = errors.New("processes outlived the scenario")

func settle(instance string) error {
	leaked, reapErr := reapInstance(instance, reapGrace)
	if len(leaked) > 0 {
		reapErr = errors.Join(reapErr, fmt.Errorf("%w: %s", errLeaked, strings.Join(leaked, "; ")))
	}
	return errors.Join(reapErr, removeRuntimeEntries(instance))
}

func newInstanceID() (string, error) {
	suffix := make([]byte, 4)
	if _, err := rand.Read(suffix); err != nil {
		return "", err
	}
	return "e2e-" + hex.EncodeToString(suffix), nil
}

const (
	userRuntimeRoot = "/run/user"
	procRoot        = "/proc"
	tmpRoot         = "/tmp"
)

func runtimeDirs() []string {
	uid := strconv.Itoa(os.Getuid())
	return []string{filepath.Join(userRuntimeRoot, uid, "herrscher"), filepath.Join(tmpRoot, "herrscher-"+uid)}
}

const (
	runtimeKeyLen     = 24
	maxUnixSocketPath = 103
)

func runtimeKey(name string) string {
	sum := sha256.Sum256([]byte(name))
	return hex.EncodeToString(sum[:])[:runtimeKeyLen]
}

func instanceLockName(instance string) string {
	return "herrscher-instance-" + runtimeKey(instance) + ".lock"
}

func socketPath(dir, stem string) string {
	plain := filepath.Join(dir, stem+".sock")
	if strings.HasPrefix(stem, "herrscher-") && len(plain) <= maxUnixSocketPath {
		return plain
	}
	return filepath.Join(dir, "hs-"+runtimeKey(stem)+".sock")
}

func daemonSocketStems(instance string) []string {
	return []string{"herrscher-command-" + instance, "herrscher-events-" + instance}
}

func removeRuntimeEntries(instance string) error {
	var failures []error
	for _, dir := range runtimeDirs() {
		failures = append(failures, removeInstanceEntries(dir, instance)...)
	}
	return errors.Join(failures...)
}

func removeInstanceEntries(dir, instance string) []error {
	if err := requireOwnedByUs(dir); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return []error{err}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []error{err}
	}
	owned := map[string]bool{instanceLockName(instance): true}
	for _, stem := range daemonSocketStems(instance) {
		owned[filepath.Base(socketPath(dir, stem))] = true
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

type foreignDirError struct {
	Dir string
}

func (e *foreignDirError) Error() string {
	return "the runtime directory " + e.Dir + " is not a directory owned by this user"
}

func requireOwnedByUs(dir string) error {
	return requireOwnedBy(dir, os.Getuid())
}

func requireOwnedBy(dir string, uid int) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || !ok || int(stat.Uid) != uid {
		return &foreignDirError{Dir: dir}
	}
	return nil
}

func commandSocket(instance string) (string, error) {
	stem := daemonSocketStems(instance)[0]
	for _, dir := range runtimeDirs() {
		if path := socketPath(dir, stem); exists(path) {
			return path, nil
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

func lockBeside(instance string) error {
	socket, err := commandSocket(instance)
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(socket), instanceLockName(instance))); err != nil {
		return fmt.Errorf("%w: %w", errNoLock, err)
	}
	return nil
}

const daemonWait = 30 * time.Second

func waitDaemon(ts *testscript.TestScript, neg bool, args []string) {
	if len(args) != 0 {
		ts.Fatalf("usage: [!] waitdaemon")
	}
	instance := ts.Getenv(instanceEnv)
	deadline := time.Now().Add(daemonWait)
	for time.Now().Before(deadline) {
		if answering(instance) != neg {
			if !neg {
				ts.Check(lockBeside(instance))
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

func answering(instance string) bool {
	path, err := commandSocket(instance)
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

const reapGrace = 5 * time.Second

func reapInstance(instance string, grace time.Duration) ([]string, error) {
	marker := []byte(instanceEnv + "=" + instance + "\x00")
	deadline := time.Now().Add(grace)
	for {
		pids, err := processesWith(marker)
		if err != nil || len(pids) == 0 {
			return nil, err
		}
		if time.Now().After(deadline) {
			return killAll(pids), nil
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func killAll(pids []int) []string {
	leaked := make([]string, 0, len(pids))
	for _, pid := range pids {
		leaked = append(leaked, describe(pid))
		if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			leaked = append(leaked, "cannot kill "+strconv.Itoa(pid)+": "+err.Error())
		}
	}
	return leaked
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
	cmdline, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return strconv.Itoa(pid)
	}
	return strconv.Itoa(pid) + " " + strings.Join(strings.Split(strings.TrimRight(string(cmdline), "\x00"), "\x00"), " ")
}
