//go:build e2e && linux

package e2e_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"

	"github.com/Herrscherd/herrscher/core/host"
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
			"waitdaemon":     waitDaemon,
			"waitlog":        waitLog,
			"waitapproval":   waitApproval,
			"descendantgone": descendantGone,
		},
	})
}

func buildHerrscher(tb testing.TB) string {
	tb.Helper()
	binDir := tb.TempDir()
	build := exec.CommandContext(tb.Context(), "go", "build", "-tags", "e2e", "-o", filepath.Join(binDir, "herrscher"), ".")
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
	root, err := scenarioRuntimeRoot(env)
	if err != nil {
		return err
	}
	home := filepath.Join(env.WorkDir, "home")
	repo := filepath.Join(env.WorkDir, "repo")
	for _, dir := range []string{home, repo} {
		if mkErr := os.MkdirAll(dir, 0o700); mkErr != nil {
			return mkErr
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
		runtimeRootEnv + "=" + root,
	}, files.Environ()...)
	proxy, listener, err := startUsageProxy(env.WorkDir)
	if err != nil {
		return fmt.Errorf("start the usage proxy: %w", err)
	}
	stopUsageProxy(env, proxy, listener)
	vars = append(vars, usageProxyEnv(proxy)...)
	for _, pair := range vars {
		name, value, _ := strings.Cut(pair, "=")
		env.Setenv(name, value)
	}
	if out, err := exec.CommandContext(context.Background(), "git", "init", "-q", repo).CombinedOutput(); err != nil {
		return fmt.Errorf("git init: %w: %s", err, out)
	}
	env.Cd = repo
	before, snapshotErr := hashedEntries(root)
	if snapshotErr != nil {
		return snapshotErr
	}
	env.Defer(func() {
		if err := settle(root, instance, before); err != nil {
			env.T().Fatal(err)
		}
	})
	return nil
}

func scenarioRuntimeRoot(env *testscript.Env) (string, error) {
	root := filepath.Join(env.WorkDir, "run")
	if _, err := host.RuntimeAt(herrscherDir(root)); err != nil {
		short, mkErr := os.MkdirTemp("", "hsr")
		if mkErr != nil {
			return "", mkErr
		}
		env.Defer(func() {
			if rmErr := os.RemoveAll(short); rmErr != nil {
				env.T().Fatal(rmErr)
			}
		})
		root = short
	}
	if err := os.MkdirAll(userDirOf(root), 0o700); err != nil {
		return "", err
	}
	return root, nil
}

func userDirOf(root string) string {
	return filepath.Join(root, strconv.Itoa(os.Getuid()))
}

func herrscherDir(root string) string {
	return filepath.Join(userDirOf(root), "herrscher")
}
