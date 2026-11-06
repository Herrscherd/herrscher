package manager

import (
	"context"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func (h *Handler) setHomeRun(ctx context.Context, input contracts.Input) (contracts.Result, error) {
	channelID, ok := input.Lookup("channel")
	if !ok || channelID == "" {
		return contracts.Result{}, h.localized(ErrMissingChannel, clitext.HostErrMissingChannel)
	}
	kind, err := h.d.Kind(ctx, channelID)
	if err != nil {
		return contracts.Result{}, h.labelled("inspect channel", clitext.HostErrInspectChannel, err)
	}
	if kind != categoryKind && kind != forumKind {
		return contracts.Result{}, &ChannelKindError{Channel: channelID, Kind: kind, Need: "home must be a category or forum", Home: true}
	}
	if err := h.st.SetHome(state.HomeRef{ID: channelID, Type: kind}); err != nil {
		return contracts.Result{}, h.labelled("persist", clitext.HostErrPersist, err)
	}
	return contracts.DataResult(h.text(clitext.HostHomeSet, clitext.Params{clitext.ParamChannel: h.d.ChannelRef(channelID), clitext.ParamKind: kind})), nil
}

func (h *Handler) setSourceRun(_ context.Context, input contracts.Input) (contracts.Result, error) {
	path, ok := input.Lookup("path")
	if !ok || path == "" {
		return contracts.Result{}, h.localized(ErrMissingPath, clitext.HostErrMissingPath)
	}
	if err := h.st.SetSource(path); err != nil {
		return contracts.Result{}, h.labelled("persist", clitext.HostErrPersist, err)
	}
	return contracts.DataResult(h.text(clitext.HostSourceSet, clitext.Params{clitext.ParamPath: path})), nil
}
