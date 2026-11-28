package drives

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"
	"unicode/utf8"
)

const (
	// InlineReadBytes is the largest file a read returns as text.
	InlineReadBytes = 1 << 20
	// CopyBytes is the largest file a read copies to disk or a write sends.
	CopyBytes = 64 << 20
	// ListEntries is the most entries a listing returns.
	ListEntries = 1000
	reasonRunes = 300
	gitSegment  = ".git"
)

var (
	// ErrNotText reports a file read inline that is not UTF-8 text.
	ErrNotText = errors.New("the file is not UTF-8 text: copy it to a file with --to")
	// ErrNoWorktree reports a session that has no worktree to read from or copy to.
	ErrNoWorktree = errors.New("the session has no worktree to hold the file")
	// ErrLocalPathOutside reports a session path that leaves its worktree, or an operator path that is not absolute.
	ErrLocalPathOutside = errors.New("a session names a file inside its worktree, the operator an absolute path")
	// ErrNotRegularFile reports a write source that is not a regular file.
	ErrNotRegularFile = errors.New("the source is not a regular file")
)

// AccessDeniedError reports an agent whose access to a drive does not cover the operation.
type AccessDeniedError struct {
	Drive string
	Agent string
	Need  Access
	Held  Access
}

func (e *AccessDeniedError) Error() string {
	agent := e.Agent
	if agent == "" {
		agent = "a session with no agent"
	}
	return fmt.Sprintf("drive %q: %s holds %s access and this needs %s. Only the operator grants it, with `herrscher drive grant %s --agent <name> --access %s`.", e.Drive, agent, e.Held, e.Need, e.Drive, e.Need)
}

// Caller is who runs a drive operation. A caller with no session is the
// operator, who holds write access on every drive.
type Caller struct {
	Session  string
	Agent    string
	Worktree string
}

func (c Caller) isOperator() bool { return c.Session == "" }

// Service runs the drive operations of one catalogue, checking access and journaling each one.
type Service struct {
	catalog   Catalog
	rclone    Rclone
	protected []string
}

// NewService binds a catalogue to the rclone that reaches its remotes.
func NewService(catalog Catalog, rclone Rclone) Service {
	return Service{catalog: catalog, rclone: rclone}
}

// Protecting is the service with names, first path segments of a worktree,
// that a session never reads into or sends from. .git, at any depth, always is.
func (s Service) Protecting(names []string) Service {
	s.protected = append([]string(nil), names...)
	return s
}

// Catalog is the catalogue the service runs on.
func (s Service) Catalog() Catalog { return s.catalog }

// Rclone is the runner the service uses.
func (s Service) Rclone() Rclone { return s.rclone }

// Listing is a drive as drive list shows it: its kind, its folders and the
// access of the agents, never an rclone setting.
type Listing struct {
	Name    string            `json:"name"`
	Kind    Kind              `json:"kind"`
	Folders []string          `json:"folders,omitempty"`
	Default Access            `json:"default"`
	Agents  map[string]Access `json:"agents,omitempty"`
}

// Visible lists the drives the caller holds at least read access on. The
// operator sees every drive and every access, a session its own access alone.
func (s Service) Visible(caller Caller) ([]Listing, error) {
	all, err := s.catalog.Load()
	if err != nil {
		return nil, err
	}
	visible := make([]Listing, 0, len(all))
	for _, drive := range all {
		if caller.isOperator() {
			visible = append(visible, Listing{Name: drive.Name, Kind: drive.Kind, Folders: drive.Folders, Default: drive.Default, Agents: drive.Agents})
			continue
		}
		if held := drive.AccessOf(caller.Agent); held.Allows(AccessRead) {
			visible = append(visible, Listing{Name: drive.Name, Kind: drive.Kind, Folders: drive.Folders, Default: held})
		}
	}
	return visible, nil
}

// Request is one drive operation as a verb asks for it: the drive, the path
// inside it, and the local file it reads into or sends from, if any.
type Request struct {
	Drive string
	Path  string
	Local string
	Op    Op
}

func (r Request) need() Access {
	if r.Op == OpWrite {
		return AccessWrite
	}
	return AccessRead
}

// Admit checks everything that does not need rclone: the access of the
// agent, the path inside the exposed folders and the local file. A refusal is
// journaled. It is what a session's call passes before the operator is asked.
func (s Service) Admit(ctx context.Context, caller Caller, request Request) (Drive, Path, error) {
	drive, path, err := s.admit(caller, request)
	if err != nil {
		s.record(ctx, caller, Entry{Drive: request.Drive, Path: request.Path, Op: request.Op, Outcome: OutcomeRefused, Reason: err.Error()})
		return Drive{}, Path{}, err
	}
	return drive, path, nil
}

func (s Service) admit(caller Caller, request Request) (Drive, Path, error) {
	drive, err := s.catalog.Find(request.Drive)
	if err != nil {
		return Drive{}, Path{}, err
	}
	if !caller.isOperator() {
		if held := drive.AccessOf(caller.Agent); !held.Allows(request.need()) {
			return Drive{}, Path{}, &AccessDeniedError{Drive: request.Drive, Agent: caller.Agent, Need: request.need(), Held: held}
		}
	}
	path, err := ParsePath(request.Path)
	if err != nil {
		return Drive{}, Path{}, err
	}
	if request.Op != OpList && path.IsRoot() {
		return Drive{}, Path{}, ErrPathRequired
	}
	if request.Op != OpList || !path.IsRoot() {
		if _, err := drive.Target(path); err != nil {
			return Drive{}, Path{}, err
		}
	}
	if request.Local != "" {
		if err := s.checkLocal(caller, request.Local); err != nil {
			return Drive{}, Path{}, err
		}
	}
	return drive, path, nil
}

// JournalError reports an operation whose journal entry could not be
// written. A read or a listing then withholds what it found, a copy removes
// what it staged, and a write, which already reached the remote, says so.
type JournalError struct {
	Op    Op
	Cause error
}

func (e *JournalError) Error() string {
	if e.Op == OpWrite {
		return "the write reached the drive, but its journal entry could not be written: " + e.Cause.Error()
	}
	return "the drive journal could not record this operation, so its result is withheld: " + e.Cause.Error()
}

func (e *JournalError) Unwrap() error { return e.Cause }

func (s Service) start(ctx context.Context, caller Caller, request Request) (Drive, Path, error) {
	drive, path, err := s.Admit(ctx, caller, request)
	if err != nil {
		return Drive{}, Path{}, err
	}
	if err := s.reserve(ctx, caller, request); err != nil {
		return Drive{}, Path{}, err
	}
	return drive, path, nil
}

func (s Service) reserve(ctx context.Context, caller Caller, request Request) error {
	err := s.catalog.Journal().Reserve(ctx, caller.Session, time.Now().UTC())
	if err != nil {
		s.record(ctx, caller, Entry{Drive: request.Drive, Path: request.Path, Op: request.Op, Outcome: OutcomeRefused, Reason: err.Error()})
	}
	return err
}

// List lists one level of a folder of the drive. The root of a drive that
// exposes folders lists those folders, without rclone and without a journal
// entry, since nothing of the remote is read.
func (s Service) List(ctx context.Context, caller Caller, name, rawPath string) ([]Item, bool, error) {
	drive, path, err := s.Admit(ctx, caller, Request{Drive: name, Path: rawPath, Op: OpList})
	if err != nil {
		return nil, false, err
	}
	folders, err := drive.ExposedFolders()
	if err != nil {
		return nil, false, err
	}
	if path.IsRoot() && len(folders) > 0 {
		items := make([]Item, 0, len(folders))
		for _, folder := range folders {
			items = append(items, Item{Path: folder.String(), Name: folder.String(), IsDir: true})
		}
		return items, false, nil
	}
	if reserveErr := s.reserve(ctx, caller, Request{Drive: name, Path: rawPath, Op: OpList}); reserveErr != nil {
		return nil, false, reserveErr
	}
	var items []Item
	var truncated bool
	runner, err := s.runnerFor(drive)
	if err == nil {
		items, truncated, err = runner.List(ctx, name+":"+path.String(), ListEntries)
	}
	if finishErr := s.finish(ctx, caller, Entry{Drive: name, Path: path.String(), Op: OpList}, err); finishErr != nil {
		return nil, false, finishErr
	}
	return items, truncated, nil
}

// Read returns a text file of the drive, refused past InlineReadBytes, when it is not UTF-8 or when it is a folder.
func (s Service) Read(ctx context.Context, caller Caller, name, rawPath string) (string, error) {
	drive, path, err := s.start(ctx, caller, Request{Drive: name, Path: rawPath, Op: OpRead})
	if err != nil {
		return "", err
	}
	var content bytes.Buffer
	size, err := s.readFile(ctx, drive, path, &content, InlineReadBytes)
	if err == nil && !utf8.Valid(content.Bytes()) {
		err = ErrNotText
	}
	if finishErr := s.finish(ctx, caller, Entry{Drive: name, Path: path.String(), Op: OpRead, Bytes: size}, err); finishErr != nil {
		return "", finishErr
	}
	return content.String(), nil
}

// ReadTo copies a file of the drive to dest, inside the session's worktree or
// at an absolute path for the operator. The copy streams into a staging file
// beside dest, renamed over it once complete, then journaled; a copy whose
// entry cannot be written is removed again.
func (s Service) ReadTo(ctx context.Context, caller Caller, name, rawPath, dest string) (int64, error) {
	drive, path, err := s.start(ctx, caller, Request{Drive: name, Path: rawPath, Local: dest, Op: OpRead})
	if err != nil {
		return 0, err
	}
	root, local, err := s.localRoot(caller, dest)
	if err != nil {
		return 0, s.finish(ctx, caller, Entry{Drive: name, Path: path.String(), Op: OpRead}, err)
	}
	defer closeRoot(root)
	staging, stagingName, err := createStaging(root, local)
	if err != nil {
		return 0, s.finish(ctx, caller, Entry{Drive: name, Path: path.String(), Op: OpRead}, err)
	}
	size, err := s.readFile(ctx, drive, path, staging, CopyBytes)
	err = errors.Join(err, staging.Close())
	if err == nil {
		err = root.Rename(stagingName, local)
	}
	if err != nil {
		discardStaging(root, stagingName)
	}
	if finishErr := s.finish(ctx, caller, Entry{Drive: name, Path: path.String(), Op: OpRead, Bytes: size}, err); finishErr != nil {
		if err == nil {
			discardStaging(root, local)
		}
		return 0, finishErr
	}
	return size, nil
}

// Write sends the local file source to the drive, from the session's worktree
// or an absolute path for the operator. A write that fails before anything
// reaches the remote is journaled as unsent, under the cap of the entries that
// carry no data.
func (s Service) Write(ctx context.Context, caller Caller, name, rawPath, source string) (int64, error) {
	drive, path, err := s.start(ctx, caller, Request{Drive: name, Path: rawPath, Local: source, Op: OpWrite})
	if err != nil {
		return 0, err
	}
	size, sent, err := s.sendFile(ctx, caller, drive, path, source)
	entry := Entry{Drive: name, Path: path.String(), Op: OpWrite, Bytes: size}
	if err != nil && !sent {
		entry.Outcome = OutcomeUnsent
	}
	if finishErr := s.finish(ctx, caller, entry, err); finishErr != nil {
		return 0, finishErr
	}
	return size, nil
}

// Log reads the journal.
func (s Service) Log(filter Filter, limit int) (Log, error) {
	return s.catalog.Journal().Read(filter, limit)
}

func (s Service) runnerFor(drive Drive) (Rclone, error) {
	env, err := s.catalog.Environment(drive)
	if err != nil {
		return Rclone{}, err
	}
	if len(drive.Settings) > 0 {
		return s.rclone.With(env).WithoutConfig(), nil
	}
	return s.rclone, nil
}

func (s Service) readFile(ctx context.Context, drive Drive, path Path, dst io.Writer, limit int64) (int64, error) {
	target, err := drive.Target(path)
	if err != nil {
		return 0, err
	}
	runner, err := s.runnerFor(drive)
	if err != nil {
		return 0, err
	}
	item, err := runner.Stat(ctx, target)
	if err != nil {
		return 0, err
	}
	if item.IsDir {
		return 0, ErrIsFolder
	}
	return runner.Read(ctx, target, dst, limit)
}

func (s Service) sendFile(ctx context.Context, caller Caller, drive Drive, path Path, source string) (int64, bool, error) {
	target, err := drive.Target(path)
	if err != nil {
		return 0, false, err
	}
	runner, err := s.runnerFor(drive)
	if err != nil {
		return 0, false, err
	}
	root, name, err := s.localRoot(caller, source)
	if err != nil {
		return 0, false, err
	}
	defer closeRoot(root)
	file, size, err := openSource(root, name)
	if err != nil {
		return 0, false, err
	}
	defer closeLocal(file)
	if size > CopyBytes {
		return 0, false, &TooLargeError{Limit: CopyBytes}
	}
	if started, err := runner.Write(ctx, target, io.LimitReader(file, size), size); err != nil {
		return 0, started, err
	}
	return size, true, nil
}

func (s Service) finish(ctx context.Context, caller Caller, entry Entry, opErr error) error {
	if entry.Outcome == "" {
		entry.Outcome = OutcomeDone
	}
	if opErr != nil {
		if entry.Outcome == OutcomeDone {
			entry.Outcome = OutcomeFailed
		}
		entry.Reason = opErr.Error()
		entry.Bytes = 0
	}
	if err := s.append(ctx, caller, entry); err != nil {
		slog.Warn("drive journal append failed", "drive", entry.Drive, "path", entry.Path, "session", caller.Session, "op", string(entry.Op), "outcome", string(entry.Outcome), "err", err)
		if opErr != nil {
			return opErr
		}
		return &JournalError{Op: entry.Op, Cause: err}
	}
	return opErr
}

func (s Service) record(ctx context.Context, caller Caller, entry Entry) {
	if err := s.append(ctx, caller, entry); err != nil {
		slog.Warn("drive journal append", "drive", entry.Drive, "err", err)
	}
}

func (s Service) append(ctx context.Context, caller Caller, entry Entry) error {
	entry.Time = time.Now().UTC()
	entry.Session = caller.Session
	entry.Agent = caller.Agent
	if runes := []rune(entry.Reason); len(runes) > reasonRunes {
		entry.Reason = string(runes[:reasonRunes])
	}
	return s.catalog.Journal().Append(context.WithoutCancel(ctx), entry)
}
