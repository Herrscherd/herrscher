package host

import (
	"context"
	"fmt"
	"os"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/authz"
)

type corePrincipalKey struct{}

func withSessionPrincipal(ctx context.Context, session string) context.Context {
	return context.WithValue(ctx, corePrincipalKey{}, authz.SessionPrincipal(session))
}

func withLocalPrincipal(ctx context.Context) context.Context {
	return context.WithValue(ctx, corePrincipalKey{}, authz.Principal{ID: authz.LocalPrincipal})
}

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

type asSession struct {
	disp    dispatcher
	session string
}

func (a asSession) Dispatch(ctx context.Context, args []string) (string, error) {
	return a.disp.Dispatch(withSessionPrincipal(ctx, a.session), args)
}

type asOperator struct{ disp dispatcher }

func (a asOperator) Dispatch(ctx context.Context, args []string) (string, error) {
	return a.disp.Dispatch(withLocalPrincipal(ctx), args)
}

var sessionArg = map[string]string{
	"session seed":       "name",
	"session send":       "name",
	"session scrollback": "name",
	"session interrupt":  "name",
	"session who":        "name",
	"session log":        "name",
	"session close":      "name",
	"session archive":    "name",
	"session rename":     "name",
	"session resume":     "name",
	"session switch":     "name",
	"session set-budget": "name",
	"approve ask":        "session",
	"approve mode":       "session",
	"schedule add":       "session",
}

var notASession = map[string]bool{
	"session create name":  true,
	"agent create name":    true,
	"agent show name":      true,
	"agent set-soul name":  true,
	"host add name":        true,
	"host check name":      true,
	"host provision name":  true,
	"host rm name":         true,
	"schedule add name":    true,
	"schedule rm name":     true,
	"schedule run name":    true,
	"schedule pause name":  true,
	"schedule resume name": true,
}

func (h *hub) authorize(ctx context.Context, args []string) error {
	p := principalOf(ctx)
	role := authz.RoleOf(p, h.roleNames())
	if role == authz.RoleNone {
		return nil
	}
	path, in, ok := h.reg.Resolve(args)
	if !ok {
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
	return &accessDeniedError{Reason: why}
}

func (h *hub) roleNames() map[string]string {
	if h.st == nil {
		return nil
	}
	names := h.st.RoleNames()
	for principal, name := range names {
		if authz.Known(name) {
			continue
		}
		if _, said := h.warnedRoles.LoadOrStore(principal+"\x00"+name, struct{}{}); !said {
			fmt.Fprintf(os.Stderr, "herrscher serve: %s holds role %q, which herrscher does not define; treating it as `observer`\n", principal, name)
		}
	}
	return names
}

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
		name = strings.TrimSpace(in.Rest[0])
	}
	if name == "" {
		return authz.Subject{}
	}
	return authz.Subject{Session: name, InSubtree: h.inSubtree(p.Session, name)}
}

func (h *hub) inSubtree(caller, target string) bool {
	if caller == target {
		return true
	}
	if h.st == nil {
		return false
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
