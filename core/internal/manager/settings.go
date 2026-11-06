package manager

import (
	"context"
	"fmt"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
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

func (h *Handler) setLangRun(_ context.Context, input contracts.Input) (contracts.Result, error) {
	raw, _ := input.Lookup("lang")
	if raw == "" || raw == "auto" {
		return h.resetLang()
	}
	lang, ok := operatorLangNamed(raw)
	if !ok {
		return contracts.Result{}, localize(h.lang, fmt.Sprintf("unsupported language %q, use en, fr or auto", raw), clitext.HostErrUnsupportedLang, clitext.Params{clitext.ParamLang: raw}, ErrUnsupportedLang)
	}
	if err := h.st.SetOperatorLang(raw); err != nil {
		return contracts.Result{}, h.labelled("persist", clitext.HostErrPersist, err)
	}
	h.changeLanguage(lang)
	return contracts.DataResult(h.text(clitext.HostLangSet, clitext.Params{clitext.ParamLang: raw})), nil
}

func (h *Handler) resetLang() (contracts.Result, error) {
	if err := h.st.SetOperatorLang(""); err != nil {
		return contracts.Result{}, h.labelled("persist", clitext.HostErrPersist, err)
	}
	h.changeLanguage(h.defaultLang)
	return contracts.DataResult(h.text(clitext.HostLangReset, clitext.Params{clitext.ParamLang: h.defaultLang.String()})), nil
}

func operatorLangNamed(name string) (i18n.Language, bool) {
	switch name {
	case "en":
		return i18n.English, true
	case "fr":
		return i18n.French, true
	}
	return 0, false
}

func (h *Handler) changeLanguage(lang i18n.Language) {
	h.SetLanguage(lang)
	if h.onLanguage != nil {
		h.onLanguage(h.lang)
	}
}
