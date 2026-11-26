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
	"strconv"
	"strings"
	"time"

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
		"HERRSCHER_SELF_TOOLS_EXPOSE",
		"HOME",
		"PATH",
	}
}

// SelfToolsCallWait is how long one self tool call may last: long enough for the operator to answer the question a refused verb becomes.
const SelfToolsCallWait = HookWait + time.Minute

var errNotOurServer = errors.New("the herrscher MCP server declared here is not the one herrscher wrote, so it is not enabled")

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
	return errors.Join(materializeClaudeSelfTools(ctx, worktree, herrscherBin), materializeCodexSelfTools(ctx, worktree, herrscherBin))
}

func materializeClaudeSelfTools(ctx context.Context, worktree, herrscherBin string) error {
	declaration, settings := ".mcp.json", filepath.Join(".claude", localSettingsFile)
	for _, rel := range []string{declaration, settings} {
		if tracked(ctx, worktree, rel) {
			return fmt.Errorf("%s: %w", rel, ErrTrackedConfig)
		}
	}
	path := filepath.Join(worktree, declaration)
	if err := writeClaudeSelfTools(path, herrscherBin); err != nil {
		return err
	}
	ours, err := declaresOurServer(path, herrscherBin)
	if err != nil {
		return err
	}
	if !ours {
		return fmt.Errorf("%s: %w", declaration, errNotOurServer)
	}
	return enableClaudeSelfTools(filepath.Join(worktree, settings))
}

func materializeCodexSelfTools(ctx context.Context, worktree, herrscherBin string) error {
	rel := filepath.Join(".codex", "config.toml")
	if tracked(ctx, worktree, rel) {
		return fmt.Errorf("%s: %w", rel, ErrTrackedConfig)
	}
	return writeCodexSelfTools(filepath.Join(worktree, rel), herrscherBin)
}

func claudeServerEntry(herrscherBin string) map[string]any {
	return map[string]any{
		"type":        stdioTransport,
		mcpCommandKey: herrscherBin,
		"args":        []any{"mcp"},
	}
}

func declaresOurServer(path, herrscherBin string) (bool, error) {
	doc, err := readJSONDoc(path)
	if err != nil {
		return false, err
	}
	servers, err := objectAt(doc, "mcpServers", "mcpServers")
	if err != nil {
		return false, err
	}
	entry, ok := servers[SelfToolsServer].(map[string]any)
	if !ok {
		return false, nil
	}
	want := claudeServerEntry(herrscherBin)
	args, argsOK := entry["args"].([]any)
	return entry[mcpCommandKey] == want[mcpCommandKey] && entry["type"] == want["type"] && argsOK && len(args) == 1 && args[0] == "mcp" && len(entry) == len(want), nil
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
	servers[SelfToolsServer] = claudeServerEntry(herrscherBin)
	doc["mcpServers"] = servers
	return writeJSONDoc(path, doc)
}

func enableClaudeSelfTools(path string) error {
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

func codexServerTable(herrscherBin string) string {
	quoted := make([]string, 0, len(selfToolsEnv()))
	for _, name := range selfToolsEnv() {
		quoted = append(quoted, tomlString(name))
	}
	return "[mcp_servers." + SelfToolsServer + "]\n" +
		"command = " + tomlString(herrscherBin) + "\n" +
		"args = [\"mcp\"]\n" +
		"env_vars = [" + strings.Join(quoted, ", ") + "]\n" +
		"tool_timeout_sec = " + strconv.Itoa(int(SelfToolsCallWait/time.Second)) + "\n"
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
	table := codexServerTable(herrscherBin)
	var out []byte
	if _, declared := servers[SelfToolsServer]; declared {
		out, err = replaceCodexServer(doc, servers, table)
	} else {
		out = appendCodexServer(buf, table)
	}
	if err != nil {
		return err
	}
	reread := make(map[string]any)
	if _, decodeErr := toml.Decode(string(out), &reread); decodeErr != nil {
		return fmt.Errorf("encode %s: %w", path, decodeErr)
	}
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, out, fileMode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func appendCodexServer(existing []byte, table string) []byte {
	out := bytes.TrimRight(existing, "\n")
	if len(out) > 0 {
		out = append(out, '\n', '\n')
	}
	return append(out, table...)
}

func replaceCodexServer(doc, servers map[string]any, table string) ([]byte, error) {
	delete(servers, SelfToolsServer)
	if len(servers) == 0 {
		delete(doc, "mcp_servers")
	}
	var out bytes.Buffer
	if err := toml.NewEncoder(&out).Encode(doc); err != nil {
		return nil, fmt.Errorf("encode the codex configuration: %w", err)
	}
	return appendCodexServer(out.Bytes(), table), nil
}
