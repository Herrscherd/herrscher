//go:build unix

package host

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/control"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestGoLiveReportsACommandSocketItCannotOpen(t *testing.T) {
	layout := isolatedRuntimeDir(t)
	name := "golive-" + newTurnID()
	if err := ensureRuntimeDir(layout, SessionCommandSocketPathIn(layout, "", name)); err != nil {
		t.Fatal(err)
	}
	answeringListener(t, SessionCommandSocketPathIn(layout, "", name))
	h := newHub(
		state.NewState(filepath.Join(t.TempDir(), "state.json")),
		nil,
		nil,
		t.TempDir(),
		nil,
		nil,
		newApprovalTable(),
		newSessionTable(),
	)
	h.runtime = layout
	err := h.goLive(t.Context(), state.Session{Name: name, Incarnation: "incarnation"})
	if !errors.Is(err, errSocketTaken) {
		t.Fatalf("goLive with a taken command socket = %v, want errSocketTaken", err)
	}
	if _, live := h.live[name]; live {
		t.Fatal("a session whose command socket is taken was reported live")
	}
	if conn, dialErr := control.Dial(control.SocketPath(layout, "", name)); dialErr == nil {
		logCleanup(conn.Close())
		t.Fatal("the control socket stayed open after the session failed to start")
	}
}
