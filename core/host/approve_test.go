package host

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
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
