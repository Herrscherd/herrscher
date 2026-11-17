package host

import (
	"context"
	"io"
	"path/filepath"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestArchivingThroughDispatchRevokesTheSessionsTokens(t *testing.T) {
	dispatchRevokesTheDoomedSession(t, "archive", func(st *state.State) error {
		return st.SetArchived("doomed", true)
	})
}

func TestClosingThroughDispatchRevokesTheSessionsTokens(t *testing.T) {
	dispatchRevokesTheDoomedSession(t, "close", func(st *state.State) error {
		return st.RemoveSession("doomed")
	})
}

func dispatchRevokesTheDoomedSession(t *testing.T, verb string, remove func(*state.State) error) {
	t.Helper()
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	for _, name := range []string{"doomed", "kept"} {
		if err := st.AddSession(state.Session{Name: name, ChannelID: "c-" + name, Incarnation: name}); err != nil {
			t.Fatal(err)
		}
	}
	sessionTab := newSessionTable(newHostMessages())
	doomedToken := sessionTab.issueToken("doomed")
	keptToken := sessionTab.issueToken("kept")
	seedCtx, endSeed := context.WithCancel(t.Context())
	t.Cleanup(endSeed)
	doomedSeedToken := sessionTab.seedToken(seedCtx, "doomed", liveSessionAllowed(st))
	keptSeedToken := sessionTab.seedToken(seedCtx, "kept", liveSessionAllowed(st))

	var reg cli.Registry
	command := contracts.New("session", verb).Do(func(context.Context, contracts.Input) (contracts.Result, error) {
		return contracts.Result{}, remove(st)
	})
	if err := reg.Add(command); err != nil {
		t.Fatal(err)
	}
	h := newHub(st, nil, nil, t.TempDir(), &reg, nil, newApprovalTable(io.Discard), sessionTab)
	h.runtime = isolatedRuntimeDir(t)
	h.startBridge = func(state.Session, time.Duration) error { return nil }
	h.stopBridge = func(string) error { return nil }
	t.Cleanup(h.stopLive)

	if !sessionTab.tokenMatches("doomed", doomedToken) {
		t.Fatal("the session token is refused before the session is removed, the check below would prove nothing")
	}
	if !sessionTab.tokenMatches("doomed", doomedSeedToken) {
		t.Fatal("the seed token is refused before the session is removed, the check below would prove nothing")
	}
	if _, err := h.Dispatch(t.Context(), []string{"session", verb}); err != nil {
		t.Fatalf("session %s: %v", verb, err)
	}

	if sessionTab.tokenMatches("doomed", doomedToken) {
		t.Fatalf("session %s left the token of the session answering approvals", verb)
	}
	if sessionTab.tokenMatches("doomed", doomedSeedToken) {
		t.Fatalf("session %s left a live one-shot seed token answering approvals", verb)
	}
	if !sessionTab.tokenMatches("kept", keptToken) {
		t.Fatalf("session %s revoked the token of a session still present", verb)
	}
	if !sessionTab.tokenMatches("kept", keptSeedToken) {
		t.Fatalf("session %s revoked the seed token of a session still present", verb)
	}
}
