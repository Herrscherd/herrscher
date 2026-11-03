package manager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
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

func TestChannelAdminInterfaceHasSend(_ *testing.T) {
	var _ channelAdmin = (*fakeChannelAdmin)(nil)
}

func TestSessionListUsesGatewayChannelRef(t *testing.T) {
	h, d, _, _, _, st := newTestHandler(t, "category")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo")); err != nil {
		t.Fatal(err)
	}
	out, err := h.sessionListRun(context.Background(), contracts.Input{})
	if err != nil {
		t.Fatal(err)
	}
	want := d.ChannelRef("new-demo")
	if !strings.Contains(out, want) {
		t.Fatalf("list output %q must render the gateway channel ref %q", out, want)
	}
}

func TestSessionListJSON(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "category")
	st.Sessions = []state.Session{
		{Name: "alpha", Agent: "roblox", Parent: ""},
		{Name: "beta", Agent: "roblox", Parent: "alpha"},
	}
	out, err := h.sessionListRun(context.Background(), contracts.Input{JSON: true})
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v (%q)", err, out)
	}
	if len(got) != 2 || got[1]["parent"] != "alpha" {
		t.Fatalf("unexpected: %v", got)
	}
	if got[0]["id"] != "alpha" || got[1]["id"] != "beta" {
		t.Fatalf("id must equal name: %v", got)
	}
	if _, present := got[0]["parent"]; present {
		t.Fatalf("root session must omit parent, got: %v", got[0])
	}
}

func TestSessionListAggregatesUsage(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "category")
	st.Sessions = []state.Session{{Name: "alpha", Agent: "roblox"}}
	p := state.TranscriptPath(h.PartDir(), "alpha")
	for _, e := range []state.TranscriptEntry{
		{Ts: "2026-07-26T00:00:00Z", Role: "user", Text: "build a tower"},
		{Ts: "2026-07-26T00:00:05Z", Role: "assistant", Text: "done", Cost: 0.02, TokensIn: 30, TokensOut: 55, CacheRead: 12, CacheCreate: 3, DurMs: 4200},
		{Ts: "2026-07-26T00:01:00Z", Role: "user", Text: "now add a door"},
		{Ts: "2026-07-26T00:01:08Z", Role: "assistant", Text: "ok", Cost: 0.03, TokensIn: 40, TokensOut: 60, CacheRead: 5, CacheCreate: 1, DurMs: 8000},
	} {
		if err := state.AppendTranscript(p, e); err != nil {
			t.Fatal(err)
		}
	}
	out, err := h.sessionListRun(context.Background(), contracts.Input{JSON: true})
	if err != nil {
		t.Fatal(err)
	}
	var rows []sessionJSON
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("not JSON: %v (%q)", err, out)
	}
	if len(rows) != 1 || rows[0].Usage == nil {
		t.Fatalf("want usage present: %+v", rows)
	}
	u := rows[0].Usage
	if u.Turns != 2 || u.TokensOut != 115 || u.TokensIn != 70 || u.CacheRead != 17 || u.CacheCreate != 4 {
		t.Fatalf("aggregate wrong: %+v", u)
	}
	if u.Cost < 0.049 || u.Cost > 0.051 {
		t.Fatalf("cost aggregate wrong: %v", u.Cost)
	}
	if u.StartedAt != "2026-07-26T00:00:00Z" {
		t.Fatalf("started_at wrong: %q", u.StartedAt)
	}
	if rows[0].Task != "now add a door" {
		t.Fatalf("task should be last user text, got: %q", rows[0].Task)
	}
}

type fakeCoordReader struct{ views map[string]CoordView }

func (f fakeCoordReader) CoordinationView(name string) (CoordView, bool) {
	v, ok := f.views[name]
	return v, ok
}

func TestSessionListEmitsCoordination(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "category")
	st.Sessions = []state.Session{
		{Name: "lead1", Agent: "roblox"},
		{Name: "solo1", Agent: "roblox"},
	}
	h.SetCoordinationReader(fakeCoordReader{views: map[string]CoordView{
		"lead1": {Role: "lead", Lead: "lead1", Reported: 2, Expected: 3, Complete: false},
	}})
	out, err := h.sessionListRun(context.Background(), contracts.Input{JSON: true})
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("not JSON: %v (%q)", err, out)
	}
	byName := make(map[string]map[string]any)
	for _, r := range rows {
		name, ok := r["name"].(string)
		if !ok {
			t.Fatalf("row without a name: %v", r)
		}
		byName[name] = r
	}
	coord, ok := byName["lead1"]["coordination"].(map[string]any)
	if !ok {
		t.Fatalf("lead1 should carry coordination, got %v", byName["lead1"])
	}
	if coord["role"] != "lead" || coord["reported"] != 2.0 || coord["expected"] != 3.0 {
		t.Fatalf("bad coordination: %v", coord)
	}
	if _, present := byName["solo1"]["coordination"]; present {
		t.Fatalf("solo1 must omit coordination: %v", byName["solo1"])
	}
}

func TestSessionListOmitsCoordinationWhenUnwired(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "category")
	st.Sessions = []state.Session{{Name: "s1", Agent: "roblox"}}
	out, err := h.sessionListRun(context.Background(), contracts.Input{JSON: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "coordination") {
		t.Fatalf("unwired handler must not emit coordination: %s", out)
	}
}

func TestSessionWhoJSON(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "category")
	st.Sessions = []state.Session{{Name: "alpha", Agent: "roblox", Parent: "lead"}}
	out, err := h.sessionWhoRun(context.Background(), contracts.Input{
		Args: map[string]string{"name": "alpha"}, JSON: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v (%q)", err, out)
	}
	if got["name"] != "alpha" || got["parent"] != "lead" {
		t.Fatalf("unexpected: %v", got)
	}
}

func TestSessionCreateUsesGatewayChannelRef(t *testing.T) {
	h, d, _, _, _, st := newTestHandler(t, "category")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	out, err := h.sessionCreateRun(context.Background(), args("name", "demo"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, d.ChannelRef("new-demo")) {
		t.Fatalf("create output %q must render the gateway channel ref", out)
	}
}

type fakeSup struct {
	started, stopped []string
	startedCmds      []string
	restarted        []state.Session
	stopErrs         []error
}

func (f *fakeSup) Start(s state.Session) {
	f.started = append(f.started, s.Name)
	f.startedCmds = append(f.startedCmds, s.Cmd)
}

func (f *fakeSup) Stop(name string) error {
	f.stopped = append(f.stopped, name)
	if len(f.stopErrs) > 0 {
		err := f.stopErrs[0]
		f.stopErrs = f.stopErrs[1:]
		return err
	}
	return nil
}

func (f *fakeSup) Restart(s state.Session) error {
	f.restarted = append(f.restarted, s)
	if err := f.Stop(s.Name); err != nil {
		return err
	}
	f.Start(s)
	return nil
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

func (f *fakeWT) PreExisting(context.Context, string, string) bool { return f.preExisting }
func (f *fakeWT) Branch(name string) string                        { return "session/" + name }
func (f *fakeWT) Remove(_ context.Context, _, name string, force bool) error {
	if f.removeErr != nil && !force {
		return f.removeErr
	}
	f.removed = append(f.removed, name)
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

func (f *fakeWT) RemoveScratch(root, name string) error {
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
	agents := agent.NewStore(t.TempDir())
	return NewHandler(d, sup, wt, fg, up, agents, st, "claude", t.TempDir(), []string{"chat"}), up, d, sup, wt, fg, st
}

func args(kv ...string) contracts.Input {
	m := make(map[string]string)
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return contracts.Input{Args: m}
}

func TestSessionCreateText(t *testing.T) {
	h, d, sup, wt, _, st := newTestHandler(t, "")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo")); err != nil {
		t.Fatal(err)
	}
	if len(d.created) != 1 || d.created[0] != "demo" {
		t.Fatalf("expected channel created: %+v", d.created)
	}
	if len(wt.created) != 1 {
		t.Fatalf("expected worktree created: %+v", wt.created)
	}
	if len(sup.started) != 1 {
		t.Fatalf("expected bridge started: %+v", sup.started)
	}
	sess, ok := st.FindSession("demo")
	if !ok || sess.Worktree != "/wt/x" {
		t.Fatalf("session not persisted with worktree: %+v", sess)
	}
	if sess.Backend != "stream" {
		t.Fatalf("expected default backend stream, got %q", sess.Backend)
	}
}

func TestSessionCreatePassesBaseToWorktree(t *testing.T) {
	h, _, _, wt, _, st := newTestHandler(t, "")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessionCreateRun(context.Background(), args("name", "beta", "base", "session/alpha")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if wt.createdBase != "session/alpha" {
		t.Fatalf("base not plumbed to worktree: %q", wt.createdBase)
	}
}

func TestSessionCreateCapturesProjectLabelInCwdMode(t *testing.T) {
	h, _, _, wt, _, st := newTestHandler(t, "")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessionCreateRun(context.Background(), args("name", "aaa", "project", "aaa")); err != nil {
		t.Fatalf("create: %v", err)
	}
	sess, ok := st.FindSession("aaa")
	if !ok {
		t.Fatalf("session aaa introuvable")
	}
	if sess.Project != "aaa" {
		t.Fatalf("project label not captured in cwd mode: %q", sess.Project)
	}
	if len(wt.createdRepos) != 1 || wt.createdRepos[0] != "" {
		t.Fatalf("worktree repo must stay cwd (empty) in cwd mode: %+v", wt.createdRepos)
	}
}

func TestSessionCreatePersistsParent(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessionCreateRun(context.Background(), args("name", "worker", "parent", "lead")); err != nil {
		t.Fatalf("create: %v", err)
	}
	sess, ok := st.FindSession("worker")
	if !ok {
		t.Fatalf("session worker introuvable")
	}
	if sess.Parent != "lead" {
		t.Fatalf("parent non persisté: %q", sess.Parent)
	}
}

func TestSessionCreateShared(t *testing.T) {
	h, _, _, wt, _, st := newTestHandler(t, "")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo", "shared", "true")); err != nil {
		t.Fatal(err)
	}
	if len(wt.created) != 0 {
		t.Fatalf("shared session should not create a worktree: %+v", wt.created)
	}
	sess, _ := st.FindSession("demo")
	if sess.Worktree != "" {
		t.Fatalf("shared session should have empty worktree: %+v", sess)
	}
}

func TestSessionCreateRejectsUnsafeName(t *testing.T) {
	for _, name := range []string{"..", "/", "---", "   ", "🙂", ""} {
		h, d, _, wt, _, st := newTestHandler(t, "")
		if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
			t.Fatal(err)
		}
		if _, err := h.sessionCreateRun(context.Background(), args("name", name)); err == nil {
			t.Fatalf("name %q: expected rejection", name)
		}
		if len(wt.created) != 0 || len(d.created) != 0 {
			t.Fatalf("name %q: nothing should be created on rejection (wt=%v ch=%v)", name, wt.created, d.created)
		}
		if _, ok := st.FindSession(name); ok {
			t.Fatalf("name %q: must not persist a session", name)
		}
	}
}

func TestSessionCreateSlugifiesName(t *testing.T) {
	cases := map[string]string{
		"../escape":   "escape",
		"a/b":         "a-b",
		"with space":  "with-space",
		"bad;rm":      "bad-rm",
		"cmd improve": "cmd-improve",
		"Feat/Login":  "feat-login",
	}
	for raw, want := range cases {
		h, _, _, _, _, st := newTestHandler(t, "")
		if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
			t.Fatal(err)
		}
		if _, err := h.sessionCreateRun(context.Background(), args("name", raw)); err != nil {
			t.Fatalf("name %q: %v", raw, err)
		}
		if _, ok := st.FindSession(want); !ok {
			t.Fatalf("name %q: expected session under slug %q, sessions=%+v", raw, want, st.SnapshotSessions())
		}
	}
}

func TestSessionCreateAcceptsSafeName(t *testing.T) {
	h, d, _, _, _, st := newTestHandler(t, "")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessionCreateRun(context.Background(), args("name", "feat_login-2")); err != nil {
		t.Fatalf("safe name should be accepted: %v", err)
	}
	if len(d.created) != 1 {
		t.Fatalf("safe name should be accepted: %+v", d.created)
	}
}

func TestSessionCreateRequiresHome(t *testing.T) {
	h, _, _, _, _, _ := newTestHandler(t, "")
	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo")); err == nil {
		t.Fatal("expected error when home unset")
	}
}

func TestSessionCreateForum(t *testing.T) {
	h, d, sup, _, _, st := newTestHandler(t, "forum")
	if err := st.SetHome(state.HomeRef{ID: "forum1", Type: "forum"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessionCreateRun(context.Background(), args("name", "topic")); err != nil {
		t.Fatal(err)
	}
	if len(d.created) != 1 || d.created[0] != "forum:topic" {
		t.Fatalf("expected forum post: %+v", d.created)
	}
	if len(sup.started) != 1 {
		t.Fatal("expected bridge started")
	}
}

func TestSessionCreateTerminalHome(t *testing.T) {
	h, d, _, _, _, st := newTestHandler(t, "terminal")
	if err := st.SetHome(state.HomeRef{ID: "term-home", Type: "terminal"}); err != nil {
		t.Fatal(err)
	}

	out, err := h.sessionCreateRun(context.Background(), args("name", "alpha", "terminal_only", "true", "shared", "true"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	sess, ok := st.FindSession("alpha")
	if !ok {
		t.Fatal("session not persisted")
	}
	if sess.Type != "text" || sess.ChannelID == "" {
		t.Fatalf("bad session: %+v", sess)
	}
	if strings.Contains(out, "<#") || strings.Contains(out, "<@") {
		t.Fatalf("output must be platform-neutral: %q", out)
	}
	if len(d.created) == 0 {
		t.Fatal("CreateUnder should have been called for terminal home")
	}
}

func TestSessionBanner(t *testing.T) {
	const repo = "/home/me/proj"
	cases := []struct {
		name     string
		worktree string
		branch   string
		shared   bool
		want     []string
		absent   []string
	}{
		{
			name:     "isolated",
			worktree: "/home/me/proj/.herrscher-sessions/demo",
			branch:   "session/demo",
			shared:   false,
			want: []string{
				"🚀 Session **demo** ready.",
				"Project: **proj** (`/home/me/proj`)",
				"Mode: isolated worktree",
				"Worktree: `/home/me/proj/.herrscher-sessions/demo`",
				"Branch: `session/demo`",
				"Command: `claude`",
			},
			absent: []string{"main checkout", "not a git repo"},
		},
		{
			name:     "shared main checkout",
			worktree: "",
			branch:   "session/demo",
			shared:   true,
			want: []string{
				"🚀 Session **demo** ready.",
				"Project: **proj** (`/home/me/proj`)",
				"Mode: shared (main checkout)",
				"Branch: none (runs on current branch)",
				"Command: `claude`",
			},
			absent: []string{"Worktree:", "isolated worktree", "not a git repo"},
		},
		{
			name:     "non-git shared",
			worktree: "",
			branch:   "session/demo",
			shared:   false,
			want: []string{
				"🚀 Session **demo** ready.",
				"Project: **proj** (`/home/me/proj`)",
				"Mode: shared (not a git repo)",
				"Command: `claude`",
			},
			absent: []string{"Worktree:", "Branch:", "isolated worktree", "main checkout"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sessionBanner(repo, "demo", tc.worktree, tc.branch, "claude", "", tc.shared)
			for _, s := range tc.want {
				if !strings.Contains(got, s) {
					t.Errorf("banner missing %q\n--- got ---\n%s", s, got)
				}
			}
			for _, s := range tc.absent {
				if strings.Contains(got, s) {
					t.Errorf("banner unexpectedly contains %q\n--- got ---\n%s", s, got)
				}
			}
		})
	}
}

func TestSessionBannerEmptyRepo(t *testing.T) {
	got := sessionBanner("", "demo", "", "session/demo", "claude", "", true)
	if !strings.Contains(got, "Project: **(cwd)**") {
		t.Errorf("empty repo should render (cwd), got:\n%s", got)
	}
	if strings.Contains(got, "**.**") || strings.Contains(got, "(``)") {
		t.Errorf("empty repo must not render misleading path, got:\n%s", got)
	}
}

func TestSessionBannerNamesTheRequestedModel(t *testing.T) {
	got := sessionBanner("/home/me/proj", "demo", "", "session/demo", "claude --model claude-opus-4-8", "claude-opus-5", true)
	if !strings.Contains(got, "Model: `claude-opus-5`") {
		t.Errorf("banner must name the requested model, got:\n%s", got)
	}
}

func TestSessionCreateUnderOverridesTheHome(t *testing.T) {
	h, d, _, _, _, st := newTestHandler(t, "category")
	if err := st.SetHome(state.HomeRef{ID: "home-cat", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	d.kinds = map[string]string{"other-cat": "category"}

	if _, err := h.sessionCreateRun(context.Background(), args("name", "here", "under", "other-cat", "shared", "true")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(d.parents) != 1 || d.parents[0] != "other-cat" {
		t.Fatalf("session must be created under the requested container, got %+v", d.parents)
	}
	if home := st.Home; home.ID != "home-cat" {
		t.Fatalf("--under must not move the home, got %+v", home)
	}
}

func TestSessionCreateUnderAForum(t *testing.T) {
	h, d, _, _, _, st := newTestHandler(t, "category")
	if err := st.SetHome(state.HomeRef{ID: "home-cat", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	d.kinds = map[string]string{"a-forum": "forum"}

	if _, err := h.sessionCreateRun(context.Background(), args("name", "post", "under", "a-forum", "shared", "true")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(d.created) != 1 || d.created[0] != "forum:post" {
		t.Fatalf("expected a forum post, got %+v", d.created)
	}
	if sess, ok := st.FindSession("post"); !ok || sess.Type != "forum" {
		t.Fatalf("session must record the forum type, got %+v", sess)
	}
}

func TestSessionCreateUnderRejectsAPlainChannel(t *testing.T) {
	h, d, _, _, _, st := newTestHandler(t, "category")
	if err := st.SetHome(state.HomeRef{ID: "home-cat", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	d.kinds = map[string]string{"a-chan": "text"}

	_, err := h.sessionCreateRun(context.Background(), args("name", "nope", "under", "a-chan", "shared", "true"))
	if err == nil {
		t.Fatal("expected a plain text channel to be refused")
	}
	if !strings.Contains(err.Error(), "channel_id") {
		t.Errorf("error should point at channel_id, got %q", err)
	}
	if _, ok := st.FindSession("nope"); ok {
		t.Error("no session may be persisted when under is invalid")
	}
}

func TestSessionCreateRefusesUnderWithTerminalOnly(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "category")
	if err := st.SetHome(state.HomeRef{ID: "home-cat", Type: "category"}); err != nil {
		t.Fatal(err)
	}

	_, err := h.sessionCreateRun(context.Background(), args("name", "clash", "under", "a-cat", "terminal_only", "true", "shared", "true"))
	if err == nil || !strings.Contains(err.Error(), "terminal_only") {
		t.Fatalf("err = %v, want the contradiction named", err)
	}
}

func TestSessionCreateRefusesUnderWithChannelID(t *testing.T) {
	h, d, _, _, _, st := newTestHandler(t, "category")
	if err := st.SetHome(state.HomeRef{ID: "home-cat", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	d.kinds = map[string]string{"a-cat": "category"}

	_, err := h.sessionCreateRun(context.Background(), args("name", "both", "under", "a-cat", "channel_id", "c1", "shared", "true"))
	if err == nil {
		t.Fatal("expected under + channel_id to be refused")
	}
}

type createBannerCase struct {
	name      string
	homeType  string
	homeRef   state.HomeRef
	wtPath    string
	shared    bool
	wantReply []string
	wantSend  []string
}

func TestSessionCreateBanner(t *testing.T) {
	cases := []createBannerCase{
		{
			name:     "category isolated",
			homeType: "",
			homeRef:  state.HomeRef{ID: "cat1", Type: "category"},
			wtPath:   "/wt/x",
			shared:   false,
			wantReply: []string{
				"✅ Running on REF(new-demo).",
				"Mode: isolated worktree",
				"Worktree: `/wt/x`",
				"Branch: `session/demo`",
				"Command: `claude`",
			},
			wantSend: []string{"Mode: isolated worktree", "Worktree: `/wt/x`"},
		},
		{
			name:     "category non-git shared",
			homeType: "",
			homeRef:  state.HomeRef{ID: "cat1", Type: "category"},
			wtPath:   "",
			shared:   false,
			wantReply: []string{
				"✅ Running on REF(new-demo).",
				"Mode: shared (not a git repo)",
			},
			wantSend: []string{"Mode: shared (not a git repo)"},
		},
		{
			name:     "category shared:true",
			homeType: "",
			homeRef:  state.HomeRef{ID: "cat1", Type: "category"},
			wtPath:   "/wt/x",
			shared:   true,
			wantReply: []string{
				"Mode: shared (main checkout)",
				"Branch: none (runs on current branch)",
			},
			wantSend: []string{"Mode: shared (main checkout)"},
		},
		{
			name:     "forum isolated",
			homeType: "forum",
			homeRef:  state.HomeRef{ID: "forum1", Type: "forum"},
			wtPath:   "/wt/x",
			shared:   false,
			wantReply: []string{
				"✅ Running on REF(post-demo).",
				"Mode: isolated worktree",
			},
			wantSend: []string{"Mode: isolated worktree"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertCreateBanner(t, tc)
		})
	}
}

func assertCreateBanner(t *testing.T, tc createBannerCase) {
	t.Helper()
	h, d, _, wt, _, st := newTestHandler(t, tc.homeType)
	wt.path = tc.wtPath
	if err := st.SetHome(tc.homeRef); err != nil {
		t.Fatal(err)
	}
	in := args("name", "demo")
	if tc.shared {
		in.Args["shared"] = "true"
	}
	reply, err := h.sessionCreateRun(context.Background(), in)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	for _, s := range tc.wantReply {
		if !strings.Contains(reply, s) {
			t.Errorf("reply missing %q\n--- got ---\n%s", s, reply)
		}
	}
	if len(d.sent) != 1 {
		t.Fatalf("expected exactly one in-channel Send, got %d: %+v", len(d.sent), d.sent)
	}
	sess, _ := st.FindSession("demo")
	if d.sent[0].channelID != sess.ChannelID {
		t.Errorf("Send went to %q, want session channel %q", d.sent[0].channelID, sess.ChannelID)
	}
	for _, s := range tc.wantSend {
		if !strings.Contains(d.sent[0].content, s) {
			t.Errorf("in-channel banner missing %q\n--- got ---\n%s", s, d.sent[0].content)
		}
	}
	if strings.Contains(d.sent[0].content, "Running on") {
		t.Errorf("in-channel banner must not carry the 'Running on' prefix:\n%s", d.sent[0].content)
	}
}

func TestSessionCreateSendFailureDoesNotFail(t *testing.T) {
	h, d, sup, _, _, st := newTestHandler(t, "")
	d.sendErr = errors.New("gateway 500")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	reply, err := h.sessionCreateRun(context.Background(), args("name", "demo"))
	if err != nil {
		t.Fatalf("create must still succeed when Send fails: %v", err)
	}
	if !strings.Contains(reply, "✅ Running on") {
		t.Fatalf("unexpected reply: %q", reply)
	}
	if _, ok := st.FindSession("demo"); !ok {
		t.Fatal("session must remain persisted despite Send failure")
	}
	if len(sup.started) != 1 {
		t.Fatal("bridge must have started")
	}
	if len(d.sent) != 0 {
		t.Fatalf("no send should be recorded on error, got %+v", d.sent)
	}
}

func TestSessionCloseStopsAndArchives(t *testing.T) {
	h, d, sup, wt, _, st := newTestHandler(t, "")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddSession(state.Session{Owned: true, Name: "demo", ChannelID: "ch9", Type: "text", Worktree: "/wt/x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessionCloseRun(context.Background(), args("name", "demo")); err != nil {
		t.Fatal(err)
	}
	if len(sup.stopped) != 1 || len(d.archived) != 1 || len(wt.removed) != 1 {
		t.Fatalf("expected stop+archive+wt-remove: %+v %+v %+v", sup.stopped, d.archived, wt.removed)
	}
	if _, ok := st.FindSession("demo"); ok {
		t.Fatal("session should be removed")
	}
}

func TestSessionCloseDirtyRefusedWithoutForce(t *testing.T) {
	h, d, _, wt, _, st := newTestHandler(t, "")
	wt.removeErr = errors.New(`worktree "demo" has uncommitted changes`)
	if err := st.AddSession(state.Session{Name: "demo", ChannelID: "ch9", Type: "text", Worktree: "/wt/x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessionCloseRun(context.Background(), args("name", "demo")); err == nil {
		t.Fatal("expected refusal")
	}
	if len(d.archived) != 0 {
		t.Fatal("must not archive when worktree removal refused")
	}
	if _, ok := st.FindSession("demo"); !ok {
		t.Fatal("session must survive a refused close")
	}
}

func TestSessionCloseDirtyForced(t *testing.T) {
	h, _, _, wt, _, st := newTestHandler(t, "")
	wt.removeErr = errors.New("dirty")
	if err := st.AddSession(state.Session{Name: "demo", ChannelID: "ch9", Type: "text", Worktree: "/wt/x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessionCloseRun(context.Background(), args("name", "demo", "force", "true")); err != nil {
		t.Fatal(err)
	}
	if len(wt.removed) != 1 {
		t.Fatalf("force should remove worktree: %+v", wt.removed)
	}
	if _, ok := st.FindSession("demo"); ok {
		t.Fatal("session should be removed after forced close")
	}
}

func TestSessionCreateUsesQualifiedTitle(t *testing.T) {
	tests := []struct {
		name       string
		instanceID string
		homeType   string
		setHome    state.HomeRef
		logical    string
		wantTitle  string
	}{
		{
			name:       "category-namespaced",
			instanceID: "alice",
			homeType:   "",
			setHome:    state.HomeRef{ID: "cat1", Type: "category"},
			logical:    "foo",
			wantTitle:  "alice__foo",
		},
		{
			name:       "forum-namespaced",
			instanceID: "bob",
			homeType:   "forum",
			setHome:    state.HomeRef{ID: "f1", Type: "forum"},
			logical:    "foo",
			wantTitle:  "forum:bob__foo",
		},
		{
			name:       "category-legacy",
			instanceID: "",
			homeType:   "",
			setHome:    state.HomeRef{ID: "cat1", Type: "category"},
			logical:    "foo",
			wantTitle:  "foo",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, d, _, _, _, st := newTestHandler(t, tt.homeType)
			st.InstanceID = tt.instanceID
			if err := st.SetHome(tt.setHome); err != nil {
				t.Fatal(err)
			}
			if _, err := h.sessionCreateRun(context.Background(), args("name", tt.logical)); err != nil {
				t.Fatal(err)
			}
			if len(d.created) != 1 || d.created[0] != tt.wantTitle {
				t.Fatalf("created titles = %+v, want [%q]", d.created, tt.wantTitle)
			}
			if _, ok := st.FindSession(tt.logical); !ok {
				t.Fatalf("session must be keyed by logical name %q", tt.logical)
			}
		})
	}
}

func TestSessionCloseOnlyTouchesOwnSession(t *testing.T) {
	h, d, sup, wt, _, st := newTestHandler(t, "")
	st.InstanceID = "bob"
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddSession(state.Session{Owned: true, Name: "foo", ChannelID: "bob-foo-ch", Type: "text", Worktree: "/wt/bob"}); err != nil {
		t.Fatal(err)
	}

	if _, err := h.sessionCloseRun(context.Background(), args("name", "foo")); err != nil {
		t.Fatal(err)
	}

	if len(d.archived) != 1 || d.archived[0] != "bob-foo-ch" {
		t.Fatalf("close must archive only bob's channel, got %+v", d.archived)
	}
	if len(sup.stopped) != 1 || sup.stopped[0] != "foo" {
		t.Fatalf("close must stop only the local logical session, got %+v", sup.stopped)
	}
	if len(wt.removed) != 1 {
		t.Fatalf("close must remove exactly bob's worktree, got %+v", wt.removed)
	}
	if _, ok := st.FindSession("foo"); ok {
		t.Fatal("bob's foo should be gone after close")
	}
}

func TestSessionCloseUnknownNameIsNoop(t *testing.T) {
	h, d, sup, _, _, st := newTestHandler(t, "")
	st.InstanceID = "bob"
	if _, err := h.sessionCloseRun(context.Background(), args("name", "alice-only")); err == nil {
		t.Fatal("expected error for unknown session")
	}
	if len(d.archived) != 0 || len(sup.stopped) != 0 {
		t.Fatalf("unknown close must be a no-op, got archived=%+v stopped=%+v", d.archived, sup.stopped)
	}
}

func TestSessionCreateUsesWorkspaceProject(t *testing.T) {
	h, _, _, wt, _, st := newTestHandler(t, "")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetWorkspace("/ws"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo", "project", "myproj")); err != nil {
		t.Fatal(err)
	}
	if len(wt.createdRepos) != 1 || wt.createdRepos[0] != "/ws/myproj" {
		t.Fatalf("expected Create on /ws/myproj, got %+v", wt.createdRepos)
	}
	sess, _ := st.FindSession("demo")
	if sess.Project != "myproj" {
		t.Fatalf("session.Project not persisted: %+v", sess)
	}
}

func TestSessionCreateNoProjectRootsAtWorkspaceRoot(t *testing.T) {
	h, _, _, wt, _, st := newTestHandler(t, "")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetWorkspace("/ws"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo")); err != nil {
		t.Fatalf("no-project create with workspace root should succeed, got: %v", err)
	}
	if len(wt.createdRepos) != 1 || wt.createdRepos[0] != "/ws" {
		t.Fatalf("expected Create rooted at /ws, got %+v", wt.createdRepos)
	}
}

func TestSessionCreateRejectsProjectTraversal(t *testing.T) {
	for _, p := range []string{"../escape", "a/b", "..", "with space"} {
		h, d, _, wt, _, st := newTestHandler(t, "")
		if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
			t.Fatal(err)
		}
		if err := st.SetWorkspace("/ws"); err != nil {
			t.Fatal(err)
		}
		if _, err := h.sessionCreateRun(context.Background(), args("name", "demo", "project", p)); err == nil {
			t.Fatalf("project %q: expected rejection", p)
		}
		if len(wt.created) != 0 || len(d.created) != 0 {
			t.Fatalf("project %q: nothing should be created", p)
		}
	}
}

func TestSessionCreateLegacyNoWorkspace(t *testing.T) {
	h, d, _, wt, _, st := newTestHandler(t, "")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo")); err != nil {
		t.Fatal(err)
	}
	if len(d.created) != 1 || len(wt.created) != 1 {
		t.Fatalf("legacy create should still work: ch=%v wt=%v", d.created, wt.created)
	}
}

func TestSessionCloseUsesProjectRepo(t *testing.T) {
	h, _, _, wt, _, st := newTestHandler(t, "")
	if err := st.SetWorkspace("/ws"); err != nil {
		t.Fatal(err)
	}
	if err := st.AddSession(state.Session{Name: "demo", ChannelID: "ch9", Type: "text", Worktree: "/ws/myproj/.herrscher-sessions/demo", Project: "myproj"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessionCloseRun(context.Background(), args("name", "demo")); err != nil {
		t.Fatal(err)
	}
	if len(wt.removed) != 1 {
		t.Fatalf("expected worktree removed: %+v", wt.removed)
	}
}

func TestSessionCreateClonesThenUsesProject(t *testing.T) {
	h, _, _, wt, fg, st := newTestHandler(t, "")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetWorkspace("/ws"); err != nil {
		t.Fatal(err)
	}
	fg.cloneDir = "/ws/app"
	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo", "clone", "me/app")); err != nil {
		t.Fatal(err)
	}
	if len(fg.cloned) != 1 || fg.cloned[0] != "me/app" {
		t.Fatalf("expected clone of me/app, got %+v", fg.cloned)
	}
	if len(wt.createdRepos) != 1 || wt.createdRepos[0] != "/ws/app" {
		t.Fatalf("expected Create on /ws/app, got %+v", wt.createdRepos)
	}
	sess, _ := st.FindSession("demo")
	if sess.Project != "app" {
		t.Fatalf("project should be derived from clone: %+v", sess)
	}
}

func TestSessionCreateCloneErrorSurfaces(t *testing.T) {
	h, d, _, wt, fg, st := newTestHandler(t, "")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetWorkspace("/ws"); err != nil {
		t.Fatal(err)
	}
	fg.cloneErr = errors.New("auth required")
	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo", "clone", "me/app")); err == nil {
		t.Fatal("expected clone error")
	}
	if len(wt.created) != 0 || len(d.created) != 0 {
		t.Fatalf("nothing should be created after clone failure")
	}
}

func TestSessionListEmptyAndPopulated(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	if out, err := h.sessionListRun(context.Background(), args()); err != nil || !strings.Contains(out, "No active sessions") {
		t.Fatalf("empty list: out=%q err=%v", out, err)
	}
	if err := st.AddSession(state.Session{Name: "demo", ChannelID: "c1", Type: "text"}); err != nil {
		t.Fatal(err)
	}
	out, err := h.sessionListRun(context.Background(), args())
	if err != nil || !strings.Contains(out, "demo") {
		t.Fatalf("populated list should mention demo: out=%q err=%v", out, err)
	}
}

func TestSessionListNeutralForTerminalHome(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "terminal")
	if err := st.SetHome(state.HomeRef{ID: "term-home", Type: "terminal"}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddSession(state.Session{Name: "alpha", ChannelID: "terminal/alpha-1", Type: "text"}); err != nil {
		t.Fatal(err)
	}
	out, err := h.sessionListRun(context.Background(), args())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "<#") {
		t.Fatalf("terminal home list must not leak channel-mention syntax: %q", out)
	}
	if !strings.Contains(out, "terminal/alpha-1") {
		t.Fatalf("list should show the bare channel id: %q", out)
	}
}

func TestSessionCreateRejectedAtLimit(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	for i := range maxSessions {
		if err := st.AddSession(state.Session{Name: fmt.Sprintf("s%d", i), ChannelID: fmt.Sprintf("c%d", i), Type: "text"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.sessionCreateRun(context.Background(), args("name", "overflow")); err == nil {
		t.Fatal("expected create to be rejected at the session limit")
	}
}

func TestSessionWhoListsParticipants(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	if err := st.AddSession(state.Session{Name: "demo", ChannelID: "c1", Type: "text"}); err != nil {
		t.Fatal(err)
	}
	jp := state.ParticipantsPath(h.PartDir(), "demo")
	if _, err := state.AppendParticipant(jp, "h1"); err != nil {
		t.Fatal(err)
	}
	if _, err := state.AppendParticipant(jp, "h2"); err != nil {
		t.Fatal(err)
	}

	out, err := h.sessionWhoRun(context.Background(), args("name", "demo"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "h1") || !strings.Contains(out, "h2") {
		t.Fatalf("who should list both participants: %q", out)
	}
}

func TestSessionWhoEmpty(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	if err := st.AddSession(state.Session{Name: "demo", ChannelID: "c1", Type: "text"}); err != nil {
		t.Fatal(err)
	}
	out, err := h.sessionWhoRun(context.Background(), args("name", "demo"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Personne") {
		t.Fatalf("empty who should say nobody wrote yet: %q", out)
	}
}

func TestSessionClosePurgesParticipants(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddSession(state.Session{Name: "demo", ChannelID: "ch9", Type: "text", Worktree: "/wt/x"}); err != nil {
		t.Fatal(err)
	}
	jp := state.ParticipantsPath(h.PartDir(), "demo")
	if _, err := state.AppendParticipant(jp, "h1"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessionCloseRun(context.Background(), args("name", "demo")); err != nil {
		t.Fatal(err)
	}
	if got := state.ReadParticipants(jp); len(got) != 0 {
		t.Fatalf("close must purge the participants journal, got %+v", got)
	}
}

func TestServiceRestart(t *testing.T) {
	h, up, _, _, _, _, _ := newTestHandlerWithUpdater(t, "")
	out, err := h.serviceRestartRun(context.Background(), args())
	if err != nil {
		t.Fatal(err)
	}
	if up.restarts != 1 {
		t.Fatalf("expected 1 restart, got %d", up.restarts)
	}
	if len(up.builds) != 0 {
		t.Fatalf("restart must not build, got %+v", up.builds)
	}
	if !strings.Contains(out, "Restarting") {
		t.Fatalf("unexpected reply: %q", out)
	}
}

func TestServiceUpdatePullsBuildsRestarts(t *testing.T) {
	h, up, _, _, _, _, _ := newTestHandlerWithUpdater(t, "")
	out, err := h.serviceUpdateRun(context.Background(), args())
	if err != nil {
		t.Fatal(err)
	}
	if len(up.builds) != 1 || up.builds[0] != true {
		t.Fatalf("expected one build with pull=true, got %+v", up.builds)
	}
	if up.restarts != 1 {
		t.Fatalf("expected restart after build, got %d", up.restarts)
	}
	if !strings.Contains(out, "abc1234") {
		t.Fatalf("reply should mention version: %q", out)
	}
}

func TestServiceUpdateNoPull(t *testing.T) {
	h, up, _, _, _, _, _ := newTestHandlerWithUpdater(t, "")
	if _, err := h.serviceUpdateRun(context.Background(), args("no_pull", "true")); err != nil {
		t.Fatal(err)
	}
	if len(up.builds) != 1 || up.builds[0] != false {
		t.Fatalf("expected build with pull=false, got %+v", up.builds)
	}
}

func TestServiceUpdateBuildFailsNoRestart(t *testing.T) {
	h, up, _, _, _, _, _ := newTestHandlerWithUpdater(t, "")
	up.buildErr = errors.New("compile boom")
	if _, err := h.serviceUpdateRun(context.Background(), args()); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected build error surfaced, got %v", err)
	}
	if up.restarts != 0 {
		t.Fatalf("must not restart when build fails")
	}
}

func TestSessionCreateAdoptsAnExistingChannel(t *testing.T) {
	h, d, sup, _, _, st := newTestHandler(t, "")
	out, err := h.sessionCreateRun(context.Background(), args("name", "ch-123", "channel_id", "123", "shared", "true"))
	if err != nil {
		t.Fatalf("create with channel_id failed: %v", err)
	}
	if len(d.created) != 0 {
		t.Fatalf("adopting a channel still created one: %+v", d.created)
	}
	sess, ok := st.FindSession("ch-123")
	if !ok || sess.ChannelID != "123" || sess.Type != "text" {
		t.Fatalf("session not bound to the adopted channel: %+v ok=%v", sess, ok)
	}
	if len(sup.started) != 1 {
		t.Fatalf("bridge not started: %+v", sup.started)
	}
	if out == "" {
		t.Fatal("create returned no banner")
	}
}

func TestSessionCreateWithoutChannelIDStillNeedsAHome(t *testing.T) {
	h, _, _, _, _, _ := newTestHandler(t, "")
	_, err := h.sessionCreateRun(context.Background(), args("name", "plain", "shared", "true"))
	if err == nil || !strings.Contains(err.Error(), "no home set") {
		t.Fatalf("err = %v, want the existing no-home error", err)
	}
}

func TestClosingAGuestSessionLeavesTheChannelAlone(t *testing.T) {
	h, d, sup, wt, _, st := newTestHandler(t, "")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddSession(state.Session{Name: "ch-42", ChannelID: "42", Type: "text", Worktree: "/wt/x"}); err != nil {
		t.Fatal(err)
	}

	out, err := h.sessionCloseRun(context.Background(), args("name", "ch-42"))
	if err != nil {
		t.Fatalf("close: %v", err)
	}
	if len(d.archived) != 0 {
		t.Fatalf("archived %v, want the operator's own channel untouched", d.archived)
	}
	if len(sup.stopped) != 1 || len(wt.removed) != 1 {
		t.Fatalf("stopped=%v removed=%v, want the session actually closed", sup.stopped, wt.removed)
	}
	if _, ok := st.FindSession("ch-42"); ok {
		t.Fatal("the session should be gone")
	}
	if !strings.Contains(out, "closed") {
		t.Fatalf("out = %q", out)
	}
}

func TestASessionFromBeforeTheFlagIsTreatedAsAGuest(t *testing.T) {
	h, d, _, _, _, st := newTestHandler(t, "")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddSession(state.Session{Name: "old", ChannelID: "ch-old", Type: "text"}); err != nil {
		t.Fatal(err)
	}

	if _, err := h.sessionCloseRun(context.Background(), args("name", "old")); err != nil {
		t.Fatalf("close: %v", err)
	}
	if len(d.archived) != 0 {
		t.Fatalf("archived %v, want an unmarked row left alone", d.archived)
	}
}

func TestAChannelThatCannotBeTidiedDoesNotStrandTheSession(t *testing.T) {
	h, d, _, _, _, st := newTestHandler(t, "")
	d.archiveErr = errors.New("archive channel: 403: Missing Permissions")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddSession(state.Session{Owned: true, Name: "demo", ChannelID: "ch9", Type: "text", Worktree: "/wt/x"}); err != nil {
		t.Fatal(err)
	}

	out, err := h.sessionCloseRun(context.Background(), args("name", "demo"))
	if err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, ok := st.FindSession("demo"); ok {
		t.Fatal("the session must be closed even when the channel could not be")
	}
	if !strings.Contains(out, "Missing Permissions") {
		t.Fatalf("out = %q, want the reason said out loud", out)
	}
}

func TestASessionTidiesTheChannelItCreated(t *testing.T) {
	h, d, _, _, _, st := newTestHandler(t, "")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddSession(state.Session{Owned: true, Name: "own", ChannelID: "new-own", Type: "text"}); err != nil {
		t.Fatal(err)
	}

	if _, err := h.sessionCloseRun(context.Background(), args("name", "own")); err != nil {
		t.Fatalf("close: %v", err)
	}
	if len(d.archived) != 1 || d.archived[0] != "new-own" {
		t.Fatalf("archived = %v, want the session's own channel", d.archived)
	}
}

func TestCreateMarksWhoOwnsTheChannel(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "category")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}

	if _, err := h.sessionCreateRun(context.Background(), args("name", "mine")); err != nil {
		t.Fatalf("create: %v", err)
	}
	mine, _ := st.FindSession("mine")
	if !mine.Owned {
		t.Fatal("a session that created its channel owns it")
	}

	if _, err := h.sessionCreateRun(context.Background(), args("name", "guest", "channel_id", "99")); err != nil {
		t.Fatalf("create adopted: %v", err)
	}
	guest, _ := st.FindSession("guest")
	if guest.Owned {
		t.Fatal("a session bound to an existing channel does not own it")
	}
}
