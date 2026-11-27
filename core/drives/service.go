package drives

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"
	"unicode/utf8"
)

const (
	// InlineReadBytes is the largest file a read returns as text.
	InlineReadBytes = 1 << 20
	// CopyBytes is the largest file a read copies to disk or a write sends.
	CopyBytes = 64 << 20
	// ListEntries is the most entries a listing returns.
	ListEntries   = 1000
	reasonRunes   = 300
	localFileMode = os.FileMode(0o644)
	localDirMode  = os.FileMode(0o755)
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
	return fmt.Sprintf("drive %q: %s holds %s access and this needs %s. The operator grants it with `drive grant %s --agent <name> --access %s`.", e.Drive, agent, e.Held, e.Need, e.Drive, e.Need)
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
	catalog Catalog
	rclone  Rclone
}

// NewService binds a catalogue to the rclone that reaches its remotes.
func NewService(catalog Catalog, rclone Rclone) Service {
	return Service{catalog: catalog, rclone: rclone}
}

// Catalog is the catalogue the service runs on.
func (s Service) Catalog() Catalog { return s.catalog }

// Rclone is the runner the service uses.
func (s Service) Rclone() Rclone { return s.rclone }

// Visible lists the drives the caller holds at least read access on, each with that access.
func (s Service) Visible(caller Caller) ([]Drive, error) {
	all, err := s.catalog.Load()
	if err != nil {
		return nil, err
	}
	if caller.isOperator() {
		return all, nil
	}
	visible := make([]Drive, 0, len(all))
	for _, drive := range all {
		held := drive.AccessOf(caller.Agent)
		if held.Allows(AccessRead) {
			visible = append(visible, Drive{Name: drive.Name, Kind: drive.Kind, Folders: drive.Folders, Default: held})
		}
	}
	return visible, nil
}

// Check finds the drive and refuses a caller whose access does not cover need. The refusal is journaled.
func (s Service) Check(ctx context.Context, caller Caller, name string, need Access, op Op, path string) (Drive, error) {
	drive, err := s.catalog.Find(name)
	if err != nil {
		return Drive{}, err
	}
	if caller.isOperator() {
		return drive, nil
	}
	held := drive.AccessOf(caller.Agent)
	if held.Allows(need) {
		return drive, nil
	}
	denied := &AccessDeniedError{Drive: name, Agent: caller.Agent, Need: need, Held: held}
	s.record(ctx, caller, Entry{Drive: name, Path: path, Op: op, Outcome: OutcomeRefused, Reason: denied.Error()})
	return Drive{}, denied
}

// List lists one level of a folder of the drive. The root of a drive that exposes folders lists those folders.
func (s Service) List(ctx context.Context, caller Caller, name, rawPath string) ([]Item, bool, error) {
	drive, path, err := s.locate(ctx, caller, name, rawPath, AccessRead, OpList)
	if err != nil {
		return nil, false, err
	}
	folders, err := drive.ExposedFolders()
	if err != nil {
		return nil, false, err
	}
	if path.IsRoot() && len(folders) > 0 {
		entries := make([]Item, 0, len(folders))
		for _, folder := range folders {
			entries = append(entries, Item{Path: folder.String(), Name: folder.String(), IsDir: true})
		}
		s.record(ctx, caller, Entry{Drive: name, Op: OpList, Outcome: OutcomeDone})
		return entries, false, nil
	}
	entries, err := s.rclone.List(ctx, name+":"+path.String())
	s.recordOutcome(ctx, caller, Entry{Drive: name, Path: path.String(), Op: OpList}, err)
	if err != nil {
		return nil, false, err
	}
	truncated := len(entries) > ListEntries
	if truncated {
		entries = entries[:ListEntries]
	}
	return entries, truncated, nil
}

// Read returns a text file of the drive, refused past InlineReadBytes or when it is not UTF-8.
func (s Service) Read(ctx context.Context, caller Caller, name, rawPath string) (string, error) {
	drive, path, err := s.locate(ctx, caller, name, rawPath, AccessRead, OpRead)
	if err != nil {
		return "", err
	}
	content, err := s.readFile(ctx, drive, path, InlineReadBytes)
	if err == nil && !utf8.Valid(content) {
		err = ErrNotText
	}
	s.recordOutcome(ctx, caller, Entry{Drive: name, Path: path.String(), Op: OpRead, Bytes: int64(len(content))}, err)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// ReadTo copies a file of the drive to dest, inside the session's worktree or at an absolute path for the operator.
func (s Service) ReadTo(ctx context.Context, caller Caller, name, rawPath, dest string) (int64, error) {
	drive, path, err := s.locate(ctx, caller, name, rawPath, AccessRead, OpRead)
	if err != nil {
		return 0, err
	}
	content, err := s.readFile(ctx, drive, path, CopyBytes)
	if err == nil {
		err = writeLocal(caller, dest, content)
	}
	s.recordOutcome(ctx, caller, Entry{Drive: name, Path: path.String(), Op: OpRead, Bytes: int64(len(content))}, err)
	if err != nil {
		return 0, err
	}
	return int64(len(content)), nil
}

// Write sends the local file source to the drive, from the session's worktree or an absolute path for the operator.
func (s Service) Write(ctx context.Context, caller Caller, name, rawPath, source string) (int64, error) {
	drive, path, err := s.locate(ctx, caller, name, rawPath, AccessWrite, OpWrite)
	if err != nil {
		return 0, err
	}
	size, err := s.sendFile(ctx, caller, drive, path, source)
	s.recordOutcome(ctx, caller, Entry{Drive: name, Path: path.String(), Op: OpWrite, Bytes: size}, err)
	if err != nil {
		return 0, err
	}
	return size, nil
}

// Log reads the journal.
func (s Service) Log(filter Filter, limit int) ([]Entry, error) {
	return s.catalog.Journal().Read(filter, limit)
}

func (s Service) locate(ctx context.Context, caller Caller, name, rawPath string, need Access, op Op) (Drive, Path, error) {
	drive, err := s.Check(ctx, caller, name, need, op, rawPath)
	if err != nil {
		return Drive{}, Path{}, err
	}
	path, err := ParsePath(rawPath)
	if err == nil && op != OpList && path.IsRoot() {
		err = ErrPathRequired
	}
	if err == nil && (op != OpList || !path.IsRoot()) {
		_, err = drive.Target(path)
	}
	if err != nil {
		s.record(ctx, caller, Entry{Drive: name, Path: rawPath, Op: op, Outcome: OutcomeRefused, Reason: err.Error()})
		return Drive{}, Path{}, err
	}
	return drive, path, nil
}

func (s Service) readFile(ctx context.Context, drive Drive, path Path, limit int64) ([]byte, error) {
	target, err := drive.Target(path)
	if err != nil {
		return nil, err
	}
	return s.rclone.Read(ctx, target, limit)
}

func (s Service) sendFile(ctx context.Context, caller Caller, drive Drive, path Path, source string) (int64, error) {
	target, err := drive.Target(path)
	if err != nil {
		return 0, err
	}
	file, err := openLocal(caller, source)
	if err != nil {
		return 0, err
	}
	defer closeLocal(file)
	info, err := file.Stat()
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() {
		return 0, ErrNotRegularFile
	}
	if info.Size() > CopyBytes {
		return 0, &TooLargeError{Limit: CopyBytes}
	}
	if err := s.rclone.Write(ctx, target, io.LimitReader(file, info.Size()), info.Size()); err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func (s Service) recordOutcome(ctx context.Context, caller Caller, entry Entry, err error) {
	entry.Outcome = OutcomeDone
	if err != nil {
		entry.Outcome = OutcomeFailed
		entry.Reason = err.Error()
		entry.Bytes = 0
	}
	s.record(ctx, caller, entry)
}

func (s Service) record(ctx context.Context, caller Caller, entry Entry) {
	entry.Time = time.Now().UTC()
	entry.Session = caller.Session
	entry.Agent = caller.Agent
	if runes := []rune(entry.Reason); len(runes) > reasonRunes {
		entry.Reason = string(runes[:reasonRunes])
	}
	if err := s.catalog.Journal().Append(context.WithoutCancel(ctx), entry); err != nil {
		slog.Warn("drive journal append", "drive", entry.Drive, "err", err)
	}
}

func localRoot(caller Caller, raw string) (*os.Root, string, error) {
	if caller.isOperator() {
		if !filepath.IsAbs(raw) {
			return nil, "", ErrLocalPathOutside
		}
		root, err := os.OpenRoot(filepath.Dir(raw))
		return root, filepath.Base(raw), err
	}
	if caller.Worktree == "" {
		return nil, "", ErrNoWorktree
	}
	if !filepath.IsLocal(raw) {
		return nil, "", ErrLocalPathOutside
	}
	root, err := os.OpenRoot(caller.Worktree)
	return root, filepath.Clean(raw), err
}

func openLocal(caller Caller, raw string) (*os.File, error) {
	root, name, err := localRoot(caller, raw)
	if err != nil {
		return nil, err
	}
	defer closeRoot(root)
	return root.Open(name)
}

func writeLocal(caller Caller, raw string, content []byte) error {
	root, name, err := localRoot(caller, raw)
	if err != nil {
		return err
	}
	defer closeRoot(root)
	if dir := filepath.Dir(name); dir != "." {
		if err := root.MkdirAll(dir, localDirMode); err != nil {
			return err
		}
	}
	return root.WriteFile(name, content, localFileMode)
}

func closeRoot(root *os.Root) {
	if err := root.Close(); err != nil {
		slog.Warn("close drive local root", "err", err)
	}
}

func closeLocal(file *os.File) {
	if err := file.Close(); err != nil {
		slog.Warn("close drive local file", "err", err)
	}
}
