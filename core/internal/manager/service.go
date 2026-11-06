package manager

import (
	"context"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/clitext"
)

func (h *Handler) serviceRestartRun(ctx context.Context, _ contracts.Input) (contracts.Result, error) {
	if h.up == nil {
		return contracts.Result{}, ErrServiceControlUnavailable
	}
	if err := h.up.Restart(ctx); err != nil {
		return contracts.Result{}, h.keyed(clitext.HostServiceRestartFailed, clitext.Params{clitext.ParamErr: err.Error()}, err)
	}
	return contracts.DataResult(h.text(clitext.HostServiceRestarting, nil)), nil
}

func (h *Handler) serviceUpdateRun(ctx context.Context, input contracts.Input) (contracts.Result, error) {
	if h.up == nil {
		return contracts.Result{}, ErrServiceControlUnavailable
	}
	pull := !input.Bool("no_pull")
	version, err := h.up.Build(ctx, pull)
	if err != nil {
		return contracts.Result{}, h.keyed(clitext.HostServiceUpdateFailed, clitext.Params{clitext.ParamErr: err.Error()}, err)
	}
	if err := h.up.Restart(ctx); err != nil {
		return contracts.Result{}, h.keyed(clitext.HostServiceRebuildFailed, clitext.Params{clitext.ParamVersion: version, clitext.ParamErr: err.Error()}, err)
	}
	reported := version
	if reported == "" {
		reported = "(unknown)"
	}
	return contracts.DataResult(h.text(clitext.HostServiceRebuilt, clitext.Params{clitext.ParamVersion: reported})), nil
}
