package tui

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	draftBeatFlash = "signe de vie du brouillon interrompu : "
	fallbackEditor = "vi"
	draftFile      = "draft-*.md"
	draftRunPrefix = "run-"
	draftLockName  = ".lock"
	draftDirMode   = 0o700
	draftLockMode  = 0o600
	draftRetention = 24 * time.Hour
	draftHeartbeat = time.Hour
)

var errUnsafeDraftDir = errors.New("dossier des brouillons ouvert à d'autres ou aux droits trop larges")

type editorDoneMsg struct {
	path string
	err  error
}

type draftRun struct {
	dir      string
	root     *os.Root
	lock     *os.File
	touch    func(*os.Root) error
	cancel   context.CancelFunc
	done     chan struct{}
	failures chan error
	once     sync.Once
	mu       sync.Mutex
	beatErr  error
}

type draftBeatFailedMsg struct {
	err error
}

func configuredEditor(env func(string) string) []string {
	if fields := strings.Fields(env("EDITOR")); len(fields) > 0 {
		return fields
	}
	return strings.Fields(env("VISUAL"))
}

func editorArgv(path string) []string {
	fields := configuredEditor(os.Getenv)
	if len(fields) == 0 {
		fields = []string{fallbackEditor}
	}
	return append(fields, path)
}

func editorCommand(ctx context.Context, path string) *exec.Cmd {
	argv := editorArgv(path)
	return exec.CommandContext(ctx, argv[0], argv[1:]...)
}

func draftsRoot() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cache, "herrscher", "drafts"), nil
}

func openDraftRun(parent context.Context, rootPath string) (*draftRun, error) {
	return startDraftRun(parent, rootPath, draftHeartbeat, touchDraftLock)
}

func startDraftRun(parent context.Context, rootPath string, every time.Duration, touch func(*os.Root) error) (*draftRun, error) {
	if err := os.MkdirAll(rootPath, draftDirMode); err != nil {
		return nil, err
	}
	if err := checkPrivateDir(rootPath); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(rootPath, draftRunPrefix)
	if err != nil {
		return nil, err
	}
	run, err := claimDraftRun(dir)
	if err != nil {
		return nil, errors.Join(err, os.RemoveAll(dir))
	}
	ctx, cancel := context.WithCancel(parent)
	run.cancel = cancel
	run.touch = touch
	go run.keepAlive(ctx, every)
	return run, nil
}

func checkPrivateDir(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !privateDir(info) {
		return fmt.Errorf("%w: %s", errUnsafeDraftDir, dir)
	}
	return nil
}

func writeDraftFile(dir, text string) (string, error) {
	file, err := os.CreateTemp(dir, draftFile)
	if err != nil {
		return "", err
	}
	_, writeErr := file.WriteString(text)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return "", errors.Join(err, os.Remove(file.Name()))
	}
	return file.Name(), nil
}

func sweepDraftsAtStart() error {
	root, err := draftsRoot()
	if err != nil {
		return err
	}
	return sweepDrafts(root)
}

func sweepDrafts(rootPath string) error {
	root, err := os.OpenRoot(rootPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return errors.Join(err, root.Close())
	}
	now := time.Now()
	var failures []error
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), draftRunPrefix) || !sweepable(root, entry.Name(), now) {
			continue
		}
		if err := root.RemoveAll(entry.Name()); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(errors.Join(failures...), root.Close())
}

func sweepable(root *os.Root, name string, now time.Time) bool {
	info, err := root.Lstat(name)
	if err != nil || !privateDir(info) {
		return false
	}
	if now.Sub(lastSignOfLife(root, name, info)) < draftRetention {
		return false
	}
	return !runDirHeld(root, name)
}

func lastSignOfLife(root *os.Root, name string, dirInfo os.FileInfo) time.Time {
	latest := dirInfo.ModTime()
	if lock, err := root.Lstat(filepath.Join(name, draftLockName)); err == nil && lock.ModTime().After(latest) {
		latest = lock.ModTime()
	}
	return latest
}

func claimDraftRun(dir string) (*draftRun, error) {
	if err := checkPrivateDir(dir); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	lock, err := acquireDraftLock(root)
	if err != nil {
		return nil, errors.Join(err, root.Close())
	}
	return &draftRun{dir: dir, root: root, lock: lock, done: make(chan struct{}), failures: make(chan error, 1)}, nil
}

func (r *draftRun) keepAlive(ctx context.Context, every time.Duration) {
	defer close(r.done)
	defer close(r.failures)
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.recordBeat(r.touch(r.root))
		}
	}
}

func (r *draftRun) recordBeat(err error) {
	r.mu.Lock()
	wasFailing := r.beatErr != nil
	r.beatErr = err
	r.mu.Unlock()
	if err == nil || wasFailing {
		return
	}
	select {
	case r.failures <- err:
	default:
	}
}

func (r *draftRun) heartbeatError() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.beatErr
}

func (r *draftRun) watchBeat() tea.Cmd {
	return func() tea.Msg {
		err, ok := <-r.failures
		if !ok {
			return nil
		}
		return draftBeatFailedMsg{err: err}
	}
}

func touchDraftLock(root *os.Root) error {
	now := time.Now()
	return root.Chtimes(draftLockName, now, now)
}

func (r *draftRun) release() error {
	var err error
	r.once.Do(func() {
		err = r.closeRun()
	})
	return err
}

func (r *draftRun) closeRun() error {
	r.cancel()
	<-r.done
	var lockErr error
	if r.lock != nil {
		lockErr = r.lock.Close()
	}
	return errors.Join(r.heartbeatError(), lockErr, r.root.Close())
}

func (m *model) releaseDrafts() error {
	run := m.draftFiles
	m.draftFiles = nil
	if run == nil {
		return nil
	}
	return run.release()
}

func (m *model) openEditor() tea.Cmd {
	path, err := m.writeDraft(m.input.Value())
	if err != nil {
		m.flash = "éditeur indisponible : " + err.Error()
		return m.armDraftBeatWatch()
	}
	if m.draftFiles != nil {
		if beatErr := m.draftFiles.heartbeatError(); beatErr != nil {
			m.flash = draftBeatFlash + beatErr.Error()
		}
	}
	m.editing = true
	return tea.Batch(m.armDraftBeatWatch(), tea.ExecProcess(editorCommand(m.runContext(), path), func(err error) tea.Msg {
		return editorDoneMsg{path: path, err: err}
	}))
}

func (m *model) armDraftBeatWatch() tea.Cmd {
	if m.draftFiles == nil || m.draftBeatWatched {
		return nil
	}
	m.draftBeatWatched = true
	return m.draftFiles.watchBeat()
}

func (m *model) watchDraftBeat() tea.Cmd {
	if m.draftFiles == nil {
		return nil
	}
	return m.draftFiles.watchBeat()
}

func (m *model) writeDraft(text string) (string, error) {
	if m.draftFiles == nil {
		root, err := draftsRoot()
		if err != nil {
			return "", err
		}
		run, err := openDraftRun(m.runContext(), root)
		if err != nil {
			return "", err
		}
		m.draftFiles = run
	}
	return writeDraftFile(m.draftFiles.dir, text)
}

func (m *model) removeDraft(path string) {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		m.flash = "brouillon non supprimé : " + err.Error()
	}
}

func (m *model) applyEditorResult(msg editorDoneMsg) {
	m.editing = false
	defer m.removeDraft(msg.path)
	if msg.err != nil {
		m.flash = "éditeur fermé en erreur : " + msg.err.Error()
		return
	}
	data, err := os.ReadFile(msg.path)
	if err != nil {
		m.flash = "relecture impossible : " + err.Error()
		return
	}
	m.input.SetValue(strings.TrimRight(string(data), "\n"))
	m.input.CursorEnd()
	m.resizeComposer()
	m.applySize()
}
