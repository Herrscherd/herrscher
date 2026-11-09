// Package outbox carries the files a backend renders during a turn from the bridge
// that runs the backend to the host that posts the reply. The bridge makes one
// directory per turn, vets the files the backend hands over, and sends them in a
// frame; the host posts them with the reply and removes the directory afterwards.
package outbox

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// EventFiles is the event type of the frame that carries the vetted files of a turn
// from the bridge to the host. The frame is sent before the terminal reply.
const EventFiles = "outbound_files"

// MaxFiles is the most files one turn sends. A file past it is refused.
const MaxFiles = 10

// DefaultMaxBytes is the size above which a file is refused when the config sets none.
const DefaultMaxBytes int64 = 25 << 20

// MaxAge is the age past which a turn directory left by a crash is purged at start.
const MaxAge = 24 * time.Hour

const privateDirMode os.FileMode = 0o700

const turnDirPrefix = "turn-"

// ErrOutsideOutbox reports a file whose resolved path leaves the turn directory,
// through a symbolic link. Match it with errors.Is.
var ErrOutsideOutbox = errors.New("file resolves outside the outbox")

// ErrNotRegular reports a file that is not a regular file, a symbolic link included.
// Match it with errors.Is.
var ErrNotRegular = errors.New("file is not a regular file")

// ErrTooLarge reports a file larger than the configured maximum. Match it with
// errors.Is.
var ErrTooLarge = errors.New("file is larger than the maximum")

// ErrTooMany reports a file past the [MaxFiles] a turn may send. Match it with
// errors.Is.
var ErrTooMany = errors.New("turn sends too many files")

// ErrBadSession reports a session name that cannot name a directory. Match it with
// errors.Is.
var ErrBadSession = errors.New("session name cannot name an outbox directory")

// Frame is the vetted outcome of one turn: the directory the files live in, the
// files a gateway sends, and the names of the files that were refused.
type Frame struct {
	Dir     string                   `json:"dir"`
	Files   []contracts.OutboundFile `json:"files,omitempty"`
	Refused []string                 `json:"refused,omitempty"`
}

// Refusal is one file the bridge did not hand to the host, with its cause.
type Refusal struct {
	Name  string
	Cause error
}

// Root returns the absolute directory that holds the outboxes of every session, next
// to the daemon state file at statePath.
func Root(statePath string) (string, error) {
	root, err := filepath.Abs(filepath.Join(filepath.Dir(statePath), "outbox"))
	if err != nil {
		return "", fmt.Errorf("resolve outbox root: %w", err)
	}
	return root, nil
}

// Encode returns the wire form of a frame, carried in the value of its event.
func Encode(frame Frame) (string, error) {
	data, err := json.Marshal(frame)
	if err != nil {
		return "", fmt.Errorf("encode outbound frame: %w", err)
	}
	return string(data), nil
}

// Decode reads the frame carried in the value of its event.
func Decode(value string) (Frame, error) {
	var frame Frame
	if err := json.Unmarshal([]byte(value), &frame); err != nil {
		return Frame{}, fmt.Errorf("decode outbound frame: %w", err)
	}
	return frame, nil
}

// NewTurnDir creates a private directory for one turn of session under root, and
// returns its absolute path.
func NewTurnDir(root, session string) (string, error) {
	sessionDir, err := sessionDirOf(root, session)
	if err != nil {
		return "", err
	}
	if err = os.MkdirAll(sessionDir, privateDirMode); err != nil {
		return "", fmt.Errorf("create outbox of session %q: %w", session, err)
	}
	dir, err := os.MkdirTemp(sessionDir, turnDirPrefix)
	if err != nil {
		return "", fmt.Errorf("create turn outbox: %w", err)
	}
	return dir, nil
}

func sessionDirOf(root, session string) (string, error) {
	if session == "" || session == "." || session == ".." || strings.ContainsAny(session, "/\\\x00") {
		return "", fmt.Errorf("%w: %q", ErrBadSession, session)
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve outbox root: %w", err)
	}
	return filepath.Join(absRoot, session), nil
}

// Vet checks the files a backend handed over for one turn held in dir. A file is
// accepted when it is a clean path inside dir that resolves inside dir, is a regular
// file, and is no larger than maxBytes. Files past [MaxFiles] are refused. Every
// refusal is returned with its cause, and the accepted files keep their order.
func Vet(dir string, files []contracts.OutboundFile, maxBytes int64) ([]contracts.OutboundFile, []Refusal) {
	var accepted []contracts.OutboundFile
	var refused []Refusal
	for index, file := range files {
		if index >= MaxFiles {
			refused = append(refused, Refusal{Name: file.Name, Cause: ErrTooMany})
			continue
		}
		if err := vet(dir, file, maxBytes); err != nil {
			refused = append(refused, Refusal{Name: file.Name, Cause: err})
			continue
		}
		accepted = append(accepted, file)
	}
	return accepted, refused
}

func vet(dir string, file contracts.OutboundFile, maxBytes int64) error {
	if err := contracts.ValidateOutboundFile(file, dir); err != nil {
		return err
	}
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return fmt.Errorf("resolve outbox: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(file.Path)
	if err != nil {
		return fmt.Errorf("resolve file: %w", err)
	}
	if !within(resolvedDir, resolved) {
		return ErrOutsideOutbox
	}
	info, err := os.Lstat(file.Path)
	if err != nil {
		return fmt.Errorf("stat file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return ErrNotRegular
	}
	if info.Size() > maxBytes {
		return ErrTooLarge
	}
	return nil
}

// Owns reports whether dir is a turn directory of a session below root, that is
// root/<session>/turn-XXXX as [NewTurnDir] creates it, so a frame can name only the
// turn directories of the outboxes the host keeps.
func Owns(root, dir string) bool {
	if !within(root, dir) {
		return false
	}
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return false
	}
	parts := strings.Split(rel, string(filepath.Separator))
	return len(parts) == 2 && parts[0] != "" && isTurnDirName(parts[1])
}

func isTurnDirName(name string) bool {
	return strings.HasPrefix(name, turnDirPrefix)
}

// Remove deletes the turn directory dir, which [Owns] must accept, so a frame can
// never name a directory outside the outboxes. A dir with unclean elements is
// cleaned first, and the cleaned path is the one removed.
func Remove(root, dir string) error {
	cleaned := filepath.Clean(dir)
	if !Owns(root, cleaned) {
		return fmt.Errorf("%w: %q", ErrOutsideOutbox, dir)
	}
	if err := os.RemoveAll(cleaned); err != nil {
		return fmt.Errorf("remove turn outbox: %w", err)
	}
	return nil
}

// Purge removes the turn directories of every session under root last modified
// before now minus [MaxAge], the ones a crash left behind. A missing root is not an
// error.
func Purge(root string, now time.Time) error {
	sessions, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read outbox root: %w", err)
	}
	cutoff := now.Add(-MaxAge)
	var errs []error
	for _, session := range sessions {
		if session.IsDir() {
			errs = append(errs, purgeSession(filepath.Join(root, session.Name()), cutoff))
		}
	}
	return errors.Join(errs...)
}

// PurgeSession removes the turn directories of one session under root last modified
// before now minus [MaxAge]. The turns of other sessions are left alone, so a turn
// still running in another session is never removed. A missing session directory is
// not an error.
func PurgeSession(root, session string, now time.Time) error {
	sessionDir, err := sessionDirOf(root, session)
	if err != nil {
		return err
	}
	return purgeSession(sessionDir, now.Add(-MaxAge))
}

func purgeSession(sessionDir string, cutoff time.Time) error {
	turns, err := os.ReadDir(sessionDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read outbox of session: %w", err)
	}
	var errs []error
	for _, turn := range turns {
		if !isTurnDirName(turn.Name()) {
			continue
		}
		info, err := turn.Info()
		if err != nil {
			errs = append(errs, fmt.Errorf("stat turn outbox: %w", err))
			continue
		}
		if info.ModTime().Before(cutoff) {
			errs = append(errs, os.RemoveAll(filepath.Join(sessionDir, turn.Name())))
		}
	}
	return errors.Join(errs...)
}

func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || rel == ".." {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && filepath.IsAbs(root)
}
