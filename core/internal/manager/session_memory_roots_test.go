package manager

import (
	"context"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

// A memory root lands on the row and changes nothing about where the session
// lives — that separation is the entire reason these fields are not Project.
func TestSessionCreatePersistsMemoryRoots(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})

	if _, err := h.sessionCreateRun(context.Background(), args(
		"name", "demo", "shared", "true",
		"memory_project", "neublox",
		"memory_agent", "tui",
	)); err != nil {
		t.Fatal(err)
	}
	sess, ok := st.FindSession("demo")
	if !ok {
		t.Fatalf("session not persisted; sessions=%+v", st.SnapshotSessions())
	}
	if sess.MemoryProject != "neublox" || sess.MemoryAgent != "tui" {
		t.Fatalf("memory roots = %q/%q, want neublox/tui", sess.MemoryProject, sess.MemoryAgent)
	}
	if sess.ProjectPinned {
		t.Fatal("a project nobody pinned must stay revisable")
	}
	if sess.Project != "" {
		t.Fatalf("Project = %q — a memory root must not become a workspace sub-dir", sess.Project)
	}
}

// The regression that made these separate fields necessary. Routing the memory
// scope through Project would have sent this session to /workspaces/demo/neublox
// on any machine with a workspace root configured. Compare with
// TestSessionCreateNoProjectRootsAtWorkspace, which fixes the expected Dir.
func TestMemoryProjectDoesNotMoveTheSession(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})
	if err := st.SetWorkspace("/workspaces/demo"); err != nil {
		t.Fatal(err)
	}

	if _, err := h.sessionCreateRun(context.Background(), args(
		"name", "demo", "shared", "true", "memory_project", "neublox",
	)); err != nil {
		t.Fatal(err)
	}
	sess, _ := st.FindSession("demo")
	if sess.Dir != "/workspaces/demo" {
		t.Fatalf("Dir = %q, want the workspace root — a memory root is not a location", sess.Dir)
	}
}

func TestSessionCreatePinsWhenAsked(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})

	if _, err := h.sessionCreateRun(context.Background(), args(
		"name", "demo", "shared", "true",
		"memory_project", "neublox", "project_pinned", "true",
	)); err != nil {
		t.Fatal(err)
	}
	sess, _ := st.FindSession("demo")
	if !sess.ProjectPinned {
		t.Fatal("project_pinned did not pin")
	}
}

// A memory agent is not a provisioning directive, so it must not drag in the
// isolated-worktree rule that guards the real one.
func TestMemoryAgentDoesNotNeedAWorktree(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})

	if _, err := h.sessionCreateRun(context.Background(), args(
		"name", "demo", "shared", "true", "memory_agent", "tui",
	)); err != nil {
		t.Fatalf("memory_agent on a shared session should be fine: %v", err)
	}
}

// …and it must not have relaxed it either.
func TestAgentStillNeedsAWorktree(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})

	if _, err := h.sessionCreateRun(context.Background(), args(
		"name", "demo", "shared", "true", "agent", "x",
	)); err == nil {
		t.Fatal("agent on a shared session must still be refused")
	}
}

func TestInvalidMemoryProjectIsRefused(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})

	if _, err := h.sessionCreateRun(context.Background(), args(
		"name", "demo", "shared", "true", "memory_project", "a/b",
	)); err == nil {
		t.Fatal("a memory project with a slash must be refused")
	}
}
