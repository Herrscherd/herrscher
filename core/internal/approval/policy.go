// Package approval decides whether one tool call runs, needs a human, or is
// refused. It is pure: no files, no sockets, no clock, no human. The daemon
// wires it to all three (core/host/approve.go); everything worth arguing about
// is decided here, where a test can argue back.
package approval

import (
	"fmt"
	"strings"
)

// Decision is one verdict on a tool call, on a severity ladder: a stricter
// decision always wins, whatever order the rules were written in. That is what
// lets an agent's own rules be appended to the daemon's without a merge that
// could quietly widen them.
type Decision string

const (
	Allow Decision = "allow"
	Ask   Decision = "ask"
	Deny  Decision = "deny"
)

// severity ranks decisions. Unknown values rank lowest so a malformed rule can
// never out-vote a well formed one.
func severity(d Decision) int {
	switch d {
	case Deny:
		return 2
	case Ask:
		return 1
	default:
		return 0
	}
}

// Rule is one line of policy: a decision, the tool it speaks about, and a glob
// the tool's subject must match. Tool "*" is any tool; an empty pattern is any
// subject.
type Rule struct {
	Decision Decision
	Tool     string
	Pattern  string
}

// Policy is an unordered rule set. Order carries no meaning: Decide takes the
// strictest match.
type Policy []Rule

// String renders a rule back to the form ParseRule accepts.
func (r Rule) String() string {
	if r.Pattern == "" {
		return string(r.Decision) + " " + r.Tool
	}
	return string(r.Decision) + " " + r.Tool + "(" + r.Pattern + ")"
}

// ParseRule reads one rule: "<decision> <Tool>" or "<decision> <Tool>(<glob>)".
func ParseRule(s string) (Rule, error) {
	s = strings.TrimSpace(s)
	head, rest, ok := strings.Cut(s, " ")
	if !ok {
		return Rule{}, fmt.Errorf("rule %q: expected `<allow|ask|deny> <Tool>[(<pattern>)]`", s)
	}
	d := Decision(strings.ToLower(strings.TrimSpace(head)))
	switch d {
	case Allow, Ask, Deny:
	default:
		return Rule{}, fmt.Errorf("rule %q: unknown decision %q", s, head)
	}
	rest = strings.TrimSpace(rest)
	r := Rule{Decision: d, Tool: rest}
	if open := strings.Index(rest, "("); open >= 0 {
		if !strings.HasSuffix(rest, ")") {
			return Rule{}, fmt.Errorf("rule %q: unclosed pattern", s)
		}
		r.Tool = strings.TrimSpace(rest[:open])
		r.Pattern = rest[open+1 : len(rest)-1]
	}
	if r.Tool == "" {
		return Rule{}, fmt.Errorf("rule %q: names no tool", s)
	}
	return r, nil
}

// ParsePolicy reads one rule per line, skipping blanks and # comments. It
// returns every rule it could read AND every error it met: one bad line in an
// agent's file must not silently drop the rules around it.
func ParsePolicy(text string) (Policy, []error) {
	var p Policy
	var errs []error
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		r, err := ParseRule(line)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		p = append(p, r)
	}
	return p, errs
}

// Request is one tool call awaiting a verdict. Subject is the part a rule
// matches, and which part that is depends on the tool: see SubjectOf.
type Request struct {
	Tool    string
	Subject string
}

// Mode is a session's own stance, folded in after the rules have spoken.
type Mode string

const (
	ModeAsk    Mode = "ask"    // the policy's own verdict; also the zero value
	ModeBypass Mode = "bypass" // nothing is ever asked
	ModeStrict Mode = "strict" // anything not explicitly allowed is asked
)

// Decide returns the strictest decision any rule reaches for req, and whether
// any rule matched at all. Nothing matching is Allow: an empty policy is the
// behaviour herrscher had before approvals existed, which is what an existing
// install must keep on first boot.
func (p Policy) Decide(req Request) (Decision, bool) {
	out, matched := Allow, false
	for _, r := range p {
		if r.Tool != "*" && r.Tool != req.Tool {
			continue
		}
		if r.Pattern != "" && !glob(r.Pattern, req.Subject) {
			continue
		}
		matched = true
		if severity(r.Decision) > severity(out) {
			out = r.Decision
		}
	}
	return out, matched
}

// Merge is concatenation, and that is the whole point. Decide takes the
// strictest match, so appending an agent's rules to the daemon's can only ever
// raise a verdict. There is no case to handle where an agent widens what the
// operator narrowed, because the shape makes it unrepresentable.
func Merge(daemon, agent Policy) Policy {
	out := make(Policy, 0, len(daemon)+len(agent))
	return append(append(out, daemon...), agent...)
}

// Apply folds the session's mode into a verdict.
func Apply(m Mode, d Decision, matched bool) Decision {
	switch m {
	case ModeBypass:
		return Allow
	case ModeStrict:
		if d == Allow && !matched {
			return Ask
		}
	}
	return d
}

// glob matches pattern against s, where * stands for any run of characters,
// path separators included. Two pointers with one backtrack mark rather than a
// regexp: the subject is model-written text, and a pattern like "*a*a*a*" must
// not be a way to make the daemon think for a second per tool call.
func glob(pattern, s string) bool {
	var si, pi, star, mark int
	star = -1
	for si < len(s) {
		switch {
		case pi < len(pattern) && (pattern[pi] == s[si] || pattern[pi] == '?'):
			si++
			pi++
		case pi < len(pattern) && pattern[pi] == '*':
			star, mark = pi, si
			pi++
		case star >= 0:
			pi = star + 1
			mark++
			si = mark
		default:
			return false
		}
	}
	for pi < len(pattern) && pattern[pi] == '*' {
		pi++
	}
	return pi == len(pattern)
}
