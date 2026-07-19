package manager

import (
	"context"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

// TestSessionCreateTerminalOnlyIgnoresWorkspaceProject guards the TUI's default
// `main` tab: a terminal-only session is repo-agnostic, so it must create even
// when a workspace root is configured and no --project is given. Before the fix
// the workspace-root branch demanded a project and the TUI bootstrap failed
// silently, leaving an empty tab bar.
func TestSessionCreateTerminalOnlyIgnoresWorkspaceProject(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "terminal")
	st.SetHome(state.HomeRef{ID: "terminal", Type: "terminal"})
	if err := st.SetWorkspace("/home/shan/dev"); err != nil {
		t.Fatal(err)
	}

	if _, err := h.sessionCreateRun(context.Background(),
		args("name", "main", "terminal_only", "true", "shared", "true")); err != nil {
		t.Fatalf("terminal-only create with workspace root should succeed, got: %v", err)
	}

	if _, ok := st.FindSession("main"); !ok {
		t.Fatal("terminal-only session was not created")
	}
}

// TestSessionCreateNoProjectRootsAtWorkspace verifies the pwd-relative rule: a
// session created with a workspace root but no --project no longer errors — it
// runs rooted at the workspace root, so sess.Dir is the workspace root itself.
func TestSessionCreateNoProjectRootsAtWorkspace(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "category")
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})
	if err := st.SetWorkspace("/home/shan/dev"); err != nil {
		t.Fatal(err)
	}

	if _, err := h.sessionCreateRun(context.Background(),
		args("name", "demo", "shared", "true")); err != nil {
		t.Fatalf("no-project create with workspace root should succeed, got: %v", err)
	}
	sess, ok := st.FindSession("demo")
	if !ok {
		t.Fatal("session was not created")
	}
	if sess.Dir != "/home/shan/dev" {
		t.Fatalf("Dir = %q, want workspace root", sess.Dir)
	}
}

// TestSessionCreateNoWorkspaceInheritsPwd verifies the fallback: with no
// workspace root configured and no project, sess.Dir is empty so the supervisor
// leaves cmd.Dir unset and the bridge inherits the launcher's cwd (pwd).
func TestSessionCreateNoWorkspaceInheritsPwd(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "category")
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})

	if _, err := h.sessionCreateRun(context.Background(),
		args("name", "demo", "shared", "true")); err != nil {
		t.Fatalf("no-workspace create should succeed, got: %v", err)
	}
	sess, ok := st.FindSession("demo")
	if !ok {
		t.Fatal("session was not created")
	}
	if sess.Dir != "" {
		t.Fatalf("Dir = %q, want empty (pwd fallback)", sess.Dir)
	}
}

// TestSessionCreateTerminalOnlyRoutesToTerminalAdmin verifies Point 3: with a
// chat home configured AND a terminal admin wired, a terminal_only session is
// minted through the terminal admin (local `terminal/…` channel), never the
// chat one. Close then archives it back through the terminal admin.
func TestSessionCreateTerminalOnlyRoutesToTerminalAdmin(t *testing.T) {
	h, chat, _, _, _, st := newTestHandler(t, "category")
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})
	term := &fakeChannelAdmin{homeType: "terminal", idPrefix: "terminal/"}
	h.SetTerminalAdmin(term)

	if _, err := h.sessionCreateRun(context.Background(),
		args("name", "main", "terminal_only", "true", "shared", "true")); err != nil {
		t.Fatalf("terminal-only create should succeed, got: %v", err)
	}
	if len(term.created) != 1 || term.created[0] != "main" {
		t.Fatalf("terminal admin should mint the channel, got %+v", term.created)
	}
	if len(chat.created) != 0 {
		t.Fatalf("chat admin must not mint a terminal-only channel, got %+v", chat.created)
	}

	if _, err := h.sessionCloseRun(context.Background(), args("name", "main")); err != nil {
		t.Fatalf("close should succeed, got: %v", err)
	}
	if len(term.archived) != 1 {
		t.Fatalf("terminal admin should archive the channel, got %+v", term.archived)
	}
	if len(chat.archived) != 0 {
		t.Fatalf("chat admin must not archive a terminal-only channel, got %+v", chat.archived)
	}
}
