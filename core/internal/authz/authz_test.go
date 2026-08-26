package authz

import (
	"strings"
	"testing"
)

func req(principal Principal, path ...string) Request {
	return Request{Principal: principal, Path: path}
}

func TestOwnerRunsEverythingIncludingRole(t *testing.T) {
	for _, path := range [][]string{{"role", "grant"}, {"approve", "rule"}, {"host", "rm"}, {"session", "close"}} {
		if ok, why := Decide(req(Principal{ID: LocalPrincipal}, path...), RoleOwner); !ok {
			t.Fatalf("owner refused %v: %s", path, why)
		}
	}
}

func TestOperatorRunsEverythingButRole(t *testing.T) {
	p := Principal{ID: "chat:1"}
	if ok, _ := Decide(req(p, "approve", "rule"), RoleOperator); !ok {
		t.Fatal("operator must run approve rule")
	}
	ok, why := Decide(req(p, "role", "grant"), RoleOperator)
	if ok {
		t.Fatal("operator must not run role grant: it could elevate itself")
	}
	if why == "" {
		t.Fatal("a refusal must say something")
	}
}

func TestObserverReadsAndNothingElse(t *testing.T) {
	p := Principal{ID: "chat:1"}
	if ok, why := Decide(req(p, "session", "list"), RoleObserver); !ok {
		t.Fatalf("observer must run session list: %s", why)
	}
	if ok, _ := Decide(req(p, "session", "send"), RoleObserver); ok {
		t.Fatal("observer must not run session send")
	}
	if ok, _ := Decide(req(p, "approve", "allow"), RoleObserver); ok {
		t.Fatal("observer must not answer an approval")
	}
}

// The allow list is a list: a verb nobody named is refused, so a verb a later
// version adds does not arrive already permitted.
func TestUnknownVerbIsRefusedForAllowListRoles(t *testing.T) {
	p := Principal{ID: "chat:1"}
	for _, role := range []Role{RoleObserver, RoleAgent} {
		if ok, _ := Decide(req(p, "quantum", "entangle"), role); ok {
			t.Fatalf("%s must refuse a verb no list names", role)
		}
	}
	if ok, _ := Decide(req(p, "quantum", "entangle"), RoleOwner); !ok {
		t.Fatal("owner must run a verb no list names")
	}
}

func TestAgentMayWorkAndMayNotDisarmItself(t *testing.T) {
	p := SessionPrincipal("revue")
	allowed := [][]string{
		{"session", "send"}, {"session", "seed"},
		{"memory", "search"}, {"memory", "record"}, {"approve", "ask"},
		{"models", "list"}, {"commands"}, {"whoami"},
	}
	for _, path := range allowed {
		if ok, why := Decide(req(p, path...), RoleAgent); !ok {
			t.Fatalf("agent refused %v: %s", path, why)
		}
	}
	refused := [][]string{
		{"approve", "rule"}, {"approve", "mode"}, {"approve", "allow"}, {"approve", "list"},
		{"host", "rm"}, {"role", "grant"}, {"schedule", "add"}, {"service", "restart"},
		{"set", "home"}, {"skill", "approve"}, {"memory", "forget"},
		{"session", "create"},
		{"session", "close"}, {"session", "archive"}, {"session", "switch"},
	}
	for _, path := range refused {
		if ok, _ := Decide(req(p, path...), RoleAgent); ok {
			t.Fatalf("agent must not run %v", path)
		}
	}
}

// Denying `approve mode` is decorative if `session create` is open beside it: a
// session opened from argv with no --agent materializes no hook, and one opened
// with `--approvals bypass` says so outright. Either is a second session where
// the caller's own policy does not apply, reachable in one command. So the verb
// is refused, and the refusal has to point at delegation, which produces a
// worker that runs a registered agent and answers to a policy like its lead.
func TestAnAgentCannotOpenASessionThatEscapesItsOwnPolicy(t *testing.T) {
	ok, why := Decide(req(SessionPrincipal("revue"), "session", "create"), RoleAgent)
	if ok {
		t.Fatal("a session that can create a session can create one with no policy")
	}
	if !strings.Contains(why, "delegate") {
		t.Fatalf("refusal must name the way that stays open: %s", why)
	}
}

// The approve family is a deny list of exact verbs and not "approve *", since
// deny wins and the family would take `approve ask` with it. What keeps a later
// approve verb bounded is the allow list, which refuses what it does not name.
func TestAnApproveVerbNobodyNamedIsStillRefusedToAnAgent(t *testing.T) {
	p := SessionPrincipal("revue")
	if ok, why := Decide(req(p, "approve", "ask"), RoleAgent); !ok {
		t.Fatalf("agent must reach approve ask, which is how its own hook works: %s", why)
	}
	if ok, _ := Decide(req(p, "approve", "escalate"), RoleAgent); ok {
		t.Fatal("an approve verb no list names must be refused")
	}
}

// An agent reads its refusal as a tool result, so it is written for a model:
// it must not read as a crash, and it must say who to ask.
func TestAgentRefusalTellsTheModelWhatToDo(t *testing.T) {
	_, why := Decide(req(SessionPrincipal("revue"), "approve", "rule"), RoleAgent)
	if why == "" {
		t.Fatal("a refusal must say something")
	}
	for _, want := range []string{"approve rule", "operator"} {
		if !strings.Contains(why, want) {
			t.Fatalf("refusal %q must mention %q", why, want)
		}
	}
}

// A human's refusal names the role that would run it, so the operator knows
// what to grant rather than only what was refused.
func TestHumanRefusalNamesTheRoleThatWouldRunIt(t *testing.T) {
	_, why := Decide(req(Principal{ID: "chat:5678"}, "approve", "rule"), RoleObserver)
	for _, want := range []string{"approve rule", "operator", "observer", "chat:5678"} {
		if !strings.Contains(why, want) {
			t.Fatalf("refusal %q must mention %q", why, want)
		}
	}
}

func TestPluginVerbsAreAllowedToAnAgentAndNotToAnObserver(t *testing.T) {
	r := Request{Principal: SessionPrincipal("revue"), Path: []string{"chat", "channel", "post"}, Contributed: true}
	if ok, why := Decide(r, RoleAgent); !ok {
		t.Fatalf("agent must reach the verbs its capabilities block promises: %s", why)
	}
	r.Principal = Principal{ID: "chat:1"}
	if ok, _ := Decide(r, RoleObserver); ok {
		t.Fatal("observer must not run a contributed verb")
	}
}

func TestScopeBindsSessionsToTheirOwnSubtree(t *testing.T) {
	p := SessionPrincipal("revue")
	own := Request{Principal: p, Path: []string{"session", "send"}, Subject: Subject{Session: "revue", InSubtree: true}}
	if ok, why := Decide(own, RoleAgent); !ok {
		t.Fatalf("a session must reach itself: %s", why)
	}
	child := Request{Principal: p, Path: []string{"session", "send"}, Subject: Subject{Session: "revue-worker", InSubtree: true}}
	if ok, why := Decide(child, RoleAgent); !ok {
		t.Fatalf("a session must reach a session it delegated: %s", why)
	}
	cousin := Request{Principal: p, Path: []string{"session", "send"}, Subject: Subject{Session: "release", InSubtree: false}}
	ok, why := Decide(cousin, RoleAgent)
	if ok {
		t.Fatal("a session must not drive a session outside its subtree")
	}
	for _, want := range []string{"release", "revue"} {
		if !strings.Contains(why, want) {
			t.Fatalf("refusal %q must name both sessions, missing %q", why, want)
		}
	}
}

// Scope is a session's confinement. A human operating from a gateway is not
// confined to a subtree, so a subject outside one changes nothing for them.
func TestScopeDoesNotBindHumans(t *testing.T) {
	r := Request{
		Principal: Principal{ID: "chat:1"},
		Path:      []string{"session", "send"},
		Subject:   Subject{Session: "release", InSubtree: false},
	}
	if ok, why := Decide(r, RoleOperator); !ok {
		t.Fatalf("an operator must reach any session: %s", why)
	}
}

func TestRoleNoneDecidesNothing(t *testing.T) {
	if ok, _ := Decide(req(Principal{ID: "chat:1"}, "host", "rm"), RoleNone); !ok {
		t.Fatal("no role means the daemon does not decide, which is not the same as refusing")
	}
}

func TestRoleOfLocalIsAlwaysOwner(t *testing.T) {
	assignments := map[string]string{LocalPrincipal: "observer"}
	if got := RoleOf(Principal{ID: LocalPrincipal}, assignments); got != RoleOwner {
		t.Fatalf("RoleOf(local) = %q, want owner: opening that socket already needs the daemon's uid", got)
	}
}

func TestRoleOfASessionIsAlwaysAgent(t *testing.T) {
	assignments := map[string]string{"session:revue": "owner"}
	if got := RoleOf(SessionPrincipal("revue"), assignments); got != RoleAgent {
		t.Fatalf("RoleOf(session) = %q, want agent: the role is structural and not grantable", got)
	}
}

func TestAnEmptyTableEnforcesNothingOnHumans(t *testing.T) {
	if got := RoleOf(Principal{ID: "chat:1"}, nil); got != RoleNone {
		t.Fatalf("RoleOf = %q, want none: an install that configured nothing must not change", got)
	}
}

func TestTheFirstAssignmentMakesEveryoneElseAnObserver(t *testing.T) {
	assignments := map[string]string{"chat:1": "owner"}
	if got := RoleOf(Principal{ID: "chat:1"}, assignments); got != RoleOwner {
		t.Fatalf("RoleOf(named) = %q, want owner", got)
	}
	if got := RoleOf(Principal{ID: "chat:2"}, assignments); got != RoleObserver {
		t.Fatalf("RoleOf(unnamed) = %q, want observer", got)
	}
}

func TestAnUnknownRoleNameFallsBackToObserver(t *testing.T) {
	assignments := map[string]string{"chat:1": "archduke"}
	if got := RoleOf(Principal{ID: "chat:1"}, assignments); got != RoleObserver {
		t.Fatalf("RoleOf = %q, want observer: a hand-edited typo must not widen anything", got)
	}
}

// agent is structural. Granting it by hand in state.json would say a human is
// a session, which is a claim the entry point already answers.
func TestAgentIsNotGrantable(t *testing.T) {
	if Grantable("agent") {
		t.Fatal("agent must not be grantable")
	}
	for _, name := range []string{"owner", "operator", "observer"} {
		if !Grantable(name) {
			t.Fatalf("%s must be grantable", name)
		}
	}
	if got := RoleOf(Principal{ID: "chat:1"}, map[string]string{"chat:1": "agent"}); got != RoleObserver {
		t.Fatalf("RoleOf = %q, want observer: agent hand-written into the table is not a grant", got)
	}
}
