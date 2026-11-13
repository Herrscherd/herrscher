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
		},
	})
}

func buildHerrscher(t *testing.T) string {
	t.Helper()
	binDir := t.TempDir()
	build := exec.CommandContext(t.Context(), "go", "build", "-o", filepath.Join(binDir, "herrscher"), ".")
	build.Dir = ".."
	build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOWORK=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build herrscher: %v\n%s", err, out)
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
		if leaked := reapInstance(instance); len(leaked) > 0 {
			env.T().Fatal("processes outlived the scenario: " + strings.Join(leaked, "; "))
		}
		if err := removeRuntimeEntries(instance); err != nil {
			env.T().Fatal(err)
		}
	})
	return nil
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

func instanceLockName(instance string) string {
	sum := sha256.Sum256([]byte(instance))
	return "herrscher-instance-" + hex.EncodeToString(sum[:])[:24] + ".lock"
}

func removeRuntimeEntries(instance string) error {
	lock := instanceLockName(instance)
	var failures []error
	for _, dir := range runtimeDirs() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if name := entry.Name(); strings.Contains(name, instance) || name == lock {
				if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
					failures = append(failures, err)
				}
			}
		}
	}
	return errors.Join(failures...)
}

func commandSocket(instance string) (string, error) {
	name := "herrscher-command-" + instance + ".sock"
	for _, dir := range runtimeDirs() {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return filepath.Join(dir, name), nil
		}
	}
	return "", errNoSocket
}

var errNoSocket = errors.New("no command socket yet")

const daemonWait = 30 * time.Second

func waitDaemon(ts *testscript.TestScript, neg bool, args []string) {
	if len(args) != 0 {
		ts.Fatalf("usage: [!] waitdaemon")
	}
	instance := ts.Getenv(instanceEnv)
	deadline := time.Now().Add(daemonWait)
	for time.Now().Before(deadline) {
		if answering(instance) != neg {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	if neg {
		ts.Fatalf("the daemon of %s still answers after %s", instance, daemonWait)
	}
	ts.Fatalf("no daemon of %s answered within %s", instance, daemonWait)
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

func reapInstance(instance string) []string {
	marker := []byte(instanceEnv + "=" + instance + "\x00")
	deadline := time.Now().Add(reapGrace)
	for {
		pids := processesWith(marker)
		if len(pids) == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			leaked := make([]string, 0, len(pids))
			for _, pid := range pids {
				leaked = append(leaked, describe(pid))
				if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
					leaked = append(leaked, "cannot kill "+strconv.Itoa(pid)+": "+err.Error())
				}
			}
			return leaked
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func processesWith(marker []byte) []int {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return nil
	}
	self := os.Getpid()
	var pids []int
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == self {
			continue
		}
		environ, err := os.ReadFile(filepath.Join(procRoot, entry.Name(), "environ"))
		if err != nil || !bytes.Contains(append(environ, 0), marker) {
			continue
		}
		pids = append(pids, pid)
	}
	return pids
}

func describe(pid int) string {
	cmdline, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return strconv.Itoa(pid)
	}
	return strconv.Itoa(pid) + " " + strings.Join(strings.Split(strings.TrimRight(string(cmdline), "\x00"), "\x00"), " ")
}
