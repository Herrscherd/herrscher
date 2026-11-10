package host

import (
	"context"
	"testing"

	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/supervisor"
)

func TestHubScrollbackAndResume(t *testing.T) {
	approvalTab := newApprovalTable()
	sessionTab := newSessionTable()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	st := state.NewState(t.TempDir() + "/s.json")
	sup := supervisor.NewSupervisor(ctx, "/nonexistent/herrscher")
	partDir := t.TempDir()
	h := newHub(st, sup, nil, partDir, &cli.Registry{}, nil, approvalTab, sessionTab)
	h.runtime = isolatedRuntimeDir(t)

	if err := st.AddSession(state.Session{Name: "seam", ChannelID: "c", Type: "text", Archived: true, ResumeToken: "tok", Vendor: "claude", Project: "alpha"}); err != nil {
		t.Fatal(err)
	}
	p := state.TranscriptPath(partDir, "seam")
	logCleanup(state.AppendTranscript(p, state.TranscriptEntry{Ts: "t1", Role: "user", Text: "hi"}))
	logCleanup(state.AppendTranscript(p, state.TranscriptEntry{Ts: "t2", Role: "assistant", Text: "yo"}))

	lines := h.Scrollback("seam")
	if len(lines) != 2 || lines[0].Role != "user" || lines[1].Text != "yo" {
		t.Fatalf("scrollback wrong: %+v", lines)
	}

	infos := h.Sessions()
	if len(infos) != 1 {
		t.Fatalf("want 1 session, got %d", len(infos))
	}
	si := infos[0]
	if !si.Archived || !si.Resumable || si.Vendor != "claude" || si.Project != "alpha" || si.LastTs != "t2" {
		t.Fatalf("SessionInfo not enriched: %+v", si)
	}

	if err := h.Resume("seam"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { logCleanup(sup.Stop("seam")) })
	got, _ := st.FindSession("seam")
	if got.Archived {
		t.Fatalf("resume should unarchive")
	}
	h.mu.Lock()
	_, up := h.live["seam"]
	h.mu.Unlock()
	if !up {
		t.Fatalf("resume should bring the session live")
	}

	if err := h.Resume("nope"); err == nil {
		t.Fatalf("resume of unknown session should error")
	}
}
