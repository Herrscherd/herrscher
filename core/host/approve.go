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
	"github.com/Herrscherd/herrscher/core/internal/state"
)

// approvalPickPrefix marks a select-menu value as an answer to an approval
// rather than a choice the model itself posed. The two ride the same Pick path
// from a gateway, and only the prefix tells them apart; it is written by this
// file and read by this file, so a gateway never learns the encoding.
const approvalPickPrefix = "herrscher-approval:"

// subjectMaxLen bounds how much model-written text is rendered into a menu or a
// terminal. A tool input can be a whole file.
const subjectMaxLen = 200

// maxPendingPerSession and maxPendingApprovals bound the registry below.
//
// Every waiting request costs a daemon goroutine, a command-socket connection
// held open for the whole wait, a map entry and a menu posted to a human. The
// caller is a hook the agent's own tool calls spawn, so an agent that loops on
// `approve ask` opens one of each per iteration, and nothing about a request
// that nobody has answered yet makes the next one cheaper.
//
// Four per session, because a session is one agent talking to one human: Claude
// Code can emit a small block of tool calls in parallel, so one is too few, and
// past a handful the human is the queue and the session is already unusable.
// Forty in total, an order of magnitude above it, is ten such sessions all at
// their own limit: enough that a real fleet never meets it, small enough that
// the goroutines, the connections and the chat posts stay countable.
//
// A request over either cap is refused rather than allowed. It is a decision
// the daemon makes on purpose, not a failure of the hook to reach it, which is
// the case that must always allow.
const (
	maxPendingPerSession = 4
	maxPendingApprovals  = 40
)

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
		wait = state.DefaultApprovalWait
	}

	p := &pendingApproval{
		id:      newApprovalID(),
		session: session,
		req:     req,
		asked:   time.Now(),
		answer:  make(chan approvalVerdict, 1),
	}
	// Registered before anything is posted anywhere: over the cap the answer is
	// known already, and the point of the cap is that the expensive part never
	// runs. release drops the entry on every way out of this function, the
	// timeout and the answer that arrives too late included, so a cap can only
	// be reached by requests a human really has in front of him.
	release, refusal := registerApproval(p)
	if refusal != "" {
		return approval.Deny, refusal
	}
	defer release()

	// Warned here, on a call that is really about to wait for a human, and not in
	// the `approve ask` verb: the injected hook matches every tool call, so a
	// warning raised before the policy has spoken would put one identical line on
	// the daemon's stderr per tool call across the whole fleet, the vast majority
	// of which ask nobody. os.Stderr is the daemon's own; the hook process runs
	// this over the command socket and never writes it to its own stderr, which
	// no operator reads.
	warnWaitOutlivesTheHook(os.Stderr, wait)

	// The fan-out's own context is bounded by wait so a gateway whose RouteMenu
	// hangs cannot stretch the effective wait past the deadline the model was
	// promised; it is not detached into a goroutine because posting a menu for
	// an already-dead request is worse than a bounded wait.
	octx, cancel := context.WithTimeout(ctx, wait)
	askApprovalOn(octx, session, approvalPrompt(req), p.id)
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

// registerApproval puts p in the registry unless a cap is reached, and returns
// the function that takes it back out. The second return is empty when the
// request was registered, and otherwise is the refusal the model reads: it
// names the cap that was hit and what to do about it, because the agent whose
// tool call this is has no other way of learning why the answer came back at
// once.
//
// Both counts are read under the same lock the insert takes, so two hooks
// racing cannot both find room for the last slot.
func registerApproval(p *pendingApproval) (release func(), refusal string) {
	approvals.mu.Lock()
	defer approvals.mu.Unlock()
	if len(approvals.m) >= maxPendingApprovals {
		return nil, fmt.Sprintf("herrscher: %s was denied: %d tool calls across the fleet are already waiting for a human, which is the limit; each one answered frees a slot", describeRequest(p.req), maxPendingApprovals)
	}
	mine := 0
	for _, other := range approvals.m {
		if other.session == p.session {
			mine++
		}
	}
	if mine >= maxPendingPerSession {
		return nil, fmt.Sprintf("herrscher: %s was denied: session %q already has %d tool calls waiting for a human, which is the limit; wait for those to be answered, and ask about fewer things at once", describeRequest(p.req), p.session, maxPendingPerSession)
	}
	approvals.m[p.id] = p
	return func() {
		approvals.mu.Lock()
		delete(approvals.m, p.id)
		approvals.mu.Unlock()
	}, ""
}

// warnWaitOutlivesTheHook says so when one ask can hold the hook longer than
// the vendor will wait for it to answer. Past that point the CLI stops waiting
// and runs the tool call, while the request is still listed as waiting and the
// operator still believes a human is being asked: a silent allow, which is the
// one outcome this feature exists to prevent.
//
// What is compared is the worst case and not the configured wait: the menu
// fan-out is bounded by the wait, and the timer that follows it runs for the
// wait again, so an ask can take twice what was configured. The comparison
// halves the hook's timeout rather than doubling the wait, so an absurd
// configured duration cannot overflow into a negative that warns about nothing.
//
// It warns and does not cap. The wait is the operator's to choose, and a wait
// quietly shortened to fit the vendor would be its own surprise. Written on
// every ask that really waits and would run long, rather than once at startup,
// because the wait is read from state at each ask and can change under a
// running daemon.
func warnWaitOutlivesTheHook(w io.Writer, wait time.Duration) {
	if wait <= agent.HookWait/2 {
		return
	}
	fmt.Fprintf(w, "approvals: a %s wait can hold the hook for twice that (a slow menu post, then the timer), longer than the %s the CLI gives it: past %s the tool call runs and nobody is told\n", wait, agent.HookWait, agent.HookWait)
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
