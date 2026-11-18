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

func addRoleCommands(reg *cli.Registry, cliMessages *clitext.Messages, lang languageSource, hostState *state.State) error {
	reg.SetHelpParams(clitext.CmdParamRoleGrantRole, clitext.Params{"Roles": strings.Join(authz.GrantableRoles(), ", ")})
	return addVerbs(
		reg,
		roleListVerb(cliMessages, lang, hostState),
		roleGrantVerb(cliMessages, lang, hostState),
		roleRevokeVerb(cliMessages, lang, hostState),
		roleShowVerb(cliMessages, lang, hostState),
	)
}

func roleListVerb(cliMessages *clitext.Messages, lang languageSource, store *state.State) contracts.Cmd {
	return contracts.New("role", "list").
		Help(clitext.CmdHelpRoleList).
		Do(func(_ context.Context, in contracts.Input) (contracts.Result, error) {
			return cli.Data(listRoles(cliMessages, lang(), store, in.JSON))
		})
}

func listRoles(cliMessages *clitext.Messages, lang i18n.Language, st *state.State, asJSON bool) (string, error) {
	assignments := st.RoleAssignments()
	if asJSON {
		body, err := json.Marshal(assignments)
		return string(body), err
	}
	var body strings.Builder
	writeRoleAssignments(cliMessages, lang, &body, assignments)
	_, _ = body.WriteString(replyText(cliMessages, lang).Text(clitext.HostRoleGrantable, clitext.Params{clitext.ParamRoles: strings.Join(authz.GrantableRoles(), ", ")}) + "\n")
	_, _ = body.WriteString(replyText(cliMessages, lang).Text(clitext.HostRoleAgentNote, nil))
	return body.String(), nil
}

func writeRoleAssignments(cliMessages *clitext.Messages, lang i18n.Language, body *strings.Builder, assignments map[string]state.RoleAssignment) {
	if len(assignments) == 0 {
		_, _ = body.WriteString(replyText(cliMessages, lang).Text(clitext.HostRoleNone, nil) + "\n")
		_, _ = body.WriteString(replyText(cliMessages, lang).Text(clitext.HostRoleFirstGrant, nil) + "\n\n")
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

func roleGrantVerb(cliMessages *clitext.Messages, lang languageSource, store *state.State) contracts.Cmd {
	return contracts.New("role", "grant").
		Help(clitext.CmdHelpRoleGrant).
		ValueParam("principal", clitext.CmdParamRoleGrantPrincipal, false).
		ValueParam("role", clitext.CmdParamRoleGrantRole, false).
		ValueParam("label", clitext.CmdParamRoleGrantLabel, false).
		Do(func(_ context.Context, in contracts.Input) (contracts.Result, error) {
			return cli.Data(grantRole(cliMessages, lang(), store, in))
		})
}

func grantRole(cliMessages *clitext.Messages, lang i18n.Language, store *state.State, input contracts.Input) (string, error) {
	ops := parseGrantOperands(input)
	principal, role := ops.principal, ops.role
	if err := grantable(principal, role); err != nil {
		return "", err
	}
	first, err := store.SetRole(principal, role, strings.TrimSpace(input.Get("label")))
	if err != nil {
		return "", err
	}
	out := replyText(cliMessages, lang).Text(clitext.HostRoleGranted, clitext.Params{clitext.ParamPrincipal: principal, clitext.ParamRole: role})
	if first {
		out += "\n" + replyText(cliMessages, lang).Text(clitext.HostRoleFirstGrant, nil)
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

func roleRevokeVerb(cliMessages *clitext.Messages, lang languageSource, store *state.State) contracts.Cmd {
	return contracts.New("role", "revoke").
		Help(clitext.CmdHelpRoleRevoke).
		ValueParam("principal", clitext.CmdParamRoleRevokePrincipal, false).
		Do(func(_ context.Context, in contracts.Input) (contracts.Result, error) {
			return cli.Data(revokeRole(cliMessages, lang(), store, in))
		})
}

func revokeRole(cliMessages *clitext.Messages, lang i18n.Language, store *state.State, in contracts.Input) (string, error) {
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
		return replyText(cliMessages, lang).Text(clitext.HostRoleRevokedLast, clitext.Params{clitext.ParamPrincipal: principal}), nil
	}
	return replyText(cliMessages, lang).Text(clitext.HostRoleRevoked, clitext.Params{clitext.ParamPrincipal: principal}), nil
}

func roleShowVerb(cliMessages *clitext.Messages, lang languageSource, store *state.State) contracts.Cmd {
	return contracts.New("role", "show").
		Help(clitext.CmdHelpRoleShow).
		ValueParam("principal", clitext.CmdParamRoleShowPrincipal, false).
		Do(func(_ context.Context, in contracts.Input) (contracts.Result, error) {
			return cli.Data(showRole(cliMessages, lang(), store, in))
		})
}

func showRole(cliMessages *clitext.Messages, lang i18n.Language, store *state.State, in contracts.Input) (string, error) {
	principal := strings.TrimSpace(firstOf(in.Get("principal"), in.Rest))
	if principal == "" {
		return "", errPrincipalMissing
	}
	role := authz.RoleOf(authz.Principal{ID: principal}, store.RoleNames())
	return replyText(cliMessages, lang).Text(clitext.HostRoleHolds, clitext.Params{clitext.ParamPrincipal: principal, clitext.ParamRole: string(role)}) + "\n" + authz.Describe(role), nil
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
