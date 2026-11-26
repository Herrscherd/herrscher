package host

import (
	"context"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/approval"
	"github.com/Herrscherd/herrscher/core/internal/authz"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/supervisor"
)

func authzHub(t *testing.T, st *state.State, verbs ...[]string) *hub {
	t.Helper()
	reg := cli.NewRegistry(clitext.NewMessages())
	for _, path := range verbs {
		cmd := contracts.New(path...).Help("test verb")
		if path[0] == "session" || path[0] == "schedule" {
			cmd = cmd.ValueParam("name", "session name", false)
		}
		if path[0] == "approve" {
			cmd = cmd.ValueParam("session", "session name", false)
		}
		if err := reg.Add(cmd.Do(func(context.Context, contracts.Input) (contracts.Result, error) {
			return contracts.DataResult("ran"), nil
		})); err != nil {
			t.Fatal(err)
		}
	}
	return testHub(t, &hub{messages: newHostMessages(), cliMessages: clitext.NewMessages(), warnings: io.Discard, st: st, reg: reg, live: make(map[string]liveSession)})
}

func authzState(t *testing.T, sessions []state.Session, roles map[string]string) *state.State {
	t.Helper()
	st, err := state.LoadState(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatal(err)
	}
	for idx := range sessions {
		s := sessions[idx]
		if err := st.AddSession(s); err != nil {
			t.Fatal(err)
		}
	}
	for principal, role := range roles {
		if _, err := st.SetRole(principal, role, ""); err != nil {
			t.Fatal(err)
		}
	}
	return st
}

func TestASessionMayNotRewriteThePolicyItIsBoundBy(t *testing.T) {
	st := authzState(t, []state.Session{{Name: "revue"}}, nil)
	h := authzHub(t, st, []string{"approve", "rule"})
	_, err := h.authorize(withSessionPrincipal(context.Background(), "revue"), []string{"approve", "rule", "allow Bash(*)"})
	if err == nil {
		t.Fatal("a session must not widen the rules that decide its own tool calls")
	}
	if !strings.Contains(err.Error(), "approve rule") {
		t.Fatalf("refusal %q must name the verb", err)
	}
}

func TestTheOperatorSocketMayRunAnything(t *testing.T) {
	st := authzState(t, nil, nil)
	h := authzHub(t, st, []string{"approve", "rule"}, []string{"role", "grant"})
	for _, argv := range [][]string{{"approve", "rule"}, {"role", "grant"}} {
		if _, err := h.authorize(withLocalPrincipal(context.Background()), argv); err != nil {
			t.Fatalf("the operator socket was refused %v: %v", argv, err)
		}
	}
}

func TestASessionMayNotSpeakForAnother(t *testing.T) {
	st := authzState(t, []state.Session{{Name: "revue"}, {Name: "release"}}, nil)
	h := authzHub(t, st, []string{"approve", "ask"})
	ctx := withSessionPrincipal(context.Background(), "revue")
	if _, err := h.authorize(ctx, []string{"approve", "ask", "--session", "revue"}); err != nil {
		t.Fatalf("a session must ask for itself: %v", err)
	}
	_, err := h.authorize(ctx, []string{"approve", "ask", "--session", "release"})
	if err == nil {
		t.Fatal("a session must not open an approval in another session's name")
	}
	if !strings.Contains(err.Error(), "release") || !strings.Contains(err.Error(), "revue") {
		t.Fatalf("refusal %q must name both sessions", err)
	}
}

func TestASessionReachesTheSessionsItDelegated(t *testing.T) {
	st := authzState(t, []state.Session{
		{Name: "lead"},
		{Name: "worker", Parent: "lead"},
		{Name: "grandchild", Parent: "worker"},
		{Name: "stranger"},
	}, nil)
	h := authzHub(t, st, []string{"session", "send"})
	ctx := withSessionPrincipal(context.Background(), "lead")
	for _, target := range []string{"lead", "worker", "grandchild"} {
		if _, err := h.authorize(ctx, []string{"session", "send", "--name", target}); err != nil {
			t.Fatalf("lead was refused its own subtree at %q: %v", target, err)
		}
	}
	if _, err := h.authorize(ctx, []string{"session", "send", "--name", "stranger"}); err == nil {
		t.Fatal("lead must not drive a session it did not delegate")
	}
}

func TestASubtreeWalkTerminatesOnACycle(t *testing.T) {
	st := authzState(t, []state.Session{
		{Name: "a", Parent: "b"},
		{Name: "b", Parent: "a"},
	}, nil)
	h := authzHub(t, st, []string{"session", "send"})
	ctx := withSessionPrincipal(context.Background(), "outsider")
	if _, err := h.authorize(ctx, []string{"session", "send", "--name", "a"}); err == nil {
		t.Fatal("a cycle that reaches nobody must refuse")
	}
}

func TestAnEmptyRoleTableLeavesGatewaysExactlyAsTheyWere(t *testing.T) {
	st := authzState(t, nil, nil)
	h := authzHub(t, st, []string{"host", "rm"})
	ctx := contracts.WithPrincipal(context.Background(), "chat:1234")
	if _, err := h.authorize(ctx, []string{"host", "rm"}); err != nil {
		t.Fatalf("an install that configured nothing must not change: %v", err)
	}
}

func TestTheFirstGrantMakesEveryOtherAccountAnObserver(t *testing.T) {
	st := authzState(t, nil, map[string]string{"chat:1": "owner"})
	h := authzHub(t, st, []string{"host", "rm"})
	if _, err := h.authorize(contracts.WithPrincipal(context.Background(), "chat:1"), []string{"host", "rm"}); err != nil {
		t.Fatalf("the owner was refused: %v", err)
	}
	_, err := h.authorize(contracts.WithPrincipal(context.Background(), "chat:2"), []string{"host", "rm"})
	if err == nil {
		t.Fatal("an unnamed account must be an observer once the table holds anything")
	}
}

func TestAGatewayCannotClaimToBeTheOperatorSocketOrASession(t *testing.T) {
	st := authzState(t, []state.Session{{Name: "revue"}}, map[string]string{"chat:1": "owner"})
	h := authzHub(t, st, []string{"role", "grant"})
	for _, claim := range []string{authz.LocalPrincipal, authz.SessionPrefix + "revue"} {
		ctx := contracts.WithPrincipal(context.Background(), claim)
		if _, err := h.authorize(ctx, []string{"role", "grant"}); err == nil {
			t.Fatalf("a gateway claiming %q was believed", claim)
		}
	}
}

func TestDispatchRefusesBeforeReachingTheRegistry(t *testing.T) {
	st := authzState(t, []state.Session{{Name: "revue"}}, nil)
	ran := false
	reg := cli.NewRegistry(clitext.NewMessages())
	if err := reg.Add(contracts.New("approve", "rule").
		Help("test verb").
		Do(func(context.Context, contracts.Input) (contracts.Result, error) {
			ran = true
			return contracts.DataResult("ran"), nil
		})); err != nil {
		t.Fatal(err)
	}
	h := testHub(t, &hub{messages: newHostMessages(), cliMessages: clitext.NewMessages(), warnings: io.Discard, st: st, reg: reg, live: make(map[string]liveSession)})
	if _, err := h.Dispatch(withSessionPrincipal(context.Background(), "revue"), []string{"approve", "rule", "allow Bash(*)"}); err == nil {
		t.Fatal("Dispatch must refuse")
	}
	if ran {
		t.Fatal("the handler ran, so the refusal decided nothing")
	}
	if _, err := h.Dispatch(withLocalPrincipal(context.Background()), []string{"approve", "rule", "allow Bash(*)"}); err != nil {
		t.Fatalf("the operator socket was refused: %v", err)
	}
	if !ran {
		t.Fatal("the operator's call never reached the handler")
	}
}

func TestAnUnknownVerbIsDispatchsErrorAndNotARefusal(t *testing.T) {
	st := authzState(t, []state.Session{{Name: "revue"}}, nil)
	h := authzHub(t, st, []string{"session", "send"})
	ctx := withSessionPrincipal(context.Background(), "revue")
	if _, err := h.authorize(ctx, []string{"quantum", "entangle"}); err != nil {
		t.Fatalf("an unregistered verb must fall through to Dispatch, which names it: %v", err)
	}
}

func operatorRegistryForTest(t *testing.T) *cli.Registry {
	t.Helper()
	approvalTab := newApprovalTable(io.Discard)
	sessionTab := newSessionTable(newHostMessages(), clitext.NewMessages())
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	dir := t.TempDir()
	st := state.NewState(dir + "/state.json")
	sup := supervisor.NewSupervisor(ctx, "/nonexistent/herrscher", testRuntime(t))
	reg, _, err := buildRegistry(ctx, Deps{}, Options{Runtime: testRuntime(t), StatePath: dir + "/state.json", DefaultCmd: "claude"}, st, sup, "", nil, approvalTab, sessionTab)
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func TestEverySessionNamingVerbIsClassified(t *testing.T) {
	reg := operatorRegistryForTest(t)
	specs, specsErr := reg.Specs(i18n.English)
	if specsErr != nil {
		t.Fatal(specsErr)
	}
	for _, spec := range specs {
		verb := strings.Join(spec.Path, " ")
		for _, p := range spec.Params {
			if p.Name != "name" && p.Name != "session" {
				continue
			}
			if sessionArgs()[verb] == p.Name {
				continue
			}
			if notASessionArgs()[verb+" "+p.Name] {
				continue
			}
			t.Errorf("`%s --%s` names something. Add it to sessionArg if that is a session "+
				"the caller acts on, or to notASession with the reason if it is not.", verb, p.Name)
		}
	}
}

func TestASessionReachesOnlyItsOwnPullRequests(t *testing.T) {
	st := authzState(t, []state.Session{{Name: "revue"}, {Name: "release"}}, nil)
	verbs := [][]string{
		{"session", "pr", "open"},
		{"session", "pr", "status"},
		{"session", "pr", "checks"},
		{"session", "pr", "comments"},
	}
	h := authzHub(t, st, verbs...)
	ctx := withSessionPrincipal(context.Background(), "revue")
	for _, verb := range verbs {
		own := append(slices.Clone(verb), "--name", "revue")
		if _, err := h.authorize(ctx, own); err != nil {
			t.Fatalf("a session was refused its own %v: %v", verb, err)
		}
		other := append(slices.Clone(verb), "--name", "release")
		if _, err := h.authorize(ctx, other); err == nil {
			t.Fatalf("a session may not run %v on another session", verb)
		}
		if _, err := h.authorize(withLocalPrincipal(context.Background()), other); err != nil {
			t.Fatalf("the operator socket was refused %v: %v", verb, err)
		}
	}
}

func TestAGatewayCannotClaimTheIdentityOfAPairedDevice(t *testing.T) {
	st := authzState(t, nil, map[string]string{"device:0123": "owner", "chat:1": "owner"})
	h := authzHub(t, st, []string{"role", "grant"})
	ctx := contracts.WithPrincipal(context.Background(), "device:0123")
	if err := h.authorize(ctx, []string{"role", "grant"}); err == nil {
		t.Fatal("a gateway claiming the identity of an owner device was believed")
	}
}

func TestDeviceVerbsNeedARoleTableForGatewaysButNotTheLocalOperator(t *testing.T) {
	st := authzState(t, nil, nil)
	h := authzHub(t, st, []string{"device", "list"}, []string{"session", "list"})
	gateway := contracts.WithPrincipal(context.Background(), "chat:1")
	if err := h.authorize(gateway, []string{"device", "list"}); err == nil {
		t.Fatal("with no role table a gateway account must not run a device verb")
	}
	if err := h.authorize(gateway, []string{"session", "list"}); err != nil {
		t.Fatalf("with no role table a gateway keeps the old behaviour on other verbs: %v", err)
	}
	if err := h.authorize(withLocalPrincipal(context.Background()), []string{"device", "list"}); err != nil {
		t.Fatalf("the local operator must run a device verb with no role table: %v", err)
	}
}

func answerHub(t *testing.T, roles map[string]string, pending map[string]approval.Request) *hub {
	t.Helper()
	st := authzState(t, nil, roles)
	h := authzHub(t, st, []string{"approve", "allow"}, []string{"approve", "deny"})
	h.approvals = newApprovalTable(io.Discard)
	for id, request := range pending {
		h.approvals.m[id] = &pendingApproval{id: id, req: request, answer: make(chan approvalVerdict, 1), deadline: time.Now().Add(time.Hour)}
	}
	return h
}

func TestOnlyOwnerAndOperatorLeaveAStandingAnswer(t *testing.T) {
	h := answerHub(t, map[string]string{"chat:1": "owner", "chat:2": "operator", "chat:3": "approver"}, nil)
	args := []string{"approve", "allow", "abc", "--session", "revue"}
	if err := h.authorize(contracts.WithPrincipal(context.Background(), "chat:3"), args); err == nil {
		t.Fatal("an approver must not leave a standing answer with --session")
	}
	if err := h.authorize(contracts.WithPrincipal(context.Background(), "chat:2"), args); err != nil {
		t.Fatalf("an operator may leave a standing answer: %v", err)
	}
	if err := h.authorize(contracts.WithPrincipal(context.Background(), "chat:1"), args); err != nil {
		t.Fatalf("an owner may leave a standing answer: %v", err)
	}
	if err := h.authorize(withLocalPrincipal(context.Background()), args); err != nil {
		t.Fatalf("the local operator may leave a standing answer: %v", err)
	}
}

func TestAnApproverAnswersOnceButNotAStandingAnswerWithAlways(t *testing.T) {
	h := answerHub(t, map[string]string{"chat:3": "approver"}, nil)
	if err := h.authorize(contracts.WithPrincipal(context.Background(), "chat:3"), []string{"approve", "allow", "abc"}); err != nil {
		t.Fatalf("an approver answers a request once: %v", err)
	}
	if err := h.authorize(contracts.WithPrincipal(context.Background(), "chat:3"), []string{"approve", "allow", "abc", "--session", "revue"}); err == nil {
		t.Fatal("an approver must not turn an answer into a standing rule")
	}
}

func TestOnlyTheOwnerAnswersARequestAboutDevicesOrRoles(t *testing.T) {
	pending := map[string]approval.Request{"abc": {Tool: "Bash", Subject: "herrscher role grant --principal chat:9 --role owner"}}
	h := answerHub(t, map[string]string{"chat:1": "owner", "chat:2": "operator", "chat:3": "approver"}, pending)
	for _, claim := range []string{"chat:2", "chat:3"} {
		ctx := contracts.WithPrincipal(context.Background(), claim)
		if err := h.authorize(ctx, []string{"approve", "deny", "abc"}); err == nil {
			t.Fatalf("%s must not decide a request about herrscher's roles", claim)
		}
		if err := h.authorize(ctx, []string{"approve", "allow", "abc"}); err == nil {
			t.Fatalf("%s must not allow a request about herrscher's roles", claim)
		}
	}
	if err := h.authorize(contracts.WithPrincipal(context.Background(), "chat:1"), []string{"approve", "allow", "abc"}); err != nil {
		t.Fatalf("the owner decides a request about herrscher's roles: %v", err)
	}
}

func TestAGatewayWithoutARoleTableCannotAnswerOnBehalfOfThePerson(t *testing.T) {
	h := answerHub(t, nil, nil)
	if err := h.authorize(contracts.WithPrincipal(context.Background(), "chat:1"), []string{"approve", "allow", "abc", "--session", "revue"}); err == nil {
		t.Fatal("with no role table a gateway must not leave a standing answer")
	}
}

func TestTheRoleVerbsNeedTheLocalOperatorWhileNoRoleTableExists(t *testing.T) {
	st := authzState(t, nil, nil)
	h := authzHub(t, st, []string{"role", "grant"}, []string{"role", "list"})
	gateway := contracts.WithPrincipal(context.Background(), "chat:1")
	if err := h.authorize(gateway, []string{"role", "grant"}); err == nil {
		t.Fatal("with no role table a gateway must not grant a role")
	}
	if err := h.authorize(gateway, []string{"role", "list"}); err == nil {
		t.Fatal("with no role table a gateway must not list roles either")
	}
	if err := h.authorize(withLocalPrincipal(context.Background()), []string{"role", "grant"}); err != nil {
		t.Fatalf("the local operator grants the first role: %v", err)
	}
}
