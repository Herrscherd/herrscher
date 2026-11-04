package approval

import "testing"

func TestParseRuleRoundTrip(t *testing.T) {
	for _, s := range []string{
		"ask Bash(git push*)",
		"deny Bash(rm -rf /*)",
		"allow Read",
		"deny *",
		"ask mcp__neublox__place",
	} {
		r, err := ParseRule(s)
		if err != nil {
			t.Fatalf("ParseRule(%q): %v", s, err)
		}
		if got := r.String(); got != s {
			t.Fatalf("round trip: got %q, want %q", got, s)
		}
	}
}

func TestParseRuleRejects(t *testing.T) {
	for _, s := range []string{"", "Bash(ls)", "maybe Bash(ls)", "ask", "ask (ls)"} {
		if _, err := ParseRule(s); err == nil {
			t.Fatalf("ParseRule(%q): expected an error", s)
		}
	}
}

func TestParsePolicySkipsBlanksAndComments(t *testing.T) {
	p, errs := ParsePolicy("# rules\n\nask Bash(git push*)\nnonsense\ndeny Bash(sudo*)\n")
	if len(p) != 2 {
		t.Fatalf("got %d rules, want 2", len(p))
	}
	if len(errs) != 1 {
		t.Fatalf("got %d errors, want 1", len(errs))
	}
}

func TestDecideStrictestWinsWhateverTheOrder(t *testing.T) {
	req := Request{Tool: "Bash", Subject: "git push origin master"}
	forward := Policy{{Allow, "Bash", ""}, {Deny, "Bash", "git push*"}}
	backward := Policy{{Deny, "Bash", "git push*"}, {Allow, "Bash", ""}}
	for name, p := range map[string]Policy{"forward": forward, "backward": backward} {
		d, matched := p.Decide(req)
		if d != Deny || !matched {
			t.Fatalf("%s: got %q matched=%v, want deny matched=true", name, d, matched)
		}
	}
}

func TestDecideDefaultsToAllowUnmatched(t *testing.T) {
	p := Policy{{Deny, "Bash", "sudo*"}}
	d, matched := p.Decide(Request{Tool: "Read", Subject: "/etc/passwd"})
	if d != Allow || matched {
		t.Fatalf("got %q matched=%v, want allow matched=false", d, matched)
	}
}

func TestDecideStarToolAndEmptyPattern(t *testing.T) {
	p := Policy{{Ask, "*", ""}}
	if d, matched := p.Decide(Request{Tool: "WebFetch", Subject: "https://x"}); d != Ask || !matched {
		t.Fatalf("got %q matched=%v, want ask matched=true", d, matched)
	}
}

func TestGlobCrossesSlashes(t *testing.T) {
	p := Policy{{Deny, "Bash", "rm -rf *"}}
	if d, _ := p.Decide(Request{Tool: "Bash", Subject: "rm -rf /home/shan/dev"}); d != Deny {
		t.Fatalf("got %q, want deny: a glob must cross path separators", d)
	}
}

func TestGlobInTheMiddle(t *testing.T) {
	p := Policy{{Ask, "Bash", "git*push*"}}
	if d, _ := p.Decide(Request{Tool: "Bash", Subject: "git -C /repo push"}); d != Ask {
		t.Fatalf("got %q, want ask", d)
	}
}

func TestMergeCannotWiden(t *testing.T) {
	daemon := Policy{{Deny, "Bash", "git push*"}}
	agent := Policy{{Allow, "Bash", ""}, {Allow, "Bash", "git push*"}}
	d, _ := Merge(daemon, agent).Decide(Request{Tool: "Bash", Subject: "git push"})
	if d != Deny {
		t.Fatalf("got %q, want deny: an agent may tighten, never widen", d)
	}
}

func TestMergeTightens(t *testing.T) {
	daemon := Policy{{Ask, "Bash", "git push*"}}
	agent := Policy{{Deny, "Bash", "git push*"}}
	d, _ := Merge(daemon, agent).Decide(Request{Tool: "Bash", Subject: "git push"})
	if d != Deny {
		t.Fatalf("got %q, want deny", d)
	}
}

func TestApplyModes(t *testing.T) {
	cases := []struct {
		name    string
		mode    Mode
		tool    string
		in      Decision
		matched bool
		want    Decision
	}{
		{"bypass ignores deny", ModeBypass, "Bash", Deny, true, Allow},
		{"bypass ignores ask", ModeBypass, "Bash", Ask, true, Allow},
		{"auto lets an unmatched command through", ModeAuto, "Bash", Allow, false, Allow},
		{"auto lets an asked command through", ModeAuto, "Bash", Ask, true, Allow},
		{"auto keeps a deny rule", ModeAuto, "Bash", Deny, true, Deny},
		{"ask keeps a rule's ask", ModeAsk, "Read", Ask, true, Ask},
		{"ask asks an unmatched edit", ModeAsk, "Edit", Allow, false, Ask},
		{"ask asks an unmatched command", ModeAsk, "Bash", Allow, false, Ask},
		{"ask lets an unmatched read through", ModeAsk, "Read", Allow, false, Allow},
		{"ask honours an allow rule on a command", ModeAsk, "Bash", Allow, true, Allow},
		{"legacy empty mode keeps the policy only", "", "Bash", Allow, false, Allow},
		{"legacy empty mode keeps a deny", "", "Bash", Deny, true, Deny},
		{"accept-edits lets an unmatched edit through", ModeAcceptEdits, "Edit", Allow, false, Allow},
		{"accept-edits lets a write through", ModeAcceptEdits, "Write", Allow, false, Allow},
		{"accept-edits asks an unmatched command", ModeAcceptEdits, "Bash", Allow, false, Ask},
		{"accept-edits keeps a deny on an edit", ModeAcceptEdits, "Edit", Deny, true, Deny},
		{"plan refuses an edit", ModePlan, "Edit", Allow, false, Deny},
		{"plan refuses a command", ModePlan, "Bash", Allow, false, Deny},
		{"plan refuses a command an allow rule names", ModePlan, "Bash", Allow, true, Deny},
		{"plan refuses an unknown tool", ModePlan, "mcp__x__write", Allow, false, Deny},
		{"plan lets a read through", ModePlan, "Read", Allow, false, Allow},
		{"plan keeps a read's ask", ModePlan, "Grep", Ask, true, Ask},
		{"strict asks an unmatched tool", ModeStrict, "Read", Allow, false, Ask},
		{"strict honours an allow rule", ModeStrict, "Read", Allow, true, Allow},
		{"strict keeps a deny", ModeStrict, "Read", Deny, true, Deny},
	}
	for _, c := range cases {
		if got := Apply(c.mode, c.tool, c.in, c.matched); got != c.want {
			t.Fatalf("%s: Apply(%q,%q,%q,%v): got %q, want %q", c.name, c.mode, c.tool, c.in, c.matched, got, c.want)
		}
	}
}

func TestParseRiskReadsTheFirstWord(t *testing.T) {
	for answer, want := range map[string]Risk{
		"low":               RiskLow,
		" Low.\n":           RiskLow,
		"medium, it pushes": RiskMedium,
		"HIGH":              RiskHigh,
	} {
		got, ok := ParseRisk(answer)
		if !ok || got != want {
			t.Fatalf("ParseRisk(%q) = %q, %v; want %q", answer, got, ok, want)
		}
	}
}

func TestParseRiskRefusesAnythingElse(t *testing.T) {
	for _, answer := range []string{"", "maybe", "I think it is fine", "lowest"} {
		if got, ok := ParseRisk(answer); ok {
			t.Fatalf("ParseRisk(%q) = %q, want no rating", answer, got)
		}
	}
}

func TestOnlyALowRiskPasses(t *testing.T) {
	if RiskLow.Decision() != Allow {
		t.Fatal("a low risk must pass alone")
	}
	for _, risk := range []Risk{RiskMedium, RiskHigh} {
		if risk.Decision() != Ask {
			t.Fatalf("risk %q must go to the prompt", risk)
		}
	}
}

func TestSmartRatesOnlyWhatTheRulesLeaveOpen(t *testing.T) {
	cases := []struct {
		decided Decision
		matched bool
		want    bool
	}{
		{Allow, false, true},
		{Ask, true, true},
		{Allow, true, false},
		{Deny, true, false},
	}
	for _, c := range cases {
		if got := Judged(ModeSmart, "Bash", c.decided, c.matched); got != c.want {
			t.Fatalf("Judged(smart, %s, %v) = %v, want %v", c.decided, c.matched, got, c.want)
		}
	}
	if Judged(ModeSmart, "Read", Allow, false) {
		t.Fatal("a read is never sent to the model, as before")
	}
	if Judged(ModeAsk, "Bash", Allow, false) {
		t.Fatal("only smart mode asks a model")
	}
}

func TestSmartWithoutAnyRuleStillPassesWhenTheModelSaysSo(t *testing.T) {
	if got := Apply(ModeSmart, "Bash", Ask, true); got != Ask {
		t.Fatalf("smart must leave a rule's ask to the model, got %q", got)
	}
	if got := Apply(ModeSmart, "Bash", Allow, false); got != Allow {
		t.Fatalf("smart must leave an unmatched call to the model, got %q", got)
	}
}

func TestRuleForGeneralizesABashCallToItsTwoFirstWords(t *testing.T) {
	rule, ok := RuleFor("Bash", "git push origin feat/x")
	if !ok {
		t.Fatal("a bash command must get a rule")
	}
	if rule.String() != "allow Bash(git push*)" {
		t.Fatalf("got %q", rule.String())
	}
	if !(Policy{rule}).matches(Request{Tool: "Bash", Subject: "git push origin feat/x"}) {
		t.Fatal("the rule must let its own call through")
	}
	if (Policy{rule}).matches(Request{Tool: "Bash", Subject: "git status"}) {
		t.Fatal("the rule must not let another command through")
	}
}

func TestRuleForKeepsOneWordBashAndOtherToolsExact(t *testing.T) {
	for _, c := range []struct{ tool, subject, want string }{
		{"Bash", "ls", "allow Bash(ls)"},
		{"Edit", "/repo/a b.go", "allow Edit(/repo/a b.go)"},
		{"WebFetch", "https://x.dev/a b", "allow WebFetch(https://x.dev/a b)"},
	} {
		rule, ok := RuleFor(c.tool, c.subject)
		if !ok || rule.String() != c.want {
			t.Fatalf("RuleFor(%q, %q) = %q, %v; want %q", c.tool, c.subject, rule, ok, c.want)
		}
	}
}

func TestRuleForRefusesWhatWouldWiden(t *testing.T) {
	for _, c := range []struct{ tool, subject string }{
		{"Bash", ""},
		{"", "ls"},
		{"*", "ls"},
		{"Bash", "git push*"},
		{"Bash", "ls?"},
		{"Bash", "echo\nrm -rf x"},
		{"Ba(sh", "ls"},
	} {
		if rule, ok := RuleFor(c.tool, c.subject); ok {
			t.Fatalf("RuleFor(%q, %q) = %q, must refuse", c.tool, c.subject, rule)
		}
	}
}

func (p Policy) matches(req Request) bool {
	decided, matched := p.Decide(req)
	return matched && decided == Allow
}
