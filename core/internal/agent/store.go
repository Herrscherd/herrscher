package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const defaultUserBudget = 1500

// Store owns the directory holding every agent home: <root>/<name>/.
type Store struct {
	root       string
	userBudget int
}

// NewStore returns a Store rooted at root (created lazily on first Create).
func NewStore(root string) *Store { return &Store{root: root, userBudget: defaultUserBudget} }

// SetUserBudget overrides the per-agent USER.md rune budget. A value <= 0
// disables the check (see contracts.EnforceBudget).
func (s *Store) SetUserBudget(runes int) { s.userBudget = runes }

// UserBudgetFromEnv parses AGENT_USER_BUDGET via get (os.Getenv in production).
// It returns (0, false) when the variable is unset or not an integer, so the
// caller keeps the default budget; a valid value (including 0, which disables
// the check) returns (n, true).
func UserBudgetFromEnv(get func(string) string) (int, bool) {
	value := get("AGENT_USER_BUDGET")
	if value == "" {
		return 0, false
	}
	count, err := strconv.Atoi(value)
	if err != nil {
		return 0, false
	}
	return count, true
}

// Root returns the directory under which agent homes live.
func (s *Store) Root() string { return s.root }

// CreateSpec declares a new agent. Soul is the persona text (SOUL.md); MCP is an
// optional stdio MCP server command line ("neublox serve --project {{WORKTREE}}")
// whose first token names the server and is its command.
type CreateSpec struct {
	Name    string
	Soul    string
	User    string
	MCP     string
	Backend string
	Cmd     string
	Host    string
	Tags    []string
}

const defaultSoul = `# Companion

You are a durable companion agent. You keep working on this project across
sessions, remember what matters, and act carefully inside your worktree.
`

type mcpServer struct {
	Type    string   `json:"type"`
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}

type mcpConfig struct {
	MCPServers map[string]mcpServer `json:"mcpServers"`
}

func parseMCP(cmdline string) (string, mcpServer, bool) {
	fields := strings.Fields(cmdline)
	if len(fields) == 0 {
		return "", mcpServer{}, false
	}
	return fields[0], mcpServer{Type: "stdio", Command: fields[0], Args: fields[1:]}, true
}

func buildSettings(serverName string) ([]byte, error) {
	var allow []string
	if serverName != "" {
		allow = append(allow, "mcp__"+serverName+"__*")
	}
	allow = append(allow, "Bash", "Edit", "Write")
	type perms struct {
		DefaultMode string   `json:"defaultMode"`
		Allow       []string `json:"allow"`
	}
	type cfg struct {
		EnableAllProjectMCPServers bool  `json:"enableAllProjectMcpServers"`
		Permissions                perms `json:"permissions"`
	}
	return json.MarshalIndent(cfg{
		EnableAllProjectMCPServers: true,
		Permissions:                perms{DefaultMode: "acceptEdits", Allow: allow},
	}, "", "  ")
}

func validateName(name string) bool {
	return name != "" && name != "." && !strings.ContainsAny(name, `/\`) && !strings.Contains(name, "..")
}

func readTags(home string) []string {
	buf, err := os.ReadFile(filepath.Join(home, tagsFile))
	if err != nil {
		return nil
	}
	seen := make(map[string]bool)
	var out []string
	for _, tok := range strings.Fields(strings.ReplaceAll(string(buf), ",", " ")) {
		lowered := strings.ToLower(tok)
		if !seen[lowered] {
			seen[lowered] = true
			out = append(out, lowered)
		}
	}
	return out
}

func readBackend(home string) string {
	buf, err := os.ReadFile(filepath.Join(home, backendFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(buf))
}

func readCmd(home string) string {
	buf, err := os.ReadFile(filepath.Join(home, cmdFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(buf))
}

func readHost(home string) string {
	buf, err := os.ReadFile(filepath.Join(home, hostFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(buf))
}

// Create writes a new agent home and seeds its source files. It errors if
// the name is unsafe or the agent already exists.
func (s *Store) Create(spec CreateSpec) (Agent, error) {
	name := spec.Name
	if !validateName(name) {
		return Agent{}, &InvalidNameError{Name: name}
	}
	home := filepath.Join(s.root, name)
	if err := os.MkdirAll(s.root, dirMode); err != nil {
		return Agent{}, fmt.Errorf("create agents root: %w", err)
	}
	if err := os.Mkdir(home, dirMode); err != nil {
		if os.IsExist(err) {
			return Agent{}, &ExistsError{Name: name}
		}
		return Agent{}, fmt.Errorf("create agent home: %w", err)
	}
	created := false
	defer func() {
		if !created {
			if err := os.RemoveAll(home); err != nil {
				fmt.Fprintf(os.Stderr, "herrscher: remove agent home %q failed: %v\n", home, err)
			}
		}
	}()

	files, err := seedFiles(spec)
	if err != nil {
		return Agent{}, err
	}
	for _, file := range files {
		if err := os.WriteFile(filepath.Join(home, file.name), file.data, fileMode); err != nil {
			return Agent{}, fmt.Errorf("write %s: %w", file.name, err)
		}
	}
	created = true
	return Agent{Name: name, Home: home, Backend: spec.Backend, Cmd: spec.Cmd, Host: spec.Host, Tags: readTags(home)}, nil
}

type seedFile struct {
	name string
	data []byte
}

func seedFiles(spec CreateSpec) ([]seedFile, error) {
	soul := spec.Soul
	if soul == "" {
		soul = defaultSoul
	}
	serverName := ""
	servers := make(map[string]mcpServer)
	if srvName, srv, ok := parseMCP(spec.MCP); ok {
		serverName = srvName
		servers[srvName] = srv
	}
	mcpBuf, err := json.MarshalIndent(mcpConfig{MCPServers: servers}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("render mcp.json: %w", err)
	}
	settingsBuf, err := buildSettings(serverName)
	if err != nil {
		return nil, fmt.Errorf("render settings.json: %w", err)
	}
	tags := ""
	if len(spec.Tags) > 0 {
		tags = strings.Join(spec.Tags, " ") + "\n"
	}
	files := []seedFile{{soulFile, []byte(soul)}, {mcpFile, mcpBuf}, {settingsFile, settingsBuf}}
	files = append(files, optionalSeed(userFile, spec.User)...)
	files = append(files, optionalSeed(backendFile, spec.Backend)...)
	files = append(files, optionalSeed(cmdFile, spec.Cmd)...)
	files = append(files, optionalSeed(hostFile, spec.Host)...)
	files = append(files, optionalSeed(tagsFile, tags)...)
	return files, nil
}

func optionalSeed(name, value string) []seedFile {
	if value == "" {
		return nil
	}
	return []seedFile{{name, []byte(value)}}
}

// SetSoul overwrites <home>/SOUL.md for an existing agent. It never creates a
// home. An absent agent is an error (callers seed via Create). Homes are the
// source of truth; an edited soul applies to the next session materialized.
func (s *Store) SetSoul(name, soul string) error {
	if !validateName(name) {
		return &InvalidNameError{Name: name}
	}
	home := filepath.Join(s.root, name)
	if info, err := os.Stat(home); err != nil || !info.IsDir() {
		return &NotFoundError{Name: name}
	}
	return os.WriteFile(filepath.Join(home, soulFile), []byte(soul), fileMode)
}

// SetUser overwrites <home>/USER.md for an existing agent, rejecting content
// over the store's user budget (contracts.EnforceBudget) before touching disk.
// It never creates a home. An absent agent is an error.
func (s *Store) SetUser(name, text string) error {
	if !validateName(name) {
		return &InvalidNameError{Name: name}
	}
	home := filepath.Join(s.root, name)
	if info, err := os.Stat(home); err != nil || !info.IsDir() {
		return &NotFoundError{Name: name}
	}
	if err := contracts.EnforceBudget("user:"+name, text, s.userBudget); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(home, userFile), []byte(text), fileMode)
}

// Get returns the agent named name, or false if no such home directory exists.
func (s *Store) Get(name string) (Agent, bool) {
	if !validateName(name) {
		return Agent{}, false
	}
	home := filepath.Join(s.root, name)
	info, err := os.Stat(home)
	if err != nil || !info.IsDir() {
		return Agent{}, false
	}
	return Agent{Name: name, Home: home, Tags: readTags(home), Backend: readBackend(home), Cmd: readCmd(home), Host: readHost(home)}, true
}

// List returns every agent home under the store root, sorted by name. A missing
// root yields an empty list (no agents created yet).
func (s *Store) List() ([]Agent, error) {
	entries, err := os.ReadDir(s.root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Agent
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		home := filepath.Join(s.root, entry.Name())
		out = append(out, Agent{Name: entry.Name(), Home: home, Tags: readTags(home), Backend: readBackend(home), Cmd: readCmd(home), Host: readHost(home)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
