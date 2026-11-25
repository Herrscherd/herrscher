package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
)

// CurrentFormat is the version of the state file this build writes.
const CurrentFormat = 1

// ErrNoGatewayToBind reports a session with a channel and no gateway binding while no gateway is offered to bind it to.
var ErrNoGatewayToBind = errors.New("a session carries a channel but no gateway binding, and no gateway is built to bind it to")

// NewerFormatError reports a state file written by a newer build than this one.
type NewerFormatError struct {
	Path      string
	Found     int
	Supported int
}

func (e *NewerFormatError) Error() string {
	return fmt.Sprintf("state file %s has format %d, this build reads up to format %d", e.Path, e.Found, e.Supported)
}

// FormatChangeKind names one kind of change a format migration makes.
type FormatChangeKind string

const (
	// ChangeInstanceAssigned reports the instance id written into the state.
	ChangeInstanceAssigned FormatChangeKind = "instance_assigned"
	// ChangeWorkspaceFromRepo reports the old repo field copied into the workspace.
	ChangeWorkspaceFromRepo FormatChangeKind = "workspace_from_repo"
	// ChangeRepoDropped reports the old repo field dropped because a workspace already applies.
	ChangeRepoDropped FormatChangeKind = "repo_dropped"
	// ChangeGatewaysBound reports a session given the gateway binding it was implicitly using.
	ChangeGatewaysBound FormatChangeKind = "gateways_bound"
	// ChangeSessionRelocated reports a session whose worktree or directory moved into the instance layout.
	ChangeSessionRelocated FormatChangeKind = "session_relocated"
)

// FormatChange is one change a format migration made, as announced to the operator.
type FormatChange struct {
	Kind    FormatChangeKind
	Session string
	Value   string
}

// Relocation is where a session's worktree and working directory now live.
type Relocation struct {
	Worktree string
	Dir      string
}

// FormatMigration carries the decisions the platform-blind state cannot take alone.
type FormatMigration struct {
	InstanceID      string
	DefaultGateways []string
	ConfigWorkspace string
	Relocated       map[string]Relocation
}

type legacyFields struct {
	repo string
}

type stateFields State

type stateFile struct {
	*stateFields

	Repo string `json:"repo,omitempty"`
}

func (s *State) decode(buf []byte) error {
	s.Format = 0
	file := stateFile{stateFields: (*stateFields)(s)}
	if err := json.Unmarshal(buf, &file); err != nil {
		return err
	}
	if s.Format > CurrentFormat {
		return &NewerFormatError{Path: s.path, Found: s.Format, Supported: CurrentFormat}
	}
	s.legacy.repo = file.Repo
	return nil
}

func (s *State) encodeLocked() ([]byte, error) {
	return json.MarshalIndent(stateFile{stateFields: (*stateFields)(s), Repo: s.legacy.repo}, "", "  ")
}

// Path returns the file this state is bound to.
func (s *State) Path() string { return s.path }

// Outdated reports whether the state was read from a file of an older format.
func (s *State) Outdated() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Format < CurrentFormat
}

// LegacyRepo returns the repo field an older format stored in place of the workspace.
func (s *State) LegacyRepo() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.legacy.repo
}

// UnboundChannelSessions names the sessions that carry a channel and no gateway binding.
func (s *State) UnboundChannelSessions() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var names []string
	for idx := range s.Sessions {
		if needsGatewayBinding(s.Sessions[idx]) {
			names = append(names, s.Sessions[idx].Name)
		}
	}
	return names
}

func needsGatewayBinding(sess Session) bool {
	return len(sess.Gateways) == 0 && sess.ChannelID != ""
}

// BackupPath is where the migration to CurrentFormat keeps the file it replaced.
func (s *State) BackupPath() string {
	return fmt.Sprintf("%s.before-format-%d.bak", s.path, CurrentFormat)
}

// MigrateFormat brings an outdated state to CurrentFormat, keeps a copy of the old file next to it, writes the new one atomically and lists every change.
func (s *State) MigrateFormat(migration FormatMigration) ([]FormatChange, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Format >= CurrentFormat {
		return nil, nil
	}
	if err := s.checkMigrationLocked(migration); err != nil {
		return nil, err
	}
	if err := s.backupLocked(); err != nil {
		return nil, err
	}
	changes := s.migrateInstanceLocked(migration.InstanceID)
	changes = append(changes, s.migrateWorkspaceLocked(migration.ConfigWorkspace)...)
	changes = append(changes, s.migrateSessionsLocked(migration)...)
	s.Format = CurrentFormat
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return changes, nil
}

func (s *State) checkMigrationLocked(migration FormatMigration) error {
	if s.InstanceID == "" && migration.InstanceID == "" {
		return errNoInstanceID
	}
	if len(migration.DefaultGateways) > 0 {
		return nil
	}
	for idx := range s.Sessions {
		if needsGatewayBinding(s.Sessions[idx]) {
			return fmt.Errorf("%w: %q", ErrNoGatewayToBind, s.Sessions[idx].Name)
		}
	}
	return nil
}

var errNoInstanceID = errors.New("a format migration needs an instance id")

func (s *State) backupLocked() error {
	if _, err := os.Lstat(s.BackupPath()); err == nil {
		return nil
	}
	err := os.Link(s.path, s.BackupPath())
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (s *State) migrateInstanceLocked(instanceID string) []FormatChange {
	if s.InstanceID != "" {
		return nil
	}
	s.InstanceID = instanceID
	return []FormatChange{{Kind: ChangeInstanceAssigned, Value: instanceID}}
}

func (s *State) migrateWorkspaceLocked(configWorkspace string) []FormatChange {
	repo := s.legacy.repo
	if repo == "" {
		return nil
	}
	s.legacy.repo = ""
	if s.Workspace != "" || configWorkspace != "" {
		return []FormatChange{{Kind: ChangeRepoDropped, Value: repo}}
	}
	s.Workspace = repo
	return []FormatChange{{Kind: ChangeWorkspaceFromRepo, Value: repo}}
}

func (s *State) migrateSessionsLocked(migration FormatMigration) []FormatChange {
	var changes []FormatChange
	for idx := range s.Sessions {
		sess := &s.Sessions[idx]
		if needsGatewayBinding(*sess) {
			sess.Gateways = slices.Clone(migration.DefaultGateways)
			changes = append(changes, FormatChange{Kind: ChangeGatewaysBound, Session: sess.Name, Value: strings.Join(sess.Gateways, ", ")})
		}
		if moved, ok := migration.Relocated[sess.Name]; ok {
			sess.Worktree, sess.Dir = moved.Worktree, moved.Dir
			changes = append(changes, FormatChange{Kind: ChangeSessionRelocated, Session: sess.Name, Value: moved.Worktree})
		}
	}
	return changes
}
