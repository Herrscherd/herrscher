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
	ID      string // "local", "session:revue", "chat:1234"
	Session string // non-empty for a session principal alone
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
	Path        []string // the verb, e.g. []string{"approve", "rule"}
	Contributed bool     // the verb was contributed by a plugin
	Subject     Subject
}

// definition is what one role may run. allowAll and allow are two shapes of the
// same field on purpose: owner and operator are deny lists, because a role
// meant to run the daemon should not have to be told about each new verb;
// observer and agent are allow lists, because a verb a later version adds must
// not arrive already permitted to a caller that is meant to be bounded.
type definition struct {
	allowAll bool
	allow    []string // verb patterns: an exact verb, or a family as "session *"
	deny     []string // wins over any allow
	plugins  bool     // verbs a plugin contributed are allowed
}

var definitions = map[Role]definition{
	RoleOwner:    {allowAll: true, plugins: true},
	RoleOperator: {allowAll: true, deny: []string{"role *"}, plugins: true},
	RoleObserver: {allow: []string{
		"commands", "whoami",
		"session list", "session info", "session log", "session who", "session scrollback",
		"agent list", "agent show",
		"models list",
		"approve list",
		"host list",
		"schedule list",
		"memory search", "memory locate",
	}},
	// What an agent may run is what the <capabilities> block already promises
	// it: speaking on its gateways, its memory, delegating, and asking for an
	// approval. Plugin verbs are allowed, which is the one hole in the allow
	// list and is deliberate: without it the capabilities block would promise a
	// publication the agent does not have until every gateway ships a release,
	// and compiling a plugin into the binary is already a trust decision of the
	// same order as choosing the binary.
	RoleAgent: {
		allow: []string{
			"commands", "whoami",
			"session send", "session seed", "session create",
			"session list", "session info", "session log", "session who", "session scrollback",
			"agent list", "agent show",
			"models list",
			"memory search", "memory locate", "memory record", "memory unlink", "memory restore",
			"approve ask",
		},
		// Named even where the allow list already refuses them, so the intent
		// reads in the table instead of only in its silence. memory forget
		// destroys with no recourse, where record and unlink are reversible.
		// skill approve is the human door a private skill goes through to
		// become a shared one.
		//
		// The approve family is spelled out rather than written "approve *",
		// because deny wins over allow and the family pattern would take
		// `approve ask` with it, which is the one approve verb an agent has to
		// reach. A later approve verb is still refused: it reaches no allow
		// entry, and an allow list refuses what it does not name.
		deny: []string{
			"approve list", "approve allow", "approve deny", "approve rule", "approve mode",
			"host *", "role *", "schedule *", "service *", "set *", "plugin *",
			"skill approve", "memory forget",
			"session close", "session archive", "session resume", "session interrupt",
			"session switch", "session set-budget",
		},
		plugins: true,
	},
}

// grantOrder is the order a refusal searches for the role that would run a
// verb, least powerful first, so the message names the smallest grant that
// works. RoleAgent is absent: it is structural, and naming it would tell an
// operator to grant something no verb can grant.
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

// permits reports whether role runs verb. The deny list wins, then allowAll,
// then the plugin exception, then the allow list.
func permits(role Role, verb string, contributed bool) bool {
	def, ok := definitions[role]
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

// refusal writes the message. An agent's is read by a model as a tool result,
// so it says what to do next instead of naming a role the model cannot obtain.
// A human's names the smallest role that would run the verb, so an operator
// knows what to grant and not only what was refused.
func refusal(r Request, role Role, verb string) string {
	if role == RoleAgent {
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

// match reports whether pattern names verb. A pattern is an exact verb
// ("approve ask") or a family ("session *"), which names the family verb and
// everything under it. Deliberately not a glob: a pattern nobody can write
// creatively is a pattern nobody has to read creatively.
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
// or not. The daemon calls it when it loads the table, so a name nobody defines
// is warned about once at boot rather than falling back silently at every
// dispatch.
func Known(name string) bool {
	_, ok := definitions[Role(name)]
	return ok
}

// Describe renders what a role runs, for `role show` and `role list`.
func Describe(role Role) string {
	def, ok := definitions[role]
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
