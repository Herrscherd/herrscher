package manager

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

type sentMsg struct{ channelID, content string }

type fakeChannelAdmin struct {
	created    []string
	parents    []string
	archived   []string
	homeType   string
	kinds      map[string]string
	kindErr    error
	idPrefix   string
	sent       []sentMsg
	sendErr    error
	archiveErr error
}

func (f *fakeChannelAdmin) Kind(_ context.Context, id string) (string, error) {
	if f.kindErr != nil {
		return "", f.kindErr
	}
	if k, ok := f.kinds[id]; ok {
		return k, nil
	}
	return f.homeType, nil
}

func (f *fakeChannelAdmin) CreateUnder(_ context.Context, parentID, name string) (string, error) {
	f.created = append(f.created, name)
	f.parents = append(f.parents, parentID)
	prefix := f.idPrefix
	if prefix == "" {
		prefix = "new-"
	}
	return prefix + name, nil
}

func (f *fakeChannelAdmin) ForumPost(_ context.Context, forumID, name, _ string) (string, error) {
	f.created = append(f.created, "forum:"+name)
	f.parents = append(f.parents, forumID)
	return "post-" + name, nil
}

func (f *fakeChannelAdmin) Archive(_ context.Context, id string) error {
	if f.archiveErr != nil {
		return f.archiveErr
	}
	f.archived = append(f.archived, id)
	return nil
}

func (f *fakeChannelAdmin) Send(_ context.Context, channelID, content string) error {
	if f.sendErr != nil {
		return f.sendErr
	}
	f.sent = append(f.sent, sentMsg{channelID: channelID, content: content})
	return nil
}

func (f *fakeChannelAdmin) ChannelRef(id string) string { return "REF(" + id + ")" }

type fakeSup struct {
	started, stopped []string
	startedCmds      []string
	restarted        []state.Session
	stopErrs         []error
}

func (f *fakeSup) StartWithin(s state.Session, _ time.Duration) error {
	f.started = append(f.started, s.Name)
	f.startedCmds = append(f.startedCmds, s.Cmd)
	return nil
}

func (f *fakeSup) StopWithin(name string, _ time.Duration) error {
	f.stopped = append(f.stopped, name)
	if len(f.stopErrs) > 0 {
		err := f.stopErrs[0]
		f.stopErrs = f.stopErrs[1:]
		return err
	}
	return nil
}

func (f *fakeSup) RestartWithin(s state.Session, grace time.Duration) error {
	f.restarted = append(f.restarted, s)
	if err := f.StopWithin(s.Name, grace); err != nil {
		return err
	}
	return f.StartWithin(s, grace)
}

type fakeWT struct {
	createdRepos []string
	created      []string
	removed      []string
	path         string
	removeErr    error
	createdBase  string
	preExisting  bool
	scratched    []string
	scratchErr   error
	removedScrap []string
	scratchBase  string
}

func (f *fakeWT) Create(_ context.Context, repo, name, base string) (string, error) {
	f.createdRepos = append(f.createdRepos, repo)
	f.created = append(f.created, name)
	f.createdBase = base
	return f.path, nil
}

func (f *fakeWT) Path(repo, name string) string {
	return filepath.Join(repo, ".herrscher-sessions", "inst", name)
}

func (f *fakeWT) PreExisting(context.Context, string, string) bool { return f.preExisting }
func (f *fakeWT) Branch(name string) string                        { return "session/inst/" + name }
func (f *fakeWT) BranchAt(context.Context, string) (string, error) {
	return "", errors.New("no git in the fake")
}

func (f *fakeWT) RemoveAt(_ context.Context, _, path string, force bool) error {
	if f.removeErr != nil && !force {
		return f.removeErr
	}
	f.removed = append(f.removed, path)
	return nil
}

func (f *fakeWT) Scratch(root, name string) (string, error) {
	if f.scratchErr != nil {
		return "", f.scratchErr
	}
	f.scratched = append(f.scratched, name)
	p := f.ScratchPath(root, name)
	if err := os.MkdirAll(p, 0o700); err != nil {
		return "", err
	}
	return p, nil
}

func (f *fakeWT) ScratchPath(root, name string) string {
	if f.scratchBase != "" {
		root = f.scratchBase
	}
	return filepath.Join(root, ".herrscher-scratch", name)
}

func (f *fakeWT) RemoveScratchAt(root, path string) error {
	name := filepath.Base(path)
	f.removedScrap = append(f.removedScrap, name)
	return os.RemoveAll(f.ScratchPath(root, name))
}

type fakeForge struct {
	cloneDir string
	cloneErr error
	cloned   []string
}

func (f *fakeForge) Clone(_ context.Context, spec, _ string) (string, error) {
	f.cloned = append(f.cloned, spec)
	if f.cloneErr != nil {
		return "", f.cloneErr
	}
	return f.cloneDir, nil
}

type fakeUpdater struct {
	version    string
	buildErr   error
	restartErr error
	builds     []bool
	restarts   int
}

func (f *fakeUpdater) Build(_ context.Context, pull bool) (string, error) {
	f.builds = append(f.builds, pull)
	return f.version, f.buildErr
}
func (f *fakeUpdater) Restart(_ context.Context) error { f.restarts++; return f.restartErr }

const testInstance = "inst"

func newTestHandler(t *testing.T, homeType string) (*Handler, *fakeChannelAdmin, *fakeSup, *fakeWT, *fakeForge, *state.State) {
	t.Helper()
	h, _, d, sup, wt, fg, st := newTestHandlerWithUpdater(t, homeType)
	return h, d, sup, wt, fg, st
}

func newTestHandlerWithUpdater(t *testing.T, homeType string) (*Handler, *fakeUpdater, *fakeChannelAdmin, *fakeSup, *fakeWT, *fakeForge, *state.State) {
	t.Helper()
	d := &fakeChannelAdmin{homeType: homeType}
	sup := &fakeSup{}
	wt := &fakeWT{path: "/wt/x", scratchBase: t.TempDir()}
	fg := &fakeForge{}
	up := &fakeUpdater{version: "abc1234"}
	st := state.NewState(t.TempDir() + "/s.json")
	st.InstanceID = testInstance
	agents := agent.NewStore(t.TempDir())
	return NewHandler(clitext.NewMessages(), d, sup, wt, fg, up, agents, st, "claude", t.TempDir(), []string{"chat"}, nil, nil), up, d, sup, wt, fg, st
}

func args(kv ...string) contracts.Input {
	m := make(map[string]string)
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return contracts.Input{Args: m}
}
