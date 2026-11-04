package checkpoint

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	checkpointRefRoot = "refs/herrscher/checkpoints/"
	undoRefRoot       = "refs/herrscher/undo/"
	turnTrailer       = "Herrscher-Turn"
	entryFormat       = "%(refname)%00%(objectname)%00%(creatordate:unix)%00%(contents:subject)%00%(trailers:key=" + turnTrailer + ",valueonly,separator=%x20)"
	identityName      = "herrscher"
	identityMail      = "checkpoint@herrscher.invalid"
)

var (
	ErrNoCheckpoint = errors.New("no checkpoint")
	ErrNoUndo       = errors.New("nothing to undo")
	ErrBadSession   = errors.New("invalid session name")

	gitMu sync.Mutex
)

type Meta struct {
	Turn string
	Text string
}

type Entry struct {
	Index  int
	Commit string
	Turn   string
	Text   string
	At     time.Time
}

type Change struct {
	Path    string
	Added   int
	Removed int
}

func Take(ctx context.Context, dir, session string, meta Meta) (Entry, error) {
	if err := validSession(session); err != nil {
		return Entry{}, err
	}
	gitMu.Lock()
	defer gitMu.Unlock()
	entries, err := listLocked(ctx, dir, session)
	if err != nil {
		return Entry{}, err
	}
	next := 1
	if len(entries) > 0 {
		next = entries[len(entries)-1].Index + 1
	}
	commit, err := snapshotLocked(ctx, dir, commitMessage(meta))
	if err != nil {
		return Entry{}, err
	}
	ref := checkpointRefRoot + session + "/" + strconv.Itoa(next)
	if _, err := git(ctx, dir, nil, nil, "update-ref", ref, commit); err != nil {
		return Entry{}, err
	}
	return Entry{Index: next, Commit: commit, Turn: meta.Turn, Text: meta.Text, At: time.Now().UTC()}, nil
}

func List(ctx context.Context, dir, session string) ([]Entry, error) {
	if err := validSession(session); err != nil {
		return nil, err
	}
	gitMu.Lock()
	defer gitMu.Unlock()
	return listLocked(ctx, dir, session)
}

func Get(ctx context.Context, dir, session string, index int) (Entry, error) {
	entries, err := List(ctx, dir, session)
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

func Current(ctx context.Context, dir string) (string, error) {
	gitMu.Lock()
	defer gitMu.Unlock()
	return snapshotTree(ctx, dir)
}

func Changes(ctx context.Context, dir, from, to string) ([]Change, error) {
	out, err := git(ctx, dir, nil, nil, "diff-tree", "-r", "-z", "--numstat", "--no-renames", from, to)
	if err != nil {
		return nil, err
	}
	return parseNumstat(out), nil
}

func Restore(ctx context.Context, dir, session string, index int) (Entry, error) {
	target, err := Get(ctx, dir, session, index)
	if err != nil {
		return Entry{}, err
	}
	gitMu.Lock()
	defer gitMu.Unlock()
	if err := restoreLocked(ctx, dir, session, undoRefRoot+session, target.Commit); err != nil {
		return Entry{}, err
	}
	return target, nil
}

func Undo(ctx context.Context, dir, session string) error {
	if err := validSession(session); err != nil {
		return err
	}
	gitMu.Lock()
	defer gitMu.Unlock()
	undoRef := undoRefRoot + session
	previous, err := resolveRef(ctx, dir, undoRef)
	if err != nil {
		return err
	}
	if previous == "" {
		return fmt.Errorf("%w: %s", ErrNoUndo, session)
	}
	return restoreLocked(ctx, dir, session, undoRef, previous)
}

func HasUndo(ctx context.Context, dir, session string) bool {
	if validSession(session) != nil {
		return false
	}
	gitMu.Lock()
	defer gitMu.Unlock()
	ref, err := resolveRef(ctx, dir, undoRefRoot+session)
	return err == nil && ref != ""
}

func restoreLocked(ctx context.Context, dir, session, undoRef, target string) error {
	current, err := snapshotLocked(ctx, dir, "herrscher: état avant restauration")
	if err != nil {
		return err
	}
	if _, err := git(ctx, dir, nil, nil, "update-ref", undoRef, current); err != nil {
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

func applyTree(ctx context.Context, dir, from, to string) error {
	raw, err := git(ctx, dir, nil, nil, "diff-tree", "-r", "-z", "--name-status", "--no-renames", from, to)
	if err != nil {
		return err
	}
	var written []string
	for _, change := range parseNameStatus(raw) {
		if change.status == "D" {
			if err := removeInside(dir, change.path); err != nil {
				return err
			}
			continue
		}
		written = append(written, change.path)
	}
	if len(written) == 0 {
		return nil
	}
	index, cleanup, err := tempIndex()
	if err != nil {
		return err
	}
	defer cleanup()
	env := []string{"GIT_INDEX_FILE=" + index}
	if _, err := git(ctx, dir, env, nil, "read-tree", to); err != nil {
		return err
	}
	stdin := bytes.Join(pathsWithNul(written), nil)
	_, err = git(ctx, dir, env, bytes.NewReader(stdin), "checkout-index", "-f", "-z", "--stdin")
	return err
}

func snapshotLocked(ctx context.Context, dir, message string) (string, error) {
	tree, err := snapshotTree(ctx, dir)
	if err != nil {
		return "", err
	}
	return commitTree(ctx, dir, tree, message)
}

func snapshotTree(ctx context.Context, dir string) (string, error) {
	index, cleanup, err := tempIndex()
	if err != nil {
		return "", err
	}
	defer cleanup()
	env := []string{"GIT_INDEX_FILE=" + index}
	if _, err := git(ctx, dir, env, nil, "add", "-A"); err != nil {
		return "", err
	}
	out, err := git(ctx, dir, env, nil, "write-tree")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
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
	sort.Slice(entries, func(i, j int) bool { return entries[i].Index < entries[j].Index })
	return entries, nil
}

func resolveRef(ctx context.Context, dir, ref string) (string, error) {
	out, err := git(ctx, dir, nil, nil, "for-each-ref", "--format=%(objectname)", ref)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
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

func tempIndex() (string, func(), error) {
	dir, err := os.MkdirTemp("", "herrscher-checkpoint-")
	if err != nil {
		return "", nil, err
	}
	return filepath.Join(dir, "index"), func() { _ = os.RemoveAll(dir) }, nil
}

func removeInside(dir, rel string) error {
	if !filepath.IsLocal(rel) {
		return fmt.Errorf("checkpoint: path outside worktree: %q", rel)
	}
	err := os.Remove(filepath.Join(dir, rel))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
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
		subject = "(message vide)"
	}
	return subject + "\n\n" + turnTrailer + ": " + meta.Turn + "\n"
}

func parseEntries(out []byte, prefix string) []Entry {
	var entries []Entry
	for _, record := range strings.Split(string(out), "\n") {
		fields := strings.Split(record, "\x00")
		if len(fields) < 5 {
			continue
		}
		index, err := strconv.Atoi(strings.TrimPrefix(fields[0], prefix))
		if err != nil {
			continue
		}
		unix, _ := strconv.ParseInt(fields[2], 10, 64)
		entries = append(entries, Entry{
			Index:  index,
			Commit: fields[1],
			Text:   fields[3],
			Turn:   strings.TrimSpace(fields[4]),
			At:     time.Unix(unix, 0).UTC(),
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
	for i := 0; i+1 < len(fields); i += 2 {
		status := fields[i]
		if status == "" {
			continue
		}
		changes = append(changes, statusChange{status: status[:1], path: fields[i+1]})
	}
	return changes
}

func parseNumstat(raw []byte) []Change {
	fields := strings.Split(string(raw), "\x00")
	var changes []Change
	for _, field := range fields {
		parts := strings.SplitN(field, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		added, _ := strconv.Atoi(parts[0])
		removed, _ := strconv.Atoi(parts[1])
		changes = append(changes, Change{Path: parts[2], Added: added, Removed: removed})
	}
	return changes
}

func pathsWithNul(paths []string) [][]byte {
	out := make([][]byte, 0, len(paths))
	for _, path := range paths {
		out = append(out, []byte(path+"\x00"))
	}
	return out
}

func Materialize(ctx context.Context, dir, commit string) error {
	gitMu.Lock()
	defer gitMu.Unlock()
	from, err := snapshotTree(ctx, dir)
	if err != nil {
		return err
	}
	to, err := treeOf(ctx, dir, commit)
	if err != nil {
		return err
	}
	return applyTree(ctx, dir, from, to)
}

func Head(ctx context.Context, dir string) (string, error) {
	out, err := git(ctx, dir, nil, nil, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
