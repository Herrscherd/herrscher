package host

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/authz"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func addRoleCommands(reg *cli.Registry, lang i18n.Language, hostState *state.State) error {
	return addVerbs(
		reg,
		roleListVerb(lang, hostState),
		roleGrantVerb(lang, hostState),
		roleRevokeVerb(lang, hostState),
		roleShowVerb(lang, hostState),
	)
}

func roleListVerb(lang i18n.Language, store *state.State) contracts.Cmd {
	return contracts.New("role", "list").
		Help("who holds which role, and what each role runs").
		Do(func(_ context.Context, in contracts.Input) (contracts.Result, error) {
			return cli.Data(listRoles(lang, store, in.JSON))
		})
}

func listRoles(lang i18n.Language, st *state.State, asJSON bool) (string, error) {
	assignments := st.RoleAssignments()
	if asJSON {
		body, err := json.Marshal(assignments)
		return string(body), err
	}
	var body strings.Builder
	writeRoleAssignments(lang, &body, assignments)
	_, _ = body.WriteString(replyText(lang).Text(clitext.HostRoleGrantable, clitext.Params{clitext.ParamRoles: strings.Join(authz.GrantableRoles(), ", ")}) + "\n")
	_, _ = body.WriteString(replyText(lang).Text(clitext.HostRoleAgentNote, nil))
	return body.String(), nil
}

func writeRoleAssignments(lang i18n.Language, body *strings.Builder, assignments map[string]state.RoleAssignment) {
	if len(assignments) == 0 {
		_, _ = body.WriteString(replyText(lang).Text(clitext.HostRoleNone, nil) + "\n")
		_, _ = body.WriteString(replyText(lang).Text(clitext.HostRoleFirstGrant, nil) + "\n\n")
		return
	}
	for _, principal := range sortedPrincipals(assignments) {
		agent := assignments[principal]
		line := "- " + principal + "  " + agent.Role
		if agent.Label != "" {
			line += "  (" + agent.Label + ")"
		}
		_, _ = body.WriteString(line + "\n")
	}
	_, _ = body.WriteString("\n")
}

func roleGrantVerb(lang i18n.Language, store *state.State) contracts.Cmd {
	return contracts.New("role", "grant").
		Help("give a principal a role; copy the principal from the refusal it read").
		ValueParam("principal", "who, as a refusal spells it, e.g. chat:1234; also takes a bare argument", false).
		ValueParam("role", "one of: "+strings.Join(authz.GrantableRoles(), ", ")+"; also takes a second bare argument", false).
		ValueParam("label", "a readable name for `role list`; it decides nothing", false).
		Do(func(_ context.Context, in contracts.Input) (contracts.Result, error) {
			return cli.Data(grantRole(lang, store, in))
		})
}

func grantRole(lang i18n.Language, store *state.State, input contracts.Input) (string, error) {
	ops := parseGrantOperands(input)
	principal, role := ops.principal, ops.role
	if err := grantable(principal, role); err != nil {
		return "", err
	}
	first, err := store.SetRole(principal, role, strings.TrimSpace(input.Get("label")))
	if err != nil {
		return "", err
	}
	out := replyText(lang).Text(clitext.HostRoleGranted, clitext.Params{clitext.ParamPrincipal: principal, clitext.ParamRole: role})
	if first {
		out += "\n" + replyText(lang).Text(clitext.HostRoleFirstGrant, nil)
	}
	return out, nil
}

type grantOperands struct {
	principal string
	role      string
}

func parseGrantOperands(input contracts.Input) grantOperands {
	rest := input.Rest
	principal := strings.TrimSpace(input.Get("principal"))
	if principal == "" && len(rest) > 0 {
		principal, rest = strings.TrimSpace(rest[0]), rest[1:]
	}
	role := strings.TrimSpace(input.Get("role"))
	if role == "" && len(rest) > 0 {
		role = strings.TrimSpace(rest[len(rest)-1])
	}
	return grantOperands{principal: principal, role: role}
}

func roleRevokeVerb(lang i18n.Language, store *state.State) contracts.Cmd {
	return contracts.New("role", "revoke").
		Help("take a principal's role back").
		ValueParam("principal", "who, as `role list` spells it; also takes a bare argument", false).
		Do(func(_ context.Context, in contracts.Input) (contracts.Result, error) {
			return cli.Data(revokeRole(lang, store, in))
		})
}

func revokeRole(lang i18n.Language, store *state.State, in contracts.Input) (string, error) {
	principal := strings.TrimSpace(firstOf(in.Get("principal"), in.Rest))
	if principal == "" {
		return "", errPrincipalMissing
	}
	removed, err := store.RemoveRole(principal)
	if err != nil {
		return "", err
	}
	if !removed {
		return "", &noRoleError{Principal: principal}
	}
	if len(store.RoleAssignments()) == 0 {
		return replyText(lang).Text(clitext.HostRoleRevokedLast, clitext.Params{clitext.ParamPrincipal: principal}), nil
	}
	return replyText(lang).Text(clitext.HostRoleRevoked, clitext.Params{clitext.ParamPrincipal: principal}), nil
}

func roleShowVerb(lang i18n.Language, store *state.State) contracts.Cmd {
	return contracts.New("role", "show").
		Help("what one principal may run").
		ValueParam("principal", "who, as `role list` spells it; also takes a bare argument", false).
		Do(func(_ context.Context, in contracts.Input) (contracts.Result, error) {
			return cli.Data(showRole(lang, store, in))
		})
}

func showRole(lang i18n.Language, store *state.State, in contracts.Input) (string, error) {
	principal := strings.TrimSpace(firstOf(in.Get("principal"), in.Rest))
	if principal == "" {
		return "", errPrincipalMissing
	}
	role := authz.RoleOf(authz.Principal{ID: principal}, store.RoleNames())
	return replyText(lang).Text(clitext.HostRoleHolds, clitext.Params{clitext.ParamPrincipal: principal, clitext.ParamRole: string(role)}) + "\n" + authz.Describe(role), nil
}

func grantable(principal, role string) error {
	if principal == "" {
		return errPrincipalExample
	}
	if principal == authz.LocalPrincipal {
		return errLocalPrincipalRole
	}
	if strings.HasPrefix(principal, authz.SessionPrefix) {
		return &sessionRoleError{Principal: principal}
	}
	if role == "" {
		return &roleRequiredError{Grantable: authz.GrantableRoles()}
	}
	if !authz.Grantable(role) {
		return &unknownRoleError{Role: role, Grantable: authz.GrantableRoles()}
	}
	return nil
}

func sortedPrincipals(assignments map[string]state.RoleAssignment) []string {
	out := make([]string, 0, len(assignments))
	for k := range assignments {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
