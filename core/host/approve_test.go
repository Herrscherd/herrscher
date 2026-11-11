package host

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/approval"
)

type menuGatewayFake struct {
	fanRecorder

	channel  string
	routeErr error
	routed   []string
}

func (m *menuGatewayFake) DefaultChannel() string { return m.channel }

func (m *menuGatewayFake) RouteMenu(_ context.Context, _, _, prompt, _ string, _ []contracts.Choice) (contracts.MessageID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.routed = append(m.routed, prompt)
	if m.routeErr != nil {
		return "", m.routeErr
	}
	return "mid", nil
}

func TestAskApprovalAnswersWithoutAskingAnyone(t *testing.T) {
	approvalTab := newApprovalTable(io.Discard)
	sessionTab := newSessionTable()
	pol := approval.Policy{{Decision: approval.Deny, Tool: "Bash", Pattern: "sudo*"}}
	req := approval.Request{Tool: "Bash", Subject: "sudo rm"}
	d, reason := askApproval(context.Background(), approvalTab, sessionTab, "s1", req, pol, approval.ModeAsk, time.Minute)
	if d != approval.Deny {
		t.Fatalf("got %q, want deny", d)
	}
	if reason == "" {
		t.Fatal("a refusal must carry a reason the model can read")
	}
	if n := len(pendingApprovals(approvalTab)); n != 0 {
		t.Fatalf("%d pending: a decided request must never be registered", n)
	}
}

func TestAskApprovalWaitsThenIsAnswered(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		approvalTab := newApprovalTable(io.Discard)
		sessionTab := newSessionTable()
		pol := approval.Policy{{Decision: approval.Ask, Tool: "Bash", Pattern: "git push*"}}
		req := approval.Request{Tool: "Bash", Subject: "git push origin master"}
		done := make(chan approval.Decision, 1)
		go func() {
			d, _ := askApproval(context.Background(), approvalTab, sessionTab, "s1", req, pol, approval.ModeAsk, 5*time.Second)
			done <- d
		}()
		id := awaitPending(t, approvalTab, 1)[0].ID
		if !answerApproval(approvalTab, id, approval.Allow) {
			t.Fatal("answerApproval reported no such request")
		}
		if d := <-done; d != approval.Allow {
			t.Fatalf("got %q, want allow", d)
		}
		if n := len(pendingApprovals(approvalTab)); n != 0 {
			t.Fatalf("%d pending: an answered request must be dropped", n)
		}
	})
}

func TestAskApprovalTimesOutIntoADenial(t *testing.T) {
	approvalTab := newApprovalTable(io.Discard)
	sessionTab := newSessionTable()
	pol := approval.Policy{{Decision: approval.Ask, Tool: "*", Pattern: ""}}
	req := approval.Request{Tool: "Bash", Subject: "git push"}
	d, reason := askApproval(context.Background(), approvalTab, sessionTab, "s1", req, pol, approval.ModeAsk, 30*time.Millisecond)
	if d != approval.Deny {
		t.Fatalf("got %q, want deny", d)
	}
	if !strings.Contains(reason, "30ms") {
		t.Fatalf("reason %q must name the wait it gave up on", reason)
	}
	if n := len(pendingApprovals(approvalTab)); n != 0 {
		t.Fatalf("%d pending: a timed-out request must be dropped", n)
	}
}

func answerApproval(approvalTab *approvalTable, id string, decision approval.Decision) bool {
	scope := scopeOnce
	if decision == approval.Deny {
		scope = scopeDeny
	}
	return settleApproval(approvalTab, newSessionTable(), id, approvalAnswer{scope: scope})
}

func waitingFor(approvalTab *approvalTable, session string) int {
	n := 0
	for _, p := range pendingApprovals(approvalTab) {
		if p.Session == session {
			n++
		}
	}
	return n
}

func fillApprovals(t *testing.T, approvalTab *approvalTable, sessionTab *sessionTable, sessions ...string) func() {
	t.Helper()
	pol := approval.Policy{{Decision: approval.Ask, Tool: "*", Pattern: ""}}
	mine := make(map[string]bool)
	for _, s := range sessions {
		mine[s] = true
	}
	var wg sync.WaitGroup
	for _, s := range sessions {
		wg.Add(1)
		go func(session string) {
			defer wg.Done()
			askApproval(context.Background(), approvalTab, sessionTab, session, approval.Request{Tool: "Bash", Subject: session}, pol, approval.ModeAsk, time.Minute)
		}(s)
	}
	var ids []string
	requireSettled(t, func() bool {
		ids = ids[:0]
		for _, p := range pendingApprovals(approvalTab) {
			if mine[p.Session] {
				ids = append(ids, p.ID)
			}
		}
		return len(ids) == len(sessions)
	}, "every session's approval registered")
	return func() {
		for _, id := range ids {
			answerApproval(approvalTab, id, approval.Deny)
		}
		wg.Wait()
	}
}

func TestOneSessionCannotFillTheRegistry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		approvalTab := newApprovalTable(io.Discard)
		sessionTab := newSessionTable()
		full := make([]string, 0, maxPendingPerSession)
		for range maxPendingPerSession {
			full = append(full, "s-cap")
		}
		settle := fillApprovals(t, approvalTab, sessionTab, full...)

		pol := approval.Policy{{Decision: approval.Ask, Tool: "*", Pattern: ""}}
		started := time.Now()
		d, reason := askApproval(context.Background(), approvalTab, sessionTab, "s-cap", approval.Request{Tool: "Bash", Subject: "one too many"}, pol, approval.ModeAsk, time.Minute)
		if d != approval.Deny {
			t.Fatalf("got %q, want deny: over the cap is a decision, not a failure to reach anyone", d)
		}
		if !strings.Contains(reason, strconv.Itoa(maxPendingPerSession)) {
			t.Fatalf("reason %q must name the cap it hit", reason)
		}
		if elapsed := time.Since(started); elapsed > 5*time.Second {
			t.Fatalf("the refusal took %s: over the cap must be decided without waiting for anyone", elapsed)
		}
		if n := waitingFor(approvalTab, "s-cap"); n != maxPendingPerSession {
			t.Fatalf("%d waiting, want %d: a refused request must not be registered", n, maxPendingPerSession)
		}

		fillApprovals(t, approvalTab, sessionTab, "s-cap-neighbour")()

		settle()
		if n := waitingFor(approvalTab, "s-cap"); n != 0 {
			t.Fatalf("%d still waiting after every request was answered", n)
		}
		fillApprovals(t, approvalTab, sessionTab, "s-cap")()
	})
}

func TestTheRegistryIsBoundedAcrossSessions(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		approvalTab := newApprovalTable(io.Discard)
		sessionTab := newSessionTable()
		sessions := make([]string, 0, maxPendingApprovals)
		for i := range maxPendingApprovals {
			sessions = append(sessions, fmt.Sprintf("s-fleet-%d", i))
		}
		settle := fillApprovals(t, approvalTab, sessionTab, sessions...)

		pol := approval.Policy{{Decision: approval.Ask, Tool: "*", Pattern: ""}}
		d, reason := askApproval(context.Background(), approvalTab, sessionTab, "s-fleet-extra", approval.Request{Tool: "Bash", Subject: "x"}, pol, approval.ModeAsk, time.Minute)
		if d != approval.Deny {
			t.Fatalf("got %q, want deny", d)
		}
		if !strings.Contains(reason, strconv.Itoa(maxPendingApprovals)) {
			t.Fatalf("reason %q must name the cap it hit", reason)
		}

		settle()
		fillApprovals(t, approvalTab, sessionTab, "s-fleet-extra")()
	})
}

func TestATimedOutRequestFreesItsSlot(t *testing.T) {
	approvalTab := newApprovalTable(io.Discard)
	sessionTab := newSessionTable()
	pol := approval.Policy{{Decision: approval.Ask, Tool: "*", Pattern: ""}}
	var reason string
	for range maxPendingPerSession + 1 {
		var d approval.Decision
		d, reason = askApproval(context.Background(), approvalTab, sessionTab, "s-expiry", approval.Request{Tool: "Bash", Subject: "x"}, pol, approval.ModeAsk, 20*time.Millisecond)
		if d != approval.Deny {
			t.Fatalf("got %q, want deny: nobody answered", d)
		}
	}
	if !strings.Contains(reason, "nobody answered") {
		t.Fatalf("reason %q: more asks than the cap allows still timed out one by one, so none of them may be refused for the cap", reason)
	}
	if n := waitingFor(approvalTab, "s-expiry"); n != 0 {
		t.Fatalf("%d still waiting: a timed-out request must leave the registry", n)
	}
}

func TestAnswerApprovalUnknownID(t *testing.T) {
	approvalTab := newApprovalTable(io.Discard)
	if answerApproval(approvalTab, "nope", approval.Allow) {
		t.Fatal("answering an unknown id must report false")
	}
}

func TestTwoSessionsWaitIndependently(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		approvalTab := newApprovalTable(io.Discard)
		sessionTab := newSessionTable()
		pol := approval.Policy{{Decision: approval.Ask, Tool: "*", Pattern: ""}}
		got := make(chan string, 2)
		for _, name := range []string{"s1", "s2"} {
			go func(session string) {
				d, _ := askApproval(context.Background(), approvalTab, sessionTab, session, approval.Request{Tool: "Bash", Subject: session}, pol, approval.ModeAsk, 5*time.Second)
				got <- session + ":" + d.String()
			}(name)
		}
		var byID map[string]string
		requireSettled(t, func() bool {
			byID = pendingBySession(approvalTab)
			return len(byID) >= 2
		}, "two approvals registered")
		if len(byID) != 2 {
			t.Fatalf("got %d pending, want 2: one session's answer must not settle another's", len(byID))
		}
		answerSessionsApart(approvalTab, byID)
		seen := map[string]bool{<-got: true, <-got: true}
		if !seen["s1:allow"] || !seen["s2:deny"] {
			t.Fatalf("got %v, want s1 allowed and s2 denied", seen)
		}
	})
}

func pendingBySession(approvalTab *approvalTable) map[string]string {
	byID := make(map[string]string)
	for _, p := range pendingApprovals(approvalTab) {
		byID[p.ID] = p.Session
	}
	return byID
}

func answerSessionsApart(approvalTab *approvalTable, byID map[string]string) {
	for id, session := range byID {
		if session == "s1" {
			answerApproval(approvalTab, id, approval.Allow)
			continue
		}
		answerApproval(approvalTab, id, approval.Deny)
	}
}

func TestSessionDriverAskApproval(t *testing.T) {
	tests := []struct {
		name       string
		build      func() (d *sessionDriver, gw *menuGatewayFake)
		wantRouted int
		wantText   int
	}{
		{
			name: "routes a menu when the reader can route and has a channel",
			build: func() (*sessionDriver, *menuGatewayFake) {
				gw := &menuGatewayFake{channel: "chan1"}
				d := &sessionDriver{name: "s1", gateways: []contracts.GatewaySet{{Gateway: gw, Reader: gw}}}
				return d, gw
			},
			wantRouted: 1,
			wantText:   0,
		},
		{
			name: "falls back to text when RouteMenu errors",
			build: func() (*sessionDriver, *menuGatewayFake) {
				gw := &menuGatewayFake{channel: "chan1", routeErr: errors.New("boom")}
				d := &sessionDriver{name: "s1", gateways: []contracts.GatewaySet{{Gateway: gw, Reader: gw}}}
				return d, gw
			},
			wantRouted: 1,
			wantText:   1,
		},
		{
			name: "falls back to text when the reader has no channel at all",
			build: func() (*sessionDriver, *menuGatewayFake) {
				gw := &menuGatewayFake{channel: ""}
				d := &sessionDriver{name: "s1", gateways: []contracts.GatewaySet{{Gateway: gw, Reader: gw}}}
				return d, gw
			},
			wantRouted: 0,
			wantText:   1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, gw := tt.build()
			d.askApproval(context.Background(), "id1", approval.Request{Tool: "Bash", Subject: "rm"}, time.Now().Add(time.Minute))
			gw.mu.Lock()
			defer gw.mu.Unlock()
			if len(gw.routed) != tt.wantRouted {
				t.Fatalf("RouteMenu called %d times, want %d", len(gw.routed), tt.wantRouted)
			}
			if len(gw.posted) != tt.wantText {
				t.Fatalf("text fallback posted %d times, want %d", len(gw.posted), tt.wantText)
			}
		})
	}

	t.Run("no panic and nothing posted with a nil Gateway and no usable router", func(t *testing.T) {
		reader := &fanRecorder{}
		d := &sessionDriver{name: "s1", gateways: []contracts.GatewaySet{{Gateway: nil, Reader: reader}}}
		d.askApproval(context.Background(), "id1", approval.Request{Tool: "Bash", Subject: "rm"}, time.Now().Add(time.Minute))
		reader.mu.Lock()
		defer reader.mu.Unlock()
		if len(reader.posted) != 0 {
			t.Fatalf("posted %v, want nothing: no Gateway to fall back to", reader.posted)
		}
	})
}

func TestPickRoutesApprovalValuesAwayFromTheBackend(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		approvalTab := newApprovalTable(io.Discard)
		sessionTab := newSessionTable()
		pol := approval.Policy{{Decision: approval.Ask, Tool: "*", Pattern: ""}}
		done := make(chan approval.Decision, 1)
		go func() {
			d, _ := askApproval(context.Background(), approvalTab, sessionTab, "s-no-driver", approval.Request{Tool: "Bash", Subject: "x"}, pol, approval.ModeAsk, 5*time.Second)
			done <- d
		}()
		id := awaitPending(t, approvalTab, 1)[0].ID
		if !(&hub{warnings: io.Discard, approvals: approvalTab, sessions: sessionTab}).Pick("s-no-driver", approvalPickPrefix+id+":deny") {
			t.Fatal("an approval pick must be handled even with no live driver")
		}
		if d := <-done; d != approval.Deny {
			t.Fatalf("got %q, want deny", d)
		}
	})
}

func TestALongWaitIsCalledOutButNotShortened(t *testing.T) {
	var warnings bytes.Buffer
	approvalTab := newApprovalTable(&warnings)
	sessionTab := newSessionTable()
	ask := approval.Policy{{Decision: approval.Ask, Tool: "*", Pattern: ""}}
	req := approval.Request{Tool: "Bash", Subject: "git push"}
	fits, outlives := agent.HookWait/2, agent.HookWait/2+time.Minute
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	askApproval(ctx, approvalTab, sessionTab, "s-warn", req, ask, approval.ModeAsk, fits)
	if warnings.Len() != 0 {
		t.Fatalf("a wait the vendor will sit through must say nothing, got %q", warnings.String())
	}

	askApproval(ctx, approvalTab, sessionTab, "s-warn", req, nil, 0, outlives)
	if warnings.Len() != 0 {
		t.Fatalf("a call nobody is asked about must say nothing, got %q", warnings.String())
	}

	askApproval(ctx, approvalTab, sessionTab, "s-warn", req, ask, approval.ModeAsk, outlives)
	got := warnings.String()
	if !strings.Contains(got, outlives.String()) {
		t.Fatalf("warning %q must name the wait that was asked for", got)
	}
	if !strings.Contains(got, agent.HookWait.String()) {
		t.Fatalf("warning %q must name what the vendor will wait", got)
	}
}

func awaitPending(t *testing.T, tab *approvalTable, want int) []PendingApproval {
	t.Helper()
	var pending []PendingApproval
	requireSettled(t, func() bool {
		pending = pendingApprovals(tab)
		return len(pending) == want
	}, "the approval registered")
	return pending
}
