package host

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/approval"
)

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
