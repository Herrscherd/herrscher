// Package checkpoint keeps the history of a session's worktree as git commits,
// one per user message, so the worktree can be rewound to any of them and the
// rewind undone. It owns that git plumbing and nothing else: the caller decides
// when a checkpoint is taken and which worktree it belongs to.
package checkpoint

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	checkpointRefRoot   = "refs/herrscher/checkpoints/"
	undoRefRoot         = "refs/herrscher/undo/"
	turnTrailer         = "Herrscher-Turn"
	conversationTrailer = "Herrscher-Conversation"
	entryFormat         = "%(refname)%00%(objectname)%00%(creatordate:unix)%00%(contents:subject)%00%(trailers:key=" + turnTrailer + ",valueonly,separator=%x20)%00%(trailers:key=" + conversationTrailer + ",valueonly,separator=%x20)"
	identityName        = "herrscher"
	identityMail        = "checkpoint@herrscher.invalid"
	restorePointSubject = "herrscher: état avant retour en arrière"
	emptyTurnSubject    = "(message vide)"
	entryFieldCount     = 6
	numstatFieldCount   = 3
	nameStatusStride    = 2
	binaryCount         = "-"
	deletedStatus       = "D"
	indexFileName       = "index"
	tempDirPattern      = "herrscher-checkpoint-"
)

var (
	// ErrNoCheckpoint is returned when a session has no checkpoint with the asked number.
	ErrNoCheckpoint = errors.New("no checkpoint")
	// ErrNoUndo is returned when there is no rewind to undo.
	ErrNoUndo = errors.New("nothing to undo")
	// ErrBadSession is returned for a session name that cannot name a git ref.
	ErrBadSession = errors.New("invalid session name")
	// ErrOutsideWorktree is returned when a checkpoint would touch a path outside its worktree.
	ErrOutsideWorktree = errors.New("path outside worktree")
)

// Store serializes the git operations of one process, so that two checkpoints
// of a session cannot take the same number. Its zero value is ready to use, and
// a Store must not be copied after its first use.
type Store struct {
	mu sync.Mutex
}

// Meta describes the user message a checkpoint is taken before.
type Meta struct {
	Turn         string
	Text         string
	Conversation string
}

// Entry is one checkpoint of a session, as listed by [Store.List].
type Entry struct {
	Index        int
	Commit       string
	Turn         string
	Text         string
	Conversation string
	At           time.Time
}

// Change is one file a checkpoint interval touched, with its line counts.
type Change struct {
	Path    string
	Added   int
	Removed int
}

// Take records the current state of dir as the next checkpoint of session. It
// returns an error wrapping [ErrBadSession] for a name that cannot name a ref.
func (s *Store) Take(ctx context.Context, dir, session string, meta Meta) (Entry, error) {
	if err := validSession(session); err != nil {
		return Entry{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := listLocked(ctx, dir, session)
	if err != nil {
		return Entry{}, err
	}
	index := nextIndex(entries)
	commit, err := snapshotLocked(ctx, dir, commitMessage(meta))
	if err != nil {
		return Entry{}, err
	}
	_, err = git(ctx, dir, nil, nil, "update-ref", checkpointRef(session, index), commit)
	if err != nil {
		return Entry{}, err
	}
	return Entry{Index: index, Commit: commit, Turn: meta.Turn, Text: meta.Text, Conversation: meta.Conversation, At: time.Now().UTC()}, nil
}

// List returns the checkpoints of session, oldest first. It returns no entries
// when the session has none.
func (s *Store) List(ctx context.Context, dir, session string) ([]Entry, error) {
	if err := validSession(session); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return listLocked(ctx, dir, session)
}

// Get returns checkpoint number index of session. It returns an error wrapping
// [ErrNoCheckpoint] when there is none with that number.
func (s *Store) Get(ctx context.Context, dir, session string, index int) (Entry, error) {
	entries, err := s.List(ctx, dir, session)
	if err != nil {
		return Entry{}, err
	}
	for _, entry := range entries {
		if entry.Index == index {
			return entry, nil
		}
	}
	return Entry{}, fmt.Errorf("%w: %s #%d", ErrNoCheckpoint, session, index)
}

// Current returns the tree of dir as it is now, without recording it.
func (s *Store) Current(ctx context.Context, dir string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return snapshotTree(ctx, dir)
}

// Changes lists the files that differ between two commits, as git reports them.
func Changes(ctx context.Context, dir, from, to string) ([]Change, error) {
	out, err := git(ctx, dir, nil, nil, "diff-tree", "-r", "-z", "--numstat", "--no-renames", from, to)
	if err != nil {
		return nil, err
	}
	return parseNumstat(out), nil
}

// Restore rewinds the worktree of session to checkpoint index. It first records
// the state it replaces, so [Store.Undo] can bring it back. It returns the
// checkpoint restored.
func (s *Store) Restore(ctx context.Context, dir, session string, index int) (Entry, error) {
	target, err := s.Get(ctx, dir, session, index)
	if err != nil {
		return Entry{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := restoreLocked(ctx, dir, undoRefRoot+session, target.Commit); err != nil {
		return Entry{}, err
	}
	return target, nil
}

// Undo brings back the state that the last [Store.Restore] of session replaced.
// It returns an error wrapping [ErrNoUndo] when no restore is pending.
func (s *Store) Undo(ctx context.Context, dir, session string) error {
	if err := validSession(session); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	undoRef := undoRefRoot + session
	previous, err := resolveRef(ctx, dir, undoRef)
	if err != nil {
		return err
	}
	if previous == "" {
		return fmt.Errorf("%w: %s", ErrNoUndo, session)
	}
	return restoreLocked(ctx, dir, undoRef, previous)
}

// HasUndo reports whether [Store.Undo] would find a restore to undo.
func (s *Store) HasUndo(ctx context.Context, dir, session string) bool {
	if validSession(session) != nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ref, err := resolveRef(ctx, dir, undoRefRoot+session)
	return err == nil && ref != ""
}

// Materialize applies commit to the worktree of dir, replacing its files the
// same way [Store.Restore] does, without recording a checkpoint for it.
func (s *Store) Materialize(ctx context.Context, dir, commit string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	fromTree, err := snapshotTree(ctx, dir)
	if err != nil {
		return err
	}
	toTree, err := treeOf(ctx, dir, commit)
	if err != nil {
		return err
	}
	return applyTree(ctx, dir, fromTree, toTree)
}

// Head returns the commit that HEAD of dir points to.
func Head(ctx context.Context, dir string) (string, error) {
	out, err := git(ctx, dir, nil, nil, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func restoreLocked(ctx context.Context, dir, undoRef, target string) error {
	current, err := snapshotLocked(ctx, dir, restorePointSubject)
	if err != nil {
		return err
	}
	if _, err = git(ctx, dir, nil, nil, "update-ref", undoRef, current); err != nil {
		return err
	}
	currentTree, err := treeOf(ctx, dir, current)
	if err != nil {
		return err
	}
	targetTree, err := treeOf(ctx, dir, target)
	if err != nil {
		return err
	}
	return applyTree(ctx, dir, currentTree, targetTree)
}

func applyTree(ctx context.Context, dir, fromTree, toTree string) error {
	raw, err := git(ctx, dir, nil, nil, "diff-tree", "-r", "-z", "--name-status", "--no-renames", fromTree, toTree)
	if err != nil {
		return err
	}
	var written []string
	for _, change := range parseNameStatus(raw) {
		if change.status != deletedStatus {
			written = append(written, change.path)
			continue
		}
		if err := removeInside(dir, change.path); err != nil {
			return err
		}
	}
	if len(written) == 0 {
		return nil
	}
	return withTempIndex(func(index string) error {
		env := indexEnv(index)
		if _, err := git(ctx, dir, env, nil, "read-tree", toTree); err != nil {
			return err
		}
		stdin := bytes.Join(pathsWithNul(written), nil)
		_, err := git(ctx, dir, env, bytes.NewReader(stdin), "checkout-index", "-f", "-z", "--stdin")
		return err
	})
}

func snapshotLocked(ctx context.Context, dir, message string) (string, error) {
	tree, err := snapshotTree(ctx, dir)
	if err != nil {
		return "", err
	}
	return commitTree(ctx, dir, tree, message)
}

func snapshotTree(ctx context.Context, dir string) (string, error) {
	var tree string
	err := withTempIndex(func(index string) error {
		env := indexEnv(index)
		if _, err := git(ctx, dir, env, nil, "add", "-A"); err != nil {
			return err
		}
		out, err := git(ctx, dir, env, nil, "write-tree")
		if err != nil {
			return err
		}
		tree = strings.TrimSpace(string(out))
		return nil
	})
	return tree, err
}

func commitTree(ctx context.Context, dir, tree, message string) (string, error) {
	env := []string{
		"GIT_AUTHOR_NAME=" + identityName, "GIT_AUTHOR_EMAIL=" + identityMail,
		"GIT_COMMITTER_NAME=" + identityName, "GIT_COMMITTER_EMAIL=" + identityMail,
	}
	out, err := git(ctx, dir, env, nil, "commit-tree", tree, "-m", message)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func treeOf(ctx context.Context, dir, commit string) (string, error) {
	out, err := git(ctx, dir, nil, nil, "rev-parse", commit+"^{tree}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func listLocked(ctx context.Context, dir, session string) ([]Entry, error) {
	prefix := checkpointRefRoot + session + "/"
	out, err := git(ctx, dir, nil, nil, "for-each-ref", "--format="+entryFormat, prefix)
	if err != nil {
		return nil, err
	}
	entries := parseEntries(out, prefix)
	slices.SortFunc(entries, func(a, b Entry) int { return cmp.Compare(a.Index, b.Index) })
	return entries, nil
}

func resolveRef(ctx context.Context, dir, ref string) (string, error) {
	out, err := git(ctx, dir, nil, nil, "for-each-ref", "--format=%(objectname)", ref)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func checkpointRef(session string, index int) string {
	return checkpointRefRoot + session + "/" + strconv.Itoa(index)
}

func nextIndex(entries []Entry) int {
	if len(entries) == 0 {
		return 1
	}
	return entries[len(entries)-1].Index + 1
}

func indexEnv(index string) []string {
	return []string{"GIT_INDEX_FILE=" + index}
}

func git(ctx context.Context, dir string, env []string, stdin io.Reader, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = stdin
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %s: %w", args[0], strings.TrimSpace(stderr.String()), err)
	}
	return stdout.Bytes(), nil
}

func withTempIndex(run func(index string) error) error {
	dir, err := os.MkdirTemp("", tempDirPattern)
	if err != nil {
		return err
	}
	return errors.Join(run(filepath.Join(dir, indexFileName)), os.RemoveAll(dir))
}

func removeInside(dir, rel string) error {
	if !filepath.IsLocal(rel) {
		return fmt.Errorf("%w: %q", ErrOutsideWorktree, rel)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	removeErr := root.Remove(rel)
	if errors.Is(removeErr, os.ErrNotExist) {
		removeErr = nil
	}
	return errors.Join(removeErr, root.Close())
}

func validSession(session string) error {
	if session == "" || strings.ContainsAny(session, "/\x00") || session == "." || session == ".." {
		return fmt.Errorf("%w: %q", ErrBadSession, session)
	}
	return nil
}

func commitMessage(meta Meta) string {
	subject := strings.Join(strings.Fields(meta.Text), " ")
	if subject == "" {
		subject = emptyTurnSubject
	}
	return subject + "\n\n" + turnTrailer + ": " + meta.Turn + "\n" + conversationTrailer + ": " + meta.Conversation + "\n"
}

func parseEntries(out []byte, prefix string) []Entry {
	var entries []Entry
	for _, record := range strings.Split(string(out), "\n") {
		fields := strings.Split(record, "\x00")
		if len(fields) < entryFieldCount {
			continue
		}
		index, err := strconv.Atoi(strings.TrimPrefix(fields[0], prefix))
		if err != nil {
			continue
		}
		unix, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil {
			continue
		}
		entries = append(entries, Entry{
			Index:        index,
			Commit:       fields[1],
			Text:         fields[3],
			Turn:         strings.TrimSpace(fields[4]),
			Conversation: strings.TrimSpace(fields[5]),
			At:           time.Unix(unix, 0).UTC(),
		})
	}
	return entries
}

type statusChange struct {
	status string
	path   string
}

func parseNameStatus(raw []byte) []statusChange {
	fields := strings.Split(string(raw), "\x00")
	var changes []statusChange
	for offset := 0; offset+1 < len(fields); offset += nameStatusStride {
		status := fields[offset]
		if status == "" {
			continue
		}
		changes = append(changes, statusChange{status: status[:1], path: fields[offset+1]})
	}
	return changes
}

func parseNumstat(raw []byte) []Change {
	fields := strings.Split(string(raw), "\x00")
	var changes []Change
	for _, field := range fields {
		parts := strings.SplitN(field, "\t", numstatFieldCount)
		if len(parts) != numstatFieldCount {
			continue
		}
		added, addedOK := parseCount(parts[0])
		removed, removedOK := parseCount(parts[1])
		if !addedOK || !removedOK {
			continue
		}
		changes = append(changes, Change{Path: parts[2], Added: added, Removed: removed})
	}
	return changes
}

func parseCount(text string) (int, bool) {
	if text == binaryCount {
		return 0, true
	}
	count, err := strconv.Atoi(text)
	return count, err == nil
}

func pathsWithNul(paths []string) [][]byte {
	out := make([][]byte, 0, len(paths))
	for _, path := range paths {
		out = append(out, []byte(path+"\x00"))
	}
	return out
}
