//go:build !windows

package host

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/approval"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

type escalationRig struct {
	store     *state.State
	approvals *approvalTable
	sessions  *sessionTable
	socket    string
	envFile   string
	events    *eventLog
}

type eventLog struct {
	mu     sync.Mutex
	notice []string
}

func (l *eventLog) publish(session string, event contracts.Event) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.notice = append(l.notice, session+": "+event.Text)
}

func (l *eventLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.notice)
}

func newEscalationRig(t *testing.T) escalationRig {
	t.Helper()
	st, err := state.LoadState(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	for _, name := range []string{"s1", "s2"} {
		if addErr := st.AddSession(state.Session{Name: name}); addErr != nil {
			t.Fatalf("AddSession: %v", addErr)
		}
	}
	envFile := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envFile, []byte("# kept\nHERRSCHER_BACKEND=claude\nGATEWAY_BOT_TOKEN=s3cret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reg := cli.NewRegistry(clitext.NewMessages())
	approvalTab := newApprovalTable(io.Discard)
	sessionTab := newSessionTable(newHostMessages(), clitext.NewMessages())
	if approveErr := addApproveCommands(reg, newHostMessages(), clitext.NewMessages(), fixedLanguage(i18n.English), io.Discard, st, nil, approvalTab, sessionTab); approveErr != nil {
		t.Fatalf("addApproveCommands: %v", approveErr)
	}
	if err := addVerbs(reg, contracts.New("session", "send").Param(paramName, "", true).Param("text", "", true).Do(func(context.Context, contracts.Input) (contracts.Result, error) {
		return contracts.DataResult("sent"), nil
	})); err != nil {
		t.Fatal(err)
	}
	notices := &noticeSlot{}
	if err := addVerbs(reg, envVerbs(clitext.NewMessages(), fixedLanguage(i18n.English), func() string { return envFile }, newEditLock(), notices)...); err != nil {
		t.Fatal(err)
	}
	desk := newApprovalDesk(newHostMessages(), fixedLanguage(i18n.English), io.Discard, st, nil, approvalTab, sessionTab)
	events := &eventLog{}
	h := testHub(t, &hub{messages: newHostMessages(), cliMessages: clitext.NewMessages(), warnings: io.Discard, st: st, reg: reg, desk: desk, live: make(map[string]liveSession), eventPublisher: events.publish, selfRestarts: newSelfRestarts(st)})
	notices.bind(h.announce)
	socket := filepath.Join(t.TempDir(), "s1.sock")
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	serveTestCommandSocket(ctx, t, isolatedRuntimeDir(t), socket, asSession{disp: h, session: "s1", lang: h.sessionLanguageSource("s1"), messages: clitext.NewMessages()}, defaultCommandReadTimeout)
	return escalationRig{store: st, approvals: approvalTab, sessions: sessionTab, socket: socket, envFile: envFile, events: events}
}

func (r escalationRig) dispatchAsync(t *testing.T, argv []string) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		_, _, err := dispatchLiveCommand(t.Context(), newHostMessages(), isolatedRuntimeDir(t), r.socket, argv)
		done <- err
	}()
	return done
}

func (r escalationRig) dispatch(t *testing.T, argv ...string) (string, error) {
	t.Helper()
	out, _, err := dispatchLiveCommand(t.Context(), newHostMessages(), isolatedRuntimeDir(t), r.socket, argv)
	return out, err
}

func TestAVerbTheAgentMayNotRunIsPutToTheOperatorAndRunsOnAllow(t *testing.T) {
	rig := newEscalationRig(t)
	done := rig.dispatchAsync(t, []string{"approve", "rule", "allow Read(*)"})

	pending := onlyPending(t, rig.approvals)
	if pending.Session != "s1" || pending.Tool != approval.VerbTool || pending.Subject != `approve rule "allow Read(*)"` {
		t.Fatalf("pending = %+v, want the verb asked as Herrscher(approve rule \"allow Read(*)\") for s1", pending)
	}
	if !answerApproval(rig.approvals, pending.ID, approval.Allow) {
		t.Fatal("the question vanished before the operator answered")
	}
	if err := <-done; err != nil {
		t.Fatalf("the allowed verb failed: %v", err)
	}
	own, _ := rig.store.SessionApprovalRules("s1")
	if global := rig.store.ApprovalRules(); len(global) != 0 || len(own) != 1 {
		t.Fatalf("daemon rules = %v, s1 rules = %v, want the allowed rule bound to s1 alone", global, own)
	}
}

func TestAVerbTheOperatorRefusesNeverRuns(t *testing.T) {
	rig := newEscalationRig(t)
	done := rig.dispatchAsync(t, []string{"approve", "rule", "allow Bash(*)"})

	pending := onlyPending(t, rig.approvals)
	if !answerApproval(rig.approvals, pending.ID, approval.Deny) {
		t.Fatal("the question vanished before the operator answered")
	}
	if err := <-done; err == nil {
		t.Fatal("a refused verb must come back as an error")
	}
	if got := rig.store.ApprovalRules(); len(got) != 0 {
		t.Fatalf("rules = %v, want none after a refusal", got)
	}
}

func TestAnsweringAnApprovalIsRefusedWithoutAQuestion(t *testing.T) {
	rig := newEscalationRig(t)
	err := <-rig.dispatchAsync(t, []string{"approve", "allow", "abc"})
	if err == nil {
		t.Fatal("a session must not answer an approval")
	}
	if pending := pendingApprovals(rig.approvals); len(pending) != 0 {
		t.Fatalf("pending = %+v, want no question: a session cannot ask to answer its own", pending)
	}
}

func TestAnAllowedVerbIsAnnouncedToTheSession(t *testing.T) {
	rig := newEscalationRig(t)
	done := rig.dispatchAsync(t, []string{"env", "set", "--key", "HERRSCHER_BACKEND", "--value", "codex"})
	pending := onlyPending(t, rig.approvals)
	if !answerApproval(rig.approvals, pending.ID, approval.Allow) {
		t.Fatal("the question vanished before the operator answered")
	}
	if err := <-done; err != nil {
		t.Fatalf("the allowed verb failed: %v", err)
	}
	notices := rig.events.all()
	if len(notices) != 1 || !strings.HasPrefix(notices[0], "s1: ") || !strings.Contains(notices[0], "env set") || !strings.Contains(notices[0], "allowed by the operator") {
		t.Fatalf("notices = %q, want one telling s1 the operator allowed env set", notices)
	}
	raw, err := os.ReadFile(rig.envFile)
	if err != nil || !strings.Contains(string(raw), "HERRSCHER_BACKEND=codex") || !strings.Contains(string(raw), "# kept") {
		t.Fatalf("env file = %q, %v, want the new value and the comment kept", raw, err)
	}
}

func TestASecretIsRefusedBeforeTheOperatorIsAskedAndHiddenOnRead(t *testing.T) {
	rig := newEscalationRig(t)
	if _, err := rig.dispatch(t, "env", "set", "--key", "GATEWAY_BOT_TOKEN", "--value", "leaked"); err == nil || !strings.Contains(err.Error(), "never writes a secret") {
		t.Fatalf("setting a secret from a session: %v, want the refusal", err)
	}
	if pending := pendingApprovals(rig.approvals); len(pending) != 0 {
		t.Fatalf("pending = %+v: the secret must never reach a question", pending)
	}
	out, err := rig.dispatch(t, "env", "get")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "s3cret") || !strings.Contains(out, "GATEWAY_BOT_TOKEN=(set, hidden)") || !strings.Contains(out, "HERRSCHER_BACKEND=claude") {
		t.Fatalf("env get = %q, want the secret hidden and the rest shown", out)
	}
}

func TestApproveListShowsASessionOnlyItsOwnQuestions(t *testing.T) {
	rig := newEscalationRig(t)
	other := make(chan approval.Decision, 1)
	go func() {
		decision, _ := askApproval(t.Context(), rig.approvals, rig.sessions, "s2", approval.Request{Tool: "Bash", Subject: "git push"},
			approval.Policy{{Decision: approval.Ask, Tool: "Bash", Pattern: "git push*"}}, approval.ModeAsk, time.Minute, 0)
		other <- decision
	}()
	onlyPending(t, rig.approvals)
	out, err := rig.dispatch(t, "approve", "list", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "s2") || strings.Contains(out, "git push") {
		t.Fatalf("approve list from s1 = %q, want nothing of s2's question", out)
	}
	pending := pendingApprovals(rig.approvals)
	if !answerApproval(rig.approvals, pending[0].ID, approval.Deny) {
		t.Fatal("s2's question vanished")
	}
	<-other
}

func TestAClientThatHangsUpWithdrawsItsQuestion(t *testing.T) {
	rig := newEscalationRig(t)
	ctx, hangUp := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, _, err := dispatchLiveCommand(ctx, newHostMessages(), isolatedRuntimeDir(t), rig.socket, []string{"approve", "rule", "allow Read(*)"})
		done <- err
	}()
	onlyPending(t, rig.approvals)
	hangUp()
	if err := <-done; err == nil {
		t.Fatal("a call withdrawn by its client reported success")
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(pendingApprovals(rig.approvals)) != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("pending = %+v: the question outlived the call that asked it", pendingApprovals(rig.approvals))
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := rig.store.ApprovalRules(); len(got) != 0 {
		t.Fatalf("rules = %v, want none: the call was withdrawn", got)
	}
}

func TestASessionAsksTheOperatorForASecretWithoutSendingIt(t *testing.T) {
	rig := newEscalationRig(t)
	out, err := rig.dispatch(t, "env", "request", "--key", "GATEWAY_BOT_TOKEN", "--reason", "the gateway needs it")
	if err != nil {
		t.Fatalf("env request: %v", err)
	}
	notices := rig.events.all()
	if len(notices) != 1 || !strings.Contains(notices[0], "GATEWAY_BOT_TOKEN") || !strings.Contains(notices[0], "herrscher env set") {
		t.Fatalf("notices = %q, out = %q, want one asking the operator to set the key", notices, out)
	}
	if pending := pendingApprovals(rig.approvals); len(pending) != 0 {
		t.Fatalf("pending = %+v, want no question: a request carries no value", pending)
	}
}

func TestADirectWriteIsAnnouncedAndAReadIsNot(t *testing.T) {
	rig := newEscalationRig(t)
	if _, err := rig.dispatch(t, "session", "send", "--name", "s1", "--text", "hello"); err != nil {
		t.Fatalf("session send: %v", err)
	}
	if _, err := rig.dispatch(t, "approve", "list"); err != nil {
		t.Fatalf("approve list: %v", err)
	}
	notices := rig.events.all()
	if len(notices) != 1 || !strings.Contains(notices[0], "session send") {
		t.Fatalf("notices = %q, want the send announced and the read left silent", notices)
	}
}

func TestASessionNeverUnsetsASecret(t *testing.T) {
	rig := newEscalationRig(t)
	if _, err := rig.dispatch(t, "env", "unset", "--key", "GATEWAY_BOT_TOKEN"); err == nil {
		t.Fatal("a session unset a secret")
	}
	if pending := pendingApprovals(rig.approvals); len(pending) != 0 {
		t.Fatalf("pending = %+v, want the refusal before any question", pending)
	}
}
