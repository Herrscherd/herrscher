package host

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/approval"
)

// menuGatewayFake is a fanRecorder that also plays the reader shape
// askApproval must handle: a channel that may or may not route menus. Its
// Manifest, like fanRecorder's, declares no SelectMenus capability, so
// contracts.Degrade's text fallback posts through the embedded fanRecorder's
// Post (recorded in .posted) rather than through Menu.
type menuGatewayFake struct {
	fanRecorder
	channel  string
	routeErr error
	routed   []string // prompts passed to RouteMenu
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
	pol := approval.Policy{{Decision: approval.Deny, Tool: "Bash", Pattern: "sudo*"}}
	req := approval.Request{Tool: "Bash", Subject: "sudo rm"}
	d, reason := askApproval(context.Background(), "s1", req, pol, approval.ModeAsk, time.Minute)
	if d != approval.Deny {
		t.Fatalf("got %q, want deny", d)
	}
	if reason == "" {
		t.Fatal("a refusal must carry a reason the model can read")
	}
	if n := len(pendingApprovals()); n != 0 {
		t.Fatalf("%d pending: a decided request must never be registered", n)
	}
}

func TestAskApprovalWaitsThenIsAnswered(t *testing.T) {
	pol := approval.Policy{{Decision: approval.Ask, Tool: "Bash", Pattern: "git push*"}}
	req := approval.Request{Tool: "Bash", Subject: "git push origin master"}
	done := make(chan approval.Decision, 1)
	go func() {
		d, _ := askApproval(context.Background(), "s1", req, pol, approval.ModeAsk, 5*time.Second)
		done <- d
	}()
	var id string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if p := pendingApprovals(); len(p) == 1 {
			id = p[0].ID
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if id == "" {
		t.Fatal("the request was never registered")
	}
	if !answerApproval(id, approval.Allow, "") {
		t.Fatal("answerApproval reported no such request")
	}
	if d := <-done; d != approval.Allow {
		t.Fatalf("got %q, want allow", d)
	}
	if n := len(pendingApprovals()); n != 0 {
		t.Fatalf("%d pending: an answered request must be dropped", n)
	}
}

func TestAskApprovalTimesOutIntoADenial(t *testing.T) {
	pol := approval.Policy{{Decision: approval.Ask, Tool: "*", Pattern: ""}}
	req := approval.Request{Tool: "Bash", Subject: "git push"}
	d, reason := askApproval(context.Background(), "s1", req, pol, approval.ModeAsk, 30*time.Millisecond)
	if d != approval.Deny {
		t.Fatalf("got %q, want deny", d)
	}
	if !strings.Contains(reason, "30ms") {
		t.Fatalf("reason %q must name the wait it gave up on", reason)
	}
	if n := len(pendingApprovals()); n != 0 {
		t.Fatalf("%d pending: a timed-out request must be dropped", n)
	}
}

// waitingFor counts what the registry holds for one session.
func waitingFor(session string) int {
	n := 0
	for _, p := range pendingApprovals() {
		if p.Session == session {
			n++
		}
	}
	return n
}

// fillApprovals opens one waiting request per session named and blocks until
// they are all registered. The returned function answers every one of them and
// waits for the asks to return, so a test leaves the registry as it found it.
//
// The wait is long on purpose: these requests are settled by hand, and a timer
// that fired first would free the very slot the cap is being read through.
func fillApprovals(t *testing.T, sessions ...string) func() {
	t.Helper()
	pol := approval.Policy{{Decision: approval.Ask, Tool: "*", Pattern: ""}}
	mine := map[string]bool{}
	for _, s := range sessions {
		mine[s] = true
	}
	var wg sync.WaitGroup
	for _, s := range sessions {
		wg.Add(1)
		go func(session string) {
			defer wg.Done()
			askApproval(context.Background(), session, approval.Request{Tool: "Bash", Subject: session}, pol, approval.ModeAsk, time.Minute)
		}(s)
	}
	var ids []string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ids = ids[:0]
		for _, p := range pendingApprovals() {
			if mine[p.Session] {
				ids = append(ids, p.ID)
			}
		}
		if len(ids) == len(sessions) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(ids) != len(sessions) {
		t.Fatalf("%d of %d requests registered", len(ids), len(sessions))
	}
	return func() {
		for _, id := range ids {
			answerApproval(id, approval.Deny, "")
		}
		wg.Wait()
	}
}

// One session cannot hold more than its share of the registry. Every waiting
// request costs a goroutine, a command-socket connection and a menu somebody
// has to read, and `approve ask` takes the session it is told: unbounded, an
// agent looping on it floods a channel that is not even its own.
func TestOneSessionCannotFillTheRegistry(t *testing.T) {
	full := make([]string, 0, maxPendingPerSession)
	for range maxPendingPerSession {
		full = append(full, "s-cap")
	}
	settle := fillApprovals(t, full...)

	pol := approval.Policy{{Decision: approval.Ask, Tool: "*", Pattern: ""}}
	started := time.Now()
	d, reason := askApproval(context.Background(), "s-cap", approval.Request{Tool: "Bash", Subject: "one too many"}, pol, approval.ModeAsk, time.Minute)
	if d != approval.Deny {
		t.Fatalf("got %q, want deny: over the cap is a decision, not a failure to reach anyone", d)
	}
	if !strings.Contains(reason, strconv.Itoa(maxPendingPerSession)) {
		t.Fatalf("reason %q must name the cap it hit", reason)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("the refusal took %s: over the cap must be decided without waiting for anyone", elapsed)
	}
	if n := waitingFor("s-cap"); n != maxPendingPerSession {
		t.Fatalf("%d waiting, want %d: a refused request must not be registered", n, maxPendingPerSession)
	}

	// A neighbour is not punished for it: the cap is per session precisely so
	// one agent's flood cannot stop the rest of the fleet from asking.
	fillApprovals(t, "s-cap-neighbour")()

	// Settled, the registry gives the room back, and the session asks again.
	settle()
	if n := waitingFor("s-cap"); n != 0 {
		t.Fatalf("%d still waiting after every request was answered", n)
	}
	fillApprovals(t, "s-cap")()
}

// The same bound across the whole fleet, so a caller naming a fresh session
// every time cannot walk around the per-session one.
func TestTheRegistryIsBoundedAcrossSessions(t *testing.T) {
	sessions := make([]string, 0, maxPendingApprovals)
	for i := range maxPendingApprovals {
		sessions = append(sessions, fmt.Sprintf("s-fleet-%d", i))
	}
	settle := fillApprovals(t, sessions...)

	pol := approval.Policy{{Decision: approval.Ask, Tool: "*", Pattern: ""}}
	d, reason := askApproval(context.Background(), "s-fleet-extra", approval.Request{Tool: "Bash", Subject: "x"}, pol, approval.ModeAsk, time.Minute)
	if d != approval.Deny {
		t.Fatalf("got %q, want deny", d)
	}
	if !strings.Contains(reason, strconv.Itoa(maxPendingApprovals)) {
		t.Fatalf("reason %q must name the cap it hit", reason)
	}

	settle()
	fillApprovals(t, "s-fleet-extra")()
}

// A request nobody answers has to free its slot as surely as an answered one.
// Leaked entries would fill the cap with requests no human ever sees, and the
// session would stop asking without anything having gone wrong.
func TestATimedOutRequestFreesItsSlot(t *testing.T) {
	pol := approval.Policy{{Decision: approval.Ask, Tool: "*", Pattern: ""}}
	var reason string
	for range maxPendingPerSession + 1 {
		var d approval.Decision
		d, reason = askApproval(context.Background(), "s-expiry", approval.Request{Tool: "Bash", Subject: "x"}, pol, approval.ModeAsk, 20*time.Millisecond)
		if d != approval.Deny {
			t.Fatalf("got %q, want deny: nobody answered", d)
		}
	}
	if !strings.Contains(reason, "nobody answered") {
		t.Fatalf("reason %q: more asks than the cap allows still timed out one by one, so none of them may be refused for the cap", reason)
	}
	if n := waitingFor("s-expiry"); n != 0 {
		t.Fatalf("%d still waiting: a timed-out request must leave the registry", n)
	}
}

func TestAnswerApprovalUnknownID(t *testing.T) {
	if answerApproval("nope", approval.Allow, "") {
		t.Fatal("answering an unknown id must report false")
	}
}

func TestTwoSessionsWaitIndependently(t *testing.T) {
	pol := approval.Policy{{Decision: approval.Ask, Tool: "*", Pattern: ""}}
	got := make(chan string, 2)
	for _, name := range []string{"s1", "s2"} {
		go func(session string) {
			d, _ := askApproval(context.Background(), session, approval.Request{Tool: "Bash", Subject: session}, pol, approval.ModeAsk, 5*time.Second)
			got <- session + ":" + string(d)
		}(name)
	}
	byID := map[string]string{}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(byID) < 2 {
		for _, p := range pendingApprovals() {
			byID[p.ID] = p.Session
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(byID) != 2 {
		t.Fatalf("got %d pending, want 2: one session's answer must not settle another's", len(byID))
	}
	for id, session := range byID {
		if session == "s1" {
			answerApproval(id, approval.Allow, "")
		} else {
			answerApproval(id, approval.Deny, "")
		}
	}
	seen := map[string]bool{<-got: true, <-got: true}
	if !seen["s1:allow"] || !seen["s2:deny"] {
		t.Fatalf("got %v, want s1 allowed and s2 denied", seen)
	}
}

// TestSessionDriverAskApproval covers the four shapes askApproval must
// handle when fanning a question out to a session's gateways: a gateway that
// routes a real menu, one whose RouteMenu fails and must fall back to text,
// one with neither a usable router nor a Gateway to fall back to, and one
// with no channel to post on at all.
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
			d.askApproval(context.Background(), "allow Bash(rm)?", "id1")
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
		reader := &fanRecorder{} // implements ChannelReader but not contracts.MenuRouter
		d := &sessionDriver{name: "s1", gateways: []contracts.GatewaySet{{Gateway: nil, Reader: reader}}}
		d.askApproval(context.Background(), "allow Bash(rm)?", "id1")
		reader.mu.Lock()
		defer reader.mu.Unlock()
		if len(reader.posted) != 0 {
			t.Fatalf("posted %v, want nothing: no Gateway to fall back to", reader.posted)
		}
	})
}

func TestPickRoutesApprovalValuesAwayFromTheBackend(t *testing.T) {
	pol := approval.Policy{{Decision: approval.Ask, Tool: "*", Pattern: ""}}
	done := make(chan approval.Decision, 1)
	go func() {
		d, _ := askApproval(context.Background(), "s-no-driver", approval.Request{Tool: "Bash", Subject: "x"}, pol, approval.ModeAsk, 5*time.Second)
		done <- d
	}()
	var id string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if p := pendingApprovals(); len(p) == 1 {
			id = p[0].ID
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if id == "" {
		t.Fatal("the request was never registered")
	}
	// No live driver by that name: without the interception this Pick would
	// report false, and the waiting request would sit until it timed out.
	if !Pick("s-no-driver", approvalPickPrefix+id+":deny") {
		t.Fatal("an approval pick must be handled even with no live driver")
	}
	if d := <-done; d != approval.Deny {
		t.Fatalf("got %q, want deny", d)
	}
}

// A wait that can outlive the vendor's hook timeout is a silent allow: the CLI
// stops waiting for the hook and runs the tool call, while the operator is
// still looking at a request that says it is waiting for him. It is said on the
// ask that will really wait, and only there: the injected hook matches every
// tool call, so a warning raised before the policy has spoken would repeat
// itself once per tool call for calls nobody is ever asked about.
//
// What is compared is the worst case, twice the wait: the menu fan-out is
// bounded by the wait and the timer that follows it runs for the wait again. A
// wait of half the hook's timeout is the last one that always fits.
func TestALongWaitIsCalledOutButNotShortened(t *testing.T) {
	ask := approval.Policy{{Decision: approval.Ask, Tool: "*", Pattern: ""}}
	req := approval.Request{Tool: "Bash", Subject: "git push"}
	fits, outlives := agent.HookWait/2, agent.HookWait/2+time.Minute
	// A context already done ends the wait at once. What is under test is what
	// was written before the wait began, not how the request settles.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	quiet := captureDaemonStderr(t, func() {
		askApproval(ctx, "s-warn", req, ask, approval.ModeAsk, fits)
	})
	if quiet != "" {
		t.Fatalf("a wait the vendor will sit through must say nothing, got %q", quiet)
	}

	allowed := captureDaemonStderr(t, func() {
		askApproval(ctx, "s-warn", req, nil, approval.ModeAsk, outlives)
	})
	if allowed != "" {
		t.Fatalf("a call nobody is asked about must say nothing, got %q", allowed)
	}

	got := captureDaemonStderr(t, func() {
		askApproval(ctx, "s-warn", req, ask, approval.ModeAsk, outlives)
	})
	if !strings.Contains(got, outlives.String()) {
		t.Fatalf("warning %q must name the wait that was asked for", got)
	}
	if !strings.Contains(got, agent.HookWait.String()) {
		t.Fatalf("warning %q must name what the vendor will wait", got)
	}
}

// captureDaemonStderr runs fn with the daemon's stderr on a pipe, and returns
// what was written to it. The warning is aimed at the operator reading the
// daemon's log, so the stream it lands on is part of what is under test.
func captureDaemonStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	saved := os.Stderr
	os.Stderr = w
	// Deferred: fn panicking would otherwise leave the package's own stderr on a
	// pipe nobody drains, and every test after this one would write into it.
	defer func() { os.Stderr = saved }()
	read := make(chan string, 1)
	go func() {
		var b bytes.Buffer
		_, _ = io.Copy(&b, r)
		read <- b.String()
	}()
	fn()
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	out := <-read
	if err := r.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return out
}
