package host

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/cli"
)

type capturingDispatcher struct {
	token string
}

func (c *capturingDispatcher) Dispatch(ctx context.Context, _ []string) (string, error) {
	c.token = callerTokenFrom(ctx)
	return "ok", nil
}

func TestCommandSocketCarriesTheCallerToken(t *testing.T) {
	server, client := net.Pipe()
	disp := &capturingDispatcher{}
	done := make(chan struct{})
	go func() {
		handleCommandConn(context.Background(), server, disp, time.Second)
		close(done)
	}()
	if _, err := client.Write([]byte(`{"argv":["approve","ask"],"token":"tok-s1"}` + "\n")); err != nil {
		t.Fatalf("write request: %v", err)
	}
	if _, err := bufio.NewReader(client).ReadBytes('\n'); err != nil {
		t.Fatalf("read answer: %v", err)
	}
	<-done
	if disp.token != "tok-s1" {
		t.Fatalf("the dispatch saw token %q, want the one the request carried", disp.token)
	}
}

func approveAskWithToken(t *testing.T, sessionTab *sessionTable, token, session string) (string, error) {
	t.Helper()
	approvalTab := newApprovalTable(io.Discard)
	reg, _ := approveReg(t, approvalTab, sessionTab)
	ctx := context.Background()
	if token != "" {
		ctx = withCallerToken(ctx, token)
	}
	return cli.Plain(reg.Dispatch(ctx, []string{"approve", "ask", "--session", session, "--tool", "Bash", "--subject", "ls -la"}))
}

func TestApproveAskRefusesASessionWithoutAToken(t *testing.T) {
	sessionTab := newSessionTable()
	if _, err := approveAskWithToken(t, sessionTab, "", "ghost"); err == nil {
		t.Fatal("approve ask answered for a session nobody issued a token to")
	}
}

func TestApproveAskRefusesAnEmptyTokenForAKnownSession(t *testing.T) {
	sessionTab := newSessionTable()
	sessionTab.issueToken("s1")
	_, err := approveAskWithToken(t, sessionTab, "", "s1")
	if !errors.Is(err, errApproveAskUnauthenticated) {
		t.Fatalf("err = %v, want approve.ask_unauthenticated for a known session with an empty token", err)
	}
}

func TestClosedSessionTokenIsRevoked(t *testing.T) {
	sessionTab := newSessionTable()
	driver := &sessionDriver{}
	sessionTab.register("s1", driver)
	old := sessionTab.issueToken("s1")
	sessionTab.unregister("s1", driver)
	if sessionTab.tokenMatches("s1", old) {
		t.Fatal("the token of a closed session still answers approvals")
	}
	recreated := sessionTab.issueToken("s1")
	if recreated == old {
		t.Fatal("a session recreated under the same name kept the token of the closed one")
	}
	if sessionTab.tokenMatches("s1", old) {
		t.Fatal("the closed session's token is accepted again after the name is reused")
	}
}

func TestApproveAskRefusesAGuessedToken(t *testing.T) {
	sessionTab := newSessionTable()
	sessionTab.issueToken("s1")
	if _, err := approveAskWithToken(t, sessionTab, "guess", "s1"); err == nil {
		t.Fatal("approve ask answered for a token that is not the session's")
	}
}

func TestApproveAskRefusesAnotherSessionsToken(t *testing.T) {
	sessionTab := newSessionTable()
	sessionTab.issueToken("A")
	tokenB := sessionTab.issueToken("B")
	if _, err := approveAskWithToken(t, sessionTab, tokenB, "A"); err == nil {
		t.Fatal("approve ask answered for session A with B's token")
	}
	if _, err := approveAskWithToken(t, sessionTab, sessionTab.issueToken("A"), "B"); err == nil {
		t.Fatal("approve ask answered for session B with A's token")
	}
}

func TestApproveAskAcceptsItsOwnSessionsToken(t *testing.T) {
	approvalTab := newApprovalTable(io.Discard)
	sessionTab := newSessionTable()
	reg, _ := approveReg(t, approvalTab, sessionTab)
	ctx := context.Background()
	if _, err := reg.Dispatch(ctx, []string{"approve", "rule", "allow Bash(ls*)"}); err != nil {
		t.Fatalf("approve rule: %v", err)
	}
	ctx = withCallerToken(ctx, sessionTab.issueToken("s1"))
	out, err := cli.Plain(reg.Dispatch(ctx, []string{"approve", "ask", "--session", "s1", "--tool", "Bash", "--subject", "ls -la"}))
	if err != nil {
		t.Fatalf("approve ask with its own token: %v", err)
	}
	if !strings.Contains(out, `"decision":"allow"`) {
		t.Fatalf("out = %q, want the rule's allow", out)
	}
}

func TestSessionTokensAreStablePerSessionAndDistinct(t *testing.T) {
	sessionTab := newSessionTable()
	first := sessionTab.issueToken("s1")
	if first == "" || first != sessionTab.issueToken("s1") {
		t.Fatal("a session's token must not change while it runs")
	}
	if first == sessionTab.issueToken("s2") {
		t.Fatal("two sessions share a token")
	}
}

func TestSeedTokenDoesNotSurviveItsSeed(t *testing.T) {
	sessionTab := newSessionTable()
	seedCtx, endSeed := context.WithCancel(context.Background())
	token := sessionTab.seedToken(seedCtx, "dormant", allowEverySession)
	if !sessionTab.tokenMatches("dormant", token) {
		t.Fatal("the seed's token is refused while its seed runs")
	}
	endSeed()
	if sessionTab.tokenMatches("dormant", token) {
		t.Fatal("the token of a finished one-shot seed still answers approvals")
	}
}

func TestDriverStartedDuringASeedKeepsItsToken(t *testing.T) {
	sessionTab := newSessionTable()
	seedCtx, endSeed := context.WithCancel(context.Background())
	seedTok := sessionTab.seedToken(seedCtx, "s1", allowEverySession)
	driver := &sessionDriver{}
	sessionTab.register("s1", driver)
	driverTok := sessionTab.issueToken("s1")
	endSeed()
	if !sessionTab.tokenMatches("s1", driverTok) {
		t.Fatal("the driver's token was revoked when the seed that overlapped it ended")
	}
	if sessionTab.tokenMatches("s1", seedTok) {
		t.Fatal("the finished seed's token still answers for the session")
	}
	sessionTab.unregister("s1", driver)
	if sessionTab.tokenMatches("s1", driverTok) {
		t.Fatal("the driver's token outlived its run")
	}
}

func TestTokensOfSessionsGoneFromTheStoreAreRevoked(t *testing.T) {
	sessionTab := newSessionTable()
	gone := sessionTab.issueToken("gone")
	kept := sessionTab.issueToken("kept")
	archived := sessionTab.issueToken("archived")
	sessionTab.revokeTokensExcept(func(name string) bool { return name == "kept" })
	if sessionTab.tokenMatches("gone", gone) {
		t.Fatal("the token of a session removed from the store still answers approvals")
	}
	if sessionTab.tokenMatches("archived", archived) {
		t.Fatal("the token of an archived session still answers approvals")
	}
	if !sessionTab.tokenMatches("kept", kept) {
		t.Fatal("the token of a session still in the store was revoked")
	}
}
