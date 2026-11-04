// Package approval decides whether one tool call runs, needs a human, or is
// refused. It is pure: no files, no sockets, no clock, no human. The daemon
// wires it to all three (core/host/approve.go); everything worth arguing about
// is decided here, where a test can argue back.
package approval

import (
	"errors"
	"fmt"
	"strconv"
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
type Decision uint8

const (
	// Allow lets the action run without asking.
	Allow Decision = iota + 1
	// Ask defers the decision to the approval prompt.
	Ask
	// Deny refuses the action.
	Deny
)

// String returns the lower case word the policy language uses for d.
func (d Decision) String() string {
	switch d {
	case Allow:
		return "allow"
	case Ask:
		return "ask"
	case Deny:
		return "deny"
	}
	return ""
}

// ParseDecision reads a decision word, ignoring case and surrounding space. It
// reports false for any word that is not allow, ask or deny.
func ParseDecision(word string) (Decision, bool) {
	word = strings.ToLower(strings.TrimSpace(word))
	for _, decision := range []Decision{Allow, Ask, Deny} {
		if word == decision.String() {
			return decision, true
		}
	}
	return 0, false
}

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
		return r.Decision.String() + " " + r.Tool
	}
	return r.Decision.String() + " " + r.Tool + "(" + r.Pattern + ")"
}

// ParseRule reads one rule: "<decision> <Tool>" or "<decision> <Tool>(<glob>)".
func ParseRule(spec string) (Rule, error) {
	spec = strings.TrimSpace(spec)
	head, rest, ok := strings.Cut(spec, " ")
	if !ok {
		return Rule{}, fmt.Errorf("%w %q: expected `<allow|ask|deny> <Tool>[(<pattern>)]`", errMalformedRule, spec)
	}
	decision, ok := ParseDecision(head)
	if !ok {
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
type Mode uint8

const (
	// ModeAsk routes file edits and commands through the approval prompt.
	ModeAsk Mode = iota + 1
	// ModeAcceptEdits lets file edits through and still asks before a command.
	ModeAcceptEdits
	// ModePlan is read-only: anything that is not a read is refused.
	ModePlan
	// ModeAuto lets everything through except what a deny rule refuses.
	ModeAuto
	// ModeBypass skips the approval prompt.
	ModeBypass
	// ModeStrict refuses anything the policy does not explicitly allow.
	ModeStrict
	// ModeSmart asks a model to rate each call that the rules leave open, and
	// asks a human only when the rating is not low.
	ModeSmart
)

// Modes lists every approval mode, in the order the operator sees them.
func Modes() []Mode {
	return []Mode{ModeAsk, ModeAcceptEdits, ModePlan, ModeAuto, ModeBypass, ModeStrict, ModeSmart}
}

// String returns the word an operator types for m, which is also the word the
// session state stores.
func (m Mode) String() string {
	switch m {
	case ModeAsk:
		return "ask"
	case ModeAcceptEdits:
		return "accept-edits"
	case ModePlan:
		return "plan"
	case ModeAuto:
		return "auto"
	case ModeBypass:
		return "bypass"
	case ModeStrict:
		return "strict"
	case ModeSmart:
		return "smart"
	}
	return ""
}

// ParseMode reads an approval mode word. It reports false for any word that is
// not a mode name.
func ParseMode(word string) (Mode, bool) {
	for _, mode := range Modes() {
		if word == mode.String() {
			return mode, true
		}
	}
	return 0, false
}

// Risk is the rating a judge gives a tool call: how much harm it could do if
// it were allowed. The zero value means no rating.
type Risk uint8

const (
	// RiskLow is a call that can be allowed without a human.
	RiskLow Risk = iota + 1
	// RiskMedium is a call a human must read first.
	RiskMedium
	// RiskHigh is a call a human must read first, and that is likely harmful.
	RiskHigh
)

// Risks lists every rating, from the least to the most harmful.
func Risks() []Risk {
	return []Risk{RiskLow, RiskMedium, RiskHigh}
}

// String returns the lower case word a judge answers with.
func (r Risk) String() string {
	switch r {
	case RiskLow:
		return "low"
	case RiskMedium:
		return "medium"
	case RiskHigh:
		return "high"
	}
	return strconv.Itoa(int(r))
}

// ParseRisk reads the rating from the first word of a judge's answer, ignoring
// case and trailing punctuation. It reports false for any other word.
func ParseRisk(answer string) (Risk, bool) {
	words := strings.Fields(strings.ToLower(answer))
	if len(words) == 0 {
		return 0, false
	}
	word := strings.Trim(words[0], ".,;:!?`'\"*")
	for _, risk := range Risks() {
		if word == risk.String() {
			return risk, true
		}
	}
	return 0, false
}

// Decision returns the verdict a rating leads to: only a low risk is allowed.
func (r Risk) Decision() Decision {
	if r == RiskLow {
		return Allow
	}
	return Ask
}

// IsRead reports whether tool only reads, so a smart judge never rates it.
func IsRead(tool string) bool {
	switch tool {
	case toolRead, "Glob", "Grep", "LS":
		return true
	}
	return false
}

// Judged reports whether mode sends this call to the judge: smart mode judges
// every call that is not a read, is not refused, and is not allowed by a rule.
func Judged(m Mode, tool string, decided Decision, matched bool) bool {
	return m == ModeSmart && !IsRead(tool) && decided != Deny && (decided != Allow || !matched)
}

// RuleFor proposes the rule that allows a call like this one from now on. It
// reports false when the subject cannot be written as a safe rule.
func RuleFor(tool, subject string) (Rule, bool) {
	if tool == "" || subject == "" || strings.ContainsAny(tool, "()* \r\n") || strings.ContainsAny(subject, "*?\r\n") {
		return Rule{}, false
	}
	pattern := subject
	if words := strings.Fields(subject); tool == toolBash && len(words) > 1 {
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

// ReadOnly reports whether a tool only reads: the only tools plan mode lets run.
// Anything unknown, an MCP tool or a sub-agent included, counts as a write.
func ReadOnly(tool string) bool {
	switch tool {
	case toolRead, "Grep", "Glob", "LS", "WebFetch", "WebSearch", "TodoWrite":
		return true
	}
	return false
}

func editsFiles(tool string) bool {
	switch tool {
	case "Edit", "Write", "NotebookEdit":
		return true
	}
	return false
}

func runsCommand(tool string) bool {
	return tool == toolBash
}

// Apply folds the session's mode into a verdict. Manual (ask) and accept-edits
// only assume an unmatched file edit or command should be asked about, so a rule
// that names the tool still decides it. Plan refuses every tool that is not a
// read, whatever the rules say.
func Apply(mode Mode, tool string, decided Decision, matched bool) Decision {
	unmatched := decided == Allow && !matched
	switch mode {
	case ModeBypass:
		return Allow
	case ModeAuto:
		return autoVerdict(decided)
	case ModePlan:
		return planVerdict(tool, decided)
	case ModeAcceptEdits:
		if unmatched && runsCommand(tool) {
			return Ask
		}
	case ModeStrict:
		if unmatched {
			return Ask
		}
	case ModeAsk:
		if unmatched && (editsFiles(tool) || runsCommand(tool)) {
			return Ask
		}
	case ModeSmart:
	}
	return decided
}

func autoVerdict(decided Decision) Decision {
	if decided == Deny {
		return Deny
	}
	return Allow
}

func planVerdict(tool string, decided Decision) Decision {
	if decided == Deny || !ReadOnly(tool) {
		return Deny
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
