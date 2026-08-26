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

// firstGrantWarning is what an operator reads the one time it matters. Until
// the table holds something, the daemon decides nothing about humans and every
// gateway's own rules stand alone. The first grant ends that for everyone at
// once, and an operator who learns it from the silence of a colleague's refused
// command learns it too late.
const firstGrantWarning = "⚠️ this is the first role: from now on every gateway account without one is `observer`. " +
	"Name the others with `role grant` before they find out."

// addRoleCommands registers the role verbs. They are neutral argv, like host
// and approve, so a chat gateway binds them as they are. Only an owner runs
// them, which core/internal/authz decides and this file does not repeat.
func addRoleCommands(reg *cli.Registry, st *state.State) error {
	if err := reg.Add(contracts.New("role", "list").
		Help("who holds which role, and what each role runs").
		Do(func(_ context.Context, in contracts.Input) (string, error) {
			assignments := st.RoleAssignments()
			if in.JSON {
				b, err := json.Marshal(assignments)
				return string(b), err
			}
			var b strings.Builder
			if len(assignments) == 0 {
				b.WriteString("nobody holds a role, so herrscher decides nothing about humans " +
					"and each gateway's own rules stand alone.\n")
				b.WriteString(firstGrantWarning + "\n\n")
			} else {
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
			b.WriteString("roles you can grant: " + strings.Join(authz.GrantableRoles(), ", ") + "\n")
			b.WriteString("`agent` is not one of them: it is what a session holds, decided by the socket it arrives on.")
			return b.String(), nil
		})); err != nil {
		return err
	}

	if err := reg.Add(contracts.New("role", "grant").
		Help("give a principal a role; copy the principal from the refusal it read").
		ValueParam("principal", "who, as a refusal spells it, e.g. chat:1234; also takes a bare argument", false).
		ValueParam("role", "one of: "+strings.Join(authz.GrantableRoles(), ", ")+"; also takes a second bare argument", false).
		ValueParam("label", "a readable name for `role list`; it decides nothing", false).
		Do(func(_ context.Context, in contracts.Input) (string, error) {
			// Same shape as `approve mode`: the principal takes the first bare
			// argument when no flag gave it, and the role the last one still
			// unclaimed. Counting from the end is what makes the mixed form
			// work, and matching that verb is what makes `role grant chat:1234
			// owner` read the way the rest of the CLI does.
			rest := in.Rest
			principal := strings.TrimSpace(in.Get("principal"))
			if principal == "" && len(rest) > 0 {
				principal, rest = strings.TrimSpace(rest[0]), rest[1:]
			}
			role := strings.TrimSpace(in.Get("role"))
			if role == "" && len(rest) > 0 {
				role = strings.TrimSpace(rest[len(rest)-1])
			}
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
		})); err != nil {
		return err
	}

	if err := reg.Add(contracts.New("role", "revoke").
		Help("take a principal's role back").
		ValueParam("principal", "who, as `role list` spells it; also takes a bare argument", false).
		Do(func(_ context.Context, in contracts.Input) (string, error) {
			principal := strings.TrimSpace(firstOf(in.Get("principal"), in.Rest))
			if principal == "" {
				return "", fmt.Errorf("name a principal (see `role list`)")
			}
			removed, err := st.RemoveRole(principal)
			if err != nil {
				return "", err
			}
			if !removed {
				return "", fmt.Errorf("%s holds no role", principal)
			}
			// Worth saying, because it is not obvious: revoking the last role
			// puts the daemon back to deciding nothing about humans.
			if len(st.RoleAssignments()) == 0 {
				return principal + " holds no role, and neither does anyone else: herrscher is back to deciding nothing about humans", nil
			}
			return principal + " holds no role, so it is an `observer`", nil
		})); err != nil {
		return err
	}

	return reg.Add(contracts.New("role", "show").
		Help("what one principal may run").
		ValueParam("principal", "who, as `role list` spells it; also takes a bare argument", false).
		Do(func(_ context.Context, in contracts.Input) (string, error) {
			principal := strings.TrimSpace(firstOf(in.Get("principal"), in.Rest))
			if principal == "" {
				return "", fmt.Errorf("name a principal (see `role list`)")
			}
			role := authz.RoleOf(authz.Principal{ID: principal}, st.RoleNames())
			return fmt.Sprintf("%s holds `%s`\n%s", principal, role, authz.Describe(role)), nil
		}))
}

// grantable refuses what the table cannot change. The two structural
// principals are answered by the entry point a connection arrives on, and the
// agent role is what a session holds by being a session: writing either here
// would record a claim nothing reads.
func grantable(principal, role string) error {
	if principal == "" {
		return fmt.Errorf("name a principal: a refusal spells it, e.g. chat:1234")
	}
	if principal == authz.LocalPrincipal {
		return fmt.Errorf("`%s` is the daemon's own socket, and opening it already needs the daemon's uid: it is always `owner`", authz.LocalPrincipal)
	}
	if strings.HasPrefix(principal, authz.SessionPrefix) {
		return fmt.Errorf("`%s` is a session, and a session is always `agent`: the socket it arrives on decides that, not this table", principal)
	}
	if !authz.Grantable(role) {
		return fmt.Errorf("unknown role %q: grant one of %s", role, strings.Join(authz.GrantableRoles(), ", "))
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
