package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	dirMode         = 0o700
	credentialsMode = 0o600
	credentialsFile = ".credentials.json"
	fileMode        = 0o600
	vaultDir        = "vault"
	vaultEnv        = "OBSIDIAN_VAULT"
	agedNodeFile    = "aged.md"
	agedNodeText    = "---\ntype: fact\ntitle: old fact\nlastSeen: 2001-01-01T00:00:00Z\n---\nThe old fact.\n"
	repoDir         = "membench-repo"
	memoryAgentEnv  = "TERMINAL_MEMORY_AGENT"
)

type sandbox struct {
	root   string
	state  string
	home   string
	repo   string
	config string
}

func newSandbox(ctx context.Context, parent string) (sandbox, error) {
	root, err := os.MkdirTemp(parent, "run-")
	if err != nil {
		return sandbox{}, sandboxError(err)
	}
	created := sandbox{
		root:   root,
		state:  filepath.Join(root, "state"),
		home:   filepath.Join(root, "home"),
		config: filepath.Join(root, "claude"),
	}
	created.repo = filepath.Join(created.home, repoDir)
	for _, dir := range []string{created.state, created.repo, created.config} {
		if err := os.MkdirAll(dir, dirMode); err != nil {
			return sandbox{}, errors.Join(sandboxError(err), created.close())
		}
	}
	if err := created.initRepo(ctx); err != nil {
		return sandbox{}, errors.Join(err, created.close())
	}
	return created, nil
}

func (s sandbox) initRepo(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "git", "init", "--quiet")
	cmd.Dir = s.repo
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + s.home, "GIT_CONFIG_NOSYSTEM=1"}
	if err := cmd.Run(); err != nil {
		return sandboxError(err)
	}
	return nil
}

func (s sandbox) close() error {
	if err := os.RemoveAll(s.root); err != nil {
		return sandboxError(err)
	}
	return nil
}

func (s sandbox) vault() string {
	return filepath.Join(s.root, vaultDir)
}

func (s sandbox) plantAgedNode() error {
	if err := os.MkdirAll(s.vault(), dirMode); err != nil {
		return vaultError(err)
	}
	if err := os.WriteFile(filepath.Join(s.vault(), agedNodeFile), []byte(agedNodeText), fileMode); err != nil {
		return vaultError(err)
	}
	return nil
}

func (s sandbox) installCredentials(source string) error {
	if source == "" {
		return nil
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return pathError(msgErrAuthFileRead, source, err)
	}
	if err := os.WriteFile(filepath.Join(s.config, credentialsFile), data, credentialsMode); err != nil {
		return sandboxError(err)
	}
	return nil
}

func (s sandbox) environ(model, owner string, assignments []string) []string {
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + s.home,
		"XDG_CONFIG_HOME=" + filepath.Join(s.home, ".config"),
		"XDG_DATA_HOME=" + filepath.Join(s.home, ".local", "share"),
		"XDG_STATE_HOME=" + filepath.Join(s.home, ".local", "state"),
		"XDG_CACHE_HOME=" + filepath.Join(s.home, ".cache"),
		"CLAUDE_CONFIG_DIR=" + s.config,
		"HERRSCHER_STATE_DIR=" + s.state,
		"HERRSCHER_INSTANCE_ID=" + filepath.Base(s.root),
		"HERRSCHER_OWNER_ID=" + owner,
		memoryAgentEnv + "=" + owner,
		"HERRSCHER_DEFAULT_MODEL=" + model,
		"HERRSCHER_MODELS_DISCOVER=none",
		"CLAUDE_CODE_DISABLE_AUTO_MEMORY=1",
		"ENABLE_CLAUDEAI_MCP_SERVERS=false",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1",
	}
	return append(env, assignments...)
}
