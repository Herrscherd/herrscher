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
	"slices"
	"sort"
	"strings"
)

// Role names a set of verbs. The roles are defined here and not configurable:
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
	// RoleAgent is what a connection on a session's command socket holds.
	RoleAgent Role = "agent"
	// RoleSteward is a session the operator trusts to configure the daemon: it runs everything an operator does except role and answering approvals.
	RoleSteward Role = "steward"
)

const (
	roleFamily    = "role *"
	sessionCreate = "session create"
)

// LocalPrincipal is the caller on the daemon's operator socket, which any process of the daemon's uid reaches, an approved agent shell included.
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
		return definition{allowAll: true, deny: []string{roleFamily}, plugins: true}, true
	case RoleObserver:
		return definition{allow: []string{
			"commands", "whoami",
			"session list", "session info", "session log", "session history", "session watch", "session who", "session scrollback", "session checkpoints",
			"agent list", "agent show",
			"models list",
			"approve list",
			"host list",
			"schedule list",
			"memory search", "memory locate",
			"session issue list", "session issue show",
			"health", "usage", "skill list", "config get", "env get", "plugin list", "connect status", "setup check",
			"session pr status", "session pr checks", "session pr comments", "session get-mode",
			"drive list", "drive ls", "drive read", "drive log",
		}}, true
	case RoleAgent:
		return definition{
			allow: []string{
				"commands", "whoami",
				"session send", "session seed", "session handoff",
				"session list", "session info", "session log", "session history", "session watch", "session who", "session scrollback", "session checkpoints",
				"session pr open", "session pr status", "session pr checks", "session pr comments", "session pr reply",
				"session issue open", "session issue comment",
				"agent list", "agent show",
				"models list",
				"memory search", "memory locate", "memory record", "memory unlink", "memory restore",
				"approve ask", "approve list",
				"health", "usage", "skill list", "schedule list", "host list", "plugin list", "connect status", "setup check", "env get", "env request",
				"config get", "session issue list", "session issue show",
				"drive list", "drive ls", "drive read", "drive log",
			},
			deny: []string{
				"approve allow", "approve deny", "approve rule", "approve mode",
				"host add", "host check", "host provision", "host rm", roleFamily,
				"schedule add", "schedule rm", "schedule run", "schedule pause", "schedule resume",
				"service *", "set *", "plugin add", "plugin rm",
				"skill approve", "memory forget",
				sessionCreate,
				"session close", "session archive", "session resume", "session interrupt",
				"session switch", "session set-budget",
			},
			plugins: true,
		}, true
	case RoleSteward:
		return definition{allowAll: true, deny: append(neverForSessions(), stewardAsks()...), plugins: true}, true
	case RoleNone:
	}
	return definition{}, false
}

func neverForSessions() []string {
	return []string{roleFamily, "approve allow", "approve deny", "drive grant"}
}

func stewardAsks() []string {
	return []string{"approve rule", "approve mode", sessionCreate, "config set", "config unset", "env set", "env unset", "drive write", "drive remove"}
}

func isNeverForSessions(verb string) bool {
	for _, pattern := range neverForSessions() {
		if match(pattern, verb) {
			return true
		}
	}
	return false
}

// Escalable reports whether a request the role refuses may be put to the operator instead:
// a session asking for a verb its role does not run, inside its own subtree, and never a verb
// that grants rights or answers an approval.
func Escalable(req Request, role Role) bool {
	if (role != RoleAgent && role != RoleSteward) || req.Principal.Session == "" {
		return false
	}
	if req.Subject.Session != "" && !req.Subject.InSubtree {
		return false
	}
	verb := strings.Join(req.Path, " ")
	return !permits(role, verb, req.Contributed) && !isNeverForSessions(verb)
}

// Decide answers whether the request may run, and why not when it may not.
// RoleNone always allows. Subject is consulted for a session principal alone,
// since a human operating from a gateway is not confined to a subtree.
func Decide(req Request, role Role) (bool, string) {
	if role == RoleNone {
		return true, ""
	}
	verb := strings.Join(req.Path, " ")
	if !permits(role, verb, req.Contributed) {
		return false, refusal(req, role, verb)
	}
	if req.Principal.Session != "" && req.Subject.Session != "" && !req.Subject.InSubtree {
		return false, fmt.Sprintf(
			"herrscher: %#q names session %q, and you are session %q. A session reaches itself and the sessions it delegated, nothing else.",
			verb, req.Subject.Session, req.Principal.Session)
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

func agentHint(verb string) (string, bool) {
	if verb == sessionCreate {
		return "To delegate, end your turn with `⟢ delegate: <agent>` and the task: " +
			"the worker that opens runs a registered agent, so it answers to a policy the way you do.", true
	}
	return "", false
}

func refusal(req Request, role Role, verb string) string {
	if role == RoleAgent || role == RoleSteward {
		return sessionRefusal(verb)
	}
	for _, candidate := range []Role{RoleObserver, RoleOperator, RoleOwner} {
		if permits(candidate, verb, req.Contributed) {
			return fmt.Sprintf("herrscher: %#q needs the %#q role, and %s has %#q",
				verb, candidate, req.Principal.ID, role)
		}
	}
	return fmt.Sprintf("herrscher: %#q is not something any role may run", verb)
}

func sessionRefusal(verb string) string {
	if isNeverForSessions(verb) {
		return fmt.Sprintf("herrscher: %#q is never run for a session, even with the operator's approval: it grants rights or answers an approval.", verb)
	}
	if hint, ok := agentHint(verb); ok {
		return fmt.Sprintf("herrscher: %#q is not something a session may run. %s", verb, hint)
	}
	return fmt.Sprintf("herrscher: %#q is not something a session may run alone. "+
		"It is put to the operator when the daemon can ask, the operator can grant this session the `steward` role, "+
		"or run it for you.", verb)
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
// Three rules carry the defaults. local is always owner. A session is agent,
// or steward when the table grants it that. A gateway principal takes its
// assignment, or observer when the table holds a human grant, or no role when
// it holds none. That last one is what makes an upgrade a no-op for humans, and
// the first human grant a cliff: the verb that writes it says so.
func RoleOf(principal Principal, assignments map[string]string) Role {
	if principal.ID == LocalPrincipal {
		return RoleOwner
	}
	if principal.Session != "" {
		if Role(assignments[principal.ID]) == RoleSteward {
			return RoleSteward
		}
		return RoleAgent
	}
	if !HoldsHumanAssignment(assignments) {
		return RoleNone
	}
	if name, ok := assignments[principal.ID]; ok && Grantable(name) {
		return Role(name)
	}
	return RoleObserver
}

// HoldsHumanAssignment reports whether the table grants a role to anyone but a session, which is what turns every other human into an observer.
func HoldsHumanAssignment(assignments map[string]string) bool {
	for principal := range assignments {
		if !strings.HasPrefix(principal, SessionPrefix) {
			return true
		}
	}
	return false
}

// SessionGrantableRoles lists the roles a session principal can be granted, for a message that has to name the choice.
func SessionGrantableRoles() []string {
	return []string{string(RoleSteward)}
}

// GrantableToSession reports whether name is a role an operator may assign to a session principal: steward alone.
func GrantableToSession(name string) bool {
	return Role(name) == RoleSteward
}

// Grantable reports whether name is a role an operator may assign. agent is
// not: it is what the entry point already answers, and writing it into the
// table by hand would be claiming a human is a session.
func Grantable(name string) bool {
	switch Role(name) {
	case RoleOwner, RoleOperator, RoleObserver:
		return true
	case RoleNone, RoleAgent, RoleSteward:
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
	var buf strings.Builder
	if def.allowAll {
		buf.WriteString("runs everything")
	} else {
		buf.WriteString("runs " + strings.Join(def.allow, ", "))
	}
	if def.plugins && !def.allowAll {
		buf.WriteString(", and the verbs plugins contribute")
	}
	if len(def.deny) > 0 {
		buf.WriteString("\nnever runs " + strings.Join(def.deny, ", "))
	}
	return buf.String()
}

func readWords() []string {
	return []string{"list", "show", "get", "read", "status", "info", "checks", "comments", "log", "history", "search", "locate", "who", "scrollback", "watch", "checkpoints"}
}

// Writes reports whether a verb changes something, which is what the daemon announces when a session runs it.
// A verb that declared itself read only is a read. Among the daemon's own verbs, so is what an observer runs and a verb whose
// last word names a read; a plugin's verb is a read only when it declared it. approve ask and env request say so on their own.
func Writes(path []string, contributed, declaredRead bool) bool {
	verb := strings.Join(path, " ")
	if declaredRead || verb == "approve ask" || verb == "env request" || len(path) == 0 {
		return false
	}
	if contributed {
		return true
	}
	last, _, _ := strings.Cut(path[len(path)-1], "-")
	if slices.Contains(readWords(), last) {
		return false
	}
	return !permits(RoleObserver, verb, contributed)
}

// AgentRuns reports whether a plain session runs the verb without asking, which is what tells a read from a configuration change.
func AgentRuns(path []string, contributed bool) bool {
	return permits(RoleAgent, strings.Join(path, " "), contributed)
}
