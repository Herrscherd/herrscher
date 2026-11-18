package manager

import (
	"context"
	"errors"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func (h *Handler) sessionSwitchRun(ctx context.Context, input contracts.Input) (contracts.Result, error) {
	name, ok := input.Lookup(nameParam)
	if !ok {
		return contracts.Result{}, h.localized(ErrMissingName, clitext.HostErrMissingName)
	}
	vendor := input.Get("vendor")
	cmd, cmdSupplied := input.Lookup("cmd")
	cmdSupplied = cmdSupplied && cmd != ""
	handoff := input.Get("handoff")
	if handoff == "" {
		handoff = "none"
	}
	prior, exists := h.st.FindSession(name)
	if !exists {
		return contracts.Result{}, &SessionNotFoundError{Name: name}
	}
	modelID := prior.ModelID
	if v, supplied := input.Lookup("model"); supplied {
		modelID = v
	}
	if err := h.checkModel(vendor, modelID); err != nil {
		return contracts.Result{}, err
	}
	if err := h.checkSpawnSource(cmdSupplied, modelID); err != nil {
		return contracts.Result{}, err
	}
	var targeted bool
	persistErr := h.persisted(func() error {
		var err error
		targeted, err = h.st.SetBackendTarget(name, vendor, cmd, modelID)
		return err
	})
	if !targeted {
		return contracts.Result{}, &SessionNotFoundError{Name: name}
	}
	if persistErr != nil {
		return contracts.Result{}, h.labelled("persist", clitext.HostErrPersist, persistErr)
	}
	sess, _ := h.st.FindSession(name)
	switchNote := h.switchApprovalsNote(sess)
	if err := h.sup.RestartWithin(sess, supervisorGrace); err != nil {
		return contracts.Result{}, h.revertSwitch(name, prior, err)
	}
	return contracts.DataResult(h.switchReply(ctx, name, vendor, handoff, switchNote)), nil
}

func (h *Handler) revertSwitch(name string, prior state.Session, restartErr error) error {
	if rbErr := h.rollbackSwitch(name, prior.Vendor, prior.Cmd, prior.ModelID, prior.ResumeToken); rbErr != nil {
		return restartLostError(h.messages, h.currentLanguage(), name, restartErr, rbErr)
	}
	return h.messages.Failure(h.currentLanguage(), restartErr, clitext.SessionSwitchRestartRestored, clitext.Params{clitext.ParamName: name, clitext.ParamVendor: prior.Vendor, clitext.ParamErr: restartErr.Error()})
}

func (h *Handler) switchReply(ctx context.Context, name, vendor, handoff, switchNote string) string {
	if handoff == "none" {
		return h.text(clitext.SessionSwitchedNoResume, clitext.Params{clitext.ParamName: name, clitext.ParamVendor: vendor, clitext.ParamNote: switchNote})
	}
	seedText := buildHandoffSeed(
		state.ReadTranscript(state.TranscriptPath(h.partDir, name), sessionLogTranscriptCap),
		handoff,
	)
	if seedText != "" && h.seed != nil && !h.injectSeed(ctx, name, seedText) {
		return h.text(clitext.SessionSwitchUnconfirmed, clitext.Params{clitext.ParamName: name, clitext.ParamVendor: vendor, clitext.ParamHandoff: handoff, clitext.ParamNote: switchNote})
	}
	return h.text(clitext.SessionSwitchedResumed, clitext.Params{clitext.ParamName: name, clitext.ParamVendor: vendor, clitext.ParamHandoff: handoff, clitext.ParamNote: switchNote})
}

const seedRetryDelay = 150 * time.Millisecond

func (h *Handler) injectSeed(ctx context.Context, name, task string) bool {
	for range 20 {
		if h.seed(name, task) {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(seedRetryDelay):
		}
	}
	return false
}

func (h *Handler) rollbackSwitch(name, oldVendor, oldCmd, oldModelID, oldToken string) error {
	persistErr := h.persisted(func() error {
		_, err := h.st.SetBackendTarget(name, oldVendor, oldCmd, oldModelID)
		return errors.Join(err, h.st.SetResumeToken(name, oldToken))
	})
	sess, _ := h.st.FindSession(name)
	return errors.Join(persistErr, h.sup.RestartWithin(sess, supervisorGrace))
}
