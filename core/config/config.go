// Package config loads the daemon's declarative defaults from a user-authored
// ~/.config/herrscher/config.json. The daemon only ever READS this file (it never
// rewrites it), so it is safe to comment and hand-edit — unlike state.json,
// which the daemon rewrites atomically on every /set.
//
// Secrets never live here: gateway tokens/channel ids and any owner
// token material stay in the 0600 env file. config.json carries only non-secret
// knobs and declarative runtime defaults.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Herrscherd/herrscher/core/envx"
)

// HomeRef mirrors state.HomeRef so config.json can declare the session home
// without this low-level package importing state.
type HomeRef struct {
	ID   string `json:"id"`
	Type string `json:"type"` // "category" | "forum"
}

// Config is the parsed config.json. Every field is optional; an absent field
// falls through to the next source in the precedence chain (env / built-in
// default for knobs, live state.json for the declarative runtime fields).
type Config struct {
	// Daemon knobs (precedence: explicit flag > env > this > built-in default).
	Cmd           string `json:"cmd"`           // base bridged command for new sessions
	HealthAddr    string `json:"healthAddr"`    // serve --health-addr ("" disables)
	StatusChannel string `json:"statusChannel"` // self-updating status embed channel
	Instance      string `json:"instance"`      // per-daemon instance slug
	Owner         string `json:"owner"`         // owner id; per-daemon instance-id fallback
	// Stale threshold for `session clean`: sessions inactive longer than this
	// many days are reported as stale. 0 disables stale detection. Unset (zero)
	// means the built-in default (14) is applied by the caller.
	SessionMaxIdleDays int `json:"sessionMaxIdleDays,omitempty"`

	// Declarative runtime defaults (precedence: live state.json > this > empty).
	Home      *HomeRef `json:"home"`      // session home category/forum
	Workspace string   `json:"workspace"` // workspace root holding projects
	Source    string   `json:"source"`    // herrscher source checkout for /service update

	Skills *SkillsConfig `json:"skills,omitempty"`
}

// SkillsConfig configures cross-backend skill injection. A nil pointer, or a nil
// Enabled, means the feature is on. Roots are extra skill roots appended after
// the built-in workspace and global (~/.claude/skills) defaults.
type SkillsConfig struct {
	Enabled *bool    `json:"enabled,omitempty"`
	Roots   []string `json:"roots,omitempty"`
}

// DefaultPath returns where the daemon looks for config.json. It sits beside
// state.json: under $HERRSCHER_STATE_DIR if set, else
// ~/.config/herrscher.
func DefaultPath() string {
	if d := envx.Get("STATE_DIR"); d != "" {
		return filepath.Join(d, "config.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "herrscher", "config.json")
}

// Load reads and parses config.json. A missing file yields a zero Config and no
// error (config.json is entirely optional). Full-line // comments are stripped
// before parsing so the file can be commented.
func Load(path string) (Config, error) {
	var c Config
	buf, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(stripComments(buf), &c); err != nil {
		return c, fmt.Errorf("parse %s: %w", path, err)
	}
	return c, nil
}

// stripComments removes full-line // comments (a line whose first non-space
// characters are //). Only full-line comments are honored — trailing inline
// comments are left intact so a value containing // (e.g. a URL) is never
// corrupted. encoding/json then ignores the resulting blank lines.
func stripComments(b []byte) []byte {
	var out bytes.Buffer
	for _, line := range bytes.Split(b, []byte("\n")) {
		if bytes.HasPrefix(bytes.TrimSpace(line), []byte("//")) {
			continue
		}
		out.Write(line)
		out.WriteByte('\n')
	}
	return out.Bytes()
}

// Template returns a commented config.json scaffold. cmd pre-fills the "cmd"
// value (from `herrscher service install --cmd …`); empty leaves it "" so the daemon
// falls back to the built-in "claude" default. healthAddr pre-fills the
// "healthAddr" value (the install --health-addr default).
func Template(cmd, healthAddr string) string {
	cmdJSON, _ := json.Marshal(cmd)
	healthJSON, _ := json.Marshal(healthAddr)
	return `// herrscher config — declarative defaults the daemon reads at startup.
// Secrets (gateway tokens/channel ids, owner token) are NOT here;
// keep them in the 0600 env file (herrscher.env). The daemon never rewrites this
// file, so your comments and edits are safe.
//
// Precedence: CLI flag > env var > this file > built-in default.
// Only full-line // comments are supported (a trailing // in a value is kept).
{
  // Base bridged command for new sessions (sets model/effort/etc.).
  // A per-session "cmd:" option still overrides this. Example:
  //   "claude --model claude-opus-4-8 --effort low"   (Opus 4.8, low, 200k ctx)
  "cmd": ` + string(cmdJSON) + `,

  // Health endpoint address; "" disables it.
  "healthAddr": ` + string(healthJSON) + `,

  // Self-updating status embed channel id; "" disables it.
  "statusChannel": "",

  // Per-daemon instance slug namespacing shared gateway/git resources;
  // "" = legacy non-namespaced mode.
  "instance": "",

  // Owner id; the per-daemon instance-id fallback (like HERRSCHER_OWNER_ID).
  "owner": "",

  // "session clean" stale threshold in days: sessions with no message for longer
  // are reported as stale (acted on only with all:true + stale). 0 disables.
  // Omit to use the built-in default of 14.
  // "sessionMaxIdleDays": 14,

  // Default session home (category or forum). Uncomment and fill to pin it:
  // "home": { "id": "123456789012345678", "type": "category" },

  // Default workspace root holding projects (absolute path).
  // A live "/set workspace …" overrides this.
  "workspace": "",

  // herrscher source checkout for "/service update" (absolute path).
  // A live "/set source …" overrides this.
  "source": ""
}
`
}
