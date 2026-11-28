package host

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func allowEverySession(string) bool { return true }

func TestSeedTokenIsNotCreatedForASessionPurgedBeforeIt(t *testing.T) {
	sessionTab := newSessionTable(newHostMessages(), clitext.NewMessages(), nil, nil)
	sessionTab.revokeTokensExcept(func(string) bool { return false })
	seedCtx, endSeed := context.WithCancel(t.Context())
	t.Cleanup(endSeed)
	token := sessionTab.seedToken(seedCtx, "gone", func(string) bool { return false })
	if sessionTab.tokenMatches("gone", token) {
		t.Fatal("a seed started after its session was purged got a token that answers approvals")
	}
}

func TestSeedTokenRefusesAContextThatNeverEnds(t *testing.T) {
	sessionTab := newSessionTable(newHostMessages(), clitext.NewMessages(), nil, nil)
	if token := sessionTab.seedToken(context.Background(), "s", allowEverySession); token != "" {
		t.Fatal("a seed lease on a context that never ends was issued, it would answer forever")
	}
}

func TestLiveSessionAllowedAdmitsOnlyActiveSessions(t *testing.T) {
	store := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	for _, name := range []string{"live", "doomed"} {
		if err := store.AddSession(state.Session{Name: name, ChannelID: "c-" + name, Incarnation: name}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SetArchived("doomed", true); err != nil {
		t.Fatal(err)
	}
	allowed := liveSessionAllowed(store)
	if !allowed("live") {
		t.Fatal("an active session is refused by the live predicate")
	}
	if allowed("doomed") {
		t.Fatal("an archived session is admitted by the live predicate")
	}
	if allowed("ghost") {
		t.Fatal("a session absent from the store is admitted by the live predicate")
	}
}

func TestSeedTokenIsRefusedForAnArchivedSession(t *testing.T) {
	store := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := store.AddSession(state.Session{Name: "doomed", ChannelID: "c-doomed", Incarnation: "doomed"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetArchived("doomed", true); err != nil {
		t.Fatal(err)
	}
	sessionTab := newSessionTable(newHostMessages(), clitext.NewMessages(), nil, nil)
	seedCtx, endSeed := context.WithCancel(t.Context())
	t.Cleanup(endSeed)
	if token := sessionTab.seedToken(seedCtx, "doomed", liveSessionAllowed(store)); token != "" {
		t.Fatal("a seed lease was issued for an archived session")
	}
}

func TestSeedLeaseDiesWithItsContext(t *testing.T) {
	sessionTab := newSessionTable(newHostMessages(), clitext.NewMessages(), nil, nil)
	seedCtx, endSeed := context.WithCancel(t.Context())
	token := sessionTab.seedToken(seedCtx, "s", allowEverySession)
	if !sessionTab.tokenMatches("s", token) {
		t.Fatal("the seed lease is refused while its context lives")
	}
	endSeed()
	if sessionTab.tokenMatches("s", token) {
		t.Fatal("the seed lease still answers after its context ended")
	}
}
