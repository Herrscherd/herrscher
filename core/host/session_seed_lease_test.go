package host

import (
	"context"
	"testing"
)

func allowEverySession(string) bool { return true }

func TestSeedTokenIsNotCreatedForASessionPurgedBeforeIt(t *testing.T) {
	sessionTab := newSessionTable()
	sessionTab.revokeTokensExcept(func(string) bool { return false })
	seedCtx, endSeed := context.WithCancel(t.Context())
	t.Cleanup(endSeed)
	token := sessionTab.seedToken(seedCtx, "gone", func(string) bool { return false })
	if sessionTab.tokenMatches("gone", token) {
		t.Fatal("a seed started after its session was purged got a token that answers approvals")
	}
}

func TestSeedTokenRefusesAContextThatNeverEnds(t *testing.T) {
	sessionTab := newSessionTable()
	if token := sessionTab.seedToken(context.Background(), "s", allowEverySession); token != "" {
		t.Fatal("a seed lease on a context that never ends was issued, it would answer forever")
	}
}

func TestSeedLeaseDiesWithItsContext(t *testing.T) {
	sessionTab := newSessionTable()
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
