package host

import (
	"context"
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

func sessionArgs() map[string]string {
	return map[string]string{
		"session seed":       paramName,
		"session send":       paramName,
		"session scrollback": paramName,
		"session interrupt":  paramName,
		"session who":        paramName,
		"session log":        paramName,
		"session close":      paramName,
		"session archive":    paramName,
		"session rename":     paramName,
		"session resume":     paramName,
		"session switch":     paramName,
		"session set-budget": paramName,
		"session set-mode":   paramName,
		"session get-mode":   paramName,
		"approve ask":        paramSession,
		"approve mode":       paramSession,
		"schedule add":       paramSession,
	}
}

func notASessionArgs() map[string]bool {
	return map[string]bool{
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
}

func (h *hub) authorize(ctx context.Context, args []string) error {
	principal := principalOf(ctx)
	role := authz.RoleOf(principal, h.roleNames())
	if role == authz.RoleNone {
		return nil
	}
	path, input, ok := h.reg.Resolve(args)
	if !ok {
		return nil
	}
	allowed, why := authz.Decide(authz.Request{
		Principal:   principal,
		Path:        path,
		Contributed: h.isContributedCommand(args),
		Subject:     h.subjectOf(principal, path, input),
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
			warnStderr("herrscher serve: %s holds role %q, which herrscher does not define; treating it as `observer`\n", principal, name)
		}
	}
	return names
}

func (h *hub) subjectOf(principal authz.Principal, path []string, input contracts.Input) authz.Subject {
	if principal.Session == "" {
		return authz.Subject{}
	}
	arg, ok := sessionArgs()[strings.Join(path, " ")]
	if !ok {
		return authz.Subject{}
	}
	name := strings.TrimSpace(input.Get(arg))
	if name == "" && len(input.Rest) > 0 {
		name = strings.TrimSpace(input.Rest[0])
	}
	if name == "" {
		return authz.Subject{}
	}
	return authz.Subject{Session: name, InSubtree: h.inSubtree(principal.Session, name)}
}

func (h *hub) inSubtree(caller, target string) bool {
	if caller == target {
		return true
	}
	if h.st == nil {
		return false
	}
	parents := make(map[string]string)
	sessions := h.st.SnapshotSessions()
	for idx := range sessions {
		s := sessions[idx]
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
