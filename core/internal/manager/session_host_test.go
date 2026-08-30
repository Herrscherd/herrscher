package manager

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

// fakeHosts stands in for the composition root's placer: it knows one host and
// refuses every other name, which is what an unregistered host looks like.
type fakeHosts struct {
	name         string
	wt           *fakeWT
	workspace    string
	notReady     error    // what Ready answers, e.g. a version drift
	materialized []string // worktree paths passed to Materialize
	hooked       []bool   // whether each Materialize call asked for the approval hook
	ensured      []string // "project@host" per EnsureProject call
}

func (f *fakeHosts) Ready(name string) error {
	if err := f.check(name); err != nil {
		return err
	}
	return f.notReady
}

func (f *fakeHosts) check(name string) error {
	if name != f.name {
		return errors.New("unknown host " + name)
	}
	return nil
}

func (f *fakeHosts) Worktrees(name string) (Worktrees, error) {
	if err := f.check(name); err != nil {
		return nil, err
	}
	return f.wt, nil
}

func (f *fakeHosts) Workspace(name string) (string, error) {
	if err := f.check(name); err != nil {
		return "", err
	}
	return f.workspace, nil
}

func (f *fakeHosts) Materialize(ctx context.Context, name string, a agent.Agent, worktreePath string, hook bool) error {
	if err := f.check(name); err != nil {
		return err
	}
	f.materialized = append(f.materialized, worktreePath)
	f.hooked = append(f.hooked, hook)
	return nil
}

func (f *fakeHosts) EnsureProject(ctx context.Context, name, project, repo string) error {
	if name == "" || project == "" {
		return nil
	}
	if err := f.check(name); err != nil {
		return err
	}
	f.ensured = append(f.ensured, project+"@"+name)
	return nil
}

// handlerWithHosts wires a handler that knows one remote host named build1.
func handlerWithHosts(t *testing.T) (*Handler, *fakeHosts, *fakeWT, *state.State) {
	t.Helper()
	h, _, _, localWT, _, st := newTestHandler(t, "category")
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})
	hs := &fakeHosts{name: "build1", wt: &fakeWT{path: "/srv/work/demo-wt", scratchBase: t.TempDir()}, workspace: "/srv/work"}
	h.SetHosts(hs)
	return h, hs, localWT, st
}

func TestSessionCreateOnHostUsesRemoteWorktree(t *testing.T) {
	h, hs, localWT, st := handlerWithHosts(t)
	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo", "host", "build1", "project", "app")); err != nil {
		t.Fatal(err)
	}
	if len(hs.wt.created) != 1 {
		t.Fatalf("the remote worktree must be the one created, got %v", hs.wt.created)
	}
	if len(localWT.created) != 0 {
		t.Fatalf("the local worktree must stay untouched, got %v", localWT.created)
	}
	if got := hs.wt.createdRepos[0]; got != "/srv/work/app" {
		t.Fatalf("repo must be resolved under the host workspace, got %q", got)
	}
	sess, ok := st.FindSession("demo")
	if !ok || sess.Host != "build1" {
		t.Fatalf("the session must record its host, got %+v", sess)
	}
	if len(hs.ensured) != 1 || hs.ensured[0] != "app@build1" {
		t.Fatalf("the project must be ensured on the host, got %v", hs.ensured)
	}
}

func TestSessionCreateLocalHostIsThisMachine(t *testing.T) {
	h, hs, localWT, st := handlerWithHosts(t)
	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo", "host", state.LocalHost)); err != nil {
		t.Fatal(err)
	}
	if len(localWT.created) != 1 {
		t.Fatalf("`local` must run here, got local=%v remote=%v", localWT.created, hs.wt.created)
	}
	sess, _ := st.FindSession("demo")
	if sess.Host != "" {
		t.Fatalf("`local` must be stored as the empty host, got %q", sess.Host)
	}
}

func TestSessionCreateTakesTheAgentDefaultHost(t *testing.T) {
	h, hs, _, st := handlerWithHosts(t)
	if _, err := h.Agents().Create(agent.CreateSpec{Name: "nova", Host: "build1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo", "agent", "nova")); err != nil {
		t.Fatal(err)
	}
	sess, _ := st.FindSession("demo")
	if sess.Host != "build1" {
		t.Fatalf("the agent's default host must apply, got %q", sess.Host)
	}
	if len(hs.materialized) != 1 || hs.materialized[0] != "/srv/work/demo-wt" {
		t.Fatalf("the agent must be materialized on the host worktree, got %v", hs.materialized)
	}
}

func TestSessionCreateExplicitHostBeatsTheAgentDefault(t *testing.T) {
	h, _, localWT, st := handlerWithHosts(t)
	localWT.path = t.TempDir() // a local agent really writes its files
	if _, err := h.Agents().Create(agent.CreateSpec{Name: "nova", Host: "build1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo", "agent", "nova", "host", state.LocalHost)); err != nil {
		t.Fatal(err)
	}
	sess, _ := st.FindSession("demo")
	if sess.Host != "" {
		t.Fatalf("an explicit --host must win over the agent's default, got %q", sess.Host)
	}
	if len(localWT.created) != 1 {
		t.Fatalf("the session must run here, got %v", localWT.created)
	}
}

func TestSessionCreateRefusesAnUnknownHost(t *testing.T) {
	h, _, localWT, st := handlerWithHosts(t)
	_, err := h.sessionCreateRun(context.Background(), args("name", "demo", "host", "nowhere"))
	if err == nil {
		t.Fatal("an unregistered host must refuse the create")
	}
	if !strings.Contains(err.Error(), "nowhere") {
		t.Fatalf("the refusal must name the host, got %v", err)
	}
	if _, ok := st.FindSession("demo"); ok {
		t.Fatal("no session row may survive a refused create")
	}
	if len(localWT.created) != 0 {
		t.Fatalf("nothing may be created locally as a fallback, got %v", localWT.created)
	}
}

func TestSessionCreateRefusesCloneOnAHost(t *testing.T) {
	h, _, _, st := handlerWithHosts(t)
	if err := st.SetWorkspace(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	_, err := h.sessionCreateRun(context.Background(), args("name", "demo", "host", "build1", "clone", "owner/repo"))
	if err == nil || !strings.Contains(err.Error(), "clone") {
		t.Fatalf("clone onto a host must refuse, got %v", err)
	}
}

func TestSessionCreateRefusesAHostThatIsNotReady(t *testing.T) {
	h, hs, _, st := handlerWithHosts(t)
	hs.notReady = errors.New("host \"build1\" runs herrscher abc but this daemon builds def")
	_, err := h.sessionCreateRun(context.Background(), args("name", "demo", "host", "build1"))
	if err == nil || !strings.Contains(err.Error(), "this daemon builds def") {
		t.Fatalf("a host that cannot carry a session must refuse the create, got %v", err)
	}
	if len(hs.wt.created) != 0 {
		t.Fatalf("nothing may be created before the refusal, got %v", hs.wt.created)
	}
	if _, ok := st.FindSession("demo"); ok {
		t.Fatal("no session row may survive a refused create")
	}
}

// The mirror of the test above: what stops a new session must not strand the
// ones already running. Closing removes a worktree over there, which asks
// nothing of the remote version.
func TestSessionCloseWorksOnAHostThatIsNoLongerReady(t *testing.T) {
	h, hs, _, st := handlerWithHosts(t)
	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo", "host", "build1")); err != nil {
		t.Fatal(err)
	}
	hs.notReady = errors.New("version drift")
	if _, err := h.sessionCloseRun(context.Background(), args("name", "demo")); err != nil {
		t.Fatalf("close must not depend on the host being ready for a new session: %v", err)
	}
	if len(hs.wt.removed) != 1 {
		t.Fatalf("close must still remove the remote worktree, got %v", hs.wt.removed)
	}
	if _, ok := st.FindSession("demo"); ok {
		t.Fatal("the session row must be gone")
	}
}

func TestSessionCloseRemovesTheWorktreeWhereItIs(t *testing.T) {
	h, hs, localWT, _ := handlerWithHosts(t)
	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo", "host", "build1")); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessionCloseRun(context.Background(), args("name", "demo")); err != nil {
		t.Fatal(err)
	}
	if len(hs.wt.removed) != 1 {
		t.Fatalf("close must remove the remote worktree, got %v", hs.wt.removed)
	}
	if len(localWT.removed) != 0 {
		t.Fatalf("close must not touch the local worktree, got %v", localWT.removed)
	}
}

func TestSessionCreateRefusesUnknownApprovalMode(t *testing.T) {
	h, _, localWT, st := handlerWithHosts(t)
	_, err := h.sessionCreateRun(context.Background(), args("name", "demo", "approvals", "loose"))
	if err == nil || !strings.Contains(err.Error(), "ask, bypass or strict") {
		t.Fatalf("got %v, want a refusal naming the three modes", err)
	}
	if _, ok := st.FindSession("demo"); ok {
		t.Fatal("no session row may survive a refused create")
	}
	if len(localWT.created) != 0 {
		t.Fatalf("nothing may be created before the refusal, got %v", localWT.created)
	}
}

func TestSessionCreateStoresTheApprovalMode(t *testing.T) {
	h, _, _, st := handlerWithHosts(t)
	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo", "approvals", "bypass")); err != nil {
		t.Fatalf("create: %v", err)
	}
	sess, ok := st.FindSession("demo")
	if !ok || sess.Approvals != "bypass" {
		t.Fatalf("got %q ok=%v, want bypass", sess.Approvals, ok)
	}
}

// A bypass session is the one case that must reach the far side with no hook at
// all: its settings are then the ones herrscher wrote before approvals existed.
func TestSessionCreateOnHostMaterializesWithoutAHookInBypass(t *testing.T) {
	h, hs, _, _ := handlerWithHosts(t)
	if _, err := h.Agents().Create(agent.CreateSpec{Name: "nova", Host: "build1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo", "agent", "nova", "approvals", "bypass")); err != nil {
		t.Fatal(err)
	}
	if len(hs.hooked) != 1 || hs.hooked[0] {
		t.Fatalf("bypass must materialize with no hook, got %v", hs.hooked)
	}
}

func TestSessionCreateOnHostMaterializesWithAHookByDefault(t *testing.T) {
	h, hs, _, _ := handlerWithHosts(t)
	if _, err := h.Agents().Create(agent.CreateSpec{Name: "nova", Host: "build1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo", "agent", "nova")); err != nil {
		t.Fatal(err)
	}
	if len(hs.hooked) != 1 || !hs.hooked[0] {
		t.Fatalf("an unset mode must still get a hook, got %v", hs.hooked)
	}
}

// A warned session is still a session. The mode is recorded whatever the
// warning says, so `approve mode` has something to change once the operator
// recreates the session on a backend that can enforce it.
func TestSessionCreateKeepsTheModeItWarnedAbout(t *testing.T) {
	h, _, _, st := handlerWithHosts(t)
	out, err := h.sessionCreateRun(context.Background(), args("name", "demo", "approvals", "ask"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !strings.Contains(out, "ungated") {
		t.Fatalf("create said %q, want a warning that the mode binds nothing", out)
	}
	sess, ok := st.FindSession("demo")
	if !ok || sess.Approvals != "ask" {
		t.Fatalf("the session must exist with the mode asked for, got %q ok=%v", sess.Approvals, ok)
	}
}
