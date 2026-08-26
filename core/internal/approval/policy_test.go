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
		mode    Mode
		in      Decision
		matched bool
		want    Decision
	}{
		{ModeBypass, Deny, true, Allow},
		{ModeBypass, Ask, true, Allow},
		{ModeAsk, Ask, true, Ask},
		{"", Deny, true, Deny},
		{ModeStrict, Allow, false, Ask},
		{ModeStrict, Allow, true, Allow},
		{ModeStrict, Deny, true, Deny},
	}
	for _, c := range cases {
		if got := Apply(c.mode, c.in, c.matched); got != c.want {
			t.Fatalf("Apply(%q,%q,%v): got %q, want %q", c.mode, c.in, c.matched, got, c.want)
		}
	}
}
