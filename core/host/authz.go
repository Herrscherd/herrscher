package host

import (
	"context"
	"strconv"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/approval"
	"github.com/Herrscherd/herrscher/core/internal/authz"
	"github.com/Herrscherd/herrscher/core/internal/state"
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
	disp     resultDispatcher
	session  string
	lang     languageSource
	messages *clitext.Messages
}

func (asSession) cancelsOnHangUp() bool { return true }

func (a asSession) Dispatch(ctx context.Context, args []string) (string, error) {
	res, err := a.disp.Dispatch(withSessionPrincipal(ctx, a.session), args)
	return cli.Render(a.messages, res, err, a.lang())
}

type asOperator struct {
	disp     resultDispatcher
	lang     languageSource
	messages *clitext.Messages
}

func (a asOperator) Dispatch(ctx context.Context, args []string) (string, error) {
	res, err := a.disp.Dispatch(withLocalPrincipal(ctx), args)
	return cli.Render(a.messages, res, err, a.lang())
}

func sessionArgs() map[string]string {
	return map[string]string{
		"session seed":          paramName,
		"session send":          paramName,
		"session scrollback":    paramName,
		"session interrupt":     paramName,
		"session pick":          paramName,
		"session compact":       paramName,
		"session seen":          paramName,
		"session who":           paramName,
		"session log":           paramName,
		"session history":       paramName,
		"session watch":         paramName,
		"session close":         paramName,
		"session archive":       paramName,
		"session rename":        paramName,
		"session resume":        paramName,
		"session switch":        paramName,
		"session handoff":       paramName,
		"session set-budget":    paramName,
		"session set-compact":   paramName,
		"session set-account":   paramName,
		"session lang":          paramName,
		"session accounts":      paramName,
		"session set-mode":      paramName,
		"session get-mode":      paramName,
		"session shell":         paramName,
		"session checkpoints":   paramName,
		"session rewind":        paramName,
		"session undo":          paramName,
		"session fork":          paramName,
		"session pr open":       paramName,
		"session pr status":     paramName,
		"session pr checks":     paramName,
		"session pr comments":   paramName,
		"session pr reply":      paramName,
		"session issue list":    paramName,
		"session issue show":    paramName,
		"session issue open":    paramName,
		"session issue comment": paramName,
		"approve ask":           paramSession,
		"approve mode":          paramSession,
		"schedule add":          paramSession,
	}
}

func notASessionArgs() map[string]bool {
	return map[string]bool{
		"session create name":   true,
		"agent create name":     true,
		"agent show name":       true,
		"agent set-soul name":   true,
		"host add name":         true,
		"host check name":       true,
		"host provision name":   true,
		"host rm name":          true,
		"approve allow session": true,
		"approve rule session":  true,
		"schedule add name":     true,
		"schedule rm name":      true,
		"schedule run name":     true,
		"schedule pause name":   true,
		"schedule resume name":  true,
	}
}

type admission struct {
	session string
	verb    string
	subject string
	how     string
}

const (
	admittedAsked   = "asked"
	admittedSteward = "steward"
	admittedDirect  = "direct"
)

func (h *hub) authorize(ctx context.Context, args []string) (admission, error) {
	principal := principalOf(ctx)
	role := authz.RoleOf(principal, h.roleNames())
	if role == authz.RoleNone {
		return admission{}, nil
	}
	path, input, ok := h.reg.Resolve(args)
	if !ok {
		return admission{}, nil
	}
	req := authz.Request{
		Principal:   principal,
		Path:        path,
		Contributed: h.isContributedCommand(args),
		Subject:     h.subjectOf(principal, path, input),
	}
	if principal.Session != "" {
		if err := h.guardSessionCall(principal.Session, strings.Join(path, " "), input); err != nil {
			return admission{}, err
		}
	}
	allowed, why := authz.Decide(req, role)
	if allowed {
		return h.admitted(principal.Session, path, args, role, req.Contributed)
	}
	if authz.Escalable(req, role) && h.desk != nil {
		return h.escalate(ctx, principal.Session, path, args)
	}
	return admission{}, &accessDeniedError{Reason: why}
}

func (h *hub) admitted(session string, path, args []string, role authz.Role, contributed bool) (admission, error) {
	if session == "" {
		return admission{}, nil
	}
	if err := h.selfRestarts.admit(session, strings.Join(path, " ")); err != nil {
		return admission{}, err
	}
	if !authz.Writes(path, contributed) {
		return admission{}, nil
	}
	how := admittedDirect
	if role == authz.RoleSteward && !authz.AgentRuns(path, contributed) {
		how = admittedSteward
	}
	return admission{session: session, verb: strings.Join(path, " "), subject: verbSubject(args), how: how}, nil
}

func (h *hub) escalate(ctx context.Context, session string, path, args []string) (admission, error) {
	subject := verbSubject(args)
	decision, reason := h.desk.decideVerb(ctx, session, approval.Request{Tool: approval.VerbTool, Subject: subject})
	if decision != approval.Allow {
		h.announceSelfCall(session, "selftools.refused", subject, reason)
		return admission{}, &accessDeniedError{Reason: reason}
	}
	if err := h.selfRestarts.admit(session, strings.Join(path, " ")); err != nil {
		return admission{}, err
	}
	return admission{session: session, verb: strings.Join(path, " "), subject: subject, how: admittedAsked}, nil
}

func verbSubject(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, token := range args {
		if token == "" || strings.ContainsAny(token, " \t\"'\\") {
			token = strconv.Quote(token)
		}
		quoted = append(quoted, token)
	}
	return strings.Join(quoted, " ")
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
			warnLive(h.messages, h.warnings, h.textLanguage(), "warn.role_undefined", map[string]any{nameParam: principal, roleParam: name})
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
	return inSubtreeOf(h.st, caller, target)
}

func inSubtreeOf(store *state.State, caller, target string) bool {
	if caller == target {
		return true
	}
	if store == nil {
		return false
	}
	parents := make(map[string]string)
	sessions := store.SnapshotSessions()
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
