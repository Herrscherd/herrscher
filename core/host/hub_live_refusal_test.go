package host

import (
	"errors"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/control"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/supervisor"
)

func TestRefusedBridgeLeavesAnAlreadyLiveSessionListening(t *testing.T) {
	layout := isolatedRuntimeDir(t)
	name := "live-refusal-" + newTurnID()
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := st.AddSession(state.Session{Name: name, ChannelID: "c1"}); err != nil {
		t.Fatal(err)
	}
	h := newHub(st, nil, nil, t.TempDir(), nil, nil, newApprovalTable(io.Discard), newSessionTable())
	h.runtime = layout
	t.Cleanup(func() { h.goDead(name) })
	sess := state.Session{Name: name, ChannelID: "c1"}
	if err := h.goLive(t.Context(), sess); err != nil {
		t.Fatal(err)
	}
	h.startBridge = func(state.Session, time.Duration) error { return supervisor.ErrStillStopping }

	_, err := h.goLiveAndPersist(t.Context(), sess, func() error { return nil })
	if !errors.Is(err, supervisor.ErrStillStopping) {
		t.Fatalf("goLiveAndPersist = %v, want ErrStillStopping", err)
	}
	if !h.isLive(name) {
		t.Fatal("a refused bridge dropped a session that was already live")
	}
	conn, err := control.Dial(control.SocketPath(layout, "", name))
	if err != nil {
		t.Fatalf("the control socket of the live session was closed by a refused bridge: %v", err)
	}
	logCleanup(conn.Close())
}
