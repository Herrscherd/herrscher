package manager

import (
	"context"
	"fmt"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/approval"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

type planUnsupportedError struct {
	Session string
	Vendor  string
	Why     string
}

func (e *planUnsupportedError) Error() string {
	return fmt.Sprintf("session %s cannot run in plan mode: %s cannot enforce it (%s), so its writes would run; choose another mode", e.Session, vendorLabel(e.Vendor), e.Why)
}

func (h *Handler) sessionSetModeRun(_ context.Context, input contracts.Input) (contracts.Result, error) {
	name, ok := input.Lookup(nameParam)
	if !ok {
		return contracts.Result{}, ErrMissingName
	}
	rawMode, _ := input.Lookup("mode")
	mode := strings.TrimSpace(rawMode)
	if mode == "" {
		return contracts.Result{}, &UnknownApprovalModeError{Mode: mode}
	}
	if err := validateApprovalsMode(mode); err != nil {
		return contracts.Result{}, err
	}
	sess, ok := h.st.FindSession(name)
	if !ok {
		return contracts.Result{}, &SessionNotFoundError{Name: name}
	}
	gate := h.gateFor(sess.Vendor)
	if parsed, _ := approval.ParseMode(mode); parsed == approval.ModePlan && !gate.enforced {
		return contracts.Result{}, &planUnsupportedError{Session: name, Vendor: sess.Vendor, Why: gate.why}
	}
	if err := h.st.SetSessionApprovals(name, mode); err != nil {
		return contracts.Result{}, err
	}
	return contracts.DataResult(h.setModeReply(sess, mode, gate)), nil
}

func (h *Handler) sessionGetModeRun(_ context.Context, input contracts.Input) (contracts.Result, error) {
	name, ok := input.Lookup(nameParam)
	if !ok {
		return contracts.Result{}, ErrMissingName
	}
	sess, ok := h.st.FindSession(name)
	if !ok {
		return contracts.Result{}, &SessionNotFoundError{Name: name}
	}
	if sess.Approvals == "" {
		return contracts.DataResult(approval.ModeAsk.String()), nil
	}
	return contracts.DataResult(sess.Approvals), nil
}

func (h *Handler) setModeReply(sess state.Session, mode string, gate approvalGate) string {
	reply := h.text(clitext.SessionModeSet, clitext.Params{clitext.ParamName: sess.Name, clitext.ParamMode: mode})
	if !gate.enforced && mode != approval.ModeBypass.String() {
		reply += h.text(clitext.SessionModeUnenforced, clitext.Params{clitext.ParamVendor: vendorLabel(sess.Vendor), clitext.ParamName: sess.Name})
	}
	if sess.Approvals == approval.ModeBypass.String() && mode != approval.ModeBypass.String() {
		reply += h.text(clitext.SessionModeBypassRecreate, nil)
	}
	return reply
}
