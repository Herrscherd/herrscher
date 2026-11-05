// Package config loads the daemon's declarative defaults from a user-authored
// ~/.config/herrscher/config.json. The daemon only ever READS this file (it never
// rewrites it), so it is safe to comment and hand-edit, unlike state.json,
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
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/envx"
)

// HomeRef mirrors state.HomeRef so config.json can declare the session home
// without this low-level package importing state.
type HomeRef struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// Config is the parsed config.json. Every field is optional; an absent field
// falls through to the next source in the precedence chain (env / built-in
// default for knobs, live state.json for the declarative runtime fields).
type Config struct {
	Language string `json:"language,omitempty"`
	// Daemon knobs (precedence: explicit flag > env > this > built-in default).
	Cmd           string         `json:"cmd"`
	HealthAddr    string         `json:"healthAddr"`
	StatusChannel string         `json:"statusChannel"`
	Instance      string         `json:"instance"`
	Owner         string         `json:"owner"`
	CompactAt     *int           `json:"compactAt,omitempty"`
	CompactStack  []string       `json:"compactStack,omitempty"`
	ContextLimits map[string]int `json:"contextLimits,omitempty"`
	// Stale threshold for `session clean`: sessions inactive longer than this
	// many days are reported as stale. 0 disables stale detection. Unset (zero)
	// means the built-in default (14) is applied by the caller.
	SessionMaxIdleDays int `json:"sessionMaxIdleDays,omitempty"`

	// Declarative runtime defaults (precedence: live state.json > this > empty).
	Home      *HomeRef `json:"home"`
	Workspace string   `json:"workspace"`
	Source    string   `json:"source"`

	Skills *SkillsConfig `json:"skills,omitempty"`

	// Keys configures the local API key proxy. Absent means no proxy.
	Keys *KeysConfig `json:"keys,omitempty"`
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
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".config", "herrscher", "config.json")
	}
	return filepath.Join(home, ".config", "herrscher", "config.json")
}

// Load reads and parses config.json. A missing file yields a zero Config and no
// error (config.json is entirely optional). Full-line // comments are stripped
// before parsing so the file can be commented.
func Load(path string) (Config, error) {
	var cfg Config
	buf, err := os.ReadFile(filepath.Clean(path))
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(stripComments(buf), &cfg); err != nil {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}
	if cfg.CompactAt != nil {
		if err := contracts.ValidateAutoCompactPercent(*cfg.CompactAt); err != nil {
			return cfg, fmt.Errorf("parse %s: compactAt: %w", path, err)
		}
	}
	if err := ValidateCompactStack(cfg.CompactStack); err != nil {
		return cfg, fmt.Errorf("parse %s: compactStack: %w", path, err)
	}
	if err := ValidateContextLimits(cfg.ContextLimits); err != nil {
		return cfg, fmt.Errorf("parse %s: contextLimits: %w", path, err)
	}
	if err := ValidateKeys(cfg.Keys); err != nil {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, nil
}

var (
	errEmptyStage      = errors.New("a stage name is empty")
	errDuplicateStage  = errors.New("a stage name is listed twice")
	errEmptyVendor     = errors.New("a vendor name is empty")
	errNotPositive     = errors.New("a window size must be a positive number of tokens")
	errDuplicateVendor = errors.New("a vendor is listed twice under spellings that differ only in case")
)

// ValidateContextLimits refuses a vendor with an empty name, a window size that is
// not a positive number of tokens, and a vendor listed twice in two spellings of
// its name. The vendors are checked in sorted order, so the first refusal is stable.
func ValidateContextLimits(limits map[string]int) error {
	seen := make(map[string]bool, len(limits))
	for _, vendor := range slices.Sorted(maps.Keys(limits)) {
		if strings.TrimSpace(vendor) == "" {
			return errEmptyVendor
		}
		if limits[vendor] <= 0 {
			return fmt.Errorf("%q: %w", vendor, errNotPositive)
		}
		key := strings.ToLower(vendor)
		if seen[key] {
			return fmt.Errorf("%q: %w", vendor, errDuplicateVendor)
		}
		seen[key] = true
	}
	return nil
}

// ValidateCompactStack refuses an empty stage name and a name listed twice, the
// stack running each stage once in the order given.
func ValidateCompactStack(stack []string) error {
	seen := make(map[string]bool, len(stack))
	for _, name := range stack {
		if strings.TrimSpace(name) == "" {
			return errEmptyStage
		}
		if seen[name] {
			return fmt.Errorf("%q: %w", name, errDuplicateStage)
		}
		seen[name] = true
	}
	return nil
}

func stripComments(data []byte) []byte {
	var out bytes.Buffer
	for _, line := range bytes.Split(data, []byte("\n")) {
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
	cmdJSON := quoteJSON(cmd)
	healthJSON := quoteJSON(healthAddr)
	return `// herrscher config: declarative defaults the daemon reads at startup.
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
  "cmd": ` + cmdJSON + `,

  // Health endpoint address; "" disables it.
  "healthAddr": ` + healthJSON + `,

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

  // Context window size in tokens, per vendor, for the compaction threshold and
  // the TUI gauge. HERRSCHER_CONTEXT_LIMIT overrides every entry. Omit to use the
  // built-in sizes.
  // "contextLimits": { "codex": 400000 },

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

func quoteJSON(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return strconv.Quote(value)
	}
	return string(encoded)
}
