// Package approval decides whether one tool call runs, needs a human, or is
// refused. It is pure: no files, no sockets, no clock, no human. The daemon
// wires it to all three (core/host/approve.go); everything worth arguing about
// is decided here, where a test can argue back.
package approval

import (
	"errors"
	"fmt"
	"strings"
)

var (
	errMalformedRule   = errors.New("malformed rule")
	errUnknownDecision = errors.New("unknown decision")
)

// Decision is one verdict on a tool call, on a severity ladder: a stricter
// decision always wins, whatever order the rules were written in. That is what
// lets an agent's own rules be appended to the daemon's without a merge that
// could quietly widen them.
type Decision string

const (
	// Allow lets the action run without asking.
	Allow Decision = "allow"
	// Ask defers the decision to the approval prompt.
	Ask Decision = "ask"
	// Deny refuses the action.
	Deny Decision = "deny"
)

const (
	severityAllow = iota
	severityAsk
	severityDeny
)

func severity(d Decision) int {
	switch d {
	case Deny:
		return severityDeny
	case Ask:
		return severityAsk
	case Allow:
	}
	return severityAllow
}

// Rule is one line of policy: a decision, the tool it speaks about, and a glob
// the tool's subject must match. Tool "*" is any tool; an empty pattern is any
// subject. In a pattern, * is any run of characters and ? is exactly one byte,
// so ? does not match an accented or non-Latin character on its own (see glob).
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
func ParseRule(spec string) (Rule, error) {
	spec = strings.TrimSpace(spec)
	head, rest, ok := strings.Cut(spec, " ")
	if !ok {
		return Rule{}, fmt.Errorf("%w %q: expected `<allow|ask|deny> <Tool>[(<pattern>)]`", errMalformedRule, spec)
	}
	decision := Decision(strings.ToLower(strings.TrimSpace(head)))
	switch decision {
	case Allow, Ask, Deny:
	default:
		return Rule{}, fmt.Errorf("%w %q in rule %q", errUnknownDecision, head, spec)
	}
	rest = strings.TrimSpace(rest)
	rule := Rule{Decision: decision, Tool: rest}
	if open := strings.Index(rest, "("); open >= 0 {
		if !strings.HasSuffix(rest, ")") {
			return Rule{}, fmt.Errorf("%w %q: unclosed pattern", errMalformedRule, spec)
		}
		rule.Tool = strings.TrimSpace(rest[:open])
		rule.Pattern = rest[open+1 : len(rest)-1]
	}
	if rule.Tool == "" {
		return Rule{}, fmt.Errorf("%w %q: names no tool", errMalformedRule, spec)
	}
	return rule, nil
}

// ParsePolicy reads one rule per line, skipping blanks and # comments. It
// returns every rule it could read AND every error it met: one bad line in an
// agent's file must not silently drop the rules around it.
func ParsePolicy(text string) (Policy, []error) {
	var policy Policy
	var errs []error
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rule, err := ParseRule(line)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		policy = append(policy, rule)
	}
	return policy, errs
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
	// ModeAsk routes actions through the approval prompt.
	ModeAsk Mode = "ask"
	// ModeBypass skips the approval prompt.
	ModeBypass Mode = "bypass"
	// ModeStrict refuses anything the policy does not explicitly allow.
	ModeStrict Mode = "strict"
	ModeSmart  Mode = "smart"
)

type Risk string

const (
	RiskLow    Risk = "low"
	RiskMedium Risk = "medium"
	RiskHigh   Risk = "high"
)

func ParseRisk(answer string) (Risk, bool) {
	words := strings.Fields(strings.ToLower(answer))
	if len(words) == 0 {
		return "", false
	}
	risk := Risk(strings.Trim(words[0], ".,;:!?`'\"*"))
	switch risk {
	case RiskLow, RiskMedium, RiskHigh:
		return risk, true
	}
	return "", false
}

func (r Risk) Decision() Decision {
	if r == RiskLow {
		return Allow
	}
	return Ask
}

var readOnlyTools = map[string]bool{"Read": true, "Glob": true, "Grep": true, "LS": true}

func IsRead(tool string) bool {
	return readOnlyTools[tool]
}

func Judged(m Mode, tool string, decided Decision, matched bool) bool {
	return m == ModeSmart && !IsRead(tool) && decided != Deny && (decided != Allow || !matched)
}

func RuleFor(tool, subject string) (Rule, bool) {
	if tool == "" || subject == "" || strings.ContainsAny(tool, "()* \r\n") || strings.ContainsAny(subject, "*?\r\n") {
		return Rule{}, false
	}
	pattern := subject
	if words := strings.Fields(subject); tool == "Bash" && len(words) > 1 {
		pattern = words[0] + " " + words[1] + "*"
	}
	rule := Rule{Decision: Allow, Tool: tool, Pattern: pattern}
	parsed, err := ParseRule(rule.String())
	if err != nil || parsed != rule {
		return Rule{}, false
	}
	return rule, true
}

// Decide returns the strictest decision any rule reaches for req, and whether
// any rule matched at all. Nothing matching is Allow: an empty policy is the
// behaviour herrscher had before approvals existed, which is what an existing
// install must keep on first boot.
func (p Policy) Decide(req Request) (Decision, bool) {
	out, matched := Allow, false
	for _, rule := range p {
		if rule.Tool != "*" && rule.Tool != req.Tool {
			continue
		}
		if rule.Pattern != "" && !glob(rule.Pattern, req.Subject) {
			continue
		}
		matched = true
		if severity(rule.Decision) > severity(out) {
			out = rule.Decision
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
func Apply(m Mode, decided Decision, matched bool) Decision {
	switch m {
	case ModeBypass:
		return Allow
	case ModeStrict:
		if decided == Allow && !matched {
			return Ask
		}
	case ModeAsk, ModeSmart:
	}
	return decided
}

func glob(pattern, subject string) bool {
	var textPos, patPos, star, mark int
	star = -1
	for textPos < len(subject) {
		switch {
		case patPos < len(pattern) && (pattern[patPos] == subject[textPos] || pattern[patPos] == '?'):
			textPos++
			patPos++
		case patPos < len(pattern) && pattern[patPos] == '*':
			star, mark = patPos, textPos
			patPos++
		case star >= 0:
			patPos = star + 1
			mark++
			textPos = mark
		default:
			return false
		}
	}
	for patPos < len(pattern) && pattern[patPos] == '*' {
		patPos++
	}
	return patPos == len(pattern)
}
