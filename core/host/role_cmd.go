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

func addRoleCommands(reg *cli.Registry, st *state.State) error {
	return addVerbs(reg,
		roleListVerb(st),
		roleGrantVerb(st),
		roleRevokeVerb(st),
		roleShowVerb(st),
	)
}

func roleListVerb(st *state.State) contracts.Cmd {
	return contracts.New("role", "list").
		Help("who holds which role, and what each role runs").
		Do(func(_ context.Context, in contracts.Input) (string, error) {
			return listRoles(st, in.JSON)
		})
}

func listRoles(st *state.State, asJSON bool) (string, error) {
	assignments := st.RoleAssignments()
	if asJSON {
		b, err := json.Marshal(assignments)
		return string(b), err
	}
	var b strings.Builder
	writeRoleAssignments(&b, assignments)
	b.WriteString("roles you can grant: " + strings.Join(authz.GrantableRoles(), ", ") + "\n")
	b.WriteString("`agent` is not one of them: it is what a session holds, decided by the socket it arrives on.")
	return b.String(), nil
}

func writeRoleAssignments(b *strings.Builder, assignments map[string]state.RoleAssignment) {
	if len(assignments) == 0 {
		b.WriteString("nobody holds a role, so herrscher decides nothing about humans " +
			"and each gateway's own rules stand alone.\n")
		b.WriteString(firstGrantWarning + "\n\n")
		return
	}
	for _, principal := range sortedPrincipals(assignments) {
		a := assignments[principal]
		line := "- " + principal + "  " + a.Role
		if a.Label != "" {
			line += "  (" + a.Label + ")"
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\n")
}

func roleGrantVerb(st *state.State) contracts.Cmd {
	return contracts.New("role", "grant").
		Help("give a principal a role; copy the principal from the refusal it read").
		ValueParam("principal", "who, as a refusal spells it, e.g. chat:1234; also takes a bare argument", false).
		ValueParam("role", "one of: "+strings.Join(authz.GrantableRoles(), ", ")+"; also takes a second bare argument", false).
		ValueParam("label", "a readable name for `role list`; it decides nothing", false).
		Do(func(_ context.Context, in contracts.Input) (string, error) {
			return grantRole(st, in)
		})
}

func grantRole(st *state.State, in contracts.Input) (string, error) {
	principal, role := grantOperands(in)
	if err := grantable(principal, role); err != nil {
		return "", err
	}
	first, err := st.SetRole(principal, role, strings.TrimSpace(in.Get("label")))
	if err != nil {
		return "", err
	}
	out := fmt.Sprintf("%s is now `%s`", principal, role)
	if first {
		out += "\n" + firstGrantWarning
	}
	return out, nil
}

func grantOperands(in contracts.Input) (string, string) {
	rest := in.Rest
	principal := strings.TrimSpace(in.Get("principal"))
	if principal == "" && len(rest) > 0 {
		principal, rest = strings.TrimSpace(rest[0]), rest[1:]
	}
	role := strings.TrimSpace(in.Get("role"))
	if role == "" && len(rest) > 0 {
		role = strings.TrimSpace(rest[len(rest)-1])
	}
	return principal, role
}

func roleRevokeVerb(st *state.State) contracts.Cmd {
	return contracts.New("role", "revoke").
		Help("take a principal's role back").
		ValueParam("principal", "who, as `role list` spells it; also takes a bare argument", false).
		Do(func(_ context.Context, in contracts.Input) (string, error) {
			return revokeRole(st, in)
		})
}

func revokeRole(st *state.State, in contracts.Input) (string, error) {
	principal := strings.TrimSpace(firstOf(in.Get("principal"), in.Rest))
	if principal == "" {
		return "", errPrincipalMissing
	}
	removed, err := st.RemoveRole(principal)
	if err != nil {
		return "", err
	}
	if !removed {
		return "", &noRoleError{Principal: principal}
	}
	if len(st.RoleAssignments()) == 0 {
		return principal + " holds no role, and neither does anyone else: herrscher is back to deciding nothing about humans", nil
	}
	return principal + " holds no role, so it is an `observer`", nil
}

func roleShowVerb(st *state.State) contracts.Cmd {
	return contracts.New("role", "show").
		Help("what one principal may run").
		ValueParam("principal", "who, as `role list` spells it; also takes a bare argument", false).
		Do(func(_ context.Context, in contracts.Input) (string, error) {
			return showRole(st, in)
		})
}

func showRole(st *state.State, in contracts.Input) (string, error) {
	principal := strings.TrimSpace(firstOf(in.Get("principal"), in.Rest))
	if principal == "" {
		return "", errPrincipalMissing
	}
	role := authz.RoleOf(authz.Principal{ID: principal}, st.RoleNames())
	return fmt.Sprintf("%s holds `%s`\n%s", principal, role, authz.Describe(role)), nil
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

func sortedPrincipals(m map[string]state.RoleAssignment) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
