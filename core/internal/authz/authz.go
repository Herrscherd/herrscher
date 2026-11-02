// Package authz decides whether a caller may run a verb. It is pure: no files,
// no sockets, no clock, no state. The daemon wires it in core/host/authz.go;
// everything worth arguing about is decided here, where a test can argue back.
//
// The shape mirrors core/internal/approval, and for the same reason: a security
// decision that can only be exercised through a live daemon is a decision
// nobody reviews.
package authz

import (
	"fmt"
	"sort"
	"strings"
)

// Role names a set of verbs. The four are defined here and not configurable:
// what a role model buys is naming a set of rights, not inventing a language to
// describe new ones. What an operator configures is who holds which.
type Role string

const (
	// RoleNone is the absence of a role, which is not a role that allows
	// nothing. It means the daemon does not decide, and whatever the entry
	// point already enforces stands alone. It is what an install that
	// configured nothing gives its humans, so an upgrade changes nothing for
	// them.
	RoleNone Role = ""
	// RoleOwner runs everything, the role family included.
	RoleOwner Role = "owner"
	// RoleOperator runs everything except role, so a delegate cannot elevate
	// itself. That is the difference between delegating and abdicating.
	RoleOperator Role = "operator"
	// RoleObserver runs only what reads.
	RoleObserver Role = "observer"
	// RoleAgent is what a session holds. It is structural: never granted,
	// never revoked, because the daemon knows a connection came from a session
	// by having opened that listener itself.
	RoleAgent Role = "agent"
)

// LocalPrincipal is the caller on the daemon's own operator socket. Opening it
// already needs the daemon's uid, so arriving there is the proof: whoever can
// do it can also read the state, rewrite it, and relaunch the binary. Asking
// them for one more proof would be theatre.
const LocalPrincipal = "local"

// SessionPrefix marks a principal minted for a session. Only the core mints
// one, from the listener a connection arrived on.
const SessionPrefix = "session:"

// Principal is who is calling. It is never declared by the caller: it is
// derived from the entry point, because locally the agent and the daemon share
// a uid, so any secret the agent can read it can also replay. Where the
// connection arrives is the one thing an agent does not get to author.
type Principal struct {
	ID      string
	Session string
}

// SessionPrincipal is the principal a session's own command socket carries.
func SessionPrincipal(session string) Principal {
	return Principal{ID: SessionPrefix + session, Session: session}
}

// Subject is the session a verb names, resolved by the caller from the parsed
// arguments. InSubtree reports that it is the calling session itself or one
// whose parent chain reaches it.
type Subject struct {
	Session   string
	InSubtree bool
}

// Request is one invocation awaiting a verdict.
type Request struct {
	Principal   Principal
	Path        []string
	Contributed bool
	Subject     Subject
}

type definition struct {
	allowAll bool
	allow    []string
	deny     []string
	plugins  bool
}

func definitionOf(role Role) (definition, bool) {
	switch role {
	case RoleOwner:
		return definition{allowAll: true, plugins: true}, true
	case RoleOperator:
		return definition{allowAll: true, deny: []string{"role *"}, plugins: true}, true
	case RoleObserver:
		return definition{allow: []string{
			"commands", "whoami",
			"session list", "session info", "session log", "session who", "session scrollback",
			"agent list", "agent show",
			"models list",
			"approve list",
			"host list",
			"schedule list",
			"memory search", "memory locate",
		}}, true
	case RoleAgent:
		return definition{
			allow: []string{
				"commands", "whoami",
				"session send", "session seed",
				"session list", "session info", "session log", "session who", "session scrollback",
				"agent list", "agent show",
				"models list",
				"memory search", "memory locate", "memory record", "memory unlink", "memory restore",
				"approve ask",
			},
			deny: []string{
				"approve list", "approve allow", "approve deny", "approve rule", "approve mode",
				"host *", "role *", "schedule *", "service *", "set *", "plugin *",
				"skill approve", "memory forget",
				"session create",
				"session close", "session archive", "session resume", "session interrupt",
				"session switch", "session set-budget",
			},
			plugins: true,
		}, true
	case RoleNone:
	}
	return definition{}, false
}

var grantOrder = []Role{RoleObserver, RoleOperator, RoleOwner}

// Decide answers whether the request may run, and why not when it may not.
// RoleNone always allows. Subject is consulted for a session principal alone,
// since a human operating from a gateway is not confined to a subtree.
func Decide(r Request, role Role) (bool, string) {
	if role == RoleNone {
		return true, ""
	}
	verb := strings.Join(r.Path, " ")
	if !permits(role, verb, r.Contributed) {
		return false, refusal(r, role, verb)
	}
	if r.Principal.Session != "" && r.Subject.Session != "" && !r.Subject.InSubtree {
		return false, fmt.Sprintf(
			"herrscher: `%s` names session %q, and you are session %q. A session reaches itself and the sessions it delegated, nothing else.",
			verb, r.Subject.Session, r.Principal.Session)
	}
	return true, ""
}

func permits(role Role, verb string, contributed bool) bool {
	def, ok := definitionOf(role)
	if !ok {
		return false
	}
	for _, p := range def.deny {
		if match(p, verb) {
			return false
		}
	}
	if def.allowAll {
		return true
	}
	if contributed && def.plugins {
		return true
	}
	for _, p := range def.allow {
		if match(p, verb) {
			return true
		}
	}
	return false
}

var agentHints = map[string]string{
	"session create": "To delegate, end your turn with `⟢ delegate: <agent>` and the task: " +
		"the worker that opens runs a registered agent, so it answers to a policy the way you do.",
}

func refusal(r Request, role Role, verb string) string {
	if role == RoleAgent {
		if hint, ok := agentHints[verb]; ok {
			return fmt.Sprintf("herrscher: `%s` is not something a session may run. %s", verb, hint)
		}
		return fmt.Sprintf(
			"herrscher: `%s` is not something a session may run. Ask your operator to run it for you.", verb)
	}
	for _, candidate := range grantOrder {
		if permits(candidate, verb, r.Contributed) {
			return fmt.Sprintf("herrscher: `%s` needs the `%s` role, and %s has `%s`",
				verb, candidate, r.Principal.ID, role)
		}
	}
	return fmt.Sprintf("herrscher: `%s` is not something any role may run", verb)
}

func match(pattern, verb string) bool {
	if pattern == verb {
		return true
	}
	if family, ok := strings.CutSuffix(pattern, " *"); ok {
		return verb == family || strings.HasPrefix(verb, family+" ")
	}
	return false
}

// RoleOf resolves the role a principal holds, given the assignment table.
// The caller flattens the stored assignments down to principal and role name
// before calling: the display label decides nothing, so the pure package has no
// reason to know the shape it is stored in.
//
// Three rules carry the defaults. local is always owner. A session is always
// agent. A gateway principal takes its assignment, or observer when the table
// holds anything at all, or no role when it is empty. That last one is what
// makes an upgrade a no-op for humans, and the first grant a cliff: the verb
// that writes it says so.
func RoleOf(p Principal, assignments map[string]string) Role {
	if p.ID == LocalPrincipal {
		return RoleOwner
	}
	if p.Session != "" {
		return RoleAgent
	}
	if len(assignments) == 0 {
		return RoleNone
	}
	if name, ok := assignments[p.ID]; ok && Grantable(name) {
		return Role(name)
	}
	return RoleObserver
}

// Grantable reports whether name is a role an operator may assign. agent is
// not: it is what the entry point already answers, and writing it into the
// table by hand would be claiming a human is a session.
func Grantable(name string) bool {
	switch Role(name) {
	case RoleOwner, RoleOperator, RoleObserver:
		return true
	case RoleNone, RoleAgent:
	}
	return false
}

// GrantableRoles lists them, sorted, for a message that has to name the choice.
func GrantableRoles() []string {
	out := []string{string(RoleOwner), string(RoleOperator), string(RoleObserver)}
	sort.Strings(out)
	return out
}

// Known reports whether name is a role this package defines at all, grantable
// or not. The daemon calls it when it reads the table, and says once what it
// found: a name nobody defines falls back to observer, and falling back in
// silence is how a typo becomes a permission nobody can explain.
func Known(name string) bool {
	_, ok := definitionOf(Role(name))
	return ok
}

// Describe renders what a role runs, for `role show` and `role list`.
func Describe(role Role) string {
	def, ok := definitionOf(role)
	if !ok {
		return "no role: herrscher decides nothing, and the gateway's own rules stand alone"
	}
	var b strings.Builder
	if def.allowAll {
		b.WriteString("runs everything")
	} else {
		b.WriteString("runs " + strings.Join(def.allow, ", "))
	}
	if def.plugins && !def.allowAll {
		b.WriteString(", and the verbs plugins contribute")
	}
	if len(def.deny) > 0 {
		b.WriteString("\nnever runs " + strings.Join(def.deny, ", "))
	}
	return b.String()
}
