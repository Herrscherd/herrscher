package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Store owns the directory holding every agent home: <root>/<name>/.
type Store struct{ root string }

// NewStore returns a Store rooted at root (created lazily on first Create).
func NewStore(root string) *Store { return &Store{root: root} }

// Root returns the directory under which agent homes live.
func (s *Store) Root() string { return s.root }

// CreateSpec declares a new agent. Soul is the persona text (SOUL.md); MCP is an
// optional stdio MCP server command line ("neublox serve --project {{WORKTREE}}")
// whose first token names the server and is its command.
type CreateSpec struct {
	Name string
	Soul string
	MCP  string
}

// defaultSoul is the persona seeded when CreateSpec.Soul is empty. It is a
// neutral companion persona; profiles pass their own via CreateSpec.Soul.
const defaultSoul = `# Companion

You are a durable companion agent. You keep working on this project across
sessions, remember what matters, and act carefully inside your worktree.
`

// mcpServer / mcpConfig model a Claude Code .mcp.json stdio server entry.
type mcpServer struct {
	Type    string   `json:"type"`
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}

type mcpConfig struct {
	MCPServers map[string]mcpServer `json:"mcpServers"`
}

// parseMCP splits a command line ("neublox serve --project {{WORKTREE}}") into a
// stdio server entry. The first token is both the server name and the command.
// Whitespace-split only (no quoting) — sufficient for tokenless CLI args.
func parseMCP(cmdline string) (name string, srv mcpServer, ok bool) {
	fields := strings.Fields(cmdline)
	if len(fields) == 0 {
		return "", mcpServer{}, false
	}
	return fields[0], mcpServer{Type: "stdio", Command: fields[0], Args: fields[1:]}, true
}

// buildSettings renders the zero-prompt Claude settings: project MCP servers
// auto-enabled, file edits auto-accepted (a headless backend can answer no
// prompt), and (when present) the agent's MCP namespace allow-listed. The
// worktree is disposable and isolated, so a permissive mode is safe.
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

// validateName rejects agent names that are empty, ".", or could traverse out of
// the store root (contain a path separator or "..").
func validateName(name string) bool {
	return name != "" && name != "." && !strings.ContainsAny(name, `/\`) && !strings.Contains(name, "..")
}

// Create writes a new agent home and seeds its three source files. It errors if
// the name is unsafe or the agent already exists.
func (s *Store) Create(spec CreateSpec) (Agent, error) {
	name := spec.Name
	if !validateName(name) {
		return Agent{}, fmt.Errorf("invalid agent name %q", name)
	}
	home := filepath.Join(s.root, name)
	if err := os.MkdirAll(s.root, 0o755); err != nil {
		return Agent{}, fmt.Errorf("create agents root: %w", err)
	}
	// os.Mkdir is the atomic claim: it fails with EEXIST if the home already
	// exists, avoiding a Stat/MkdirAll TOCTOU between concurrent creates.
	if err := os.Mkdir(home, 0o755); err != nil {
		if os.IsExist(err) {
			return Agent{}, fmt.Errorf("agent %q already exists", name)
		}
		return Agent{}, fmt.Errorf("create agent home: %w", err)
	}
	created := false
	defer func() {
		if !created {
			_ = os.RemoveAll(home)
		}
	}()

	soul := spec.Soul
	if soul == "" {
		soul = defaultSoul
	}

	serverName := ""
	servers := map[string]mcpServer{}
	if srvName, srv, ok := parseMCP(spec.MCP); ok {
		serverName = srvName
		servers[srvName] = srv
	}
	mcpBuf, err := json.MarshalIndent(mcpConfig{MCPServers: servers}, "", "  ")
	if err != nil {
		return Agent{}, fmt.Errorf("render mcp.json: %w", err)
	}
	settingsBuf, err := buildSettings(serverName)
	if err != nil {
		return Agent{}, fmt.Errorf("render settings.json: %w", err)
	}

	files := []struct {
		name string
		data []byte
	}{
		{soulFile, []byte(soul)},
		{mcpFile, mcpBuf},
		{settingsFile, settingsBuf},
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(home, f.name), f.data, 0o644); err != nil {
			return Agent{}, fmt.Errorf("write %s: %w", f.name, err)
		}
	}
	created = true
	return Agent{Name: name, Home: home}, nil
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
	return Agent{Name: name, Home: home}, true
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
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		out = append(out, Agent{Name: e.Name(), Home: filepath.Join(s.root, e.Name())})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
