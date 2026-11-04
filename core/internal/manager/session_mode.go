package manager

import (
	"context"
	"fmt"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
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

func (h *Handler) sessionSetModeRun(_ context.Context, input contracts.Input) (string, error) {
	name, ok := input.Lookup(nameParam)
	if !ok {
		return "", ErrMissingName
	}
	rawMode, _ := input.Lookup("mode")
	mode := strings.TrimSpace(rawMode)
	if mode == "" {
		return "", &UnknownApprovalModeError{Mode: mode}
	}
	if err := validateApprovalsMode(mode); err != nil {
		return "", err
	}
	sess, ok := h.st.FindSession(name)
	if !ok {
		return "", &SessionNotFoundError{Name: name}
	}
	gate := h.gateFor(sess.Vendor)
	if approval.Mode(mode) == approval.ModePlan && !gate.enforced {
		return "", &planUnsupportedError{Session: name, Vendor: sess.Vendor, Why: gate.why}
	}
	if err := h.st.SetSessionApprovals(name, mode); err != nil {
		return "", err
	}
	return setModeReply(sess, mode, gate), nil
}

func setModeReply(sess state.Session, mode string, gate approvalGate) string {
	reply := fmt.Sprintf("%s is now %s", sess.Name, mode)
	if !gate.enforced && mode != string(approval.ModeBypass) {
		reply += fmt.Sprintf(" (%s cannot enforce it, so %s runs ungated)", vendorLabel(sess.Vendor), sess.Name)
	}
	if sess.Approvals == string(approval.ModeBypass) && mode != string(approval.ModeBypass) {
		reply += " (a session created with --approvals bypass has no hook: recreate it for this to bite)"
	}
	return reply
}
