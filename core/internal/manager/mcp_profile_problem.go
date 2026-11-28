package manager

import (
	"errors"
	"strings"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/agent"
)

// MCPProfileProblem maps an error of the MCP profiles to the operator message that names it, with the parameters it renders with. The boolean is false for any other error.
func MCPProfileProblem(err error, agentName string) (clitext.Key, clitext.Params, bool) {
	if profileErr, ok := errors.AsType[*agent.UnknownProfileServerError](err); ok {
		return clitext.HostErrMCPProfileServer, clitext.Params{clitext.ParamAgent: agentName, clitext.ParamRole: profileErr.Role, clitext.ParamServer: profileErr.Server}, true
	}
	if roleErr, ok := errors.AsType[*agent.UnassignableRoleError](err); ok {
		accepted := strings.Join(agent.AssignableRoles(), ", ")
		if roleErr.InProfile {
			return clitext.HostErrMCPProfileRole, clitext.Params{clitext.ParamAgent: agentName, clitext.ParamRole: roleErr.Role, clitext.ParamAccepted: accepted}, true
		}
		return clitext.HostErrMCPRole, clitext.Params{clitext.ParamRole: roleErr.Role, clitext.ParamAccepted: accepted}, true
	}
	if nullErr, ok := errors.AsType[*agent.NullProfileEntryError](err); ok {
		return clitext.HostErrMCPProfileNull, clitext.Params{clitext.ParamAgent: agentName, clitext.ParamRole: nullErr.Role}, true
	}
	return "", nil, false
}

func (h *Handler) mcpProfileLine(lang i18n.Language, selection agent.MCPSelection) string {
	if !selection.Applied {
		return ""
	}
	return "\n" + h.textIn(lang, clitext.ToolWeightProfile, clitext.Params{
		clitext.ParamRole:    selection.Role,
		clitext.ParamKept:    h.namesOrNone(lang, selection.Kept),
		clitext.ParamDropped: h.namesOrNone(lang, selection.Dropped),
	})
}

func (h *Handler) namesOrNone(lang i18n.Language, names []string) string {
	if len(names) == 0 {
		return h.textIn(lang, clitext.ToolWeightProfileNone, nil)
	}
	return strings.Join(names, ", ")
}
