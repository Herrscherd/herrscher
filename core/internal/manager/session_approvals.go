package manager

import (
	"context"
	"fmt"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/approval"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func validateApprovalsMode(mode string) error {
	if mode == "" {
		return nil
	}
	if _, ok := approval.ParseMode(mode); ok {
		return nil
	}
	return &UnknownApprovalModeError{Mode: mode}
}

func (h *Handler) approvalsNote(ctx context.Context, req createRequest, space workspace) string {
	mode := req.mode
	if mode == "" || mode == approval.ModeBypass.String() {
		return ""
	}
	vendor, name, agentName := req.runtime.vendor, req.name, req.runtime.agent
	gate := h.gateFor(vendor)
	switch {
	case !gate.enforced:
		return "\n\n" + h.text(clitext.SessionApprovalsUnenforced, clitext.Params{clitext.ParamVendor: fmt.Sprintf("%#q", vendorLabel(vendor)), clitext.ParamWhy: h.gateReason(gate), clitext.ParamName: name})
	case req.shared || space.path == "":
		return "\n\n" + h.text(clitext.SessionApprovalsShared, clitext.Params{clitext.ParamName: name})
	case req.hostName != "" && agentName == "":
		return "\n\n" + h.text(clitext.SessionApprovalsRemoteNoAgent, clitext.Params{clitext.ParamName: name})
	case agentName == "":
		if err := agent.MaterializeHook(ctx, space.path, agent.SelfBin()); err != nil {
			h.log.Warn("materialize hook failed", "session", name, "err", err)
			return "\n\n" + h.text(clitext.SessionApprovalsHookFailed, clitext.Params{clitext.ParamErr: err.Error(), clitext.ParamName: name})
		}
	}
	return ""
}

func (h *Handler) switchApprovalsNote(sess state.Session) string {
	mode := sess.Approvals
	if mode == "" || mode == approval.ModeBypass.String() {
		return ""
	}
	gate := h.gateFor(sess.Vendor)
	if gate.enforced {
		return ""
	}
	return "\n\n" + h.text(clitext.SessionApprovalsUnenforcedNow, clitext.Params{clitext.ParamVendor: fmt.Sprintf("%#q", vendorLabel(sess.Vendor)), clitext.ParamWhy: h.gateReason(gate), clitext.ParamName: sess.Name})
}
