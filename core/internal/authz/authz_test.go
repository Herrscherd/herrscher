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
		{"session", "send"},
		{"session", "seed"},
		{"memory", "search"},
		{"memory", "record"},
		{"approve", "ask"},
		{"models", "list"},
		{"commands"},
		{"whoami"},
	}
	for _, path := range allowed {
		if ok, why := Decide(req(p, path...), RoleAgent); !ok {
			t.Fatalf("agent refused %v: %s", path, why)
		}
	}
	refused := [][]string{
		{"approve", "rule"},
		{"approve", "mode"},
		{"approve", "allow"},
		{"approve", "list"},
		{"host", "rm"},
		{"role", "grant"},
		{"schedule", "add"},
		{"service", "restart"},
		{"set", "home"},
		{"skill", "approve"},
		{"memory", "forget"},
		{"session", "create"},
		{"session", "close"},
		{"session", "archive"},
		{"session", "switch"},
		{"session", "set-compact"},
	}
	for _, path := range refused {
		if ok, _ := Decide(req(p, path...), RoleAgent); ok {
			t.Fatalf("agent must not run %v", path)
		}
	}
}

func TestAnAgentCannotOpenASessionThatEscapesItsOwnPolicy(t *testing.T) {
	ok, why := Decide(req(SessionPrincipal("revue"), "session", "create"), RoleAgent)
	if ok {
		t.Fatal("a session that can create a session can create one with no policy")
	}
	if !strings.Contains(why, "delegate") {
		t.Fatalf("refusal must name the way that stays open: %s", why)
	}
}

func TestAnApproveVerbNobodyNamedIsStillRefusedToAnAgent(t *testing.T) {
	p := SessionPrincipal("revue")
	if ok, why := Decide(req(p, "approve", "ask"), RoleAgent); !ok {
		t.Fatalf("agent must reach approve ask, which is how its own hook works: %s", why)
	}
	if ok, _ := Decide(req(p, "approve", "escalate"), RoleAgent); ok {
		t.Fatal("an approve verb no list names must be refused")
	}
}

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
