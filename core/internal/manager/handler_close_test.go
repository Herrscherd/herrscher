package manager

import (
	"context"
	"errors"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestChannelAdminInterfaceHasSend(_ *testing.T) {
	var _ channelAdmin = (*fakeChannelAdmin)(nil)
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

func TestSessionCloseAnswersWithTheClosedNotice(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddSession(state.Session{Owned: true, Name: "demo", ChannelID: "ch9", Type: "text"}); err != nil {
		t.Fatal(err)
	}
	res, err := h.sessionCloseRun(context.Background(), args("name", "demo"))
	if err != nil {
		t.Fatal(err)
	}
	notice, ok := res.Notice()
	if !ok || notice.Code != contracts.CodeSessionClosed {
		t.Fatalf("close answered %+v, want the session closed notice", res.Data())
	}
	if res.Data() != nil {
		t.Fatalf("close also answered with text %v, want the notice only", res.Data())
	}
}

func TestSessionCloseWithAChannelLeftOpenSaysSo(t *testing.T) {
	h, d, _, _, _, st := newTestHandler(t, "")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddSession(state.Session{Owned: true, Name: "demo", ChannelID: "ch9", Type: "text"}); err != nil {
		t.Fatal(err)
	}
	d.archiveErr = errors.New("channel refused")
	res, err := h.sessionCloseRun(context.Background(), args("name", "demo"))
	if err != nil {
		t.Fatal(err)
	}
	text, ok := res.Data().(string)
	if !ok || !strings.Contains(text, "channel refused") || !strings.Contains(text, "the channel stayed open") {
		t.Fatalf("close with an open channel answered %+v, want the warning said out loud", res.Data())
	}
	if _, stillThere := st.FindSession("demo"); stillThere {
		t.Fatal("the session must be removed even when its channel stays open")
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

func TestClosingAGuestSessionLeavesTheChannelAlone(t *testing.T) {
	h, d, sup, wt, _, st := newTestHandler(t, "")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddSession(state.Session{Name: "ch-42", ChannelID: "42", Type: "text", Worktree: "/wt/x"}); err != nil {
		t.Fatal(err)
	}

	out, err := cli.Plain(h.sessionCloseRun(context.Background(), args("name", "ch-42")))
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

	res, err := h.sessionCloseRun(context.Background(), args("name", "demo"))
	if err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, ok := st.FindSession("demo"); ok {
		t.Fatal("the session must be closed even when the channel could not be")
	}
	text, isText := res.Data().(string)
	if !isText || !strings.Contains(text, "Missing Permissions") {
		t.Fatalf("close answered %+v, want the reason said out loud", res.Data())
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
