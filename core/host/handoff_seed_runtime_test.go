package host

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/supervisor"
)

func daemonHandoffHub(t *testing.T, withDesk bool) *hub {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	approvalTab := newApprovalTable(io.Discard)
	sessionTab := newSessionTable(newHostMessages(), clitext.NewMessages(), nil, nil)
	partDir := t.TempDir()
	statePath := filepath.Join(partDir, "state.json")
	st := state.NewState(statePath)
	if err := st.AddSession(state.Session{Name: "orchestrator", ChannelID: "channel", Agent: "orchestrator"}); err != nil {
		t.Fatal(err)
	}
	sup := supervisor.NewSupervisor(ctx, "/nonexistent/herrscher", testRuntime(t))
	reg, _, err := buildRegistry(ctx, Deps{}, Options{Runtime: testRuntime(t), StatePath: statePath}, st, sup, "", nil, approvalTab, sessionTab)
	if err != nil {
		t.Fatal(err)
	}
	h := newHub(st, sup, nil, partDir, reg, nil, approvalTab, sessionTab)
	h.runtime = isolatedRuntimeDir(t)
	h.coordinator = &recordingCoord{}
	if withDesk {
		h.desk = hubDesk(st, approvalTab, sessionTab)
	}
	return h
}

func daemonHandoffArgs() []string {
	return []string{"session", "handoff", "--name", "orchestrator", "--to", "successor"}
}

func TestDaemonHandoffBriefRunsWithTheDaemonsApprovalAsker(t *testing.T) {
	h := daemonHandoffHub(t, true)
	failure := errors.New("stop after the backend was built")
	seen := make(chan bool, 1)
	h.seedBackend = func(ctx context.Context, _ state.Session) (contracts.Backend, error) {
		_, ok := contracts.ApprovalAskerFrom(ctx)
		seen <- ok
		return nil, failure
	}
	if _, err := h.Dispatch(context.Background(), daemonHandoffArgs()); err == nil {
		t.Fatal("the daemon's handoff succeeded although its brief backend always fails")
	}
	select {
	case ok := <-seen:
		if !ok {
			t.Fatal("the daemon's handoff built its brief backend without an approval asker")
		}
	default:
		t.Fatal("the daemon's handoff never built its brief backend")
	}
}

func TestDaemonHandoffWithoutADeskFailsBeforeItsBackendIsBuilt(t *testing.T) {
	h := daemonHandoffHub(t, false)
	built := false
	h.seedBackend = func(context.Context, state.Session) (contracts.Backend, error) {
		built = true
		return nil, errors.New("stop")
	}
	_, err := h.Dispatch(context.Background(), daemonHandoffArgs())
	if !errors.Is(err, errSeedWithoutApprovalDesk) {
		t.Fatalf("error %v, want the daemon handoff refused for its missing approval desk", err)
	}
	if built {
		t.Fatal("a daemon handoff without an approval desk built its backend anyway")
	}
}
