package host

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/authz"
)

// corePrincipalKey carries the principals the core mints for itself. It is
// separate from the one contracts exposes, and unexported, so a gateway cannot
// reach it: contracts.WithPrincipal is how a gateway names a human, and there
// must be no way for it to name the operator's own socket or a session. A
// gateway is compiled in and trusted, but the string it sets comes from its
// platform, and an identity that can be typed is an identity that can be typed
// wrong.
type corePrincipalKey struct{}

// withSessionPrincipal marks a context as arriving on one session's own command
// socket. Only the listener calls it, and the listener knows which session it
// opened, so the name never travels in the message.
func withSessionPrincipal(ctx context.Context, session string) context.Context {
	return context.WithValue(ctx, corePrincipalKey{}, authz.SessionPrincipal(session))
}

// withLocalPrincipal marks a context as arriving on the daemon's own operator
// socket. Opening it already needs the daemon's uid.
func withLocalPrincipal(ctx context.Context) context.Context {
	return context.WithValue(ctx, corePrincipalKey{}, authz.Principal{ID: authz.LocalPrincipal})
}

// principalOf reads who is calling. The core's own mark wins over anything a
// gateway set, and a gateway claiming one of the reserved names is renamed
// rather than believed: it lands on an unassigned principal, which is an
// observer once any role exists, instead of on the authority it asked for.
func principalOf(ctx context.Context) authz.Principal {
	if p, ok := ctx.Value(corePrincipalKey{}).(authz.Principal); ok {
		return p
	}
	raw := contracts.PrincipalFrom(ctx)
	if raw == "" {
		return authz.Principal{}
	}
	if raw == authz.LocalPrincipal || strings.HasPrefix(raw, authz.SessionPrefix) {
		return authz.Principal{ID: "unverified:" + raw}
	}
	return authz.Principal{ID: raw}
}

// asSession dispatches with the calling session's identity attached. It is what
// makes the identity structural: a session cannot choose which listener its
// connection lands on, and locally, under one uid, that is the only thing about
// the call an agent does not get to author.
type asSession struct {
	disp    dispatcher
	session string
}

func (a asSession) Dispatch(ctx context.Context, args []string) (string, error) {
	return a.disp.Dispatch(withSessionPrincipal(ctx, a.session), args)
}

// asOperator is the same for the daemon's own socket.
type asOperator struct{ disp dispatcher }

func (a asOperator) Dispatch(ctx context.Context, args []string) (string, error) {
	return a.disp.Dispatch(withLocalPrincipal(ctx), args)
}

// sessionArg names, per verb, the argument that designates an EXISTING session
// the caller acts on. A session principal may only name a session in its own
// subtree there.
//
// It is a table and not a rule about flag names, because the flag names lie:
// `host add --name` names a host and `schedule add --name` names a schedule,
// while `schedule add --session` is the one that names a session. A rule that
// scoped every --name would refuse the wrong verbs and let the right one
// through.
var sessionArg = map[string]string{
	"session seed":       "name",
	"session send":       "name",
	"session scrollback": "name",
	"session interrupt":  "name",
	"session who":        "name",
	"session log":        "name",
	"session close":      "name",
	"session archive":    "name",
	"session resume":     "name",
	"session switch":     "name",
	"session set-budget": "name",
	"approve ask":        "session",
	"approve mode":       "session",
	"schedule add":       "session",
}

// notASession names the verb-and-parameter pairs whose argument reads like a
// session and is not one. They are listed rather than left out, so the coverage
// test can tell a decision from an oversight: a verb added later that names a
// session and appears in neither table fails that test instead of quietly
// escaping the scope check.
var notASession = map[string]bool{
	// The session about to exist, not one the caller acts on. No scope check
	// could mean anything on a name nothing answers to yet.
	"session create name": true,
	// A durable companion agent, which is a catalog record and not a running
	// session. Sessions are started FROM one, so the two share a vocabulary and
	// nothing else.
	"agent create name":   true,
	"agent show name":     true,
	"agent set-soul name": true,
	// A host.
	"host add name":       true,
	"host check name":     true,
	"host provision name": true,
	"host rm name":        true,
	// A schedule. Its --session is in sessionArg above.
	"schedule add name":    true,
	"schedule rm name":     true,
	"schedule run name":    true,
	"schedule pause name":  true,
	"schedule resume name": true,
}

// authorize refuses args when the principal the context carries may not run
// them. It is the daemon's single door: the operator socket, every session
// socket, every gateway and every typed caller reach the registry through
// hub.Dispatch, and hub.Dispatch comes here first.
func (h *hub) authorize(ctx context.Context, args []string) error {
	p := principalOf(ctx)
	role := authz.RoleOf(p, h.roleNames())
	if role == authz.RoleNone {
		return nil
	}
	path, in, ok := h.reg.Resolve(args)
	if !ok {
		// An unregistered verb is Dispatch's error to name. Refusing here would
		// answer "you may not" to something that does not exist, which sends an
		// operator looking for a permission instead of a typo.
		return nil
	}
	allowed, why := authz.Decide(authz.Request{
		Principal:   p,
		Path:        path,
		Contributed: h.isContributedCommand(args),
		Subject:     h.subjectOf(p, path, in),
	}, role)
	if allowed {
		return nil
	}
	return errors.New(why)
}

// roleNames reads the assignment table, warning once about each principal whose
// role name it does not know. An unreadable or nonsensical table is not a
// decision: it falls back to the default behaviour and says so on the daemon's
// stderr, because a file that locks the owner out of their own daemon is worse
// than the hole it closes.
func (h *hub) roleNames() map[string]string {
	if h.st == nil {
		// A hub with no state has no table to read. It only happens in tests
		// that exercise the dispatch plumbing alone, and it is not a hole: a
		// session principal is an agent whatever the table says, since the role
		// comes from the listener and not from a file.
		return nil
	}
	names := h.st.RoleNames()
	for principal, name := range names {
		if authz.Known(name) {
			continue
		}
		// Once per principal, not once per dispatch. This runs on every command
		// the daemon answers, and a line repeated at the rate of the traffic is
		// a line an operator learns to scroll past.
		if _, said := h.warnedRoles.LoadOrStore(principal+"\x00"+name, struct{}{}); !said {
			fmt.Fprintf(os.Stderr, "herrscher serve: %s holds role %q, which herrscher does not define; treating it as `observer`\n", principal, name)
		}
	}
	return names
}

// subjectOf resolves which session a verb names, and whether the caller may
// reach it. Scope binds sessions alone: a human operating from a gateway is not
// confined to a subtree.
func (h *hub) subjectOf(p authz.Principal, path []string, in contracts.Input) authz.Subject {
	if p.Session == "" {
		return authz.Subject{}
	}
	arg, ok := sessionArg[strings.Join(path, " ")]
	if !ok {
		return authz.Subject{}
	}
	name := strings.TrimSpace(in.Get(arg))
	if name == "" && len(in.Rest) > 0 {
		// `approve mode revue bypass` is the one verb here whose session may
		// arrive as a bare argument. The others declare it required, so a
		// missing flag is a parse error and never a call that ran unscoped.
		name = strings.TrimSpace(in.Rest[0])
	}
	if name == "" {
		return authz.Subject{}
	}
	return authz.Subject{Session: name, InSubtree: h.inSubtree(p.Session, name)}
}

// inSubtree reports whether target is caller, or a session whose parent chain
// reaches caller. The walk is bounded by the session count: a Parent cycle
// written by hand into state.json must answer, not spin the daemon.
func (h *hub) inSubtree(caller, target string) bool {
	if caller == target {
		return true
	}
	if h.st == nil {
		return false // no state, no delegation to read: reach nothing
	}
	parents := map[string]string{}
	for _, s := range h.st.SnapshotSessions() {
		parents[s.Name] = s.Parent
	}
	cur := target
	for range parents {
		parent, ok := parents[cur]
		if !ok || parent == "" {
			return false
		}
		if parent == caller {
			return true
		}
		cur = parent
	}
	return false
}
