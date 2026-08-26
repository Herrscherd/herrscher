package host

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/approval"
)

// approvalPickPrefix marks a select-menu value as an answer to an approval
// rather than a choice the model itself posed. The two ride the same Pick path
// from a gateway, and only the prefix tells them apart; it is written by this
// file and read by this file, so a gateway never learns the encoding.
const approvalPickPrefix = "herrscher-approval:"

// subjectMaxLen bounds how much model-written text is rendered into a menu or a
// terminal. A tool input can be a whole file.
const subjectMaxLen = 200

// defaultApprovalWait is the fallback wait when the caller passes none, matching
// state.ApprovalWait's own default.
const defaultApprovalWait = 5 * time.Minute

// pendingApproval is one request waiting for a human.
type pendingApproval struct {
	id      string
	session string
	req     approval.Request
	asked   time.Time
	answer  chan approvalVerdict
}

type approvalVerdict struct {
	decision approval.Decision
	reason   string
}

// PendingApproval is one waiting request, as an operator sees it.
type PendingApproval struct {
	ID      string        `json:"id"`
	Session string        `json:"session"`
	Tool    string        `json:"tool"`
	Subject string        `json:"subject"`
	Age     time.Duration `json:"age"`
}

// approvals is the daemon's registry of requests awaiting a human. It is
// package state for the same reason sessionRegistry is: the verbs that answer a
// request, the Pick that carries a click, and the hook call that opened it all
// reach it from different call paths, and none of them has a pointer to hand.
var approvals = struct {
	mu sync.Mutex
	m  map[string]*pendingApproval
}{m: map[string]*pendingApproval{}}

// askApproval returns the verdict for one tool call, asking a human only when
// the policy says to. It blocks for at most wait.
func askApproval(ctx context.Context, session string, req approval.Request, pol approval.Policy, mode approval.Mode, wait time.Duration) (approval.Decision, string) {
	raw, matched := pol.Decide(req)
	switch approval.Apply(mode, raw, matched) {
	case approval.Allow:
		return approval.Allow, ""
	case approval.Deny:
		return approval.Deny, fmt.Sprintf("herrscher: %s was denied by policy", describeRequest(req))
	}

	if wait <= 0 { // time.After(0) would fire instantly and deny every ask
		wait = defaultApprovalWait
	}
	// Warned here, on a call that is really about to wait for a human, and not in
	// the `approve ask` verb: the injected hook matches every tool call, so a
	// warning raised before the policy has spoken would put one identical line on
	// the daemon's stderr per tool call across the whole fleet, the vast majority
	// of which ask nobody. os.Stderr is the daemon's own; the hook process runs
	// this over the command socket and never writes it to its own stderr, which
	// no operator reads.
	warnWaitOutlivesTheHook(os.Stderr, wait)

	p := &pendingApproval{
		id:      newApprovalID(),
		session: session,
		req:     req,
		asked:   time.Now(),
		answer:  make(chan approvalVerdict, 1),
	}
	approvals.mu.Lock()
	approvals.m[p.id] = p
	approvals.mu.Unlock()
	defer func() {
		approvals.mu.Lock()
		delete(approvals.m, p.id)
		approvals.mu.Unlock()
	}()

	// The fan-out's own context is bounded by wait so a gateway whose RouteMenu
	// hangs cannot stretch the effective wait past the deadline the model was
	// promised; it is not detached into a goroutine because posting a menu for
	// an already-dead request is worse than a bounded wait.
	octx, cancel := context.WithTimeout(ctx, wait)
	AskApprovalOn(octx, session, approvalPrompt(req), p.id)
	cancel()

	select {
	case v := <-p.answer:
		if v.decision == approval.Allow {
			return approval.Allow, ""
		}
		reason := v.reason
		if reason == "" {
			reason = fmt.Sprintf("herrscher: %s was denied by the operator", describeRequest(req))
		}
		return approval.Deny, reason
	case <-time.After(wait):
		return approval.Deny, fmt.Sprintf("herrscher: nobody answered within %s, so %s was denied", wait, describeRequest(req))
	case <-ctx.Done():
		return approval.Deny, fmt.Sprintf("herrscher: the wait for approval ended, so %s was denied", describeRequest(req))
	}
}

// warnWaitOutlivesTheHook says so when the configured wait is longer than the
// vendor will wait for the hook to answer. Past that point the CLI stops
// waiting and runs the tool call, while the request is still listed as waiting
// and the operator still believes a human is being asked: a silent allow, which
// is the one outcome this feature exists to prevent.
//
// It warns and does not cap. The wait is the operator's to choose, and a wait
// quietly shortened to fit the vendor would be its own surprise. Written on
// every ask that really waits and would run long, rather than once at startup,
// because the wait is read from state at each ask and can change under a
// running daemon.
func warnWaitOutlivesTheHook(w io.Writer, wait time.Duration) {
	if wait <= agent.HookWait {
		return
	}
	fmt.Fprintf(w, "approvals: a %s wait outlives the %s the CLI gives the hook: past %s the tool call runs and nobody is told\n", wait, agent.HookWait, agent.HookWait)
}

// answerApproval settles a waiting request, reporting whether one was waiting.
func answerApproval(id string, d approval.Decision, reason string) bool {
	approvals.mu.Lock()
	p := approvals.m[id]
	approvals.mu.Unlock()
	if p == nil {
		return false
	}
	select {
	case p.answer <- approvalVerdict{decision: d, reason: reason}:
	default: // already answered; the first answer stands
	}
	return true
}

// pendingApprovals lists what is waiting, for `approve list`.
func pendingApprovals() []PendingApproval {
	approvals.mu.Lock()
	defer approvals.mu.Unlock()
	out := make([]PendingApproval, 0, len(approvals.m))
	for _, p := range approvals.m {
		out = append(out, PendingApproval{
			ID:      p.id,
			Session: p.session,
			Tool:    p.req.Tool,
			Subject: clip(p.req.Subject),
			Age:     time.Since(p.asked).Truncate(time.Second),
		})
	}
	return out
}

// answerApprovalPick settles a request from a routed select-menu value, and
// reports whether the value was one at all.
func answerApprovalPick(value string) bool {
	rest, ok := strings.CutPrefix(value, approvalPickPrefix)
	if !ok {
		return false
	}
	id, verdict, ok := strings.Cut(rest, ":")
	if !ok {
		return true // shaped like ours but unusable: swallow it rather than let it reach a backend
	}
	d := approval.Deny
	if verdict == string(approval.Allow) {
		d = approval.Allow
	}
	answerApproval(id, d, "")
	return true
}

func newApprovalID() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		// A collision here costs one misrouted answer, and the alternative is
		// refusing to ask at all. Time is unique enough at this rate.
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// describeRequest names a request in one line, for the model.
func describeRequest(req approval.Request) string {
	if req.Subject == "" {
		return req.Tool
	}
	return req.Tool + "(" + clip(req.Subject) + ")"
}

// approvalPrompt is what a human reads.
func approvalPrompt(req approval.Request) string {
	return "allow " + describeRequest(req) + "?"
}

// clip bounds model-written text and flattens it to one line: it is rendered
// into a chat menu and a terminal, neither of which should be handed an
// arbitrary number of newlines by a model.
func clip(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r", " "), "\n", " ")
	r := []rune(s)
	if len(r) > subjectMaxLen {
		return string(r[:subjectMaxLen]) + "…"
	}
	return string(r)
}

// approvalChoices are the two answers, encoded so a pick routes back here.
func approvalChoices(id string) []contracts.Choice {
	return []contracts.Choice{
		{Label: "allow", Value: approvalPickPrefix + id + ":allow"},
		{Label: "deny", Value: approvalPickPrefix + id + ":deny"},
	}
}
