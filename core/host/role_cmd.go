package host

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/authz"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const firstGrantWarning = "⚠️ this is the first role: from now on every gateway account without one is `observer`. " +
	"Name the others with `role grant` before they find out."

func addRoleCommands(reg *cli.Registry, hostState *state.State) error {
	return addVerbs(reg,
		roleListVerb(hostState),
		roleGrantVerb(hostState),
		roleRevokeVerb(hostState),
		roleShowVerb(hostState),
	)
}

func roleListVerb(store *state.State) contracts.Cmd {
	return contracts.New("role", "list").
		Help("who holds which role, and what each role runs").
		Do(func(_ context.Context, in contracts.Input) (contracts.Result, error) {
			return cli.Data(listRoles(store, in.JSON))
		})
}

func listRoles(st *state.State, asJSON bool) (string, error) {
	assignments := st.RoleAssignments()
	if asJSON {
		body, err := json.Marshal(assignments)
		return string(body), err
	}
	var body strings.Builder
	writeRoleAssignments(&body, assignments)
	_, _ = body.WriteString("roles you can grant: " + strings.Join(authz.GrantableRoles(), ", ") + "\n")
	_, _ = body.WriteString("`agent` is not one of them: it is what a session holds, decided by the socket it arrives on.")
	return body.String(), nil
}

func writeRoleAssignments(body *strings.Builder, assignments map[string]state.RoleAssignment) {
	if len(assignments) == 0 {
		_, _ = body.WriteString("nobody holds a role, so herrscher decides nothing about humans " +
			"and each gateway's own rules stand alone.\n")
		_, _ = body.WriteString(firstGrantWarning + "\n\n")
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

func roleGrantVerb(store *state.State) contracts.Cmd {
	return contracts.New("role", "grant").
		Help("give a principal a role; copy the principal from the refusal it read").
		ValueParam("principal", "who, as a refusal spells it, e.g. chat:1234; also takes a bare argument", false).
		ValueParam("role", "one of: "+strings.Join(authz.GrantableRoles(), ", ")+"; also takes a second bare argument", false).
		ValueParam("label", "a readable name for `role list`; it decides nothing", false).
		Do(func(_ context.Context, in contracts.Input) (contracts.Result, error) {
			return cli.Data(grantRole(store, in))
		})
}

func grantRole(store *state.State, input contracts.Input) (string, error) {
	ops := parseGrantOperands(input)
	principal, role := ops.principal, ops.role
	if err := grantable(principal, role); err != nil {
		return "", err
	}
	first, err := store.SetRole(principal, role, strings.TrimSpace(input.Get("label")))
	if err != nil {
		return "", err
	}
	out := fmt.Sprintf("%s is now %#q", principal, role)
	if first {
		out += "\n" + firstGrantWarning
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

func roleRevokeVerb(store *state.State) contracts.Cmd {
	return contracts.New("role", "revoke").
		Help("take a principal's role back").
		ValueParam("principal", "who, as `role list` spells it; also takes a bare argument", false).
		Do(func(_ context.Context, in contracts.Input) (contracts.Result, error) {
			return cli.Data(revokeRole(store, in))
		})
}

func revokeRole(store *state.State, in contracts.Input) (string, error) {
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
		return principal + " holds no role, and neither does anyone else: herrscher is back to deciding nothing about humans", nil
	}
	return principal + " holds no role, so it is an `observer`", nil
}

func roleShowVerb(store *state.State) contracts.Cmd {
	return contracts.New("role", "show").
		Help("what one principal may run").
		ValueParam("principal", "who, as `role list` spells it; also takes a bare argument", false).
		Do(func(_ context.Context, in contracts.Input) (contracts.Result, error) {
			return cli.Data(showRole(store, in))
		})
}

func showRole(store *state.State, in contracts.Input) (string, error) {
	principal := strings.TrimSpace(firstOf(in.Get("principal"), in.Rest))
	if principal == "" {
		return "", errPrincipalMissing
	}
	role := authz.RoleOf(authz.Principal{ID: principal}, store.RoleNames())
	return fmt.Sprintf("%s holds %#q\n%s", principal, role, authz.Describe(role)), nil
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
