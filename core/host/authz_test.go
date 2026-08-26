package host

import (
	"context"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/authz"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/supervisor"
)

// authzHub builds the smallest hub that can authorize: a registry with the
// verbs a test names, and a state holding sessions and roles.
func authzHub(t *testing.T, st *state.State, verbs ...[]string) *hub {
	t.Helper()
	reg := &cli.Registry{}
	for _, path := range verbs {
		cmd := contracts.New(path...).Help("test verb")
		if path[0] == "session" || path[0] == "schedule" {
			cmd = cmd.ValueParam("name", "session name", false)
		}
		if path[0] == "approve" {
			cmd = cmd.ValueParam("session", "session name", false)
		}
		if err := reg.Add(cmd.Do(func(context.Context, contracts.Input) (string, error) {
			return "ran", nil
		})); err != nil {
			t.Fatal(err)
		}
	}
	return &hub{st: st, reg: reg, live: map[string]liveSession{}}
}

func authzState(t *testing.T, sessions []state.Session, roles map[string]string) *state.State {
	t.Helper()
	st, err := state.LoadState(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sessions {
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
	err := h.authorize(withSessionPrincipal(context.Background(), "revue"), []string{"approve", "rule", "allow Bash(*)"})
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
		if err := h.authorize(withLocalPrincipal(context.Background()), argv); err != nil {
			t.Fatalf("the operator socket was refused %v: %v", argv, err)
		}
	}
}

// The finding D1 left open, closed: the flag no longer decides who you are.
func TestASessionMayNotSpeakForAnother(t *testing.T) {
	st := authzState(t, []state.Session{{Name: "revue"}, {Name: "release"}}, nil)
	h := authzHub(t, st, []string{"approve", "ask"})
	ctx := withSessionPrincipal(context.Background(), "revue")
	if err := h.authorize(ctx, []string{"approve", "ask", "--session", "revue"}); err != nil {
		t.Fatalf("a session must ask for itself: %v", err)
	}
	err := h.authorize(ctx, []string{"approve", "ask", "--session", "release"})
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
		if err := h.authorize(ctx, []string{"session", "send", "--name", target}); err != nil {
			t.Fatalf("lead was refused its own subtree at %q: %v", target, err)
		}
	}
	if err := h.authorize(ctx, []string{"session", "send", "--name", "stranger"}); err == nil {
		t.Fatal("lead must not drive a session it did not delegate")
	}
}

// A Parent chain hand-written into state.json can be a cycle. The walk must
// answer, not spin.
func TestASubtreeWalkTerminatesOnACycle(t *testing.T) {
	st := authzState(t, []state.Session{
		{Name: "a", Parent: "b"},
		{Name: "b", Parent: "a"},
	}, nil)
	h := authzHub(t, st, []string{"session", "send"})
	ctx := withSessionPrincipal(context.Background(), "outsider")
	if err := h.authorize(ctx, []string{"session", "send", "--name", "a"}); err == nil {
		t.Fatal("a cycle that reaches nobody must refuse")
	}
}

func TestAnEmptyRoleTableLeavesGatewaysExactlyAsTheyWere(t *testing.T) {
	st := authzState(t, nil, nil)
	h := authzHub(t, st, []string{"host", "rm"})
	ctx := contracts.WithPrincipal(context.Background(), "chat:1234")
	if err := h.authorize(ctx, []string{"host", "rm"}); err != nil {
		t.Fatalf("an install that configured nothing must not change: %v", err)
	}
}

func TestTheFirstGrantMakesEveryOtherAccountAnObserver(t *testing.T) {
	st := authzState(t, nil, map[string]string{"chat:1": "owner"})
	h := authzHub(t, st, []string{"host", "rm"})
	if err := h.authorize(contracts.WithPrincipal(context.Background(), "chat:1"), []string{"host", "rm"}); err != nil {
		t.Fatalf("the owner was refused: %v", err)
	}
	err := h.authorize(contracts.WithPrincipal(context.Background(), "chat:2"), []string{"host", "rm"})
	if err == nil {
		t.Fatal("an unnamed account must be an observer once the table holds anything")
	}
}

// A gateway sets the principal from text its platform gave it. The two names
// the core mints for itself must not be claimable that way.
func TestAGatewayCannotClaimToBeTheOperatorSocketOrASession(t *testing.T) {
	st := authzState(t, []state.Session{{Name: "revue"}}, map[string]string{"chat:1": "owner"})
	h := authzHub(t, st, []string{"role", "grant"})
	for _, claim := range []string{authz.LocalPrincipal, authz.SessionPrefix + "revue"} {
		ctx := contracts.WithPrincipal(context.Background(), claim)
		if err := h.authorize(ctx, []string{"role", "grant"}); err == nil {
			t.Fatalf("a gateway claiming %q was believed", claim)
		}
	}
}

// authorize is where the decision is made, but Dispatch is where it has to
// bite. A refusal that still ran the handler would be a log line, not a
// guardrail.
func TestDispatchRefusesBeforeReachingTheRegistry(t *testing.T) {
	st := authzState(t, []state.Session{{Name: "revue"}}, nil)
	ran := false
	reg := &cli.Registry{}
	if err := reg.Add(contracts.New("approve", "rule").
		Help("test verb").
		Do(func(context.Context, contracts.Input) (string, error) {
			ran = true
			return "ran", nil
		})); err != nil {
		t.Fatal(err)
	}
	h := &hub{st: st, reg: reg, live: map[string]liveSession{}}
	if _, err := h.Dispatch(withSessionPrincipal(context.Background(), "revue"), []string{"approve", "rule"}); err == nil {
		t.Fatal("Dispatch must refuse")
	}
	if ran {
		t.Fatal("the handler ran, so the refusal decided nothing")
	}
	if _, err := h.Dispatch(withLocalPrincipal(context.Background()), []string{"approve", "rule"}); err != nil {
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
	if err := h.authorize(ctx, []string{"quantum", "entangle"}); err != nil {
		t.Fatalf("an unregistered verb must fall through to Dispatch, which names it: %v", err)
	}
}

// operatorRegistryForTest builds the registry the daemon serves, so the
// coverage test below walks the verbs that actually exist rather than a list
// re-typed beside them. A hand-kept list is a list that falls behind, which is
// the failure that test exists to prevent.
func operatorRegistryForTest(t *testing.T) *cli.Registry {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	dir := t.TempDir()
	st := state.NewState(dir + "/state.json")
	sup := supervisor.NewSupervisor(ctx, "/nonexistent/herrscher")
	reg, _, err := buildRegistry(ctx, Deps{}, Options{StatePath: dir + "/state.json", DefaultCmd: "claude"}, st, sup, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

// The scoping table is the thing that rots. A verb added later that names a
// session, and that nobody classified, must fail here rather than silently
// escape the scope check.
func TestEverySessionNamingVerbIsClassified(t *testing.T) {
	reg := operatorRegistryForTest(t)
	for _, spec := range reg.Specs() {
		verb := strings.Join(spec.Path, " ")
		for _, p := range spec.Params {
			if p.Name != "name" && p.Name != "session" {
				continue
			}
			if sessionArg[verb] == p.Name {
				continue
			}
			if notASession[verb+" "+p.Name] {
				continue
			}
			t.Errorf("`%s --%s` names something. Add it to sessionArg if that is a session "+
				"the caller acts on, or to notASession with the reason if it is not.", verb, p.Name)
		}
	}
}
