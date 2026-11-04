// Package accounts stores the vendor account profiles of the host and reads each one's login state.
package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const (
	loginSubcommand  = "login"
	defaultsFileName = "defaults.json"
	dirMode          = 0o700
	fileMode         = 0o600
)

var (
	// ErrExists reports a profile name already taken for its vendor.
	ErrExists = errors.New("account profile already exists")
	// ErrNotFound reports a profile that has no directory.
	ErrNotFound = errors.New("account profile does not exist")
	// ErrInUse reports a profile a live session still references.
	ErrInUse = errors.New("account profile is used by a live session")
)

// Profile is one stored account profile of a vendor.
type Profile struct {
	Vendor  string
	Name    string
	Dir     string
	Default bool
}

// Store keeps the profile directories and the vendor defaults under Root.
type Store struct {
	Root string
}

// DefaultRoot returns the accounts directory under the user home.
func DefaultRoot(home string) string {
	return filepath.Join(home, ".config", "herrscher", "accounts")
}

// Dir returns the directory of one profile.
func (s Store) Dir(vendor string, name contracts.ProfileName) string {
	return filepath.Join(s.Root, vendor, string(name))
}

// Create makes a private profile directory and refuses an existing one.
func (s Store) Create(vendor string, name contracts.ProfileName) (string, error) {
	if _, err := contracts.ProfileHomeEnv(vendor); err != nil {
		return "", err
	}
	dir := s.Dir(vendor, name)
	if _, err := os.Stat(dir); err == nil {
		return "", fmt.Errorf("%s/%s: %w", vendor, name, ErrExists)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("stat profile dir: %w", err)
	}
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return "", fmt.Errorf("create profile dir: %w", err)
	}
	if err := os.Chmod(dir, dirMode); err != nil {
		return "", fmt.Errorf("restrict profile dir: %w", err)
	}
	return dir, nil
}

// List returns every stored profile, sorted by vendor and name.
func (s Store) List() ([]Profile, error) {
	defaults, err := s.loadDefaults()
	if err != nil {
		return nil, err
	}
	vendors, err := os.ReadDir(s.Root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read accounts root: %w", err)
	}
	var profiles []Profile
	for _, vendor := range vendors {
		if !vendor.IsDir() {
			continue
		}
		vendorProfiles, err := s.listVendor(vendor.Name(), defaults)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, vendorProfiles...)
	}
	slices.SortFunc(profiles, compareProfiles)
	return profiles, nil
}

func (s Store) listVendor(vendor string, defaults map[string]string) ([]Profile, error) {
	entries, err := os.ReadDir(filepath.Join(s.Root, vendor))
	if err != nil {
		return nil, fmt.Errorf("read %s profiles: %w", vendor, err)
	}
	var profiles []Profile
	for _, entry := range entries {
		name, err := contracts.ParseProfileName(entry.Name())
		if err != nil || !entry.IsDir() {
			continue
		}
		profiles = append(profiles, Profile{
			Vendor:  vendor,
			Name:    string(name),
			Dir:     s.Dir(vendor, name),
			Default: defaults[vendor] == string(name),
		})
	}
	return profiles, nil
}

func compareProfiles(left, right Profile) int {
	if left.Vendor != right.Vendor {
		return strings.Compare(left.Vendor, right.Vendor)
	}
	return strings.Compare(left.Name, right.Name)
}

// SetDefault makes a stored profile the vendor default.
func (s Store) SetDefault(vendor string, name contracts.ProfileName) error {
	if _, err := contracts.ProfileHomeEnv(vendor); err != nil {
		return err
	}
	if _, err := os.Stat(s.Dir(vendor, name)); err != nil {
		return fmt.Errorf("%s/%s: %w", vendor, name, ErrNotFound)
	}
	defaults, err := s.loadDefaults()
	if err != nil {
		return err
	}
	defaults[vendor] = string(name)
	return s.saveDefaults(defaults)
}

// Default returns the vendor default profile name, empty when none is set.
func (s Store) Default(vendor string) (string, error) {
	defaults, err := s.loadDefaults()
	if err != nil {
		return "", err
	}
	return defaults[vendor], nil
}

// Remove deletes a profile directory unless a live session uses it.
func (s Store) Remove(vendor string, name contracts.ProfileName, inUse bool) error {
	if inUse {
		return fmt.Errorf("%s/%s: %w", vendor, name, ErrInUse)
	}
	dir := s.Dir(vendor, name)
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("%s/%s: %w", vendor, name, ErrNotFound)
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove profile dir: %w", err)
	}
	defaults, err := s.loadDefaults()
	if err != nil {
		return err
	}
	if defaults[vendor] == string(name) {
		delete(defaults, vendor)
		return s.saveDefaults(defaults)
	}
	return nil
}

// Resolve returns the profile a spawn uses: the session choice, then the vendor default.
func (s Store) Resolve(session, vendor string) (string, string, error) {
	name := session
	if name == "" {
		fallback, err := s.Default(vendor)
		if err != nil {
			return "", "", err
		}
		name = fallback
	}
	if name == "" || name == contracts.NativeProfile {
		return "", "", nil
	}
	if _, err := contracts.ProfileHomeEnv(vendor); err != nil {
		return "", "", err
	}
	profile, err := contracts.ParseProfileName(name)
	if err != nil {
		return "", "", err
	}
	dir := s.Dir(vendor, profile)
	if _, err := os.Stat(dir); err != nil {
		return "", "", fmt.Errorf("%s/%s: %w", vendor, name, ErrNotFound)
	}
	return name, dir, nil
}

func (s Store) loadDefaults() (map[string]string, error) {
	data, err := os.ReadFile(filepath.Join(s.Root, defaultsFileName))
	if errors.Is(err, fs.ErrNotExist) {
		return make(map[string]string), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read account defaults: %w", err)
	}
	defaults := make(map[string]string)
	if err := json.Unmarshal(data, &defaults); err != nil {
		return nil, fmt.Errorf("parse account defaults: %w", err)
	}
	return defaults, nil
}

func (s Store) saveDefaults(defaults map[string]string) error {
	if err := os.MkdirAll(s.Root, dirMode); err != nil {
		return fmt.Errorf("create accounts root: %w", err)
	}
	data, err := json.MarshalIndent(defaults, "", "  ")
	if err != nil {
		return fmt.Errorf("encode account defaults: %w", err)
	}
	target := filepath.Join(s.Root, defaultsFileName)
	staging := target + ".tmp"
	if err := os.WriteFile(staging, data, fileMode); err != nil {
		return fmt.Errorf("write account defaults: %w", err)
	}
	return os.Rename(staging, target)
}

// Runner runs a vendor CLI with extra environment and returns its standard output.
type Runner func(ctx context.Context, env map[string]string, argv []string) ([]byte, error)

// OSRunner runs a command on the real system with the extra environment merged in.
func OSRunner(ctx context.Context, env map[string]string, argv []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = contracts.MergeEnv(os.Environ(), env)
	return cmd.Output()
}

// LoginArgv returns the login command of a vendor CLI.
func LoginArgv(vendor string) ([]string, error) {
	switch vendor {
	case contracts.VendorClaude:
		return []string{"claude", "auth", loginSubcommand}, nil
	case contracts.VendorCodex:
		return []string{"codex", loginSubcommand}, nil
	}
	return nil, fmt.Errorf("vendor %q: %w", vendor, contracts.ErrNoProfileHome)
}

// Status reports whether the vendor CLI is logged in under env.
func Status(ctx context.Context, run Runner, vendor string, env map[string]string) (bool, error) {
	argv, err := statusArgv(vendor)
	if err != nil {
		return false, err
	}
	out, err := run(ctx, env, argv)
	if err != nil {
		return false, fmt.Errorf("%s status: %w", vendor, err)
	}
	return parseLoggedIn(vendor, out)
}

func statusArgv(vendor string) ([]string, error) {
	switch vendor {
	case contracts.VendorClaude:
		return []string{"claude", "auth", "status"}, nil
	case contracts.VendorCodex:
		return []string{"codex", loginSubcommand, "status"}, nil
	}
	return nil, fmt.Errorf("vendor %q: %w", vendor, contracts.ErrNoProfileHome)
}

func parseLoggedIn(vendor string, out []byte) (bool, error) {
	if vendor == contracts.VendorClaude {
		var status struct {
			LoggedIn bool `json:"loggedIn"`
		}
		if err := json.Unmarshal(out, &status); err != nil {
			return false, fmt.Errorf("parse claude auth status: %w", err)
		}
		return status.LoggedIn, nil
	}
	text := strings.TrimSpace(string(out))
	return strings.HasPrefix(text, "Logged in"), nil
}
