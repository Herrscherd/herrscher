package authz

import (
	"slices"
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
		{"session", "pr", "open"},
		{"session", "pr", "status"},
		{"session", "pr", "checks"},
		{"session", "pr", "comments"},
		{"approve", "list"},
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

func TestASessionIsAgentUnlessGrantedSteward(t *testing.T) {
	cases := map[string]Role{"owner": RoleAgent, "operator": RoleAgent, "steward": RoleSteward, "": RoleAgent}
	for granted, want := range cases {
		assignments := map[string]string{"session:revue": granted}
		if got := RoleOf(SessionPrincipal("revue"), assignments); got != want {
			t.Errorf("RoleOf(session granted %q) = %q, want %q", granted, got, want)
		}
	}
}

func TestAStewardSessionLeavesHumansWithoutARole(t *testing.T) {
	assignments := map[string]string{"session:revue": "steward"}
	if got := RoleOf(Principal{ID: "chat:1"}, assignments); got != RoleNone {
		t.Fatalf("RoleOf(human) = %q, want none: trusting a session must not demote every human to observer", got)
	}
}

func TestEachFamilyUnderEachRoleASessionCanHold(t *testing.T) {
	session := SessionPrincipal("revue")
	const (
		runs = iota
		asks
		never
	)
	cases := []struct {
		path    []string
		agent   int
		steward int
	}{
		{[]string{"session", "list"}, runs, runs},
		{[]string{"session", "create"}, asks, asks},
		{[]string{"session", "set-budget"}, asks, runs},
		{[]string{"schedule", "add"}, asks, runs},
		{[]string{"schedule", "list"}, runs, runs},
		{[]string{"config", "get"}, runs, runs},
		{[]string{"config", "set"}, asks, asks},
		{[]string{"config", "unset"}, asks, asks},
		{[]string{"env", "get"}, runs, runs},
		{[]string{"env", "set"}, asks, asks},
		{[]string{"env", "unset"}, asks, asks},
		{[]string{"plugin", "list"}, runs, runs},
		{[]string{"plugin", "add"}, asks, runs},
		{[]string{"setup", "check"}, runs, runs},
		{[]string{"connect", "status"}, runs, runs},
		{[]string{"service", "restart"}, asks, runs},
		{[]string{"set", "lang"}, asks, runs},
		{[]string{"host", "list"}, runs, runs},
		{[]string{"host", "add"}, asks, runs},
		{[]string{"agent", "create"}, asks, runs},
		{[]string{"approve", "list"}, runs, runs},
		{[]string{"approve", "rule"}, asks, asks},
		{[]string{"approve", "mode"}, asks, asks},
		{[]string{"approve", "allow"}, never, never},
		{[]string{"approve", "deny"}, never, never},
		{[]string{"role", "grant"}, never, never},
		{[]string{"session", "issue", "open"}, runs, runs},
	}
	verdict := func(role Role, path []string) int {
		request := req(session, path...)
		if ok, _ := Decide(request, role); ok {
			return runs
		}
		if Escalable(request, role) {
			return asks
		}
		return never
	}
	for _, tc := range cases {
		if got := verdict(RoleAgent, tc.path); got != tc.agent {
			t.Errorf("agent %v = %d, want %d (0 runs, 1 asks, 2 never)", tc.path, got, tc.agent)
		}
		if got := verdict(RoleSteward, tc.path); got != tc.steward {
			t.Errorf("steward %v = %d, want %d (0 runs, 1 asks, 2 never)", tc.path, got, tc.steward)
		}
	}
}

func TestAStewardStaysInsideItsSubtree(t *testing.T) {
	request := Request{Principal: SessionPrincipal("revue"), Path: []string{"session", "close"}, Subject: Subject{Session: "other"}}
	if ok, _ := Decide(request, RoleSteward); ok {
		t.Fatal("a steward closed a session outside its subtree")
	}
	if Escalable(request, RoleSteward) {
		t.Fatal("a steward could ask for a session outside its subtree")
	}
}

func TestANeverVerbSaysEvenApprovalWillNotRunIt(t *testing.T) {
	_, why := Decide(req(SessionPrincipal("revue"), "role", "grant"), RoleAgent)
	if !strings.Contains(why, "even with the operator's approval") {
		t.Fatalf("refusal %q, want it to say no approval runs it", why)
	}
	_, why = Decide(req(SessionPrincipal("revue"), "agent", "create"), RoleAgent)
	if !strings.Contains(why, "steward") {
		t.Fatalf("refusal %q, want it to name the steward role", why)
	}
}

func TestARefusedVerbIsPutToTheOperatorOnlyWhenNothingElseForbidsIt(t *testing.T) {
	session := SessionPrincipal("revue")
	cases := []struct {
		name string
		req  Request
		role Role
		want bool
	}{
		{"a configuration verb", req(session, "schedule", "add"), RoleAgent, true},
		{"a verb no list names", req(session, "agent", "create"), RoleAgent, true},
		{"a verb the agent already runs", req(session, "session", "list"), RoleAgent, false},
		{"granting a role", req(session, "role", "grant"), RoleAgent, false},
		{"answering an approval", req(session, "approve", "allow"), RoleAgent, false},
		{"a session outside the subtree", Request{Principal: session, Path: []string{"session", "close"}, Subject: Subject{Session: "other"}}, RoleAgent, false},
		{"its own subtree", Request{Principal: session, Path: []string{"session", "close"}, Subject: Subject{Session: "revue", InSubtree: true}}, RoleAgent, true},
		{"a human observer", req(Principal{ID: "chat:1"}, "schedule", "add"), RoleObserver, false},
	}
	for _, tc := range cases {
		if got := Escalable(tc.req, tc.role); got != tc.want {
			t.Errorf("%s: Escalable = %v, want %v", tc.name, got, tc.want)
		}
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
	if Grantable("agent") || Grantable("steward") {
		t.Fatal("agent and steward must not be grantable to a human")
	}
	if !GrantableToSession("steward") || GrantableToSession("owner") {
		t.Fatal("a session may be granted steward and nothing else")
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

func TestEachVerbAnAgentRunsIsAReadOrAnAnnouncedWrite(t *testing.T) {
	writes := []string{
		"session send", "session seed", "session handoff",
		"session pr open", "session pr reply", "session issue open", "session issue comment",
		"memory record", "memory unlink", "memory restore",
	}
	def, _ := definitionOf(RoleAgent)
	for _, verb := range def.allow {
		want := slices.Contains(writes, verb)
		if got := Writes(strings.Fields(verb), false, false); got != want {
			t.Errorf("Writes(%q) = %v, want %v", verb, got, want)
		}
	}
	if !Writes([]string{"acmechat", "channel", "comments"}, true, false) {
		t.Error("a plugin's verb that declared nothing is taken for a read from its name")
	}
	if Writes([]string{"acmechat", "channel", "read"}, true, true) {
		t.Error("a plugin's verb declared read only counts as a write")
	}
	if Writes([]string{"session", "get-mode"}, false, false) {
		t.Error("get-mode counts as a write")
	}
}

func TestAnObserverReadsWhatASessionReadsAndWritesNothing(t *testing.T) {
	human := Principal{ID: "chat:1"}
	for _, path := range [][]string{{"health"}, {"usage"}, {"config", "get"}, {"env", "get"}, {"plugin", "list"}, {"connect", "status"}, {"setup", "check"}} {
		if ok, why := Decide(req(human, path...), RoleObserver); !ok {
			t.Errorf("observer refused %v: %s", path, why)
		}
		if Writes(path, false, false) {
			t.Errorf("%v counts as a write", path)
		}
	}
	for _, path := range [][]string{{"session", "send"}, {"session", "issue", "open"}, {"memory", "record"}} {
		if !Writes(path, false, false) {
			t.Errorf("%v does not count as a write", path)
		}
	}
	if Writes([]string{"approve", "ask"}, false, false) {
		t.Error("approve ask, asked by the hook at each tool call, counts as a write")
	}
}

func TestSessionEndIsRefusedToAgentAndFollowsSessionClose(t *testing.T) {
	path := []string{"session", "end"}
	if ok, _ := Decide(req(SessionPrincipal("revue"), path...), RoleAgent); ok {
		t.Fatal("agent must not end a session")
	}
	if ok, why := Decide(req(Principal{ID: "chat:1"}, path...), RoleOperator); !ok {
		t.Fatalf("operator refused session end: %s", why)
	}
	if ok, why := Decide(req(SessionPrincipal("revue"), path...), RoleSteward); !ok {
		t.Fatalf("steward refused session end: %s", why)
	}
	closePath := []string{"session", "close"}
	for _, c := range []struct {
		name      string
		principal Principal
		role      Role
	}{
		{"agent", SessionPrincipal("revue"), RoleAgent},
		{"steward", SessionPrincipal("revue"), RoleSteward},
		{"operator", Principal{ID: "chat:1"}, RoleOperator},
	} {
		closeOK, _ := Decide(req(c.principal, closePath...), c.role)
		endOK, _ := Decide(req(c.principal, path...), c.role)
		if endOK != closeOK {
			t.Fatalf("%s: session end allowed=%v, session close allowed=%v", c.name, endOK, closeOK)
		}
	}
}
