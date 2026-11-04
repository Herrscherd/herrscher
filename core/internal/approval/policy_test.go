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
