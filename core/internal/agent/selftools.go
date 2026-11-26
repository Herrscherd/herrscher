package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

const (
	stdioTransport = "stdio"
	mcpCommandKey  = "command"
)

// SelfToolsServer is the name of the MCP server that gives a session its own daemon's verbs.
const SelfToolsServer = "herrscher"

// ErrTrackedConfig reports a session configuration file that the repository tracks, which the self tools leave untouched.
var ErrTrackedConfig = errors.New("the repository tracks this file, so herrscher leaves it as it is")

func selfToolsEnv() []string {
	return []string{
		"HERRSCHER_COMMAND_SOCKET",
		"HERRSCHER_SESSION",
		"HERRSCHER_SESSION_TOKEN",
		"HERRSCHER_ENV_FILE",
		"HERRSCHER_INSTANCE_ID",
		"HERRSCHER_STATE_DIR",
		"XDG_RUNTIME_DIR",
		"XDG_CONFIG_HOME",
		"LANG",
		"HOME",
		"PATH",
	}
}

// MaterializeSelfTools declares the herrscher MCP server in a session worktree,
// for Claude Code (.mcp.json, enabled in .claude/settings.local.json) and Codex
// (.codex/config.toml). A file the repository tracks is left alone, and the
// returned error names it with ErrTrackedConfig.
func MaterializeSelfTools(ctx context.Context, worktree, herrscherBin string) error {
	if herrscherBin == "" {
		return nil
	}
	if err := EnsureGitExcludes(ctx, worktree); err != nil {
		return err
	}
	var failures []error
	writers := []struct {
		rel   string
		write func(string, string) error
	}{
		{".mcp.json", writeClaudeSelfTools},
		{filepath.Join(".claude", localSettingsFile), enableClaudeSelfTools},
		{filepath.Join(".codex", "config.toml"), writeCodexSelfTools},
	}
	for _, target := range writers {
		if tracked(ctx, worktree, target.rel) {
			failures = append(failures, fmt.Errorf("%s: %w", target.rel, ErrTrackedConfig))
			continue
		}
		if err := target.write(filepath.Join(worktree, target.rel), herrscherBin); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func tracked(ctx context.Context, worktree, rel string) bool {
	err := exec.CommandContext(ctx, "git", "-C", worktree, "ls-files", "--error-unmatch", "--", rel).Run()
	return err == nil
}

func readOrEmpty(path, empty string) ([]byte, error) {
	buf, err := os.ReadFile(path)
	switch {
	case err == nil && strings.TrimSpace(string(buf)) != "":
		return buf, nil
	case err == nil || os.IsNotExist(err):
		return []byte(empty), nil
	}
	return nil, fmt.Errorf("read %s: %w", path, err)
}

func writeJSONDoc(path string, doc map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	if err := os.WriteFile(path, append(out, '\n'), fileMode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func readJSONDoc(path string) (map[string]any, error) {
	buf, err := readOrEmpty(path, "{}")
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := json.Unmarshal(buf, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if doc == nil {
		doc = make(map[string]any)
	}
	return doc, nil
}

func objectAt(doc map[string]any, key, path string) (map[string]any, error) {
	raw, ok := doc[key]
	if !ok || raw == nil {
		return make(map[string]any), nil
	}
	object, isObject := raw.(map[string]any)
	if !isObject {
		return nil, &SettingsShapeError{Path: path, Want: "an object", Got: raw}
	}
	return object, nil
}

func writeClaudeSelfTools(path, herrscherBin string) error {
	doc, err := readJSONDoc(path)
	if err != nil {
		return err
	}
	servers, err := objectAt(doc, "mcpServers", "mcpServers")
	if err != nil {
		return err
	}
	servers[SelfToolsServer] = map[string]any{
		"type":        stdioTransport,
		mcpCommandKey: herrscherBin,
		"args":        []any{"mcp"},
	}
	doc["mcpServers"] = servers
	return writeJSONDoc(path, doc)
}

func enableClaudeSelfTools(path, _ string) error {
	doc, err := readJSONDoc(path)
	if err != nil {
		return err
	}
	var enabled []any
	if raw, ok := doc["enabledMcpjsonServers"]; ok && raw != nil {
		list, isList := raw.([]any)
		if !isList {
			return &SettingsShapeError{Path: "enabledMcpjsonServers", Want: "a list", Got: raw}
		}
		enabled = list
	}
	if !slices.Contains(enabled, any(SelfToolsServer)) {
		enabled = append(enabled, SelfToolsServer)
	}
	doc["enabledMcpjsonServers"] = enabled
	return writeJSONDoc(path, doc)
}

func writeCodexSelfTools(path, herrscherBin string) error {
	buf, err := readOrEmpty(path, "")
	if err != nil {
		return err
	}
	doc := make(map[string]any)
	if _, decodeErr := toml.Decode(string(buf), &doc); decodeErr != nil {
		return fmt.Errorf("parse %s: %w", path, decodeErr)
	}
	servers, err := objectAt(doc, "mcp_servers", "mcp_servers")
	if err != nil {
		return err
	}
	servers[SelfToolsServer] = map[string]any{
		mcpCommandKey: herrscherBin,
		"args":        []string{"mcp"},
		"env_vars":    selfToolsEnv(),
	}
	doc["mcp_servers"] = servers
	var out bytes.Buffer
	if err := toml.NewEncoder(&out).Encode(doc); err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, out.Bytes(), fileMode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
